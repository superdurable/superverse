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

/**
 * What fixes an event that did not arrive: starting the environment, the agent fixing the
 * application, or an operator looking at the platform.
 */
export type FateRemedy = "start" | "agent" | "operator";

interface FateWords {
  /** The row's words for one event. */
  row: (environment: EventEnvironment) => string;
  /** Why `count` events did not arrive, after "didn't reach Preview recently:". */
  why: (environment: EventEnvironment, count: number) => string;
  remedy: FateRemedy | null;
}

const them = (count: number) => (count === 1 ? "it" : "them");

/** What each fate says to a person; every fate of the server's closed set has its words here. */
const FATES = {
  FATE_UNSPECIFIED: { row: () => "Unknown", why: () => "the platform did not say why", remedy: "operator" },
  FATE_RECORDED: { row: () => "Delivered", why: () => "", remedy: null },
  FATE_NOT_RUNNING: {
    row: (environment) => `Not delivered: ${environment} was not running`,
    why: () => "it was not running",
    remedy: "start",
  },
  FATE_APPLICATION_UNAVAILABLE: {
    row: () => "Not delivered: the application could not record it",
    why: (_environment, count) => `the application could not record ${them(count)}`,
    remedy: "agent",
  },
  FATE_REFUSED: {
    row: () => "Refused by the application",
    why: (_environment, count) => `the application refused ${them(count)}`,
    remedy: "agent",
  },
  FATE_TIMED_OUT: {
    row: () => "Not delivered: the application did not answer in time",
    why: () => "the application did not answer in time",
    remedy: "agent",
  },
  FATE_TOKEN_UNAVAILABLE: {
    row: () => "Not delivered: the platform could not sign it",
    why: (_environment, count) => `the platform could not sign ${them(count)}`,
    remedy: "operator",
  },
  FATE_QUEUE_FULL: {
    row: () => "Dropped: too many events at once",
    why: () => "too many arrived at once",
    remedy: "operator",
  },
  FATE_EVENT_TOO_LARGE: {
    row: () => "Dropped: the event was too large",
    why: (_environment, count) => (count === 1 ? "it was too large" : "they were too large"),
    remedy: "operator",
  },
} satisfies Record<Fate, FateWords>;

export function fateWords(fate: Fate, environment: EventEnvironment): string {
  return FATES[fate].row(environment);
}

/** Why `count` events of this fate did not arrive, in a few words. */
export function fateWhy(fate: Fate, environment: EventEnvironment, count: number): string {
  return FATES[fate].why(environment, count);
}

export function fateRemedy(fate: Fate): FateRemedy | null {
  return FATES[fate].remedy;
}

/** Only a received event is good news; every other fate is a reason nothing happened. */
export function fateDelivered(fate: Fate): boolean {
  return fate === "FATE_RECORDED";
}

/** The Slack event types a person knows by name: on a row, then counted one and many; any other keeps its raw type. */
const KINDS: Readonly<Record<string, readonly [string, string, string]>> = {
  message: ["Slack message", "Slack message", "Slack messages"],
  app_mention: ["Mention of the app", "mention of the app", "mentions of the app"],
  reaction_added: ["Reaction", "reaction", "reactions"],
  block_actions: ["Button click", "button click", "button clicks"],
};

/** The kind of one event, such as "Slack message"; an unknown type is shown as it is. */
export function eventKind(eventType: string): string {
  return KINDS[eventType]?.[0] ?? (eventType || "Event");
}

/** "2 Slack messages" when every event is of one known kind, else "2 events". */
export function countedKinds(eventTypes: readonly string[]): string {
  const count = eventTypes.length;
  const known = new Set(eventTypes).size === 1 ? KINDS[eventTypes[0]] : undefined;
  return `${count} ${known ? known[count === 1 ? 1 : 2] : count === 1 ? "event" : "events"}`;
}
