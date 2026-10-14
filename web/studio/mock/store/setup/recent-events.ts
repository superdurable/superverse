// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ListRecentTriggerEventsResponse } from "@/lib/generated/protobuf/superverse/v2/ListRecentTriggerEventsResponse";
import type { _superverse_v2_RecentTriggerEvent_Fate as Fate } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";
import { isoToTimestamp } from "@/lib/shared/protobuf";

// Recent events covering every fate, newest first, for the mock Studio; development only.
const FATES = [
  ["FATE_RECORDED", 200],
  ["FATE_NOT_RUNNING", 0],
  ["FATE_APPLICATION_UNAVAILABLE", 503],
  ["FATE_REFUSED", 400],
  ["FATE_TIMED_OUT", 0],
  ["FATE_TOKEN_UNAVAILABLE", 0],
  ["FATE_QUEUE_FULL", 0],
  ["FATE_EVENT_TOO_LARGE", 0],
] as const satisfies readonly (readonly [Fate, number])[];

/** The mock's events: one per fate, a minute apart, the recording having begun an hour ago. */
export function mockRecentTriggerEvents(nowMs: number): ListRecentTriggerEventsResponse {
  return {
    events: FATES.map(([fate, httpStatus], index) => ({
      receivedAt: isoToTimestamp(new Date(nowMs - (index + 1) * 60_000).toISOString()),
      eventId: `Ev0MOCK${index}`,
      eventType: "message",
      eventSubtype: index === 1 ? "thread_broadcast" : "",
      teamId: "T0MOCK",
      channelId: "C0MOCK",
      connectionName: "support",
      fate,
      httpStatus,
      durationMs: httpStatus === 0 ? 0 : 40 + index,
    })),
    bufferStartedAt: isoToTimestamp(new Date(nowMs - 3_600_000).toISOString()),
    bufferInstanceId: "mock-server",
  };
}
