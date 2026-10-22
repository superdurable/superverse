// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useState } from "react";
import { getEvolve, startEvolveReview } from "@/lib/api/evolve";
import { newRequestId, retainedAfterFailure, type PendingCommand } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveReviewRefusal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewRefusal";
import { delay } from "@/lib/shared/async";

/**
 * While a review runs the page reads the measurement again this often, for at most REFRESH_LIMIT_MS from the read
 * that found it running: an export of a usual Project takes minutes, and a longer one stays shown as running until
 * the page is read again.
 */
const REFRESH_MILLISECONDS = 5_000;
const REFRESH_LIMIT_MILLISECONDS = 15 * 60_000;

/** A Project's goal measurement as the page has read it; absent before its first Live deployment. */
type GoalMeasurement =
  | { state: "reading" }
  | { state: "failed"; error: unknown }
  | { state: "absent" }
  | { state: "ready"; evolve: Evolve__Output };

const READING: GoalMeasurement = { state: "reading" };

/**
 * The Project's goal measurement from `GetEvolve`, read again while a review runs until it ends, and on `reload`.
 * A read that fails after one succeeded keeps what the page shows.
 */
export function useGoalMeasurement(projectId: string) {
  const [read, setRead] = useState<{ projectId: string; measurement: GoalMeasurement } | null>(null);
  const [reads, setReads] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const { signal } = controller;
    void (async () => {
      const deadline = Date.now() + REFRESH_LIMIT_MILLISECONDS;
      for (;;) {
        let measurement: GoalMeasurement;
        try {
          const evolve = await getEvolve({ projectId }, signal);
          measurement = evolve ? { state: "ready", evolve } : { state: "absent" };
        } catch (error) {
          if (signal.aborted) return;
          setRead((current) =>
            current?.projectId === projectId && current.measurement.state === "ready"
              ? current
              : { projectId, measurement: { state: "failed", error } },
          );
          return;
        }
        if (signal.aborted) return;
        setRead({ projectId, measurement });
        if (measurement.state !== "ready" || !measurement.evolve.activeReviewId || Date.now() >= deadline) return;
        await delay(REFRESH_MILLISECONDS, signal);
        if (signal.aborted) return;
      }
    })();
    return () => controller.abort();
  }, [projectId, reads]);
  const reload = useCallback(() => setReads((count) => count + 1), []);
  return { measurement: read?.projectId === projectId ? read.measurement : READING, reload };
}

/**
 * Review now: one request at a time, whose request ID a retry after an unknown outcome repeats. A refusal is an
 * answer, kept until the next click; either answer reads the measurement again through `onAnswer`.
 */
export function useReviewNow(projectId: string, onAnswer: () => void) {
  const action = useSingleFlight<"review">();
  const [pending, setPending] = useState<PendingCommand<{ projectId: string; requestId: string }> | null>(null);
  const [refusal, setRefusal] = useState<EvolveReviewRefusal__Output | null>(null);
  const start = () =>
    void action.run("review", async (signal) => {
      setRefusal(null);
      const command = pending ?? { input: { projectId, requestId: newRequestId() }, uncertain: false };
      setPending(command);
      try {
        const answer = await startEvolveReview(command.input, signal);
        setPending(null);
        if (answer.refusal !== "EVOLVE_REVIEW_REFUSAL_UNSPECIFIED") setRefusal(answer.refusal);
      } catch (reason) {
        setPending(retainedAfterFailure(command, reason));
        throw reason;
      }
      onAnswer();
    });
  return { start, starting: action.busy !== null, refusal, failure: action.failure };
}
