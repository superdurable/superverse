// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { decideEvolveProposal, getEvolve, startEvolveReview } from "@/lib/api/evolve";
import { newRequestId, retainedAfterFailure, type PendingCommand } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { EvolveReviewRefusal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewRefusal";
import { delay } from "@/lib/shared/async";
import { agentAtWork, behindLive, type ProposalChoice, type ProposalMark } from "./proposals";

/**
 * While a review runs, the agent carries out a decision or the measurement follows a new Live, the page reads the
 * measurement again this often, a failed read included, for at most REFRESH_LIMIT_MS from the read that found it so:
 * an export of a usual Project takes minutes, and a longer one stays shown as running until the page is read again.
 */
const REFRESH_MILLISECONDS = 5_000;
const REFRESH_LIMIT_MILLISECONDS = 15 * 60_000;
/** The least time between the reads a return to the page makes (focus, or the tab shown again). */
const RETURN_MILLISECONDS = 30_000;

/** A Project's goal measurement as the page has read it; absent before its first Live deployment. */
export type GoalMeasurement =
  | { state: "reading" }
  | { state: "failed"; error: unknown }
  | { state: "absent" }
  | { state: "ready"; evolve: Evolve__Output };

const READING: GoalMeasurement = { state: "reading" };

/**
 * The Project's goal measurement from `GetEvolve`, read again until it settles while a review runs, the agent carries
 * out a decision (one it was not told of once the person told it, `told`) or the measurement follows a new `liveKey`
 * (Live's revision, whose change moves proposals); and on `reload`, a new `liveKey` or a return to the page. A read
 * that fails after one succeeded keeps what the page shows.
 */
export function useGoalMeasurement(projectId: string, liveKey: string, told: ReadonlySet<string>) {
  const [read, setRead] = useState<{ projectId: string; measurement: GoalMeasurement } | null>(null);
  const [reads, setReads] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const { signal } = controller;
    void (async () => {
      const deadline = Date.now() + REFRESH_LIMIT_MILLISECONDS;
      // Whether the last read found the measurement about to change, so the next one follows, even after a failure.
      let changing = false;
      for (;;) {
        try {
          const evolve = await getEvolve({ projectId }, signal);
          if (signal.aborted) return;
          setRead({ projectId, measurement: evolve ? { state: "ready", evolve } : { state: "absent" } });
          changing = evolve !== null && (agentAtWork(evolve, told) || behindLive(evolve, liveKey));
        } catch (error) {
          if (signal.aborted) return;
          setRead((current) =>
            current?.projectId === projectId && current.measurement.state === "ready"
              ? current
              : { projectId, measurement: { state: "failed", error } },
          );
        }
        if (!changing || Date.now() >= deadline) return;
        await delay(REFRESH_MILLISECONDS, signal);
        if (signal.aborted) return;
      }
    })();
    return () => controller.abort();
  }, [projectId, liveKey, reads, told]);
  const reload = useCallback(() => setReads((count) => count + 1), []);
  // A scheduled review may have proposed something while the page was away.
  useEffect(() => {
    let last = Date.now();
    const back = () => {
      if (document.visibilityState !== "visible" || Date.now() - last < RETURN_MILLISECONDS) return;
      last = Date.now();
      reload();
    };
    window.addEventListener("focus", back);
    document.addEventListener("visibilitychange", back);
    return () => {
      window.removeEventListener("focus", back);
      document.removeEventListener("visibilitychange", back);
    };
  }, [reload]);
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

const DECISIONS = {
  approve: "EVOLVE_PROPOSAL_DECISION_APPROVE",
  decline: "EVOLVE_PROPOSAL_DECISION_DECLINE",
} as const satisfies Record<ProposalChoice, string>;

/**
 * Approve or Decline of an offered proposal at the revision the page showed, one at a time. The server answers a
 * repeated decision with the one it recorded, so a retry after an unknown outcome sends it again. A STALE_PROPOSAL
 * refusal is an answer, kept as `stale` with the revision the server found the proposal at, until the next decision or
 * `dismissStale`; `target` names the proposal and revision of the last decision, whose `failure` holds as long as the
 * proposal stays there (`stillMarked`), and the card that sent it (`source`), which alone announces how it went.
 * Either answer reads the measurement again through `onAnswer`. `decide` resolves to whether the server answered.
 */
export function useProposalDecision(projectId: string, onAnswer: () => void) {
  const action = useSingleFlight<ProposalChoice>();
  const [target, setTarget] = useState<(ProposalMark & { source: string }) | null>(null);
  const [stale, setStale] = useState<ProposalMark | null>(null);
  const { run } = action;
  const decide = useCallback(
    async (proposal: EvolveProposal__Output, choice: ProposalChoice, reason: string, source: string) => {
      let answered = false;
      await run(choice, async (signal) => {
        const { reviewId, revision } = proposal;
        setTarget({ reviewId, revision, source });
        setStale(null);
        const answer = await decideEvolveProposal(
          { projectId, reviewId, revision, decision: DECISIONS[choice], reason },
          signal,
        );
        if (answer.refusal === "EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL")
          setStale({ reviewId, revision: answer.proposal?.revision ?? revision });
        answered = true;
        onAnswer();
      });
      return answered;
    },
    [onAnswer, projectId, run],
  );
  const dismissStale = useCallback(() => setStale(null), []);
  const { busy, failure } = action;
  return useMemo(
    () => ({
      decide,
      deciding: busy,
      target,
      stale,
      dismissStale,
      failure: failure && { choice: failure.label, reason: failure.reason },
    }),
    [busy, decide, dismissStale, failure, stale, target],
  );
}
