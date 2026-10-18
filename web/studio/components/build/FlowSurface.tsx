// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useProcessDefinitions, type StepChanges } from "@/lib/project/ProcessDefinitionsProvider";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useProjectStage } from "@/lib/project/stage/ProjectStageProvider";
import { useWaitingRuns, waitingCount } from "@/lib/project/WaitingRunsProvider";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { useDesignShown } from "@/lib/session/design/use-design-shown";
import { useFlowDesign } from "@/lib/session/design/use-flow-design";
import { flowApiRoutes, type ProjectEnvironment } from "@/lib/shared/studio-routes";
import { useQueryUpdate } from "@/lib/browser/use-query-update";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { ExampleFlowScope, LiveFlowScope, SessionFlowScope } from "@/components/flow/scope/FlowScope";
import { DraftBrief } from "@/components/flow/scope/flow-design";
import { FlowFrame } from "@/components/flow/scope/FlowSurfaces";
import { LazyFlowCanvas, LazyProjectMap } from "@/components/flow/scope/LazyFlowViews";
import { NotReady } from "@/components/ui/NotReady";
import { Glyph } from "@/components/ui/Glyph";
import { processName } from "@/lib/shared/format";
import { versionNumber } from "@/lib/shared/version/version";
import { CardBrief, DesignBar, DesignSurface } from "./DesignSurface";
import { PreviewSourceControl } from "./PreviewSourceControl";

/** How long the Steps a new build added or changed stay marked on its Process. */
const BUILD_CHANGES_MS = 8000;
/** When each build's marks on each Process end, from the first time this page drew them; never stored. */
const buildChangesUntil = new Map<string, number>();

/**
 * The Steps the newest build added or changed in a Process, for a moment from the first time the canvas draws
 * that build's Process in this page; a reload or a later visit marks nothing, and neither does a first build
 * compared with the template commit the Project started from, whose processes are not the person's.
 */
function useBuildChanges(flowType: string | null, templateCommit: string): StepChanges | null {
  const { buildChanges } = useProcessDefinitions();
  const compared = buildChanges && buildChanges.since !== templateCommit ? buildChanges : null;
  const changes = flowType ? (compared?.flows.get(flowType) ?? null) : null;
  const id = changes && compared ? `${compared.key}:${flowType}` : null;
  const [shown, setShown] = useState<string | null>(null);
  useEffect(() => {
    if (id === null) return undefined;
    const until = buildChangesUntil.get(id) ?? Date.now() + BUILD_CHANGES_MS;
    buildChangesUntil.set(id, until);
    const left = until - Date.now();
    if (left <= 0) return undefined;
    const show = window.setTimeout(() => setShown(id), 0);
    const end = window.setTimeout(() => setShown((current) => (current === id ? null : current)), left);
    return () => {
      window.clearTimeout(show);
      window.clearTimeout(end);
    };
  }, [id]);
  return id !== null && shown === id ? changes : null;
}

/**
 * The Flow of the picked Process, or the project map when none is picked. A version with one Process has
 * nothing to pick between, so the map never shows for it: the Flow tab draws that Process at once. While
 * `markChanges`, the Steps the newest build added or changed in the drawn Process are marked for a moment.
 */
function FlowView({
  environment,
  markChanges,
  templateCommit,
}: {
  environment: ProjectEnvironment;
  markChanges: boolean;
  templateCommit: string;
}) {
  const { catalog, definitions, error, ready } = useFlowCatalog();
  const picked = useSearchParams().get("flowType");
  const { queryHref, replaceQuery } = useQueryUpdate();
  const { changedSinceLive, moreStepsOnPreview } = useProcessDefinitions();
  const waiting = useWaitingRuns();
  const live = useProjectResource()?.snapshot?.live;
  const validFlowTypes = useMemo(() => {
    const valid = new Set((definitions?.definitions ?? []).filter((item) => item.valid).map((item) => item.flowName));
    return [...new Set((catalog?.flows ?? []).map((item) => item.flowType).filter((type) => valid.has(type)))].sort();
  }, [catalog, definitions]);
  const only = validFlowTypes.length === 1 ? validFlowTypes[0] : null;
  const requested = picked ?? only;
  const changes = useBuildChanges(markChanges ? requested : null, templateCommit);
  // The URL names the one Process too, so everything that reads the picked Process sees it.
  useEffect(() => {
    if (!picked && only) replaceQuery((query) => query.set("flowType", only));
  }, [only, picked, replaceQuery]);
  const processHref = (flowType: string | null) =>
    queryHref((query) => {
      if (flowType) query.set("flowType", flowType);
      else query.delete("flowType");
      query.delete("flowId");
      query.delete("runId");
    });
  const onLive = environment === "live";
  const liveBanner = onLive ? (
    <span className="flex items-center gap-1.5 rounded-lg border border-line-soft bg-panel px-2.25 py-1 text-ink-dim">
      <Glyph name="eye" className="size-3.5" />
      Live{live ? ` ${versionNumber(live.version)}` : ""}, read only here.
      <Link href={queryHref((query) => query.delete("v"))} className="text-accent hover:underline">
        See Preview
      </Link>
    </span>
  ) : null;

  if (!ready)
    return (
      <NotReady kind="placeholder" heading="Loading the Flow">
        Reading the Flow Definitions of this version.
      </NotReady>
    );
  if (error)
    return (
      <NotReady kind="error" heading="The flow did not load" retry>
        {error}
      </NotReady>
    );
  if (!requested)
    return (
      <LazyProjectMap
        names={processName}
        hrefOf={processHref}
        changed={changedSinceLive}
        changedLabel={onLive ? "differs on Preview" : "changed"}
        waiting={
          waiting
            ? (flowType) => {
                const count = waiting.get(flowType) ?? 0;
                const words =
                  count === 0
                    ? "no runs need a person"
                    : `${waitingCount(count)} need${count === 1 ? "s" : ""} a person`;
                return `${words} · ${onLive ? "Live" : "Preview"}`;
              }
            : undefined
        }
        banner={liveBanner}
      />
    );
  if (!validFlowTypes.includes(requested))
    return (
      <NotReady kind="unavailable" heading={`${processName(requested)} isn't in this version`}>
        {onLive
          ? "It exists only on Preview until someone publishes."
          : `${requested} is absent from this source. Pick another process, or All processes.`}
      </NotReady>
    );
  const more = onLive ? (moreStepsOnPreview.get(requested) ?? 0) : 0;
  const changed = !onLive && changedSinceLive.has(requested);
  // The bar's picker names the Process and leads back to all of them, so the canvas carries only what
  // the picker does not say: how this Process differs between Preview and Live.
  return (
    <section
      aria-label="Flow definition"
      className="@container/canvas relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-canvas"
      data-pick-scope={processName(requested)}
    >
      {(changed || more > 0 || liveBanner) && (
        <div className="pointer-events-none absolute top-2.5 left-3 z-5 flex max-w-[calc(100%-1.5rem)] flex-wrap items-center gap-2 text-[12.5px] text-ink-faint">
          {changed && (
            <span className="rounded-lg border border-line-soft bg-panel px-2.25 py-1 text-busy">
              changed since Live
            </span>
          )}
          {more > 0 && (
            <span className="rounded-lg border border-line-soft bg-panel px-2.25 py-1">
              Preview has {more} more step{more > 1 ? "s" : ""}
            </span>
          )}
          {liveBanner && <span className="pointer-events-auto">{liveBanner}</span>}
        </div>
      )}
      <LazyFlowCanvas flowType={requested} opensWhole changes={changes} />
    </section>
  );
}

/**
 * The current source's commit and its preparation, keyed so that a new source or Sandbox instance
 * starts a new operation.
 */
export function PreviewSource({ projectId }: { projectId: string }) {
  const resource = useSessionResource()?.resource;
  if (!resource) return null;
  const key = JSON.stringify([
    projectId,
    resource.workspace?.workspaceRequestId,
    resource.workspace?.generation,
    resource.headCommit,
  ]);
  return <PreviewSourceControl key={key} projectId={projectId} />;
}

/**
 * The Flow: the Flow Definitions of the version the bar selects. On Preview they are the prepared
 * current source of the Project's Session; on Live, the Live Release's, read-only. Before the Session
 * exists there is no source to draw; only mock mode, which wraps this surface in its fixture catalog,
 * still draws one. The real flow shows whenever one exists; a card's Flow design is drawn only before the
 * first build of a Project that started from the default template (`designFirst`), whose processes are the
 * template's until then. There, from a recorded card with a design until its build is complete, Preview draws
 * the designed Flows instead and shows them by itself once per card version; until processes are built from
 * the agent's own push, Preview draws nothing of the template: it waits calmly, with Build processes only where
 * a person must press it, and while the agent writes a card its draft shows as the drafting overview. A Project
 * started from an example, opened or matched, draws that example's own Flow Definitions while its Session's head
 * is the setup commit and the Preview has built no processes; a push past it waits for its own build. Once
 * processes are built, a change keeps the last built flow while its push builds, under the Build processes strip,
 * and the new build's processes then mark the Steps it added or changed for a moment; the strip goes once the
 * latest push is built.
 */
export function FlowSurface({
  projectId,
  environment,
  fixtureCatalog = false,
}: {
  projectId: string;
  environment: ProjectEnvironment;
  fixtureCatalog?: boolean;
}) {
  const { queryHref, replaceQuery } = useQueryUpdate();
  const requested = useSearchParams().get("flowType");
  const session = useSessionResource();
  const { canIntervene } = useFlowHost();
  const stage = useProjectStage();
  const drawsExample = environment === "preview" && stage.drawsExample;
  const design = useFlowDesign(environment === "preview" && canIntervene && stage.designFirst);
  const designed = design?.body?.flows.map((flow) => flow.flowType) ?? [];
  const firstDesigned = designed[0] ?? null;
  const drawsDesign = requested !== null && designed.includes(requested);
  const { shown, markShown } = useDesignShown(projectId, design?.body ? design.key : null);
  // A card version's design shows here by itself once; the tab's marker goes once it has.
  useEffect(() => {
    if (shown || firstDesigned === null) return;
    markShown();
    if (!drawsDesign)
      replaceQuery((query) => {
        query.set("flowType", firstDesigned);
        query.delete("flowId");
        query.delete("runId");
      });
  }, [drawsDesign, firstDesigned, markShown, replaceQuery, shown]);
  const live = useProjectResource()?.snapshot?.live;
  const headCommit = session?.resource?.headCommit;
  // A new pushed commit is a new source: drop the run selections that named the old one. The picked
  // Process stays; the Flow says so when the new source lacks it.
  const source = JSON.stringify([projectId, headCommit]);
  const previousSource = useRef(headCommit ? source : null);
  useEffect(() => {
    if (!headCommit || previousSource.current === source) return;
    const changed = previousSource.current !== null;
    previousSource.current = source;
    if (changed) replaceQuery((query) => ["flowId", "runId"].forEach((name) => query.delete(name)));
  }, [headCommit, replaceQuery, source]);

  const own = stage.hasOwnProcesses || environment === "live";
  const agent = session?.resource?.agent?.description;
  const prepared = preparedSourceOf(session?.resource);
  // Automatic builds off, or a failed build of the latest push, wait for a person's Build processes.
  const buildNeedsYou =
    !(session?.resource?.agent?.description?.isPreviewAutoDeployEnabled ?? true) ||
    (prepared?.status === "PREVIEW_SOURCE_STATUS_FAILED" && prepared.sourceCommit === headCommit);
  // Recorded and still unbuilt: building while the agent works or the Preview builds the agent's own head,
  // otherwise its turn ended short of a build.
  const cardState =
    agent?.status !== "STATUS_WAITING_FOR_MESSAGE" || (stage.buildsOwnHead && !buildNeedsYou) ? "building" : "stalled";
  // The strip says only what is under way or waits for a person: it goes once the latest push is built.
  const latestBuilt = readyPreparedSource(prepared, headCommit) !== null;
  const strip = session && !latestBuilt && (own || buildNeedsYou) ? <PreviewSource projectId={projectId} /> : null;
  const canvas = (
    <FlowView
      environment={environment}
      markChanges={environment === "preview" && own}
      templateCommit={stage.templateCommit}
    />
  );
  let body;
  if (drawsExample)
    body = (
      <>
        {strip}
        <ExampleFlowScope basePath={flowApiRoutes.preparedSource(projectId)}>{canvas}</ExampleFlowScope>
      </>
    );
  else if (design && drawsDesign && requested)
    body = (
      <>
        {strip}
        <DesignSurface design={design} flowType={requested} />
      </>
    );
  else if (!own)
    body = (
      <>
        {strip}
        {design ? (
          <NotReady kind="empty" heading="Not built yet">
            Open a process of the design above to see its steps. The processes show here once the agent has built them.
          </NotReady>
        ) : stage.hasRequirements ? (
          <CardBrief state={cardState} />
        ) : session?.requirementsDraft ? (
          <DraftBrief draft={session.requirementsDraft} />
        ) : (
          <NotReady kind="empty" heading="Reading your description…">
            The processes the agent builds from what you describe show here.
          </NotReady>
        )}
      </>
    );
  else if (fixtureCatalog) body = canvas;
  else if (environment === "live")
    body =
      live && !live.isRemoving ? (
        <LiveFlowScope key={`live:${live.deploymentId}:${live.revision}`} projectId={projectId} releaseId={null}>
          {canvas}
        </LiveFlowScope>
      ) : (
        <NotReady kind="empty" heading="Nothing is Live yet">
          The Flow shows Live once a version is published. Publish from Tools, Publishing.
        </NotReady>
      );
  else
    body = session ? (
      <>
        {strip}
        <SessionFlowScope projectId={projectId} source="prepared">
          {canvas}
        </SessionFlowScope>
      </>
    ) : (
      <NotReady kind="placeholder" heading="Waiting for the project's agent">
        The Flow opens once the project is ready and its agent has started.
      </NotReady>
    );
  return (
    <FlowFrame className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      {design && !requested && (
        <DesignBar
          design={design}
          hrefOf={(flowType) =>
            queryHref((query) => {
              query.set("flowType", flowType);
              query.delete("flowId");
              query.delete("runId");
            })
          }
        />
      )}
      {body}
    </FlowFrame>
  );
}
