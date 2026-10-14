// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect } from "react";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import {
  studioRoutes,
  type ProjectEnvironment,
  type ProjectSurface as Surface,
  type ProjectTool,
} from "@/lib/shared/studio-routes";
import { PROJECT_ROLES, projectRoleOf } from "@/lib/teamspace/project-roles";
import { canOpenSurface, surfacesFor } from "@/components/frame/surface-registry";
import { FlowSurface } from "@/components/build/FlowSurface";
import { InterfaceSurface } from "@/components/build/InterfaceSurface";
import { LiveFlowScope, SessionFlowScope } from "@/components/flow/scope/FlowScope";
import { InboxSurface, RunsSurface } from "@/components/flow/scope/FlowSurfaces";
import { NotReady } from "@/components/ui/NotReady";
import { RecentEvents } from "./activity/RecentEvents";
import { ProjectToolContent } from "./ProjectToolContent";

/**
 * The content of the surface the URL names, inside the Builder frame. A Project without Live shows
 * Preview, as the bar's switch does; a role that works on Live only always sees Live, and a surface the role
 * cannot open leads to the first one it can.
 */
export function ProjectSurface({
  projectId,
  surface,
  tool,
  environment,
  releaseId,
}: {
  projectId: string;
  surface: Surface;
  tool: ProjectTool;
  environment: ProjectEnvironment;
  releaseId: string | null;
}) {
  const owner = useProjectResource();
  const project = owner?.snapshot;
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  const role = project ? projectRoleOf(project.callerRole) : null;
  const closed = role !== null && !canOpenSurface(role, surface);
  const router = useRouter();
  const search = useSearchParams();
  useEffect(() => {
    if (role && closed)
      router.replace(studioRoutes.projectSurface(projectId, surfacesFor(role)[0], Object.fromEntries(search)));
  }, [closed, projectId, role, router, search]);
  if (!project || closed)
    return (
      <NotReady kind={owner?.error ? "error" : "placeholder"} heading="Loading project">
        {owner?.error ||
          (owner?.disconnected
            ? "Project updates disconnected. Reconnecting to verify access…"
            : "Reading the authorized project state…")}
      </NotReady>
    );
  if (project.status === "PROJECT_STATUS_DELETED" || project.status === "PROJECT_STATUS_DELETING")
    return (
      <NotReady kind="unavailable" heading="Project is being removed">
        Open the project page to observe cleanup.
      </NotReady>
    );
  const liveOnly = role !== null && PROJECT_ROLES[role].liveOnly;
  if (liveOnly && !live)
    return (
      <NotReady kind="empty" heading="Nothing is Live yet">
        This project has not been published, so there are no runs or Inbox items to work on. Its builders publish it
        from Preview.
      </NotReady>
    );
  return (
    <SurfaceContent
      projectId={projectId}
      surface={surface}
      tool={tool}
      environment={liveOnly ? "live" : live ? environment : "preview"}
      toolEnvironment={liveOnly ? "live" : environment}
      releaseId={releaseId}
      liveKey={live ? `${live.deploymentId}:${live.revision}` : null}
    />
  );
}

function SurfaceContent({
  projectId,
  surface,
  tool,
  environment,
  toolEnvironment,
  releaseId,
  liveKey,
}: {
  projectId: string;
  surface: Surface;
  tool: ProjectTool;
  environment: ProjectEnvironment;
  /** The environment a tool shows: Setup on Live before the first publish says to set Live up from Preview. */
  toolEnvironment: ProjectEnvironment;
  releaseId: string | null;
  /** The deployment Live serves, or null without Live. */
  liveKey: string | null;
}) {
  switch (surface) {
    case "flow":
      return <FlowSurface projectId={projectId} environment={environment} />;
    case "interface":
      return <InterfaceSurface projectId={projectId} environment={environment} />;
    case "tools":
      return <ProjectToolContent projectId={projectId} tool={tool} environment={toolEnvironment} />;
    case "runs":
    case "inbox": {
      const view =
        surface === "runs" ? (
          <RunsSurface
            events={<RecentEvents projectId={projectId} environment={environment === "live" ? "Live" : "Preview"} />}
          />
        ) : (
          <InboxSurface />
        );
      if (environment === "preview")
        return (
          <SessionFlowScope key={`preview:${projectId}`} projectId={projectId} source="running">
            {view}
          </SessionFlowScope>
        );
      if (!liveKey)
        return (
          <NotReady kind="empty" heading="Nothing is Live yet">
            Runs and Inbox items on Live appear once a version is published. Publish from Tools, Publishing.
          </NotReady>
        );
      return (
        <LiveFlowScope key={`live:${liveKey}`} projectId={projectId} releaseId={releaseId}>
          {view}
        </LiveFlowScope>
      );
    }
  }
}
