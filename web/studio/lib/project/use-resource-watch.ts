// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { watch, WatchConnectionError } from "@/lib/api/transport/watch";
import { delay } from "@/lib/shared/async";
import type { RpcOutput } from "@/lib/shared/rpc-methods";

interface Frames {
  project: RpcOutput<"WatchProject">;
  release: RpcOutput<"WatchRelease">;
  deployment: RpcOutput<"WatchDeployment">;
}
type Resource = keyof Frames;
type Snapshot<R extends Resource> = NonNullable<Frames[R]["snapshot"]>;

/** A dropped Watch reconnects after this pause, from the cursor of its last frame. */
const RECONNECT_MILLISECONDS = 3000;

function openWatch(
  resource: Resource,
  projectId: string,
  resourceId: string,
  options: { resumeCursor: string; signal: AbortSignal; onFrame: (frame: Frames[Resource]) => void },
): Promise<void> {
  if (resource === "release") return watch("WatchRelease", { projectId, releaseId: resourceId }, options);
  if (resource === "deployment") return watch("WatchDeployment", { projectId, deploymentId: resourceId }, options);
  return watch("WatchProject", { projectId }, options);
}

/**
 * Watches one Project, Release or Deployment. Only snapshots carry the
 * resource's state; a progress frame only shows that the Watch is live. A
 * dropped connection reconnects from its last cursor and begins with a fresh
 * snapshot. A refused Watch stays disconnected, a frame for another resource
 * ends it with an error, and a DELETED Project ends it.
 */
export function useResourceWatch<R extends Resource>(
  resource: R,
  projectId: string,
  resourceId: string | null,
  onSnapshot?: (snapshot: Snapshot<R>) => void,
) {
  const key = resourceId === null ? null : JSON.stringify([resource, projectId, resourceId]);
  const [state, setState] = useState<{
    key: string;
    snapshot: Snapshot<R> | null;
    disconnected: boolean;
    error: string;
  } | null>(null);

  useEffect(() => {
    if (key === null || resourceId === null) return;
    const controller = new AbortController();
    const update = (disconnected: boolean, error = "", snapshot?: Snapshot<R>) =>
      setState((current) => ({
        key,
        snapshot: snapshot ?? (current?.key === key ? current.snapshot : null),
        disconnected,
        error,
      }));
    const accept = (snapshot: Snapshot<R>) => {
      if (
        snapshot.projectId !== projectId ||
        (resource === "release" && (!("releaseId" in snapshot) || snapshot.releaseId !== resourceId)) ||
        (resource === "deployment" && (!("deploymentId" in snapshot) || snapshot.deploymentId !== resourceId))
      )
        throw new Error("Resource Watch returned a different resource");
      onSnapshot?.(snapshot);
      update(false, "", snapshot);
      if (resource === "project" && snapshot.status === "PROJECT_STATUS_DELETED") controller.abort();
    };
    void (async () => {
      let resumeCursor = "";
      while (!controller.signal.aborted) {
        try {
          await openWatch(resource, projectId, resourceId, {
            resumeCursor,
            signal: controller.signal,
            onFrame: (frame) => {
              // The frame belongs to this resource's Watch; its snapshot type follows R.
              if (frame.snapshot) accept(frame.snapshot as Snapshot<R>);
              else update(false);
              resumeCursor = frame.resumeCursor;
            },
          });
        } catch (reason) {
          if (!(reason instanceof WatchConnectionError))
            return update(true, reason instanceof Error ? reason.message : "Invalid resource Watch response");
          update(true);
          if (!reason.retryable) return;
          await delay(RECONNECT_MILLISECONDS, controller.signal);
        }
      }
    })();
    return () => controller.abort();
  }, [key, projectId, resource, resourceId, onSnapshot]);

  return state?.key === key ? state : null;
}
