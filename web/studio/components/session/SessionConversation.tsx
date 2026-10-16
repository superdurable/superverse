// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { NotReady } from "@/components/ui/NotReady";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { studioRoutes } from "@/lib/shared/studio-routes";
import type { LiveViewing } from "./LiveNote";
import { SessionChat } from "./SessionChat";
import { StartupSteps, type StartupStep } from "./start/StartupSteps";
import { failureProblem } from "@/lib/shared/problem/problem";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";

/** Before the Session exists: the startup steps, at the one the Project is at. */
function Waiting({ step }: { step: StartupStep }) {
  return (
    <div className="flex min-h-0 items-center justify-center p-4">
      <StartupSteps current={step} />
    </div>
  );
}

/**
 * The conversation of the Project's one Session. While Setup runs the Project has no Session yet;
 * once the Project is READY its ProjectFlow starts the Session, which the page reads as soon as
 * it exists.
 */
export function SessionConversation({ projectId, live }: { projectId: string; live?: LiveViewing }) {
  const project = useProjectResource()?.snapshot;
  const owner = useSessionResource();
  if (project?.status === "PROJECT_STATUS_SETUP_FAILED") {
    return (
      <aside className="flex min-h-0 flex-col">
        <NotReady
          kind="error"
          heading="Project setup failed"
          detail={`project ${projectId}`}
          action={{ label: "Open the project page", href: studioRoutes.project(projectId) }}
        >
          Delete this project from its project page to clean up its resources, then create a new project.
        </NotReady>
      </aside>
    );
  }
  if (project?.status === "PROJECT_STATUS_CREATING") return <Waiting step="project" />;
  if (project?.sessionStartFailure && !owner?.resource) {
    return (
      <aside className="flex min-h-0 flex-col">
        <ProblemPanel
          className="m-4"
          problem={failureProblem(project.sessionStartFailure, { operation: "Starting the Project's Agent" })}
        />
      </aside>
    );
  }
  if (!owner || (owner.loading && !owner.resource)) return <Waiting step="agent" />;
  if (owner.resource?.projectId !== projectId) {
    return (
      <aside className="flex min-h-0 flex-col">
        <NotReady kind="error" heading="The conversation did not load" detail={`project ${projectId}`}>
          {owner.error ?? "This project's agent session could not be loaded."}
        </NotReady>
      </aside>
    );
  }
  return <SessionChat live={live} />;
}
