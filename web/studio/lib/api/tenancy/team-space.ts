// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { TeamSpaceOverview__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceOverview";
import type { TeamSpaceSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceSnapshot";
import { readUnary } from "@/lib/api/transport/read-unary";
import { unary } from "@/lib/api/transport/unary";
import { required } from "@/lib/shared/protobuf";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";

// Browser client of TeamSpaceService: Teamspaces, their Home and their deletion.

/** Starts a Teamspace with the caller as its admin; a retry with the same request ID is the same Teamspace. */
export async function createTeamSpace(
  input: Required<RpcInput<"CreateTeamSpace">>,
  signal: AbortSignal,
): Promise<string> {
  return (await unary("CreateTeamSpace", input, { signal })).teamSpaceId;
}

/** The caller's Teamspaces, or every one for the platform admin. */
export function listTeamSpaces(
  input: RpcInput<"ListTeamSpaces">,
  signal: AbortSignal,
): Promise<RpcOutput<"ListTeamSpaces">> {
  return readUnary("ListTeamSpaces", input, { signal });
}

export async function getTeamSpace(teamSpaceId: string, signal: AbortSignal): Promise<TeamSpaceSnapshot__Output> {
  return required((await readUnary("GetTeamSpace", { teamSpaceId }, { signal })).teamSpace, "Teamspace");
}

/** Renames the Teamspace the page read at `expectedRevision`; a Teamspace that changed since is REVISION_CHANGED. */
export async function renameTeamSpace(
  input: Required<RpcInput<"RenameTeamSpace">>,
  signal: AbortSignal,
): Promise<TeamSpaceSnapshot__Output> {
  return required((await unary("RenameTeamSpace", input, { signal })).teamSpace, "renamed Teamspace");
}

/** Accepts the deletion of the Teamspace with its Projects; only the platform admin may. */
export async function deleteTeamSpace(
  input: Required<RpcInput<"DeleteTeamSpace">>,
  signal: AbortSignal,
): Promise<TeamSpaceSnapshot__Output> {
  return required((await unary("DeleteTeamSpace", input, { signal })).teamSpace, "deleted Teamspace");
}

export async function getTeamSpaceOverview(
  teamSpaceId: string,
  signal: AbortSignal,
): Promise<TeamSpaceOverview__Output> {
  return required((await readUnary("GetTeamSpaceOverview", { teamSpaceId }, { signal })).overview, "overview");
}
