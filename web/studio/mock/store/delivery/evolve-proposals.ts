// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's agent proposal, as EvolveService answers it: the example outreach application's newest review proposes
// "Follow up once instead of twice", with its backtest and document. Approve and Decline move it through its lifecycle
// on the mock clock as the agent and Publish would. Development only; every number is example data.
import type { EvolveProposal } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { EvolveProposalDecision } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalDecision";
import type { EvolveProposalRefusal } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalRefusal";
import type { EvolveProposalStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalStatus";
import { httpError } from "@/lib/server/request-body";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { nowIso, randomHex } from "@/mock/store/clock";
import { mockConfigurationDraft } from "@/mock/store/configuration";
import { DOCUMENT, exampleBacktest, SUMMARY } from "@/mock/store/delivery/evolve-proposal-example";
import { recordAgentCommit, type MockDeployment } from "@/mock/store/publishing";
import { listReleaseCandidates, mockCommitVersion } from "@/mock/store/resource-snapshots";
import { admittedStart, recordStart } from "@/mock/store/setup/preview-setup";
import type { MockStore } from "@/mock/store/store";

/** The example review whose proposal the mock offers, and how long before the mock clock's start it offered it. */
export const PROPOSAL_REVIEW = "r-3";
const HOUR = 3_600_000;
export const PROPOSAL_OFFERED_AGO = 28 * HOUR;
/** How long the mock agent takes to apply an approved proposal to Preview. */
const APPLY_MILLISECONDS = 12_000;
/** How long the mock agent takes to discard a declined proposal. */
const DISCARD_MILLISECONDS = 6_000;
const MAXIMUM_REASON = 500;
/** The commit the mock agent pushes the approved change as. */
const APPLIED_COMMIT = "6f1c9e2a7b4d8e0f3a5c1b9d7e2f4a6c8b0d1e3f";
const TITLE = "Follow up once instead of twice";

/** The proposal as the mock keeps it: its status a named value, its revisions decimal strings. */
type MockProposal = EvolveProposal & { status: EvolveProposalStatus__Output; revision: string; liveRevision: string };

interface MockProposalState {
  proposal: MockProposal;
  /** When the status last changed, on the mock clock. */
  changedMs: number;
  /** The Live generation when the agent applied it; a later Live deployment holds the change. */
  appliedGeneration: number;
  /** The commit version Live ran when the proposal went live; a later Live deployment of an older commit undoes it. */
  liveVersion: number;
  /** The revision the recorded decision was made on; the same decision on it repeated answers it. */
  decidedRevision: string;
}

// The page's fixture query and the BFF's mock backend are separate module instances; they share the state here.
const shared = globalThis as { superverseMockProposals?: Map<string, MockProposalState> };
const proposals = () => (shared.superverseMockProposals ??= new Map());

const nowMs = () => Date.parse(nowIso());
const at = (ms: number) => isoToTimestamp(new Date(ms).toISOString());

function liveOf(store: MockStore, projectId: string) {
  const live = store.liveDeployments.get(projectId);
  if (!live) throw httpError(404, "the Project has had no Live deployment, so nothing measures its goals yet");
  return live;
}

/** The example proposal offered at offeredMs by the review of the Live build goalsCommit. */
function offered(offeredMs: number, goalsCommit: string): MockProposalState {
  return {
    proposal: {
      reviewId: PROPOSAL_REVIEW,
      kind: "EVOLVE_PROPOSAL_KIND_CHANGE",
      undoes: "",
      title: TITLE,
      summary: SUMMARY,
      agentId: "agent-change-7",
      backtestJson: JSON.stringify(exampleBacktest(new Date(offeredMs - 20 * 60_000).toISOString(), goalsCommit)),
      status: "EVOLVE_PROPOSAL_STATUS_OFFERED",
      statusAt: at(offeredMs),
      revision: "1",
      offeredAt: at(offeredMs),
      notifyFailed: false,
      decision: null,
      appliedCommit: "",
      appliedAt: null,
      liveRevision: "0",
      liveAt: null,
      previousLiveReleaseId: "",
    },
    changedMs: offeredMs,
    appliedGeneration: 0,
    liveVersion: 0,
    decidedRevision: "",
  };
}

function move(state: MockProposalState, status: EvolveProposalStatus__Output, ms: number) {
  state.proposal.status = status;
  state.proposal.statusAt = at(ms);
  state.proposal.revision = String(Number(state.proposal.revision) + 1);
  state.changedMs = ms;
}

function stateOf(store: MockStore, projectId: string): MockProposalState {
  const live = liveOf(store, projectId);
  const all = proposals();
  let state = all.get(projectId);
  if (!state) all.set(projectId, (state = offered(nowMs() - PROPOSAL_OFFERED_AGO, live.releaseId)));
  advance(store, projectId, state, live);
  return state;
}

/**
 * The mock agent applies the change on Preview: it pushes it to main as the Session's head, a version ahead of Live
 * that Publish opens on, and Preview runs it, so the Preview record holds it.
 */
function pushApplied(store: MockStore, projectId: string) {
  const session = store.sessions.get(projectId);
  const pushed = store.mainCommits.some(
    (item) => item.projectId === projectId && item.sourceCommitSha === APPLIED_COMMIT,
  );
  if (!session || pushed) return;
  recordAgentCommit(store, session, TITLE, APPLIED_COMMIT);
  const draft = mockConfigurationDraft(store, projectId, "preview");
  const start = admittedStart({
    commandId: `start-preview-${randomHex(6)}`,
    sourceCommit: APPLIED_COMMIT,
    configurationRevision: draft.revision,
    applied: true,
    admittedAt: nowIso(),
  });
  recordStart(draft.setup, start, null, nowIso());
}

/** A fixture starts the proposal over without the agent's commit, unless Live runs it. */
function dropApplied(store: MockStore, projectId: string) {
  if (store.liveDeployments.get(projectId)?.releaseId === APPLIED_COMMIT) return;
  const index = store.mainCommits.findIndex(
    (item) => item.projectId === projectId && item.sourceCommitSha === APPLIED_COMMIT,
  );
  if (index < 0) return;
  store.mainCommits.splice(index, 1);
  const session = store.sessions.get(projectId);
  if (session?.headCommitSha !== APPLIED_COMMIT) return;
  session.headCommitSha = listReleaseCandidates(store, projectId)[0]?.sourceCommitSha ?? null;
  session.revision += 1;
}

/**
 * The release Live ran before `live`, as EvolveFlow records it for a proposal that goes live with it: the newest
 * deployment of an earlier generation that went Live, when it is another release; empty otherwise.
 */
function releaseBefore(store: MockStore, projectId: string, live: MockDeployment): string {
  const before = [...store.deployments.values()]
    .filter(
      (item) =>
        item.projectId === projectId &&
        (item.status === "READY" || item.status === "REMOVED") &&
        Number(item.generation) < Number(live.generation),
    )
    .sort((a, b) => Number(b.generation) - Number(a.generation))[0];
  return before && before.releaseId !== live.releaseId ? before.releaseId : "";
}

/** The version number of a commit of the Project's main; -1 when main has no such commit. */
function versionOf(store: MockStore, projectId: string, commit: string): number {
  return Number(mockCommitVersion(store, projectId, commit)?.number ?? -1);
}

/**
 * The mock agent applies an approved proposal, pushing its commit, and discards a declined one a few seconds later;
 * a later Live deployment of a version that holds the commit makes the proposal live, recording the release Live ran
 * before, and a later one of an older version (a rollback) undoes it.
 */
function advance(store: MockStore, projectId: string, state: MockProposalState, live: MockDeployment) {
  const elapsed = nowMs() - state.changedMs;
  const generation = Number(live.generation);
  const version = Number(live.version?.number ?? 0);
  switch (state.proposal.status) {
    case "EVOLVE_PROPOSAL_STATUS_APPROVED":
      if (elapsed < APPLY_MILLISECONDS) return;
      move(state, "EVOLVE_PROPOSAL_STATUS_APPLIED", state.changedMs + APPLY_MILLISECONDS);
      state.proposal.appliedCommit = APPLIED_COMMIT;
      state.proposal.appliedAt = state.proposal.statusAt;
      state.appliedGeneration = generation;
      pushApplied(store, projectId);
      return;
    case "EVOLVE_PROPOSAL_STATUS_DECLINED":
      if (elapsed >= DISCARD_MILLISECONDS)
        move(state, "EVOLVE_PROPOSAL_STATUS_DISCARDED", state.changedMs + DISCARD_MILLISECONDS);
      return;
    case "EVOLVE_PROPOSAL_STATUS_APPLIED":
      if (generation <= state.appliedGeneration) return;
      if (version < versionOf(store, projectId, state.proposal.appliedCommit ?? "")) return;
      move(state, "EVOLVE_PROPOSAL_STATUS_LIVE", nowMs());
      state.proposal.liveRevision = String(generation);
      state.proposal.previousLiveReleaseId = releaseBefore(store, projectId, live);
      state.proposal.liveAt = state.proposal.statusAt;
      state.liveVersion = version;
      return;
    case "EVOLVE_PROPOSAL_STATUS_LIVE":
      if (generation > Number(state.proposal.liveRevision) && version < state.liveVersion)
        move(state, "EVOLVE_PROPOSAL_STATUS_UNDONE", nowMs());
      return;
    case "EVOLVE_PROPOSAL_STATUS_UNSPECIFIED":
    case "EVOLVE_PROPOSAL_STATUS_OFFERED":
    case "EVOLVE_PROPOSAL_STATUS_DISCARDED":
    case "EVOLVE_PROPOSAL_STATUS_UNDONE":
    case "EVOLVE_PROPOSAL_STATUS_SUPERSEDED":
    case "EVOLVE_PROPOSAL_STATUS_STALE":
      // Offered waits for a person; the others end the lifecycle.
      return;
  }
}

/** GetEvolve's proposals: the example's one, newest first. */
export function mockProposals(store: MockStore, projectId: string): EvolveProposal[] {
  return [stateOf(store, projectId).proposal];
}

/** DecideEvolveProposal: a compare-and-set on the revision the page showed and status offered, as the server does. */
export function decideMockProposal(
  store: MockStore,
  projectId: string,
  request: { reviewId: string; revision: string; decision: EvolveProposalDecision; reason: string },
  actorAccountId: string,
): { refusal: EvolveProposalRefusal; proposal: EvolveProposal | null } {
  const { reviewId, revision, decision, reason } = request;
  if (!/^r-[1-9]\d*$/.test(reviewId) || !/^[1-9]\d*$/.test(revision) || [...reason].length > MAXIMUM_REASON)
    throw httpError(400, "Name a review, the revision the page showed and a reason of at most 500 characters");
  if (decision !== "EVOLVE_PROPOSAL_DECISION_APPROVE" && decision !== "EVOLVE_PROPOSAL_DECISION_DECLINE")
    throw httpError(400, "Approve or decline");
  const state = stateOf(store, projectId);
  const { proposal } = state;
  if (reviewId !== proposal.reviewId) return { refusal: "EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL", proposal: null };
  const recorded = proposal.decision;
  const repeated =
    state.decidedRevision === revision &&
    recorded?.decision === decision &&
    recorded.actorAccountId === actorAccountId &&
    recorded.reason === reason;
  if (repeated) return { refusal: "EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED", proposal };
  if (proposal.status !== "EVOLVE_PROPOSAL_STATUS_OFFERED" || proposal.revision !== revision)
    return { refusal: "EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL", proposal };
  const now = nowMs();
  const approve = decision === "EVOLVE_PROPOSAL_DECISION_APPROVE";
  move(state, approve ? "EVOLVE_PROPOSAL_STATUS_APPROVED" : "EVOLVE_PROPOSAL_STATUS_DECLINED", now);
  proposal.decision = { decision, actorAccountId, decidedAt: at(now), reason };
  state.decidedRevision = revision;
  return { refusal: "EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED", proposal };
}

/** GetEvolveProposal: the document of the example's review; NOT_FOUND for any other. */
export function getMockProposal(store: MockStore, projectId: string, reviewId: string): { markdown: string } {
  if (reviewId !== stateOf(store, projectId).proposal.reviewId) throw httpError(404, "the review has no proposal");
  return { markdown: DOCUMENT };
}

/**
 * The fixtures `?proposalFixture=<name>` seeds: the statuses each moves through, as the lifecycle reaches them from the
 * offered example, and whether the agent was not told of its decision.
 */
const FIXTURES = {
  offered: { steps: [] },
  approved: { steps: ["EVOLVE_PROPOSAL_STATUS_APPROVED"] },
  "notify-failed": { steps: ["EVOLVE_PROPOSAL_STATUS_APPROVED"], notifyFailed: true },
  applied: { steps: ["EVOLVE_PROPOSAL_STATUS_APPROVED", "EVOLVE_PROPOSAL_STATUS_APPLIED"] },
  live: {
    steps: ["EVOLVE_PROPOSAL_STATUS_APPROVED", "EVOLVE_PROPOSAL_STATUS_APPLIED", "EVOLVE_PROPOSAL_STATUS_LIVE"],
  },
  undone: {
    steps: [
      "EVOLVE_PROPOSAL_STATUS_APPROVED",
      "EVOLVE_PROPOSAL_STATUS_APPLIED",
      "EVOLVE_PROPOSAL_STATUS_LIVE",
      "EVOLVE_PROPOSAL_STATUS_UNDONE",
    ],
  },
  declined: { steps: ["EVOLVE_PROPOSAL_STATUS_DECLINED"] },
  "notify-failed-declined": { steps: ["EVOLVE_PROPOSAL_STATUS_DECLINED"], notifyFailed: true },
  discarded: { steps: ["EVOLVE_PROPOSAL_STATUS_DECLINED", "EVOLVE_PROPOSAL_STATUS_DISCARDED"] },
  superseded: { steps: ["EVOLVE_PROPOSAL_STATUS_SUPERSEDED"] },
  stale: { steps: ["EVOLVE_PROPOSAL_STATUS_APPROVED", "EVOLVE_PROPOSAL_STATUS_STALE"] },
} as const satisfies Record<string, { steps: readonly EvolveProposalStatus__Output[]; notifyFailed?: boolean }>;

/**
 * `?proposalFixture=<name>` shows the example proposal in that state on a Live Project's Goals tab, with its history
 * an hour apart; `notify-failed` and `notify-failed-declined` are an approval and a decline the agent was not told of.
 * Any other name leaves it as it is.
 */
export function seedProposalFixture(store: MockStore, projectId: string, name: string | undefined) {
  if (name === undefined || !Object.hasOwn(FIXTURES, name) || !store.liveDeployments.has(projectId)) return;
  const live = liveOf(store, projectId);
  dropApplied(store, projectId);
  const fixture: { steps: readonly EvolveProposalStatus__Output[]; notifyFailed?: boolean } =
    FIXTURES[name as keyof typeof FIXTURES];
  const { steps } = fixture;
  const start = nowMs() - (steps.length + 1) * HOUR;
  const state = offered(start, live.releaseId);
  steps.forEach((status, index) => {
    const ms = start + (index + 1) * HOUR;
    move(state, status, ms);
    if (status === "EVOLVE_PROPOSAL_STATUS_APPROVED" || status === "EVOLVE_PROPOSAL_STATUS_DECLINED")
      state.proposal.decision = {
        decision:
          status === "EVOLVE_PROPOSAL_STATUS_APPROVED"
            ? "EVOLVE_PROPOSAL_DECISION_APPROVE"
            : "EVOLVE_PROPOSAL_DECISION_DECLINE",
        actorAccountId: "mock-account",
        decidedAt: at(ms),
        reason:
          status === "EVOLVE_PROPOSAL_STATUS_DECLINED" ? "We want the second follow-up for strong-fit prospects" : "",
      };
    if (status === "EVOLVE_PROPOSAL_STATUS_APPLIED")
      Object.assign(state.proposal, { appliedCommit: APPLIED_COMMIT, appliedAt: at(ms) });
    if (status === "EVOLVE_PROPOSAL_STATUS_LIVE")
      Object.assign(state.proposal, { liveRevision: live.generation, liveAt: at(ms) });
  });
  // On Preview, the change is the Session's head, a version ahead of Live; a live one is the release Live runs.
  if (state.proposal.status === "EVOLVE_PROPOSAL_STATUS_APPLIED") pushApplied(store, projectId);
  if (state.proposal.status === "EVOLVE_PROPOSAL_STATUS_LIVE")
    Object.assign(state.proposal, {
      appliedCommit: live.releaseId,
      previousLiveReleaseId: releaseBefore(store, projectId, live),
    });
  // A seeded approval or decline stays put, as if the agent was never told; Publish and a rollback still move an
  // applied or live one.
  state.changedMs = Number.MAX_SAFE_INTEGER;
  state.appliedGeneration = Number(live.generation);
  state.liveVersion = Number(live.version?.number ?? 0);
  state.proposal.notifyFailed = fixture.notifyFailed ?? false;
  proposals().set(projectId, state);
}
