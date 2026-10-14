// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";

/** What a record without a frozen version shows: one prepared before versions were recorded. */
const NOT_RECORDED = "Version not recorded";

/** What a commit not prepared yet shows, such as the Agent's latest push or a queued deployment. */
export const LATEST_PUSH = "the latest push";

/**
 * A commit as Studio names it: "V12 · Add refund approval", the number Git gives it on the default
 * branch and its subject. A record without a version says so; the hash is never the name.
 */
export function versionLabel(version: CommitVersion__Output | null | undefined, unknown = NOT_RECORDED): string {
  if (!version) return unknown;
  return version.subject ? `V${version.number} · ${version.subject}` : `V${version.number}`;
}

/** Only the number, "V12", where a row has no room for the subject. */
export function versionNumber(version: CommitVersion__Output | null | undefined, unknown = NOT_RECORDED): string {
  return version ? `V${version.number}` : unknown;
}

/** The full commit, for folded technical details and hover hints only. */
export function commitDetail(commit: string): string {
  return commit ? `Commit ${commit}` : "";
}
