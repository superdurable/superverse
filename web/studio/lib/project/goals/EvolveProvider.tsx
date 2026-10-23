// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { offeredProposal } from "./proposals";
import { useGoalMeasurement, useProposalDecision, type GoalMeasurement } from "./use-goal-measurement";

interface EvolveContextValue {
  measurement: GoalMeasurement;
  /** Reads the measurement again. */
  reload: () => void;
  /** Approve and Decline, shared by every card of the page, so a decision shows in all of them at once. */
  decision: ReturnType<typeof useProposalDecision>;
  /** The person told the agent in the chat of the decision on a review that it did not get; it is read until done. */
  toldAgent: (reviewId: string) => void;
}

const EvolveContext = createContext<EvolveContextValue | null>(null);

/**
 * The Builder's one read of the Project's goal measurement (`GetEvolve`) and its proposal decisions, which the Goals
 * tab, the Goals tab's mark and the chat's proposal card share: a decision reads it again for all of them, so they
 * never disagree. A change of Live's revision reads it again until the measurement has followed it, since publishing
 * and rolling back move proposals.
 */
export function EvolveProvider({ projectId, children }: { projectId: string; children: ReactNode }) {
  const live = useProjectResource()?.snapshot?.live;
  const [told, setTold] = useState<ReadonlySet<string>>(() => new Set());
  const { measurement, reload } = useGoalMeasurement(projectId, live?.revision ?? "", told);
  const decision = useProposalDecision(projectId, reload);
  // Each telling reads again, so its following starts over.
  const toldAgent = useCallback((reviewId: string) => setTold((current) => new Set(current).add(reviewId)), []);
  const value = useMemo(
    () => ({ measurement, reload, decision, toldAgent }),
    [measurement, reload, decision, toldAgent],
  );
  return <EvolveContext.Provider value={value}>{children}</EvolveContext.Provider>;
}

/** The Builder's goal measurement and proposal decisions; only inside `EvolveProvider`. */
export function useEvolve(): EvolveContextValue {
  const value = useContext(EvolveContext);
  if (!value) throw new Error("useEvolve needs the Builder's EvolveProvider");
  return value;
}

/** The proposal that waits for a person's decision, from the Builder's measurement; null when none does. */
export function useOfferedProposal() {
  const { measurement } = useEvolve();
  return offeredProposal(measurement.state === "ready" ? measurement.evolve : null);
}
