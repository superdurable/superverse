// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { getReleaseBuildLog } from "@/lib/api/release";
import { errorMessage } from "@/lib/api/transport/api-error";
import { delay } from "@/lib/shared/async";

/** The log is read no faster than this, and no faster than the server's `retry_after_ms`. */
const MIN_POLL_MILLISECONDS = 2000;
/** After a failed read the next one waits this long. */
const FAILURE_POLL_MILLISECONDS = 5000;
/** The lines kept in the page; older ones are dropped and counted. */
const MAX_LINES = 5000;

/** The build whose log is read. */
export interface BuildLogTarget {
  projectId: string;
  releaseId: string;
  buildRevisionId: string;
}

interface LogState {
  key: string;
  lines: string[];
  /** Lines dropped from the start to keep MAX_LINES. */
  dropped: number;
  complete: boolean;
  error: string;
}

/**
 * A build's redacted log, read through `GetReleaseBuildLog` page by page from its cursor while `open`,
 * no faster than every 2 seconds or the server's `retry_after_ms`, and no more once it is complete.
 * Hiding the log stops the reads and keeps what was read; showing it again continues from the cursor.
 * Nothing is stored beyond this page.
 */
export function useBuildLog(target: BuildLogTarget | null, open: boolean) {
  const key = target ? JSON.stringify([target.projectId, target.releaseId, target.buildRevisionId]) : "";
  const [state, setState] = useState<LogState | null>(null);
  // Where the next read starts, so a log shown again continues instead of reading from the start.
  const resume = useRef({ key: "", cursor: "" });
  const current = state?.key === key ? state : null;
  const complete = current?.complete ?? false;

  useEffect(() => {
    if (!open || !key || complete) return;
    const [projectId, releaseId, buildRevisionId] = JSON.parse(key) as [string, string, string];
    const controller = new AbortController();
    void (async () => {
      let cursor = resume.current.key === key ? resume.current.cursor : "";
      while (!controller.signal.aborted) {
        let wait = MIN_POLL_MILLISECONDS;
        try {
          const page = await getReleaseBuildLog({ projectId, releaseId, buildRevisionId, cursor }, controller.signal);
          if (controller.signal.aborted) return;
          cursor = page.nextCursor;
          resume.current = { key, cursor };
          setState((was) => {
            const base: LogState = was?.key === key ? was : { key, lines: [], dropped: 0, complete: false, error: "" };
            const lines = [...base.lines, ...page.lines];
            const over = Math.max(0, lines.length - MAX_LINES);
            return {
              key,
              lines: over ? lines.slice(over) : lines,
              dropped: base.dropped + over,
              complete: page.complete,
              error: "",
            };
          });
          if (page.complete) return;
          wait = Math.max(MIN_POLL_MILLISECONDS, page.retryAfterMs);
        } catch (reason) {
          if (controller.signal.aborted) return;
          const error = errorMessage(reason, "Could not read the build log");
          setState((was) => ({
            ...(was?.key === key ? was : { key, lines: [], dropped: 0, complete: false }),
            error,
          }));
          wait = FAILURE_POLL_MILLISECONDS;
        }
        try {
          await delay(wait, controller.signal);
        } catch {
          return;
        }
      }
    })();
    return () => controller.abort();
  }, [open, key, complete]);

  return {
    lines: current?.lines ?? [],
    dropped: current?.dropped ?? 0,
    complete,
    error: current?.error ?? "",
    /** The first page has answered. */
    loaded: current !== null,
  };
}
