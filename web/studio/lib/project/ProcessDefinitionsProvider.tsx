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
  loaded: boolean;
}

const EMPTY: ProcessDefinitions = {
  processes: [],
  changedSinceLive: new Set(),
  moreStepsOnPreview: new Map(),
  previewCommit: null,
  currentSource: null,
  loaded: false,
};
const ProcessDefinitionsContext = createContext<ProcessDefinitions>(EMPTY);

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

type VersionSource = { kind: "preview"; key: string } | { kind: "live"; releaseId: string };

async function readDefinitions(projectId: string, source: VersionSource, signal: AbortSignal) {
  return source.kind === "preview"
    ? (await getPreviewDefinitions({ projectId }, signal)).flowDefinitions
    : (await getRelease({ projectId, releaseId: source.releaseId }, signal)).flowDefinitions;
}

/** Reads one version's Processes; an unprepared Preview or a missing Release reads as none. */
function useVersionProcesses(projectId: string, source: VersionSource | null) {
  const key = source === null ? null : JSON.stringify([projectId, source]);
  const [state, setState] = useState<{ key: string; processes: ProcessDefinition[] } | null>(null);
  useEffect(() => {
    if (key === null) return;
    const controller = new AbortController();
    const [id, version] = JSON.parse(key) as [string, VersionSource];
    readDefinitions(id, version, controller.signal).then(
      (definitions) => !controller.signal.aborted && setState({ key, processes: processesOf(definitions) }),
      () => !controller.signal.aborted && setState({ key, processes: [] }),
    );
    return () => controller.abort();
  }, [key]);
  return key !== null && state?.key === key ? state.processes : null;
}

/**
 * The Project's Processes for the Builder's frame and surfaces: those of the version shown (the
 * source prepared for the Session's head on Preview, the Release Live serves on Live), and how the
 * Preview's differ from Live's by definition digest. A new prepared source or Live Release reads
 * them again.
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
  const previewKey = previewSource && `${previewSource.commit}:${previewSource.definitionRevision}`;
  const preview = useVersionProcesses(projectId, previewKey === null ? null : { kind: "preview", key: previewKey });
  const live = useVersionProcesses(
    projectId,
    liveReleaseId === null ? null : { kind: "live", releaseId: liveReleaseId },
  );
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
  const currentSource = useMemo(
    () =>
      previewCommit !== null
        ? preview && {
            commit: previewCommit,
            graphs: new Map(preview.flatMap(({ flowType, graph }) => (graph ? [[flowType, graph] as const] : []))),
          }
        : templateSource,
    [preview, previewCommit, templateSource],
  );
  const value: ProcessDefinitions = {
    processes: shown ?? [],
    changedSinceLive,
    moreStepsOnPreview,
    previewCommit,
    currentSource,
    loaded: shown !== null,
  };
  return <ProcessDefinitionsContext.Provider value={value}>{children}</ProcessDefinitionsContext.Provider>;
}

export function useProcessDefinitions(): ProcessDefinitions {
  return useContext(ProcessDefinitionsContext);
}
