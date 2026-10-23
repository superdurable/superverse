// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { getEvolveProposal } from "@/lib/api/evolve";

/** A proposal document as the page has read it; absent when the review has none. */
type ProposalDocument =
  | { state: "reading" }
  | { state: "failed"; error: unknown }
  | { state: "absent" }
  | { state: "ready"; markdown: string };

const READING: ProposalDocument = { state: "reading" };

/**
 * The document a review's agent wrote with its proposal, from `GetEvolveProposal`, read once a row opens
 * (`reviewId` set) and kept while the page stays; `retry` reads it again after a failure.
 */
export function useProposalDocument(projectId: string, reviewId: string | null) {
  const [read, setRead] = useState<{ key: string; document: ProposalDocument } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const key = reviewId ? `${projectId}:${reviewId}` : null;
  // A read document is kept; a failed read stays failed until retry counts another attempt.
  const kept = read !== null && read.key === key && read.document.state !== "failed";
  useEffect(() => {
    if (!reviewId || !key || kept) return;
    const controller = new AbortController();
    getEvolveProposal({ projectId, reviewId }, controller.signal).then(
      (markdown) => setRead({ key, document: markdown === null ? { state: "absent" } : { state: "ready", markdown } }),
      (error: unknown) => {
        if (!controller.signal.aborted) setRead({ key, document: { state: "failed", error } });
      },
    );
    return () => controller.abort();
  }, [attempt, key, kept, projectId, reviewId]);
  return {
    document: read !== null && read.key === key ? read.document : READING,
    retry: () => setAttempt((count) => count + 1),
  };
}
