// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { _superverse_v2_RecentTriggerEvent_Fate__Output as Fate } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";

/** The environment a list of events belongs to, in the words of a fate. */
export type EventEnvironment = "Preview" | "Live";

/** What a fate says to a person; every fate of the server's closed set has its words here. */
const FATE_WORDS = {
  FATE_UNSPECIFIED: () => "Unknown",
  FATE_RECORDED: () => "Received by the application",
  FATE_NOT_RUNNING: (environment) => `Not delivered: ${environment} was not running`,
  FATE_APPLICATION_UNAVAILABLE: () => "Not delivered: the application could not record it",
  FATE_REFUSED: (_environment, status) => `Refused by the application (HTTP ${status})`,
  FATE_TIMED_OUT: () => "Not delivered: the application did not answer in time",
  FATE_TOKEN_UNAVAILABLE: () => "Not delivered: the platform could not sign it",
  FATE_QUEUE_FULL: () => "Dropped: too many events at once",
  FATE_EVENT_TOO_LARGE: () => "Dropped: the event was too large",
} satisfies Record<Fate, (environment: EventEnvironment, status: number) => string>;

export function fateWords(fate: Fate, environment: EventEnvironment, httpStatus: number): string {
  return FATE_WORDS[fate](environment, httpStatus);
}

/** Only a received event is good news; every other fate is a reason nothing happened. */
export function fateDelivered(fate: Fate): boolean {
  return fate === "FATE_RECORDED";
}
