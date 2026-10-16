// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { TeamSpaceMemberSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceMemberSnapshot";
import { readUnary } from "@/lib/api/transport/read-unary";
import { unary } from "@/lib/api/transport/unary";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";

// Browser client of TeamSpaceMemberService. Every change answers with all the members and the Teamspace's new
// revision, so the page shows its own change without reading again.

export async function listTeamSpaceMembers(
  teamSpaceId: string,
  signal: AbortSignal,
): Promise<TeamSpaceMemberSnapshot__Output[]> {
  return (await readUnary("ListTeamSpaceMembers", { teamSpaceId }, { signal })).members;
}

/** Adds a registered account; `regainedProjectCount` counts the Projects a returning member holds roles in again. */
export function addTeamSpaceMember(
  input: Required<RpcInput<"AddTeamSpaceMember">>,
  signal: AbortSignal,
): Promise<RpcOutput<"AddTeamSpaceMember">> {
  return unary("AddTeamSpaceMember", input, { signal });
}

export function setTeamSpaceMemberRole(
  input: Required<RpcInput<"SetTeamSpaceMemberRole">>,
  signal: AbortSignal,
): Promise<RpcOutput<"SetTeamSpaceMemberRole">> {
  return unary("SetTeamSpaceMemberRole", input, { signal });
}

/** Refused while the member is the only owner of a Project of the Teamspace; the refusal names the Projects. */
export function removeTeamSpaceMember(
  input: Required<RpcInput<"RemoveTeamSpaceMember">>,
  signal: AbortSignal,
): Promise<RpcOutput<"RemoveTeamSpaceMember">> {
  return unary("RemoveTeamSpaceMember", input, { signal });
}
