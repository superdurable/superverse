// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

/*
 * Studio's one entrance to the Flow views: it binds them to a Project-scoped
 * Flow API base path. Each provider resolves exactly one definition source
 * and never substitutes a sample or another App's Flow.
 */

import { useEffect, type ReactNode } from "react";
import { initialTemplateDesign, useExampleDefinitions } from "@/lib/project/initial-template-design";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { flowApiRoutes } from "@/lib/shared/studio-routes";
import { useQueryUpdate } from "@/lib/browser/use-query-update";
import type { ObservedFlowTarget } from "@/lib/api/transport/flow-api-client";
import { FlowApiProvider } from "@/lib/flow/FlowApiProvider";
import { DesignCatalogProvider, useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { FlowNavigationProvider } from "@/lib/flow/FlowNavigationProvider";
import type { FlowDefinitionCatalog, FlowRuntimeCatalog } from "@/lib/api/flow/flow-types";
import { Banner } from "@/components/ui/Banner";
import { NotReady } from "@/components/ui/NotReady";
import { LazyFlowCatalogProvider } from "./LazyFlowViews";

/** Mounts one Flow API client, Flow navigation and the catalog (with its notice) for the Flow views below. */
export function FlowScope({
  basePath = "/dex", // mock mode's unscoped fixture Flow API
  design,
  children,
  ...observed
}: ObservedFlowTarget & {
  basePath?: string;
  /** A read-only design catalog that replaces the one loaded from the Flow API. */
  design?: { catalog: FlowRuntimeCatalog; definitions: FlowDefinitionCatalog };
  children: ReactNode;
}) {
  const views = (
    <>
      <CatalogNotice />
      {children}
    </>
  );
  // A new base path remounts the catalog, closes dialogs and cancels the previous API owner.
  return (
    <FlowApiProvider key={basePath} basePath={basePath} {...observed}>
      <FlowNavigationProvider>
        {design ? (
          <DesignCatalogProvider {...design}>{views}</DesignCatalogProvider>
        ) : (
          <LazyFlowCatalogProvider>{views}</LazyFlowCatalogProvider>
        )}
      </FlowNavigationProvider>
    </FlowApiProvider>
  );
}

/** Above every Flow view, why the operation just refused was closed: its definition or deployment changed. */
function CatalogNotice() {
  const { notice } = useFlowCatalog();
  if (!notice) return null;
  return (
    <Banner className="m-3" role="status">
      {notice}
    </Banner>
  );
}

/**
 * Live Runs and Inbox use the exact Deployment that Project currently serves.
 * Its Flow API loads the pinned READY build's definitions, independently of a
 * newer failed build of that Release. A ?release= no longer Live fails closed.
 */
export function LiveFlowScope({
  projectId,
  releaseId,
  children,
}: {
  projectId: string;
  releaseId: string | null;
  children: ReactNode;
}) {
  const live = useProjectResource()?.snapshot?.live;
  const liveReleaseId = live && !live.isRemoving ? live.releaseId : null;
  const selectedId = releaseId ?? liveReleaseId;
  const { replaceQuery } = useQueryUpdate();
  useEffect(() => {
    if (!releaseId && liveReleaseId) replaceQuery((query) => query.set("release", liveReleaseId));
  }, [liveReleaseId, releaseId, replaceQuery]);

  if (!selectedId || selectedId !== liveReleaseId || !live?.deploymentId)
    return (
      <NotReady kind="unavailable" heading="No exact Release Flow Definition">
        This Release is not deployed Live. Studio will not substitute a sample or another App&apos;s Flow.
      </NotReady>
    );
  return (
    <FlowScope
      key={JSON.stringify([projectId, selectedId, live.deploymentId])}
      basePath={flowApiRoutes.liveRuntime(projectId, selectedId)}
      deploymentId={live.deploymentId}
    >
      {children}
    </FlowScope>
  );
}

/** Runs and Inbox on Preview while it does not run: why they are empty, and the way to start it. */
function PreviewNotRunning({ start }: { start?: ReactNode }) {
  return (
    <NotReady kind="unavailable" heading="Preview is not running">
      Runs and Inbox items on Preview appear while it runs; the agent&apos;s next push starts it.
      {start && <span className="block">{start}</span>}
    </NotReady>
  );
}

/**
 * Mock Studio's Runs and Inbox on Preview, which read fixture runs rather than the running Preview's own: they
 * show only while the mock Preview runs, as the real ones do, and otherwise why not with `start`.
 */
export function WhilePreviewRuns({ start, children }: { start?: ReactNode; children: ReactNode }) {
  const preview = useSessionResource()?.resource?.preview;
  return preview?.status === "PREVIEW_STATUS_READY" ? children : <PreviewNotRunning start={start} />;
}

/**
 * The Flow Definitions of the Project's Session: of its prepared current
 * source (Build's Design), or of the Project's running Preview (Preview Runs
 * and Inbox). Before the first source is prepared, a managed project's setup
 * commit shows the template's generated Design. While a newer push builds, or
 * failed to build, the prepared source keeps the last built one this browser
 * read, read-only, or else the one the running Preview serves, so the real flow
 * stays in view.
 */
export function SessionFlowScope({
  projectId,
  source,
  start,
  children,
}: {
  projectId: string;
  source: "prepared" | "running";
  /** Start Preview, offered in place while the running Preview's surfaces are empty. */
  start?: ReactNode;
  children: ReactNode;
}) {
  const project = useProjectResource()?.snapshot;
  const session = useSessionResource()?.resource;
  const { lastBuilt } = useProcessDefinitions();
  if (!session || session.projectId !== projectId)
    return (
      <NotReady kind="placeholder" heading="Loading the project's Session">
        Reading the project&apos;s agent Session.
      </NotReady>
    );
  const preview = session.preview;
  const runtimePath = flowApiRoutes.previewRuntime(projectId);
  const running =
    preview?.status === "PREVIEW_STATUS_READY" && preview.sourceCommit && preview.definitionRevision ? (
      <FlowScope
        key={JSON.stringify([runtimePath, preview.workspaceRequestId, preview.servingRevision])}
        basePath={runtimePath}
        workspaceRequestId={preview.workspaceRequestId}
        generation={preview.generation}
      >
        {children}
      </FlowScope>
    ) : null;
  if (source === "running") return running ?? <PreviewNotRunning start={start} />;
  const basePath = flowApiRoutes.preparedSource(projectId);
  const prepared = readyPreparedSource(preparedSourceOf(session), session.headCommit);
  if (prepared) {
    return (
      <FlowScope
        key={JSON.stringify([basePath, prepared.workspaceRequestId, prepared.definitionRevision])}
        basePath={basePath}
        workspaceRequestId={prepared.workspaceRequestId}
        generation={prepared.generation}
      >
        {children}
      </FlowScope>
    );
  }
  const design = initialTemplateDesign(project, session) ?? lastBuilt?.catalog;
  // The head's processes are not built yet: the last build read here, else the one the Preview serves, stays.
  if (!design)
    return (
      running ?? (
        <NotReady kind="unavailable" heading="Processes of this version are not built yet">
          Its processes appear after the next push builds them automatically. Turn off automatic builds to use Build
          processes above.
        </NotReady>
      )
    );
  // The generated template design and a kept build are analyzer output with the catalog's own shape.
  return (
    <FlowScope
      key={`initial:${design.definitions.definitionRevision}`}
      basePath={basePath}
      design={{
        catalog: design.catalog as FlowRuntimeCatalog,
        definitions: design.definitions as FlowDefinitionCatalog,
      }}
    >
      {children}
    </FlowScope>
  );
}

/**
 * The Flow Definitions of the example the Project started from, at the tree it started from, which the Flow
 * tab draws while the Session's head is the setup commit, which holds that tree, and the Preview has built no
 * processes (`drawsExample`): rendered from the example's own source with this Studio build and imported on
 * first use. Nothing here names the example. A build that holds the example at another tree draws nothing of
 * it and waits as an unbuilt version does.
 */
export function ExampleFlowScope({ basePath, children }: { basePath?: string; children: ReactNode }) {
  const example = useProjectResource()?.snapshot?.example;
  const definitions = useExampleDefinitions(example);
  if (!definitions || definitions.status === "unavailable")
    return (
      <NotReady kind="unavailable" heading="Processes of this version are not built yet">
        Its processes appear here once the Preview has built them.
      </NotReady>
    );
  if (definitions.status === "loading")
    return (
      <NotReady kind="placeholder" heading="Loading the Flow">
        Reading the Flow Definitions of this version.
      </NotReady>
    );
  if (definitions.status === "failed")
    return (
      <NotReady kind="error" heading="The flow did not load" retry>
        {definitions.error}
      </NotReady>
    );
  // The generated definitions are analyzer output with the catalog's own shape.
  return (
    <FlowScope
      key={`example:${definitions.design.definitions.definitionRevision}`}
      basePath={basePath}
      design={{
        catalog: definitions.design.catalog as FlowRuntimeCatalog,
        definitions: definitions.design.definitions as FlowDefinitionCatalog,
      }}
    >
      {children}
    </FlowScope>
  );
}
