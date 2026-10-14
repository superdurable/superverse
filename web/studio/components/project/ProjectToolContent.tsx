// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import type { ProjectEnvironment, ProjectTool } from "@/lib/shared/studio-routes";
import { projectRoleOf } from "@/lib/teamspace/project-roles";
import { PublishingPanel } from "@/components/publishing/PublishingPanel";
import { ProjectPermissions } from "@/components/teamspace/ProjectPermissions";
import { useHashFocus } from "@/lib/browser/hash-focus";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { NotReady } from "@/components/ui/NotReady";
import { SetupPage } from "./SetupPage";

const TOOL_PAGE = "max-w-6xl px-6.5 pt-5.5 pb-10";

function ToolHeading({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <h2 className="text-lg font-semibold text-ink">{title}</h2>
      <p className="mt-1 mb-4 text-[13px] text-ink-faint">{children}</p>
    </>
  );
}

/** The open tool of Tools; Setup shows the environment the bar's tab selects. */
export function ProjectToolContent({
  projectId,
  tool,
  environment,
}: {
  projectId: string;
  tool: ProjectTool;
  environment: ProjectEnvironment;
}) {
  switch (tool) {
    case "publishing":
      return <PublishingPanel key={projectId} projectId={projectId} />;
    case "setup":
      return <SetupTool projectId={projectId} environment={environment} />;
    case "permissions":
      return <PermissionsTool projectId={projectId} />;
  }
}

/**
 * Tools, Setup: on Preview, Preview's settings beside the next publish's; on Live, the next publish's alone.
 * A link to an item lands on it.
 */
function SetupTool({ projectId, environment }: { projectId: string; environment: ProjectEnvironment }) {
  const session = useSessionResource();
  useHashFocus();
  if (!session)
    return (
      <NotReady kind="placeholder" heading="Waiting for the project's agent">
        Setup opens once the project is ready and its agent has started.
      </NotReady>
    );
  return <SetupPage projectId={projectId} environment={environment} />;
}

/** The Project's people and their roles; its owners change them. */
function PermissionsTool({ projectId }: { projectId: string }) {
  const project = useProjectResource()?.snapshot;
  const role = project ? projectRoleOf(project.callerRole) : null;
  return (
    <div className={TOOL_PAGE}>
      <ToolHeading title="Permissions">
        One role per person in this project. People join from the project&apos;s TeamSpace.
      </ToolHeading>
      {project && role ? (
        <ProjectPermissions projectId={projectId} teamSpaceId={project.teamSpaceId} role={role} />
      ) : (
        <p className="text-[13px] text-ink-faint" role="status">
          Loading the project…
        </p>
      )}
    </div>
  );
}
