// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef } from "react";
import {
  newPreviewCommand,
  refreshAfterRejection,
  startPreview,
  stopPreview,
  type PreviewCommandInput,
  type ApplyOptions,
} from "@/lib/api/preview";
import { retainedAfterFailure, type PendingCommand } from "@/lib/api/transport/api-error";
import { usePreviewDraft } from "@/lib/project/setup/PreviewDraftProvider";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useProjectSessionResource } from "@/lib/session/SessionProvider";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { cannotCheckText, errorProblem, failureProblem, type Problem } from "@/lib/shared/problem/problem";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { requirementGapName } from "@/lib/shared/problem/requirement-gaps";
import { timestampToISO } from "@/lib/shared/protobuf";
import type { RemedyActions } from "@/components/ui/problem/ProblemPanel";

export type PreviewAction = "start" | "stop" | "apply";

/** What each Preview action is, as a problem says what happened. */
const ACTION_OPERATIONS: Record<PreviewAction, string> = {
  start: "Starting the Preview",
  stop: "Stopping the Preview",
  apply: "Applying Setup to the Preview",
};

/** A command's fence, with a start's options; a stop sends the fence alone. */
type CommandInput = PreviewCommandInput & { apply?: ApplyOptions };

/**
 * Starting, stopping and applying Setup to the Project's Preview. Each command is fenced to the Preview
 * revision and Sandbox instance it was decided against; a command whose acceptance is uncertain keeps
 * its request ID for the retry, and the caller remounts for a new source or instance, which starts new
 * operations. Apply flushes the autosave queue first and freezes exactly the revision its last write returned,
 * restarting a running Preview; Setup sends it on its own once edits settle, and a start sends it too while
 * saved settings differ from what Preview runs.
 */
export function usePreviewCommands(projectId: string) {
  const session = useProjectSessionResource();
  const preview = session.resource?.preview ?? null;
  const action = useSingleFlight<PreviewAction>();
  const operations = useRef<Partial<Record<PreviewAction, PendingCommand<CommandInput>>>>({});
  const source = preparedSourceOf(session.resource);
  const sourceReady = readyPreparedSource(source, session.resource?.headCommit) !== null;
  const draft = usePreviewDraft();
  const configuration = sourceReady ? (draft?.configuration ?? null) : null;
  const configurationReady =
    configuration?.status === "CONFIGURATION_STATUS_READY" && configuration.sourceCommit === source?.sourceCommit;

  const status = preview?.status;
  const runningDesired = status === "PREVIEW_STATUS_STARTING" || status === "PREVIEW_STATUS_READY";
  const stopping = status === "PREVIEW_STATUS_STOPPING";
  // isRecoverable is advisory: a retained stop failure stays retryable once its
  // displayed prerequisite is addressed, as the server re-admits stop-preview.
  // The release that cleans up after an automatic deployment that started the
  // Preview without serving it stops the same way.
  const cleanupPhase = preview?.failure?.phase;
  const retryCleanup = stopping && (cleanupPhase === "stop-preview" || cleanupPhase === "release");
  const idle = action.busy === null;
  // A push's automatic update moves the Preview's revision; a start or apply sent meanwhile would be refused, so
  // both wait for it.
  const pushUpdating = preview?.automaticDeployment?.status === "PREVIEW_DEPLOYMENT_STATUS_RUNNING";
  const allowed: Record<PreviewAction, boolean> = {
    start:
      idle &&
      !pushUpdating &&
      session.status === "SESSION_STATUS_ACTIVE" &&
      sourceReady &&
      configurationReady &&
      (status === "PREVIEW_STATUS_NOT_STARTED" || status === "PREVIEW_STATUS_STOPPED"),
    stop: idle && (runningDesired || retryCleanup),
    // Apply restarts a running Preview, and is enabled only on a READY configuration of the prepared source.
    apply:
      idle &&
      !pushUpdating &&
      draft !== null &&
      session.status === "SESSION_STATUS_ACTIVE" &&
      sourceReady &&
      configurationReady &&
      status !== "PREVIEW_STATUS_STARTING" &&
      !stopping,
  };

  const act = (kind: PreviewAction) =>
    action.run(kind, async (signal) => {
      if (!allowed[kind]) return;
      let operation = operations.current[kind];
      if (!operation) {
        const fence = newPreviewCommand(projectId, preview);
        let apply: ApplyOptions | undefined;
        // A start also takes Setup's saved settings that Preview does not run yet, as Setup applies them on its own.
        if (kind === "apply" || (kind === "start" && (configuration?.unappliedChangeCount ?? 0) > 0)) {
          // The queue's last write names the revision the person saw; without one, the read configuration's.
          const flushed = draft ? await draft.queue.flush() : null;
          apply = { applyConfiguration: true, configurationRevision: flushed ?? configuration?.revision ?? "0" };
        }
        operation = operations.current[kind] = { ...fence, input: { ...fence.input, apply } };
      }
      try {
        const { apply, ...fence } = operation.input;
        if (kind === "stop") await stopPreview(fence, signal);
        else await startPreview(fence, apply, signal);
      } catch (reason) {
        const retained = retainedAfterFailure(operation, reason);
        if (retained) {
          operations.current[kind] = retained;
          throw reason;
        }
        delete operations.current[kind];
        await refreshAfterRejection(session.refresh, signal, reason, `Could not ${kind} Preview`);
      }
      // Acceptance at this observed revision supersedes earlier intents without
      // claiming their uncertain effects succeeded. An older duplicate response
      // must not erase an intent observed at a newer revision.
      for (const [retained, other] of Object.entries(operations.current)) {
        if (BigInt(other.input.expectedRevision) <= BigInt(operation.input.expectedRevision))
          delete operations.current[retained as PreviewAction];
      }
      await session.refresh(signal, true);
    });

  // A refused request is the newest problem; otherwise the Preview's recorded failure, with the
  // configuration its refused start lacks.
  const failedCommit = preview?.automaticDeployment?.commit || preview?.sourceCommit || "";
  const problem: Problem | null = action.failure
    ? errorProblem(action.failure.reason, { operation: ACTION_OPERATIONS[action.failure.label] })
    : preview?.failure
      ? failureProblem(preview.failure, {
          operation: "The Preview",
          gaps: preview.configurationGaps.map(requirementGapName),
          commit: failedCommit,
          version: preview.preparedSource?.sourceCommit === failedCommit ? preview.preparedSource.version : null,
          at: timestampToISO(preview.automaticDeployment?.updatedAt) ?? "",
        })
      : null;
  // Trying again applies Setup when it can; otherwise it starts or stops as the Preview stands.
  const remedyActions: RemedyActions = {
    ...projectRemedyLinks(projectId),
    FAILURE_REMEDY_APPLY_AGAIN: allowed.apply
      ? { onClick: () => void act("apply") }
      : runningDesired
        ? { onClick: () => void act("stop"), disabled: !allowed.stop }
        : { onClick: () => void act("start"), disabled: !allowed.start },
  };

  return {
    session,
    preview,
    status,
    runningDesired,
    stopping,
    retryCleanup,
    pushUpdating,
    sourceReady,
    configurationReady,
    configuration,
    // A configuration that is not READY, lists no gap and names a reason cannot be checked for that reason.
    configurationError:
      draft?.error ||
      (configuration &&
      configuration.status !== "CONFIGURATION_STATUS_READY" &&
      configuration.requirementGaps.length === 0 &&
      configuration.validationErrors.length > 0
        ? cannotCheckText(configuration.validationErrors)
        : ""),
    allowed,
    busy: action.busy,
    problem,
    remedyActions,
    act,
  };
}
