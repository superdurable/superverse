// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { getRelease, prepareRelease } from "@/lib/api/release";
import { ApiError, errorMessage, newRequestId } from "@/lib/api/transport/api-error";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { ReleaseSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import { useProjectConfiguration } from "@/lib/project/use-project-configuration";
import { delay } from "@/lib/shared/async";

/** A Release's source is prepared within minutes; the popover stops waiting, not the preparation, after this. */
const PREPARATION_WAIT_MILLISECONDS = 15 * 60_000;
const PREPARATION_POLL_MILLISECONDS = 2000;

/** Where a version's Release stands for the popover. */
type Preparation =
  | { state: "reading" | "preparing" }
  | { state: "ready"; release: ReleaseSnapshot__Output }
  | { state: "failed"; failure: Failure__Output | null; error: string };

async function readRelease(projectId: string, commit: string, signal: AbortSignal) {
  try {
    return (await getRelease({ projectId, releaseId: commit }, signal)).release;
  } catch (reason) {
    if (reason instanceof ApiError && reason.status === 404) return null;
    throw reason;
  }
}

/**
 * The Release of `commit`, prepared when it has none: one PrepareRelease per commit and page, never
 * again for a Release whose preparation failed, whose failure is answered instead.
 */
function usePreparedRelease(projectId: string, commit: string | null) {
  const [read, setRead] = useState<{ key: string; preparation: Preparation } | null>(null);
  const requests = useRef(new Map<string, string>());
  const key = commit ? `${projectId}:${commit}` : "";
  useEffect(() => {
    if (!commit) return;
    const controller = new AbortController();
    const report = (preparation: Preparation) =>
      !controller.signal.aborted && setRead({ key: `${projectId}:${commit}`, preparation });
    void (async () => {
      try {
        let release = await readRelease(projectId, commit, controller.signal);
        if (!release) {
          report({ state: "preparing" });
          const requestId = requests.current.get(commit) ?? newRequestId();
          requests.current.set(commit, requestId);
          await prepareRelease({ projectId, requestId, sourceCommit: commit }, controller.signal);
        }
        const deadline = Date.now() + PREPARATION_WAIT_MILLISECONDS;
        for (;;) {
          release ??= await readRelease(projectId, commit, controller.signal);
          const status = release?.preparedSource?.status;
          if (release && status === "RELEASE_STATUS_READY") return report({ state: "ready", release });
          if (status === "RELEASE_STATUS_FAILED")
            return report({ state: "failed", failure: release?.preparedSource?.failure ?? null, error: "" });
          if (Date.now() >= deadline)
            return report({
              state: "failed",
              failure: null,
              error: "This version is still being prepared. Try again in a moment.",
            });
          report({ state: "preparing" });
          await delay(PREPARATION_POLL_MILLISECONDS, controller.signal);
          release = null;
        }
      } catch (reason) {
        report({ state: "failed", failure: null, error: errorMessage(reason, "This version could not be prepared") });
      }
    })();
    return () => controller.abort();
  }, [commit, projectId]);
  return read?.key === key ? read.preparation : { state: "reading" as const };
}

/**
 * What the Publish popover checks for one version: its Release, prepared when it has none, and Live's
 * next publish checked against it, with the layer and Preview revisions it resolved from. While the
 * Release is prepared, the version Preview's prepared source runs (`previewSource`) is checked against
 * that source. Nothing is read without a version.
 */
export function usePublishCheck(projectId: string, commit: string | null, previewSource: string, version = "") {
  const preparation = usePreparedRelease(projectId, commit);
  const releaseId = preparation.state === "ready" && commit ? commit : commit && commit === previewSource ? "" : null;
  const read = useProjectConfiguration(
    releaseId === null ? null : { projectId, scope: "PROJECT_SCOPE_LIVE", releaseId, nextPublish: true },
    version,
  );
  // Once the Release is prepared the check moves from the Preview's source to it; the version's last
  // check stays on screen until the new one answers, so what the popover shows does not blink away.
  const [last, setLast] = useState<{ commit: string; configuration: typeof read.configuration } | null>(null);
  if (commit && read.configuration && last?.configuration !== read.configuration)
    setLast({ commit, configuration: read.configuration });
  return {
    preparation,
    /** The prepared Release, whose revision a build is fenced on. */
    release: preparation.state === "ready" ? preparation.release : null,
    configuration: read.configuration ?? (last && last.commit === commit ? last.configuration : null),
    error: read.error,
    loaded: read.loaded,
    refresh: read.retry,
  };
}
