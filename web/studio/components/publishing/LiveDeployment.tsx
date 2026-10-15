// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { removeLive } from "@/lib/api/project";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { commitDetail, versionLabel, versionNumber } from "@/lib/shared/version/version";
import { failureProblem } from "@/lib/shared/problem/problem";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { RequestProblem } from "./RequestProblem";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dialog } from "@/components/ui/Dialog";

/** Stop Live's confirmation: what stops, what is removed and what is kept, before anything happens. */
function StopLiveDialog({
  live,
  retry,
  onConfirm,
  onClose,
}: {
  live: ProjectLive__Output;
  /** A failed removal is retried; nothing new stops. */
  retry: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog onDismiss={onClose} aria-label="Stop Live" data-testid="stop-live-dialog">
      <div className="grid gap-3 px-5 py-4 text-[13px]">
        <h2 className="text-[15px] font-semibold text-ink">
          {retry ? "Retry removing Live's serving resources?" : `Stop Live ${versionNumber(live.version)}?`}
        </h2>
        <ul className="grid list-disc gap-1 pl-5 text-ink-dim" data-testid="stop-live-consequences">
          <li>
            Live stops serving
            {live.url ? (
              <>
                {" "}
                at <span className="font-mono text-xs">{live.url}</span>
              </>
            ) : null}
            ; people using the application lose it.
          </li>
          <li>Its triggers stop: events such as Slack messages no longer start or reach Live runs.</li>
          <li>Its deployed resources are removed. Its configuration and application data are kept.</li>
          <li>Preview is not affected. Publishing a version again brings Live back.</li>
        </ul>
        <div className="flex justify-end gap-2 border-t border-line-faint pt-3">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="danger" onClick={onConfirm}>
            {retry ? "Retry Live cleanup" : "Stop Live"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

/** What Live serves now and Stop Live…; the Interface opens it. The publish in progress is shown above it. */
export function LiveDeployment({
  projectId,
  project,
  publishing,
}: {
  projectId: string;
  project: ProjectSnapshot__Output | null;
  /** A publish runs, which Stop Live waits for. */
  publishing: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const action = useSingleFlight<"remove">();
  const live = project?.live;
  const removing = Boolean(live?.isRemoving);
  const removalFailed = removing && Boolean(project?.failure);

  // Removal is fenced on the Live pointer this page shows; a pointer that moved since is rejected.
  const remove = (shown: ProjectLive__Output) => {
    setConfirming(false);
    void action.run("remove", async (signal) => {
      await removeLive({ projectId, deploymentId: shown.deploymentId, expectedLiveRevision: shown.revision }, signal);
    });
  };

  const failure = removalFailed ? project?.failure : null;
  const state = live ? (live.isRemoving ? "REMOVING" : "READY") : "Not deployed";
  return (
    <section className="space-y-3 rounded-xl border border-line-soft bg-panel px-4 py-3.5" aria-label="What is Live">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h3 className="flex min-w-0 items-center gap-2 text-[15px] font-semibold text-ink">
          <span
            aria-hidden="true"
            className={cx("size-2 shrink-0 rounded-full", live && !removing ? "bg-pass" : "border border-mark")}
          />
          {live ? (
            <span className="truncate" title={commitDetail(live.releaseId)}>
              Live: {versionLabel(live.version)}
            </span>
          ) : (
            "Not published yet"
          )}
        </h3>
        <span className="text-[13px] text-ink-faint">
          {!live
            ? "Publish puts the version you pick on a Live address."
            : removing
              ? removalFailed
                ? "stopping Live failed"
                : "stopping…"
              : publishing
                ? "a publish is running"
                : "serving"}
        </span>
        <span data-testid="live-serving-state" className="sr-only">
          {state}
        </span>
        {live && (
          <span className="ml-auto flex gap-2">
            <Button
              size="sm"
              variant="danger"
              disabled={action.busy !== null || publishing || (removing && !removalFailed)}
              title={publishing ? "Wait for the publish to finish, or cancel it" : undefined}
              onClick={() => setConfirming(true)}
            >
              {action.busy || (removing && !removalFailed)
                ? "Stopping Live…"
                : removalFailed
                  ? "Retry Live cleanup"
                  : "Stop Live…"}
            </Button>
          </span>
        )}
      </div>
      {live && confirming && (
        <StopLiveDialog
          live={live}
          retry={removalFailed}
          onConfirm={() => remove(live)}
          onClose={() => setConfirming(false)}
        />
      )}
      {action.failure && <RequestProblem operation="Stopping Live" reason={action.failure.reason} />}
      {failure && (
        <ProblemPanel
          problem={failureProblem(failure, { operation: "Stopping Live" })}
          actions={{
            ...projectRemedyLinks(projectId),
            FAILURE_REMEDY_PUBLISH_AGAIN: live ? { onClick: () => setConfirming(true) } : undefined,
          }}
        />
      )}
    </section>
  );
}
