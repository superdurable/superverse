// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Messages by sequence, a later copy replacing an earlier one, in sequence order. Sequences have
 * gaps wherever the server holds back an entry, so they only order messages and never count them.
 */
export function mergeSequencedMessages<TMessage extends { sequence: number }>(
  current: readonly TMessage[],
  incoming: readonly TMessage[],
): TMessage[] {
  const bySequence = new Map(current.map((message) => [message.sequence, message]));
  for (const message of incoming) bySequence.set(message.sequence, message);
  return [...bySequence.values()].sort((left, right) => left.sequence - right.sequence);
}

/** Appends an activity once per resume token, keeping only the newest `maximum`. */
export function appendActivityEvent<TEvent extends { resumeToken: string }>(
  current: TEvent[],
  incoming: TEvent,
  maximum: number,
): TEvent[] {
  if (current.some((event) => event.resumeToken === incoming.resumeToken)) return current;
  return [...current, incoming].slice(-maximum);
}
