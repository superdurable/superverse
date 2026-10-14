// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import type { PhaseTiming__Output } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import { formatElapsedDuration } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";

/** The current time, advancing each second while `running`. */
function useNow(running: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [running]);
  return now;
}

/**
 * Where the time of a build, a source preparation, a deployment, a Preview start or a workspace start went, one row
 * per measured phase in start order. A known phase code is labelled and any other is shown verbatim. A phase with no
 * duration is in progress: it shows the time elapsed since its start, which advances each second.
 */
export function PhaseTimings({
  title,
  timings,
  labels,
}: {
  title: string;
  timings: readonly PhaseTiming__Output[];
  labels: Readonly<Record<string, string>>;
}) {
  const rows = timings
    .map((timing) => ({
      phase: timing.phase,
      label: labels[timing.phase] ?? timing.phase,
      startedAt: Date.parse(timestampToISO(timing.startedAt) ?? ""),
      durationMs: Number(timing.durationMs),
    }))
    .sort((a, b) => a.startedAt - b.startedAt);
  const now = useNow(rows.some((row) => row.durationMs === 0));
  if (rows.length === 0) return null;
  return (
    <section>
      <h3 className="text-sm font-medium">{title}</h3>
      <ol className="mt-1 divide-y divide-line-soft text-sm">
        {rows.map((row) => (
          <li key={`${row.phase}@${row.startedAt}`} className="flex items-baseline gap-3 py-1.5">
            <span>{row.label}</span>
            {row.durationMs > 0 ? (
              <span className="ml-auto font-mono text-xs text-ink-dim tabular-nums">
                {formatElapsedDuration(row.durationMs)}
              </span>
            ) : (
              <span className="ml-auto font-mono text-xs text-busy tabular-nums">
                <span className="sr-only">In progress, </span>
                {formatElapsedDuration(now - row.startedAt)}
                <span className="sr-only"> elapsed</span>
              </span>
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}
