// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentHistoryPage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentHistoryPage";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import { required } from "@/lib/shared/protobuf";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { unary } from "./transport/unary";
import { readUnary } from "./transport/read-unary";

// Browser client of SessionService. A Project has exactly one Session, which its ProjectFlow
// starts when the Project becomes READY; the Project addresses it. The page's one Session Watch
// goes through transport/watch.

/** The Project's Session; a Project that is not READY yet, or whose Session is still starting, is a 404. */
export async function getSession(
  input: Required<RpcInput<"GetSession">>,
  signal: AbortSignal,
): Promise<SessionSnapshot__Output> {
  return required((await readUnary("GetSession", input, { signal })).session, "Session");
}

/**
 * Keeps the Session's READY workspace running for seven days from now; false while the Session is
 * not active or its workspace not READY, which leaves the deadline unchanged.
 */
export async function renewWorkspace(
  input: Required<RpcInput<"RenewWorkspace">>,
  signal: AbortSignal,
): Promise<boolean> {
  return (await unary("RenewWorkspace", input, { signal })).renewed;
}

/** One page of archived messages before `beforeSequence`, a ten-message archive boundary. */
export async function listArchivedMessages(
  input: RpcInput<"ListArchivedMessages">,
  signal: AbortSignal,
): Promise<AgentHistoryPage__Output> {
  return required((await readUnary("ListArchivedMessages", input, { signal })).history, "archived history");
}
