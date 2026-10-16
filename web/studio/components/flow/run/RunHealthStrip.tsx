// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";
import { RUN_GROUP_KEYS, tallyText, type RunGroupKey, type RunHealth } from "./run-groups";

const STRIP_LABEL: Record<RunGroupKey, string> = {
  person: "Need a person",
  progress: "In progress",
  failed: "Failed",
  done: "Done today",
};

/** What the list says when the picked group holds no run. */
export const GROUP_EMPTY: Record<RunGroupKey, string> = {
  person: "No runs need a person.",
  progress: "No runs in progress.",
  failed: "No failed runs.",
  done: "No runs finished today.",
};

/** A count above zero takes its group's meaning: amber where a person must act, blue in progress. */
const COUNT_TONE: Record<RunGroupKey, string> = {
  person: "text-busy",
  progress: "text-accent",
  failed: "text-fail",
  done: "text-ink",
};

/**
 * How the runs stand at a glance: four counts, each also a filter. Picking one shows only its runs;
 * picking it again shows every run. A count the read could not take shows a dash, never a guess.
 */
export function RunHealthStrip({
  health,
  picked,
  onPick,
}: {
  health: RunHealth | null;
  picked: RunGroupKey | null;
  onPick: (key: RunGroupKey) => void;
}) {
  return (
    <div className="mb-2.5 grid shrink-0 grid-cols-2 gap-1.5" role="group" aria-label="Runs at a glance">
      {RUN_GROUP_KEYS.map((key) => {
        const group = health?.groups[key] ?? null;
        const count = group === null ? "–" : tallyText(group);
        const pressed = picked === key;
        return (
          <button
            key={key}
            type="button"
            aria-pressed={pressed}
            aria-label={`${STRIP_LABEL[key]}, ${group === null ? "not known yet" : count}`}
            title={pressed ? "Show every run" : "Show only these runs"}
            data-testid="run-health-group"
            data-group={key}
            onClick={() => onPick(key)}
            className={cx(
              "flex min-w-0 flex-col items-start rounded-md border px-2 py-1.5 text-left transition-colors",
              pressed ? "border-accent-rule bg-accent-wash" : "border-line-faint hover:bg-hover",
            )}
          >
            <span
              className={cx(
                "text-title leading-tight font-semibold tabular-nums",
                group !== null && group.count > 0 ? COUNT_TONE[key] : "text-ink-faint",
              )}
            >
              {count}
            </span>
            <span className="max-w-full truncate text-caption text-ink-dim">{STRIP_LABEL[key]}</span>
          </button>
        );
      })}
    </div>
  );
}
