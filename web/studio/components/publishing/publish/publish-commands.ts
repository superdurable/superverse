// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, useState } from "react";
import { configurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import { getProjectConfiguration } from "@/lib/api/project";
import { deriveLiveConfiguration, getRelease, startReleaseBuild, type ReleaseBuildInput } from "@/lib/api/release";
import {
  ApiError,
  isRevisionConflict,
  newRequestId,
  retainedAfterFailure,
  type PendingCommand,
} from "@/lib/api/transport/api-error";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { errorProblem, failureProblem, type Problem } from "@/lib/shared/problem/problem";
import { flowApiRoutes } from "@/lib/shared/studio-routes";

/** The refusal of a build whose frozen copy was built before Live, Preview or the next publish moved. */
const CONFIGURATION_STALE = "RELEASE_CONFIGURATION_STALE";

/** A Release whose source preparation failed, with the failure it recorded. */
class PreparationFailure extends Error {
  constructor(readonly failure: Failure__Output) {
    super(failure.message);
    this.name = "PreparationFailure";
  }
}

/** Thrown when what the popover showed moved before the build was admitted; it says what changed. */
class PublishMoved extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PublishMoved";
  }
}

/** The problem of a failed Publish or of a version's preparation, in the one problem format. */
export function publishProblem(reason: unknown, commit: string, failure?: Failure__Output | null): Problem {
  if (failure) return failureProblem(failure, { operation: "Preparing this version", commit });
  if (reason instanceof PreparationFailure)
    return failureProblem(reason.failure, { operation: "Preparing this version", commit });
  return errorProblem(reason, { operation: "Publishing this version", commit });
}

/** What a Publish builds and fences on, exactly as the popover showed it. */
export interface PublishRequest {
  commit: string;
  /** The next-publish layer revision and Preview's applied revision the popover displayed. */
  nextPublishRevision: string;
  previewConfigurationRevision: string;
  expectedLiveRevision: string;
  /** Empty when Preview validated the version or Live runs its code. */
  previewSkipReason: string;
  /** The next publish the popover checked, which a move is described against. */
  shown: ProjectConfigurationSnapshot__Output;
}

/** What moved between what the popover showed and what the server holds now, in words. */
function movedWords(shown: ProjectConfigurationSnapshot__Output, now: ProjectConfigurationSnapshot__Output): string {
  const changes = [
    now.nextPublishRevision !== shown.nextPublishRevision && "the next publish's settings changed in Setup",
    now.previewConfigurationRevision !== shown.previewConfigurationRevision && "Preview applied new settings",
    now.requirementGaps.length > shown.requirementGaps.length &&
      `${now.requirementGaps.length} ${now.requirementGaps.length === 1 ? "setting needs" : "settings need"} setting up`,
  ].filter(Boolean);
  const what = changes.length ? changes.join("; ") : "Live or the settings moved";
  return `Changed since you opened Publish: ${what}. Check what happens again, then publish.`;
}

/**
 * Writes what Setup still holds for Live's next publish, so the copy is built from what the person
 * entered: the layer's queue against the version, and against the Preview's source.
 */
async function flushNextPublish(projectId: string, commit: string) {
  for (const basePath of new Set([
    flowApiRoutes.liveNextPublish(projectId, null),
    flowApiRoutes.liveNextPublish(projectId, commit),
  ]))
    await configurationDraftQueue(basePath).flush();
}

/**
 * Publish: builds the version's frozen copy from exactly the next publish the popover showed
 * (DeriveLiveConfiguration refuses a layer or Preview revision that moved), then one build of the copy
 * that deploys automatically, fenced on the Live revision shown, with the skip reason when Preview did
 * not validate the version. A move is read again and described; a build request whose acceptance is
 * uncertain is retried as the same request, while changed revisions or a changed reason are a new one.
 */
export function usePublish(projectId: string) {
  const action = useSingleFlight<"publish">();
  const pending = useRef<{ shown: string; command: PendingCommand<ReleaseBuildInput> } | null>(null);
  const [retrying, setRetrying] = useState(false);
  const [moved, setMoved] = useState("");

  const describeMove = async (request: PublishRequest, signal: AbortSignal) => {
    const now = await getProjectConfiguration(
      { target: { projectId, scope: "PROJECT_SCOPE_LIVE", releaseId: request.commit, nextPublish: true } },
      signal,
    );
    const words = movedWords(request.shown, now);
    setMoved(words);
    return new PublishMoved(words);
  };

  const publish = (request: PublishRequest, onAccepted: (buildRevisionId: string) => void) =>
    action.run("publish", async (signal) => {
      setMoved("");
      const shown = JSON.stringify([
        request.commit,
        request.nextPublishRevision,
        request.previewConfigurationRevision,
        request.expectedLiveRevision,
        request.previewSkipReason,
      ]);
      if (pending.current?.shown !== shown) {
        pending.current = null;
        await flushNextPublish(projectId, request.commit);
        let copy: ProjectConfigurationSnapshot__Output;
        try {
          copy = await deriveLiveConfiguration(
            {
              projectId,
              releaseId: request.commit,
              expectedRevision: request.nextPublishRevision,
              expectedPreviewConfigurationRevision: request.previewConfigurationRevision,
            },
            signal,
          );
        } catch (reason) {
          throw isRevisionConflict(reason) ? await describeMove(request, signal) : reason;
        }
        const release = (await getRelease({ projectId, releaseId: request.commit }, signal)).release;
        if (!release) throw new Error("This version is no longer prepared. Open Publish again.");
        if (release.preparedSource?.status === "RELEASE_STATUS_FAILED" && release.preparedSource.failure)
          throw new PreparationFailure(release.preparedSource.failure);
        pending.current = {
          shown,
          command: {
            input: {
              projectId,
              requestId: newRequestId(),
              releaseId: request.commit,
              buildRevisionId: `build-${newRequestId()}`,
              configurationRevision: copy.revision,
              expectedRevision: release.revision,
              autoDeploy: true,
              expectedLiveRevision: request.expectedLiveRevision,
              previewSkipReason: request.previewSkipReason,
            },
            uncertain: false,
          },
        };
      }
      const { command } = pending.current;
      try {
        await startReleaseBuild(command.input, signal);
      } catch (reason) {
        const kept = retainedAfterFailure(command, reason);
        pending.current = kept && { shown, command: kept };
        setRetrying(Boolean(kept?.uncertain));
        const stale = reason instanceof ApiError && reason.errorInfo?.reason === CONFIGURATION_STALE;
        throw stale || isRevisionConflict(reason) ? await describeMove(request, signal) : reason;
      }
      pending.current = null;
      setRetrying(false);
      onAccepted(command.input.buildRevisionId);
    });

  return {
    busy: action.busy !== null,
    /** The failed Publish, except a move, which `moved` describes. */
    failure: action.failure && !(action.failure.reason instanceof PublishMoved) ? action.failure : null,
    /** What moved since the popover showed it; empty when nothing did. */
    moved,
    retrying,
    publish,
  };
}
