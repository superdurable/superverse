// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { EnvironmentEvent__Output } from "@/lib/generated/protobuf/superverse/v2/EnvironmentEvent";
import { enumText, type EnumText } from "@/lib/shared/protobuf";
import { versionLabel } from "@/lib/shared/version/version";

type EventKind = Exclude<EnumText<EnvironmentEvent__Output["kind"], "KIND">, "">;

interface Wording {
  actor: string;
  revision: string;
  rolledBack: boolean;
}

/** The sentence of each kind of event, over the closed set of kinds: a new kind fails the compiler here. */
const SENTENCES: Record<EventKind, (words: Wording) => string> = {
  PREVIEW_CONFIGURATION_APPLIED: ({ actor, revision }) =>
    `${actor} applied configuration ${revision} to the Preview, on`,
  PREVIEW_STARTED: ({ actor }) => `${actor} started the Preview on`,
  // The server puts a start's outcome on its start event; this kind is declared but never listed.
  PREVIEW_START_RESULT: () => "Preview start of",
  PUBLISHED: ({ actor }) => `${actor} published`,
  DEPLOYED_EXISTING_VERSION: ({ actor, rolledBack }) => `${actor} ${rolledBack ? "rolled Live back to" : "deployed"}`,
  PUBLISH_CANCELLED: ({ actor }) => `${actor} cancelled publishing`,
};

/** How each outcome reads, and whether it is a good, bad or unfinished one. */
export const OUTCOME_WORDS = {
  RUNNING: { words: "in progress", tone: "text-busy" },
  READY: { words: "succeeded", tone: "text-pass" },
  FAILED: { words: "failed", tone: "text-fail" },
  REFUSED: { words: "refused", tone: "text-fail" },
  CANCELLED: { words: "cancelled", tone: "text-ink-dim" },
} as const;

export type OutcomeText = keyof typeof OUTCOME_WORDS;

/** The version of an event as "V12 · subject"; never its hash. */
export function eventVersion(event: EnvironmentEvent__Output): string {
  return versionLabel(event.version);
}

/**
 * The event as the words before its version, which the page shows after them. The Agent's push has no actor in the Preview; a Live event without
 * one comes from a deployment that never recorded who started it.
 */
export function eventSentence(event: EnvironmentEvent__Output, scope: "preview" | "live"): string {
  const kind = enumText(event.kind, "KIND") as EventKind;
  const actor = event.actorDisplay || (scope === "preview" ? "The agent" : "An unknown member");
  return SENTENCES[kind]({
    actor,
    revision: event.configurationRevision,
    rolledBack: event.rolledBack,
  });
}

/** The outcome of an event, or null for one that carries none (a start before its result). */
export function eventOutcome(event: EnvironmentEvent__Output): OutcomeText | null {
  const outcome = enumText(event.outcome, "START_OUTCOME");
  return outcome === "" ? null : outcome;
}

/** A key that names one event in a list: the protocol gives events no identity of their own. */
export function eventKey(event: EnvironmentEvent__Output): string {
  const { seconds, nanos } = event.occurredAt ?? { seconds: "0", nanos: 0 };
  return [event.kind, seconds, nanos, event.deploymentId, event.buildRevisionId, event.releaseId, event.commit].join(
    "|",
  );
}
