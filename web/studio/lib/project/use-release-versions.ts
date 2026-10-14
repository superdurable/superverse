// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { listReleaseCandidates } from "@/lib/api/release";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { ReleaseCandidate__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseCandidate";

/** ListReleaseCandidates answers the newest main-branch commits, at most this many. */
const CANDIDATE_LIMIT = 100;

/**
 * The Project's versions: its newest main-branch commits, newest first, and which of them are ahead
 * of the commit Live serves (`liveReleaseId`, a Release ID being its commit). A new `reloadKey`, such
 * as the Session's head commit, reads them again. When Live serves a commit older than the newest
 * hundred, every listed version is ahead and `aheadExact` is false. Without a Project, as for an operator, who
 * sees Live only, nothing is read.
 */
export function useReleaseVersions(projectId: string | null, liveReleaseId: string | null, reloadKey: unknown) {
  const [read, setRead] = useState<{
    key: string;
    candidates: ReleaseCandidate__Output[];
    error: string;
  } | null>(null);
  const key = JSON.stringify([projectId, reloadKey]);

  useEffect(() => {
    if (projectId === null) return;
    const controller = new AbortController();
    listReleaseCandidates({ projectId }, controller.signal).then(
      (candidates) => {
        if (!controller.signal.aborted) setRead({ key, candidates, error: "" });
      },
      (reason: unknown) => {
        if (!controller.signal.aborted)
          setRead((current) => ({
            key,
            candidates: current?.candidates ?? [],
            error: errorMessage(reason, "Could not read the versions"),
          }));
      },
    );
    return () => controller.abort();
  }, [key, projectId]);

  const candidates = read?.candidates ?? [];
  const liveIndex = liveReleaseId ? candidates.findIndex((item) => item.sourceCommit === liveReleaseId) : -1;
  const ahead = liveIndex >= 0 ? candidates.slice(0, liveIndex) : candidates;
  return {
    loaded: read?.key === key,
    error: read?.error ?? "",
    candidates,
    ahead,
    /** The version Live serves, when it is among the listed ones. */
    live: liveIndex >= 0 ? candidates[liveIndex] : null,
    aheadExact: liveIndex >= 0 || candidates.length < CANDIDATE_LIMIT,
  };
}

export type ReleaseVersions = ReturnType<typeof useReleaseVersions>;
