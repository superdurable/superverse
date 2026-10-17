// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Glyph } from "@/components/ui/Glyph";
import type { ConfirmedRequirements, Requirements, SequencedMessage } from "@/lib/session/agent-types";
import { RequirementsSpec } from "./RequirementsSpec";

/** A version of the requirements that a confirm_requirements call of the history recorded. */
export interface RecordedRequirementsVersion {
  version: number;
  /** The card's title, which the call's target carries. */
  title: string;
  /** The card itself, known only for the current version. */
  spec: Requirements | null;
}

const CONFIRM_REQUIREMENTS = "confirm_requirements";

/** Whether a call's target names this title: the server condenses its whitespace and cuts it at 200 characters. */
function namesTitle(target: string, title: string): boolean {
  const condensed = title.split(/\s+/).filter(Boolean).join(" ");
  return target === condensed || (target.endsWith("…") && condensed.startsWith(target.slice(0, -1)));
}

/**
 * The versions the history's confirm_requirements calls recorded, by call ID. The history carries each call's
 * target (the card's title) and its outcome, never its arguments or its version: the newest call a result
 * answers without failure recorded the current version, and each earlier one the version before it, which
 * holds however many earlier pages are loaded. A card a Session still waits at (`waitingCallId`) is not
 * recorded yet. Only the current version's card is in the Snapshot, so an older line has its title alone.
 */
export function recordedRequirementsVersions(
  messages: readonly SequencedMessage[],
  current: ConfirmedRequirements | null,
  waitingCallId: string | null,
): ReadonlyMap<string, RecordedRequirementsVersion> {
  const recorded = new Map<string, RecordedRequirementsVersion>();
  if (current === null) return recorded;
  const results = new Map(
    messages.flatMap(({ message }) => (message.toolCallId ? [[message.toolCallId, message]] : [])),
  );
  const calls = messages.flatMap(({ message }) =>
    message.toolCalls.filter((call) => {
      const outcome = results.get(call.id)?.toolExecution?.outcome;
      return (
        call.name === CONFIRM_REQUIREMENTS &&
        call.id !== waitingCallId &&
        results.has(call.id) &&
        outcome !== "known_failure" &&
        outcome !== "unknown"
      );
    }),
  );
  calls.forEach((call, index) => {
    const version = current.revision - (calls.length - 1 - index);
    if (version < 1) return;
    const isCurrent = version === current.revision && namesTitle(call.target, current.spec.title);
    recorded.set(call.id, {
      version,
      title: isCurrent ? current.spec.title : call.target,
      spec: isCurrent ? current.spec : null,
    });
  });
  return recorded;
}

/**
 * One recorded version in the conversation, where the agent recorded it: one plain line that opens to the
 * card's full text when the card is known, and scrolls away with the conversation.
 */
export function RecordedRequirementsLine({ recorded }: { recorded: RecordedRequirementsVersion }) {
  const line = `Recorded version ${String(recorded.version)}: ${recorded.title}`;
  if (recorded.spec === null)
    return (
      <p className="text-caption wrap-anywhere text-ink-dim" data-recorded-version={recorded.version}>
        {line}
      </p>
    );
  return (
    <details className="group text-caption text-ink-dim" data-recorded-version={recorded.version}>
      <summary className="flex cursor-pointer list-none items-start gap-1 rounded hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent [&::-webkit-details-marker]:hidden">
        <Glyph name="chevronRight" className="mt-0.5 size-3.5 shrink-0 transition-transform group-open:rotate-90" />
        <span className="wrap-anywhere">{line}</span>
      </summary>
      <div className="mt-2 rounded-lg border border-line-soft bg-panel px-3 py-2">
        <RequirementsSpec spec={recorded.spec} />
      </div>
    </details>
  );
}
