// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useLayoutEffect, useRef } from "react";
import type { PublishFacts } from "@/lib/project/publishing/publish-stages";
import type { useBuildLog } from "@/lib/project/publishing/use-build-log";
import { cx } from "@/components/ui/cx";
import { clockTime, logGroups } from "./log-lines";

/** How close to the end the reader must be for new lines to keep the log scrolled to the end. */
const STICK_PIXELS = 16;

const LEVEL = { info: "text-pass", warn: "text-busy", error: "text-fail" } as const;

/**
 * The inline log under the stage bar, grouped by stage, monospace, each recorded line with its time and
 * level: the build's own output (`log`, read while the log is open), and the publish's recorded phases,
 * cancellation and failure. New lines keep it scrolled to the end unless the reader scrolled up;
 * only the log box scrolls, never the page.
 */
export function PublishLog({
  facts,
  log,
  running,
  liveVersion,
}: {
  facts: PublishFacts;
  /** The build's output read so far; a rollback builds nothing. */
  log: ReturnType<typeof useBuildLog>;
  running: boolean;
  liveVersion: string;
}) {
  const box = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);
  const groups = logGroups(facts, log.lines, liveVersion);
  const count = groups.reduce((sum, group) => sum + group.lines.length, 0);
  useLayoutEffect(() => {
    const element = box.current;
    if (element && stuck.current) element.scrollTop = element.scrollHeight;
  }, [count]);
  return (
    <div
      ref={box}
      role="log"
      aria-label="Publish log"
      tabIndex={0}
      onScroll={(event) => {
        const element = event.currentTarget;
        stuck.current = element.scrollTop + element.clientHeight >= element.scrollHeight - STICK_PIXELS;
      }}
      className="max-h-[300px] overflow-auto border-t border-line-soft bg-well px-4 py-3 font-mono text-[12px] leading-[1.6] break-words whitespace-pre-wrap text-ink focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
      data-testid="publish-log"
    >
      {log.dropped > 0 && <p className="text-ink-faint">… {log.dropped} earlier lines are not shown</p>}
      {groups.map((group, index) => (
        <section key={group.stage.id} aria-label={group.stage.label}>
          <p className={cx("text-ink-faint", index > 0 && "mt-1.5")}>── {group.stage.label}</p>
          {group.lines.map((line, at) => (
            <p key={at}>
              {line.at && <span className="text-ink-faint">{clockTime(line.at)} </span>}
              {line.level && <span className={LEVEL[line.level]}>{line.level}</span>}
              {line.level && ": "}
              {line.text}
            </p>
          ))}
        </section>
      ))}
      {facts.build && !log.loaded && !log.error && <p className="text-ink-faint">Reading the build log…</p>}
      {log.error && <p className="text-fail">{log.error}. Trying again…</p>}
      {running && <p className="text-ink-faint">…</p>}
    </div>
  );
}
