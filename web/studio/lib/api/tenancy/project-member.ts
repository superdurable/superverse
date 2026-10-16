// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ProjectMemberSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectMemberSnapshot";
import { readUnary } from "@/lib/api/transport/read-unary";
import { unary } from "@/lib/api/transport/unary";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";

// Browser client of ProjectMemberService. Every change answers with all the members and the Project's new
// revision; only the Project's owners and the platform admin change them.

export async function listProjectMembers(
  projectId: string,
  signal: AbortSignal,
): Promise<ProjectMemberSnapshot__Output[]> {
  return (await readUnary("ListProjectMembers", { projectId }, { signal })).members;
}

/** Adds a member of the Project's Teamspace. */
export function addProjectMember(
  input: Required<RpcInput<"AddProjectMember">>,
  signal: AbortSignal,
): Promise<RpcOutput<"AddProjectMember">> {
  return unary("AddProjectMember", input, { signal });
}

export function setProjectMemberRole(
  input: Required<RpcInput<"SetProjectMemberRole">>,
  signal: AbortSignal,
): Promise<RpcOutput<"SetProjectMemberRole">> {
  return unary("SetProjectMemberRole", input, { signal });
}

export function removeProjectMember(
  input: Required<RpcInput<"RemoveProjectMember">>,
  signal: AbortSignal,
): Promise<RpcOutput<"RemoveProjectMember">> {
  return unary("RemoveProjectMember", input, { signal });
}
