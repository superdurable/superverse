// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { startDeployment } from "@/lib/api/deployment";
import { getRelease } from "@/lib/api/release";
import { errorMessage, newRequestId, retainedAfterFailure, type PendingCommand } from "@/lib/api/transport/api-error";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { ReleaseSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { formatMonthDayTime } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { versionNumber } from "@/lib/shared/version/version";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { RequestProblem } from "./RequestProblem";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";

/** startDeployment names the deployment after its request ID. */
type PendingDeployment = PendingCommand<Omit<Required<RpcInput<"StartDeployment">>, "deploymentId">>;

/**
 * Rolling Live back to an older version, a dialog that states the consequences first: which version and
 * build will run, that it runs with the settings that build froze, which version it replaces, that runs in
 * flight finish where they started, and that Preview and Live's next publish stay as they are, so the next
 * publish moves Live forward again. Confirming deploys that version's last successful build, fenced on the
 * Live revision shown; `onStarted` then follows the deployment in the stage bar.
 */
export function ReleaseDeployment({
  projectId,
  releaseId,
  project,
  publishing,
  onClose,
  onStarted,
}: {
  projectId: string;
  releaseId: string;
  project: ProjectSnapshot__Output | null;
  /** A publish runs, which a rollback waits for. */
  publishing: boolean;
  onClose: () => void;
  onStarted: (deploymentId: string) => void;
}) {
  const [release, setRelease] = useState<ReleaseSnapshot__Output | null>(null);
  const [loadError, setLoadError] = useState("");
  const [pendingDeployment, setPendingDeployment] = useState<PendingDeployment | null>(null);
  const action = useSingleFlight<"deploy">();
  const ready = release?.buildRevisions.find(
    (item) => item.buildRevisionId === release.readyBuildRevisionId && item.status === "RELEASE_STATUS_READY",
  );

  useEffect(() => {
    const controller = new AbortController();
    getRelease({ projectId, releaseId }, controller.signal).then(
      (result) => !controller.signal.aborted && setRelease(result.release),
      (reason: unknown) => !controller.signal.aborted && setLoadError(errorMessage(reason, "Request failed")),
    );
    return () => controller.abort();
  }, [projectId, releaseId]);

  const deploy = async (signal: AbortSignal) => {
    if (!ready) return;
    const command = pendingDeployment ?? {
      input: {
        projectId,
        requestId: newRequestId(),
        releaseId,
        buildRevisionId: ready.buildRevisionId,
        configurationRevision: ready.configurationRevision,
        expectedLiveRevision: project?.live?.revision ?? "0",
      },
      uncertain: false,
    };
    setPendingDeployment(command);
    try {
      const deploymentId = await startDeployment(command.input, signal);
      setPendingDeployment(null);
      onStarted(deploymentId);
    } catch (reason) {
      setPendingDeployment(retainedAfterFailure(command, reason));
      throw reason;
    }
  };

  const failed = action.failure?.reason ?? (loadError || null);
  const target = versionNumber(release?.preparedSource?.version);
  const current = versionNumber(project?.live?.version);
  const builtAt = ready ? formatMonthDayTime(timestampToISO(ready.createdAt) ?? "") : "";
  return (
    <Dialog width="lg" onDismiss={onClose} aria-label={`Roll Live back to ${target}`} data-testid="rollback-dialog">
      <div className="grid gap-3 px-5 py-4 text-[13px]">
        <h2 className="text-[15px] font-semibold text-ink">Roll Live back to {target}?</h2>
        {ready ? (
          <ul className="grid list-disc gap-1 pl-5 text-ink-dim" data-testid="rollback-consequences">
            <li>
              Live goes back from <b className="text-ink">{current}</b> to <b className="text-ink">{target}</b>, its
              build of {builtAt}.
            </li>
            <li>
              It runs with the settings {target} was published with: the connections and the channels its triggers
              listened in then, not those of Live&apos;s next publish today. Connection credentials are the TeamSpace
              connections&apos; current ones.
            </li>
            <li>Runs already started finish on the version they started on; new runs start on {target}.</li>
            <li>
              Preview stays on its version and Live&apos;s next publish keeps its settings, so the next publish moves
              Live forward again and its differences show as waiting.
            </li>
          </ul>
        ) : (
          <p className="text-ink-dim">
            {release ? "This version has no successful build to roll back to." : "Reading the version…"}
          </p>
        )}
        {failed && (
          <RequestProblem
            operation="Rolling Live back"
            reason={failed}
            actions={{
              ...projectRemedyLinks(projectId, studioRoutes.setup(projectId, "live")),
              FAILURE_REMEDY_PUBLISH_AGAIN: { onClick: () => void action.run("deploy", deploy) },
              FAILURE_REMEDY_APPLY_AGAIN: { onClick: () => void action.run("deploy", deploy) },
            }}
          />
        )}
        <div className="flex justify-end gap-2 border-t border-line-faint pt-3">
          <Button onClick={onClose}>Not yet</Button>
          <Button
            disabled={
              action.busy !== null ||
              !ready ||
              publishing ||
              project?.status !== "PROJECT_STATUS_READY" ||
              project.live?.isRemoving
            }
            title={publishing ? "Wait for the running publish, or cancel it" : undefined}
            onClick={() => void action.run("deploy", deploy)}
          >
            {action.busy
              ? "Starting the rollback…"
              : pendingDeployment
                ? "Retry the same rollback"
                : `Roll back to ${target}`}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
