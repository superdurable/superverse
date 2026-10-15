// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useSessionResource } from "@/lib/session/SessionProvider";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";
import { NotReady } from "@/components/ui/NotReady";
import { LiveInterface } from "./LiveInterface";
import { PreviewPanel } from "./PreviewPanel";

/**
 * The Interface: the application as its users see it, under an address bar, with its details folded
 * underneath. On Preview, the Project's Preview with its start and stop; on Live, the Live app at its
 * address, with Live's history, which those who only operate Live read here (builders also find it in
 * Tools ▸ Publishing).
 */
export function InterfaceSurface({ projectId, environment }: { projectId: string; environment: ProjectEnvironment }) {
  const session = useSessionResource();
  const live = useProjectResource()?.snapshot?.live;
  if (environment === "live") {
    if (!live || live.isRemoving)
      return (
        <NotReady kind="empty" heading="Nothing is Live yet">
          The app has a Live address once a version is published. Publish from Tools, Publishing.
        </NotReady>
      );
    return (
      <div className="min-h-0 flex-1 overflow-y-auto bg-well p-6">
        <div className="mx-auto max-w-170" data-testid="interface-live">
          <LiveInterface projectId={projectId} live={live} />
        </div>
      </div>
    );
  }
  if (!session)
    return (
      <NotReady kind="placeholder" heading="Waiting for the project's agent">
        The Preview opens once the project is ready and its agent has started.
      </NotReady>
    );
  const resource = session.resource;
  return (
    <div className="min-h-0 flex-1 overflow-y-auto bg-well p-6">
      <div className="mx-auto max-w-170" data-testid="interface-preview">
        <PreviewPanel
          key={JSON.stringify([
            projectId,
            resource?.workspace?.workspaceRequestId,
            resource?.workspace?.generation,
            resource?.headCommit,
          ])}
          projectId={projectId}
        />
      </div>
    </div>
  );
}
