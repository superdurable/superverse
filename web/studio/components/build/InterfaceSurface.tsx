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
import { FoldedTimeline } from "@/components/project/activity/FoldedTimeline";
import { NotReady } from "@/components/ui/NotReady";
import { PreviewPanel } from "./PreviewPanel";

/**
 * The Interface: the application as its users see it. On Preview, the Project's Preview with its
 * start and stop; on Live, the Live address to open and Live's history, which those who only operate
 * Live read here (builders also find it in Tools ▸ Publishing).
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
        <div className="mx-auto flex max-w-170 items-center gap-2 rounded-[9px] border border-line-soft bg-panel py-1 pr-1.5 pl-2">
          <span className="inline-flex items-center gap-1 rounded-full border border-line-soft px-2 text-[11.5px] text-ink-dim">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-pass" />
            Live
          </span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink-dim">{live.url}</span>
          <a
            href={live.url}
            target="_blank"
            rel="noreferrer"
            className="rounded-md px-2 py-1 text-[12.5px] text-accent hover:bg-accent-wash"
          >
            Open
          </a>
        </div>
        <div className="mx-auto mt-2 max-w-170 rounded-[10px] border border-line-soft bg-panel px-3.5 py-3">
          <FoldedTimeline projectId={projectId} scope="live" />
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
