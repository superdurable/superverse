// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import type { FlowDefinitionGraph } from "@/lib/api/flow/flow-definition";
import { getPreviewDefinitions } from "@/lib/api/preview";
import { getRelease } from "@/lib/api/release";
import type { FlowDefinition__Output } from "@/lib/generated/protobuf/superverse/v2/FlowDefinition";
import { buildDefinitionDexCatalog } from "@/lib/shared/flow-definition-catalog";
import { decodeStruct } from "@/lib/shared/protobuf";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";

/** One Process (a valid Flow Type) of a version: its type, definition digest, Step count and graph. */
interface ProcessDefinition {
  flowType: string;
  digest: string;
  stepCount: number;
  graph: FlowDefinitionGraph | null;
}

/** The Flow graphs of a commit by Flow Type, which a Flow design is compared with. */
export interface CurrentSource {
  commit: string;
  graphs: ReadonlyMap<string, FlowDefinitionGraph>;
}

/** A prepared source's catalog as the prepared-source Flow API serves it, built in the browser from its definitions. */
type BuiltCatalog = ReturnType<typeof buildDefinitionDexCatalog<FlowDefinitionGraph>>;

/** The Steps a build added and those whose definition changed against the build before it, by Step id. */
export interface StepChanges {
  added: ReadonlySet<string>;
  changed: ReadonlySet<string>;
}

/** What the newest build this browser read changed against the one it read before, by Flow Type. */
interface BuildChanges {
  /** The newest build's commit and definition revision, which names this change once. */
  key: string;
  /** The commit of the build it is compared with. */
  since: string;
  flows: ReadonlyMap<string, StepChanges>;
}

interface ProcessDefinitions {
  /** The Processes of the version shown, in Flow Type order. */
  processes: ProcessDefinition[];
  /** Flow Types whose definition on Preview differs from Live's, or that Live lacks. */
  changedSinceLive: ReadonlySet<string>;
  /** Steps a Process has on Preview beyond Live's, by Flow Type, where Preview has more. */
  moreStepsOnPreview: ReadonlyMap<string, number>;
  /** The commit of the prepared source whose Processes Preview shows; null without one. */
  previewCommit: string | null;
  /** The prepared source's graphs, or the template's initial design for a setup commit; null without either. */
  currentSource: CurrentSource | null;
  /**
   * The newest prepared source this browser read, kept while a newer push builds or failed to build, so the Flow
   * tab keeps drawing the real flow; null before the first and when its catalog does not build.
   */
  lastBuilt: { commit: string; catalog: BuiltCatalog } | null;
  /** What the newest prepared source changed against the one read before it here; null without both. */
  buildChanges: BuildChanges | null;
  loaded: boolean;
}

const EMPTY: ProcessDefinitions = {
  processes: [],
  changedSinceLive: new Set(),
  moreStepsOnPreview: new Map(),
  previewCommit: null,
  currentSource: null,
  lastBuilt: null,
  buildChanges: null,
  loaded: false,
};
const ProcessDefinitionsContext = createContext<ProcessDefinitions>(EMPTY);

/** A node or edge end without the source position the analyzer appends to its id, which unrelated edits move. */
const unplaced = (id: string) => id.replace(/:\d+:\d+$/, "");

/** JSON without source spans, which move with unrelated edits. */
const withoutSpans = (value: unknown) =>
  JSON.stringify(value, (key, item: unknown) => (key === "span" ? undefined : item));

/**
 * What defines each Step of a graph, by Step id: the Step and every node under it, the edges that leave them, the
 * resources they read or wait on, and the Step's group, all without source positions or edge numbering.
 */
function stepDefinitions(graph: FlowDefinitionGraph): Map<string, string> {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]));
  const stepOf = (id: string): string | undefined => {
    for (let node = nodes.get(id), depth = 0; node && depth < 16; node = nodes.get(node.parentId ?? ""), depth++)
      if (node.kind === "step") return node.id;
    return undefined;
  };
  const parts = new Map<string, string[]>();
  const add = (step: string | undefined, part: string) => {
    if (step) parts.get(step)?.push(part);
  };
  for (const node of graph.nodes) if (node.kind === "step") parts.set(node.id, []);
  for (const node of graph.nodes)
    add(stepOf(node.id), `node ${unplaced(node.id)} ${withoutSpans({ ...node, id: undefined, parentId: undefined })}`);
  for (const edge of graph.edges) {
    const fields = withoutSpans({ ...edge, id: undefined, from: undefined, to: undefined });
    add(stepOf(edge.from), `out ${unplaced(edge.to)} ${fields}`);
    if (edge.from.startsWith("resource:")) add(stepOf(edge.to), `in ${edge.from} ${fields}`);
  }
  for (const group of graph.groups ?? []) for (const step of group.stepIds) add(step, `group ${group.label}`);
  return new Map([...parts].map(([step, list]) => [step, list.sort().join("\n")]));
}

/** The Steps of each Flow Type of next that previous lacks or defines differently; unchanged Flows are left out. */
function changesBetween(previous: readonly ProcessDefinition[], next: readonly ProcessDefinition[]) {
  const flows = new Map<string, StepChanges>();
  for (const process of next) {
    const before = previous.find((item) => item.flowType === process.flowType);
    if (!process.graph || before?.digest === process.digest) continue;
    const old = before?.graph ? stepDefinitions(before.graph) : new Map<string, string>();
    const added = new Set<string>();
    const changed = new Set<string>();
    for (const [step, definition] of stepDefinitions(process.graph)) {
      const was = old.get(step);
      if (was === undefined) added.add(step);
      else if (was !== definition) changed.add(step);
    }
    if (added.size + changed.size > 0) flows.set(process.flowType, { added, changed });
  }
  return flows;
}

function processesOf(definitions: readonly FlowDefinition__Output[]): ProcessDefinition[] {
  return definitions
    .filter((definition) => definition.valid)
    .map((definition) => {
      const graph = decodeStruct(definition.definition) as unknown as FlowDefinitionGraph | null;
      const nodes: unknown = graph?.nodes;
      const stepCount = Array.isArray(nodes)
        ? nodes.filter((node) => typeof node === "object" && node !== null && "kind" in node && node.kind === "step")
            .length
        : 0;
      return { flowType: definition.flowType, digest: definition.digest, stepCount, graph };
    })
    .sort((a, b) => a.flowType.localeCompare(b.flowType));
}

/** Reads the Processes of the Release Live serves; a missing Release reads as none. */
function useLiveProcesses(projectId: string, releaseId: string | null) {
  const key = releaseId === null ? null : JSON.stringify([projectId, releaseId]);
  const [state, setState] = useState<{ key: string; processes: ProcessDefinition[] } | null>(null);
  useEffect(() => {
    if (key === null) return;
    const controller = new AbortController();
    const [id, release] = JSON.parse(key) as [string, string];
    getRelease({ projectId: id, releaseId: release }, controller.signal).then(
      ({ flowDefinitions }) => !controller.signal.aborted && setState({ key, processes: processesOf(flowDefinitions) }),
      () => !controller.signal.aborted && setState({ key, processes: [] }),
    );
    return () => controller.abort();
  }, [key]);
  return key !== null && state?.key === key ? state.processes : null;
}

/** One prepared source's definitions as this browser read them; a failed or inexact read holds no catalog. */
interface PreparedRead {
  projectId: string;
  key: string;
  commit: string;
  processes: ProcessDefinition[];
  catalog: BuiltCatalog | null;
}

/** The catalog of a read prepared source, as the prepared-source Flow API builds it; null when it would refuse it. */
function catalogOf(revision: string, definitions: readonly FlowDefinition__Output[]): BuiltCatalog | null {
  try {
    return buildDefinitionDexCatalog(
      revision,
      definitions.map((item) => ({
        ...item,
        graph: decodeStruct(item.definition) as unknown as FlowDefinitionGraph | null,
      })),
      "session-preview-source",
    );
  } catch {
    return null;
  }
}

/**
 * Reads the Processes of each prepared source the Preview builds, keeping the newest read and the last built one
 * before it: a push that is still building keeps the last built drawing, and a new build is compared with the one
 * before it. Nothing is stored; a reload starts again from the source the Preview has prepared.
 */
function usePreparedProcesses(projectId: string, source: { commit: string; definitionRevision: string } | null) {
  const key = source && JSON.stringify([projectId, source.commit, source.definitionRevision]);
  const [reads, setReads] = useState<{ newest: PreparedRead; before: PreparedRead | null } | null>(null);
  useEffect(() => {
    if (key === null) return;
    const controller = new AbortController();
    const [id, commit, revision] = JSON.parse(key) as [string, string, string];
    const settle = (read: PreparedRead) =>
      setReads((current) => {
        if (current?.newest.projectId !== read.projectId) return { newest: read, before: null };
        if (current.newest.key === read.key) return { newest: read, before: current.before };
        return { newest: read, before: current.newest.catalog ? current.newest : current.before };
      });
    getPreviewDefinitions({ projectId: id }, controller.signal).then(
      ({ preview, flowDefinitions }) => {
        if (controller.signal.aborted) return;
        const prepared = preview?.preparedSource;
        const exact = prepared?.sourceCommit === commit && prepared.definitionRevision === revision;
        const catalog = exact ? catalogOf(revision, flowDefinitions) : null;
        settle({ projectId: id, key, commit, processes: processesOf(flowDefinitions), catalog });
      },
      () => !controller.signal.aborted && settle({ projectId: id, key, commit, processes: [], catalog: null }),
    );
    return () => controller.abort();
  }, [key]);
  const own = reads?.newest.projectId === projectId ? reads : null;
  return {
    current: own && own.newest.key === key ? own.newest.processes : null,
    newest: own?.newest ?? null,
    before: own?.before ?? null,
  };
}

/**
 * The Project's Processes for the Builder's frame and surfaces: those of the version shown (the
 * source prepared for the Session's head on Preview, the Release Live serves on Live), and how the
 * Preview's differ from Live's by definition digest. A new prepared source or Live Release reads
 * them again. While a newer push builds or failed to build, Preview keeps the last built source it
 * read, and a new build says which Steps it added or changed against the one read before it.
 */
export function ProcessDefinitionsProvider({
  projectId,
  environment,
  previewSource,
  templateSource,
  liveReleaseId,
  children,
}: {
  projectId: string;
  environment: ProjectEnvironment;
  /** The prepared source: its commit and definition revision; null without one. */
  previewSource: { commit: string; definitionRevision: string } | null;
  /** The template's initial design while the setup commit has no prepared source. */
  templateSource: CurrentSource | null;
  liveReleaseId: string | null;
  children: ReactNode;
}) {
  const prepared = usePreparedProcesses(projectId, previewSource);
  // Until the prepared source is built again, the last built one stays.
  const kept = prepared.newest?.catalog ? prepared.newest : prepared.before;
  const preview = prepared.current ?? (previewSource === null && kept ? kept.processes : null);
  const live = useLiveProcesses(projectId, liveReleaseId);
  const changedSinceLive = new Set<string>();
  const moreStepsOnPreview = new Map<string, number>();
  if (preview && live) {
    for (const process of preview) {
      const shipped = live.find((item) => item.flowType === process.flowType);
      if (!shipped || shipped.digest !== process.digest) changedSinceLive.add(process.flowType);
      if (shipped && process.stepCount > shipped.stepCount)
        moreStepsOnPreview.set(process.flowType, process.stepCount - shipped.stepCount);
    }
  }
  const shown = environment === "live" ? live : preview;
  const previewCommit = previewSource?.commit ?? null;
  const current = prepared.current;
  const currentSource = useMemo(
    () =>
      previewCommit !== null
        ? current && {
            commit: previewCommit,
            graphs: new Map(current.flatMap(({ flowType, graph }) => (graph ? [[flowType, graph] as const] : []))),
          }
        : templateSource,
    [current, previewCommit, templateSource],
  );
  const { newest, before } = prepared;
  const buildChanges = useMemo(
    () =>
      newest?.catalog && before?.catalog
        ? { key: newest.key, since: before.commit, flows: changesBetween(before.processes, newest.processes) }
        : null,
    [before, newest],
  );
  const value: ProcessDefinitions = {
    processes: shown ?? [],
    changedSinceLive,
    moreStepsOnPreview,
    previewCommit,
    currentSource,
    lastBuilt: kept?.catalog ? { commit: kept.commit, catalog: kept.catalog } : null,
    buildChanges,
    loaded: shown !== null,
  };
  return <ProcessDefinitionsContext.Provider value={value}>{children}</ProcessDefinitionsContext.Provider>;
}

export function useProcessDefinitions(): ProcessDefinitions {
  return useContext(ProcessDefinitionsContext);
}
