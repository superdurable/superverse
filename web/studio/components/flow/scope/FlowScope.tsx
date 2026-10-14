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
import { initialTemplateDesign } from "@/lib/project/initial-template-design";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { flowApiRoutes, studioRoutes } from "@/lib/shared/studio-routes";
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

/**
 * The Flow Definitions of the Project's Session: of its prepared current
 * source (Build's Design), or of the Project's running Preview (Preview Runs
 * and Inbox). Before the first source is prepared, a managed project's setup
 * commit shows the template's generated Design.
 */
export function SessionFlowScope({
  projectId,
  source,
  children,
}: {
  projectId: string;
  source: "prepared" | "running";
  children: ReactNode;
}) {
  const project = useProjectResource()?.snapshot;
  const session = useSessionResource()?.resource;
  if (!session || session.projectId !== projectId)
    return (
      <NotReady kind="placeholder" heading="Loading the project's Session">
        Reading the project&apos;s agent Session.
      </NotReady>
    );
  const preview = session.preview;
  if (source === "prepared") {
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
    const design = initialTemplateDesign(project, session);
    if (!design)
      return (
        <NotReady kind="unavailable" heading="Processes of this version are not built yet">
          Its processes appear after the next push builds them automatically. Turn off automatic builds to use Build
          processes above.
        </NotReady>
      );
    // The generated template design is analyzer output with the catalog's own shape.
    return (
      <FlowScope
        key={`initial:${session.headCommit}`}
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
  if (preview?.status !== "PREVIEW_STATUS_READY" || !preview.sourceCommit || !preview.definitionRevision) {
    return (
      <NotReady
        kind="unavailable"
        heading="Preview is not running"
        action={{ label: "Start Preview", href: studioRoutes.projectSurface(projectId, "interface") }}
      >
        Runs and Inbox items on Preview appear while it runs. Start it from the Interface, or the agent&apos;s next push
        deploys it.
      </NotReady>
    );
  }
  const basePath = flowApiRoutes.previewRuntime(projectId);
  return (
    <FlowScope
      key={JSON.stringify([basePath, preview.workspaceRequestId, preview.servingRevision])}
      basePath={basePath}
      workspaceRequestId={preview.workspaceRequestId}
      generation={preview.generation}
    >
      {children}
    </FlowScope>
  );
}
