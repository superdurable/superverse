// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentSnapshot } from "./agent-types";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import type { SessionStatus__Output } from "@/lib/generated/protobuf/superverse/v2/SessionStatus";
import type { WorkspaceStatus__Output } from "@/lib/generated/protobuf/superverse/v2/WorkspaceStatus";
import { timestampToISO } from "@/lib/shared/protobuf";

/** The statuses Studio shows; sessionStatus() reads any other value as FAILED. */
export type KnownSessionStatus = Exclude<SessionStatus__Output, "SESSION_STATUS_UNSPECIFIED">;

const KNOWN_STATUSES: ReadonlySet<string> = new Set<KnownSessionStatus>([
  "SESSION_STATUS_ACTIVE",
  "SESSION_STATUS_ARCHIVED",
  "SESSION_STATUS_FAILED",
]);

/** A status this client does not know reads as failed: it cannot be offered as usable. */
export function sessionStatus({ status }: Pick<SessionSnapshot__Output, "status">): KnownSessionStatus {
  return KNOWN_STATUSES.has(status) ? (status as KnownSessionStatus) : "SESSION_STATUS_FAILED";
}

/** The Sandbox for the next tool or Preview is still starting. */
export function isWorkspacePreparing(resource: SessionSnapshot__Output): boolean {
  const status = resource.workspace?.status;
  return status === "WORKSPACE_STATUS_INITIALIZING" || status === "WORKSPACE_STATUS_PROVISIONING";
}

/**
 * When the Session reclaims its ready Sandbox: the Sandbox deadline, which the agent's work moves
 * later. Null while no instance is ready.
 */
export function workspaceDeadline(resource: SessionSnapshot__Output): string | null {
  const workspace = resource.workspace;
  return workspace?.status === "WORKSPACE_STATUS_READY" ? timestampToISO(workspace.expiresAt) : null;
}

/**
 * The active Session reclaimed its idle Sandbox: the instance closed and its volume, with every
 * file, is kept for the next one, which the agent's next use of the workspace starts.
 */
export function isWorkspacePaused(resource: SessionSnapshot__Output): boolean {
  const workspace = resource.workspace;
  return (
    sessionStatus(resource) === "SESSION_STATUS_ACTIVE" &&
    workspace?.status === "WORKSPACE_STATUS_CLOSED" &&
    workspace.failure === null
  );
}

/** A starting, ready or reclaimed Sandbox: whatever its last failure said, it never retains a dirty workspace. */
const SETTLED_OR_STARTING: ReadonlySet<WorkspaceStatus__Output> = new Set([
  "WORKSPACE_STATUS_INITIALIZING",
  "WORKSPACE_STATUS_PROVISIONING",
  "WORKSPACE_STATUS_READY",
  "WORKSPACE_STATUS_CLOSING",
  "WORKSPACE_STATUS_CLOSED",
]);

/**
 * A Sandbox that ended unsettled kept its storage because the workspace held
 * uncommitted or unverified changes.
 */
export function isWorkspaceRetainedDirty(resource: SessionSnapshot__Output): boolean {
  const workspace = resource.workspace;
  if (!workspace || SETTLED_OR_STARTING.has(workspace.status)) return false;
  return workspace.failure?.code === "DIRTY_WORKSPACE" || workspace.workspaceState === "WORKSPACE_STATE_DIRTY";
}

/**
 * The Project's durable process build. Its immutable commit bundle remains
 * ready across Sandbox replacement; runtime requests carry their own instance fence.
 */
export function preparedSourceOf(resource: SessionSnapshot__Output | null | undefined) {
  return resource?.preview?.preparedSource ?? null;
}

/** The prepared source when it is ready and is the Session's pushed head, otherwise null. */
export function readyPreparedSource(source: ReturnType<typeof preparedSourceOf>, headCommit: string | undefined) {
  return source?.status === "PREVIEW_SOURCE_STATUS_READY" && source.sourceCommit === headCommit ? source : null;
}

/** Session revisions and committed message sequences never decrease across reads. */
export function isStaleSessionSnapshot(
  currentRevision: string | null,
  nextRevision: string,
  current: AgentSnapshot | null,
  next: AgentSnapshot | null,
): boolean {
  return (
    (currentRevision !== null && BigInt(nextRevision) < BigInt(currentRevision)) ||
    (current !== null && next !== null && next.description.lastSequence < current.description.lastSequence)
  );
}
