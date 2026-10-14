// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { unary } from "./transport/unary";
import { readUnary } from "./transport/read-unary";

/*
 * Browser clients of AgentService and RemoteAgentService. Agent commands name
 * their Session and resolve on admission only; the Session Watch owns subsequent state.
 * ListMockScenarios, GetFlowDesign and the catalog and connection reads are explicit;
 * no endpoint is periodically polled here.
 * A failure whose ApiError carries AGENT_COMMAND_NOT_ADMITTED or
 * AGENT_COMMAND_NOT_DISPATCHED definitely did not run.
 */

/**
 * Sends one message. The Agent does not deduplicate by `messageId`, so a send
 * repeated after an uncertain failure may queue a second copy.
 */
export async function sendMessage(input: Required<RpcInput<"SendMessage">>, signal: AbortSignal): Promise<void> {
  await unary("SendMessage", input, { signal });
}

export async function answerQuestions(
  input: Required<RpcInput<"AnswerQuestions">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("AnswerQuestions", input, { signal });
}

export async function deleteQueuedMessage(
  input: Required<RpcInput<"DeleteQueuedMessage">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("DeleteQueuedMessage", input, { signal });
}

export async function steerQueuedMessage(
  input: Required<RpcInput<"SteerQueuedMessage">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("SteerQueuedMessage", input, { signal });
}

/**
 * Selects the Session's model at any time; the next turn starts on it. A stale `expectedRevision`
 * is refused, and an exact retry of an admitted change is acknowledged again.
 */
export async function updateSessionModel(
  input: Required<RpcInput<"UpdateSessionModel">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("UpdateSessionModel", input, { signal });
}

/**
 * The mock model's scenarios for the caller: `selectable` says whether this account may select the
 * mock model here, and the catalog is empty when it may not. The server authorizes every selection.
 */
export function listMockScenarios(signal: AbortSignal): Promise<RpcOutput<"ListMockScenarios">> {
  return readUnary("ListMockScenarios", {}, { signal });
}

/**
 * The body of the Flow design in a slot, the waiting card's or the confirmed requirements', at the
 * digest its reference carries. A slot without a design is NOT_FOUND; another digest is REVISION_CHANGED.
 */
export function getFlowDesign(
  input: Required<RpcInput<"GetFlowDesign">>,
  signal: AbortSignal,
): Promise<RpcOutput<"GetFlowDesign">> {
  return readUnary("GetFlowDesign", input, { signal });
}

/** Approves or denies the pending tool call. */
export async function resolveToolApproval(
  input: Required<RpcInput<"ResolveToolApproval">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("ResolveToolApproval", input, { signal });
}

/**
 * The Stop button: stops the turn the person saw and every running background command. `turn`
 * comes from the Snapshot, so a Stop of an earlier turn, or with nothing running, is not admitted.
 */
export async function interruptAgent(input: Required<RpcInput<"InterruptAgent">>, signal: AbortSignal): Promise<void> {
  await unary("InterruptAgent", input, { signal });
}

/** Returns execution to Superverse only if the observed executor epoch is still current. */
export async function setSessionExecutor(
  input: Required<RpcInput<"SetSessionExecutor">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("SetSessionExecutor", input, { signal });
}

/** A one-time pairing code bound to this login and the Session's current epoch. */
export function createRemoteAgentPairing(projectId: string, signal: AbortSignal) {
  return unary("CreateRemoteAgentPairing", { projectId }, { signal });
}

/** One dialog/action read; the executor read is never periodically polled. */
export function listRemoteAgentConnections(projectId: string, signal: AbortSignal) {
  return unary("ListRemoteAgentConnections", { projectId }, { signal });
}

/** Removes an exact non-current credential; the server checks current authority. */
export async function revokeRemoteAgentConnection(projectId: string, connectionId: string, signal: AbortSignal) {
  await unary("RevokeRemoteAgentConnection", { projectId, connectionId }, { signal });
}
