// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's only clock and ID source, and the phase timings laid out on it, with the phases a build and
// a deployment record; development only.
//
// With SUPERVERSE_STUDIO_MOCK_EPOCH unset the clock is the real time and IDs are crypto-random. With
// an ISO time the clock starts at that epoch on its first reading and then advances in real time in
// whole seconds, and IDs hash the epoch with a counter. Two mock servers given the same epoch then
// answer the same request sequence with the same bytes, as long as each step falls in the same second
// on both. The clock is not frozen, because the Sandbox deadline that the Agent's work and "Keep
// running" move must move forward as time passes.
import { createHash, randomBytes } from "node:crypto";

interface MockClock {
  /** The epoch in milliseconds, or null for the real time. */
  epochMs: number | null;
  /** The real time of the first reading. */
  startedAtMs: number;
  /** IDs generated so far; the counter an epoch's IDs hash. */
  issuedIds: number;
}

// Next loads route handlers and the workspace layout as separate module instances; they share one
// clock and one ID counter through globalThis, as the logins do.
const shared = globalThis as { superverseMockClock?: MockClock };

function clock(): MockClock {
  return (shared.superverseMockClock ??= startClock(process.env.SUPERVERSE_STUDIO_MOCK_EPOCH || null));
}

function startClock(epoch: string | null): MockClock {
  const epochMs = epoch === null ? null : Date.parse(epoch);
  if (Number.isNaN(epochMs)) throw new Error(`SUPERVERSE_STUDIO_MOCK_EPOCH is not an ISO time: ${epoch}`);
  return { epochMs, startedAtMs: Date.now(), issuedIds: 0 };
}

// Milliseconds since the Unix epoch on the mock's clock.
function nowMs(): number {
  const { epochMs, startedAtMs } = clock();
  if (epochMs === null) return Date.now();
  return epochMs + Math.floor((Date.now() - startedAtMs) / 1_000) * 1_000;
}

/** The mock time of the clock's first reading: the epoch, or the real time then. */
export function clockStartMs(): number {
  const { epochMs, startedAtMs } = clock();
  return epochMs ?? startedAtMs;
}

/** The mock's current time, or the time `offsetMs` away from it, as an ISO string. */
export function nowIso(offsetMs = 0): string {
  return new Date(nowMs() + offsetMs).toISOString();
}

/** `length` lowercase hex digits for a generated ID. */
export function randomHex(length: number): string {
  const state = clock();
  if (state.epochMs === null) return randomBytes(length).toString("hex").slice(0, length);
  let hex = "";
  while (hex.length < length) hex += createHash("sha256").update(`${state.epochMs}#${state.issuedIds++}`).digest("hex");
  return hex.slice(0, length);
}

/** A measured phase as the store keeps it; a zero duration marks it in progress. */
export interface MockPhaseTiming {
  phase: string;
  startedAt: string;
  durationMs: string;
}

/** Phases given as [phase, start offset, duration] in milliseconds and laid out so that the last one ends now. */
export function mockPhases(...phases: [phase: string, offsetMs: number, durationMs: number][]): MockPhaseTiming[] {
  const startMs = nowMs() - Math.max(...phases.map(([, offset, duration]) => offset + duration));
  return phases.map(([phase, offset, duration]) => ({
    phase,
    startedAt: new Date(startMs + offset).toISOString(),
    durationMs: String(duration),
  }));
}

/** A phase as [phase, start offset, duration] in milliseconds. */
type PhaseLayout = [phase: string, offsetMs: number, durationMs: number];

/** The phases of a build that published its artifacts, as the Release Flow and the builder record them. */
export const BUILD_PHASES: readonly PhaseLayout[] = [
  ["CONFIGURATION_FREEZE", 0, 400],
  ["QUEUE", 400, 2_100],
  ["HOST_PREPARATION", 2_500, 18_700],
  ["DEPENDENCY_PREPARATION", 21_200, 31_000],
  ["ANALYSIS", 52_200, 6_200],
  ["COMPILATION", 58_400, 42_000],
  ["IMAGE_PUBLICATION", 100_400, 12_000],
  ["ARTIFACT_PUBLICATION", 112_400, 1_300],
];

/** The phases of a first deployment, whose two Live preparation branches run in parallel with their join. */
export const DEPLOYMENT_PHASES: readonly PhaseLayout[] = [
  ["AdmitDeployment", 0, 200],
  ["AcquireProjectDeployment", 200, 300],
  ["VerifyReleaseAndConfiguration", 500, 1_100],
  ["PublishDefinitions", 1_600, 800],
  ["PrepareLiveExecution", 2_400, 41_600],
  ["PrepareLiveTemporal", 2_400, 38_200],
  ["PrepareLiveAccess", 2_400, 41_000],
  ["StartLiveDex", 44_000, 9_500],
  ["ApplyApplication", 53_500, 12_300],
  ["VerifyReadiness", 65_800, 6_400],
  ["CommitCurrentLive", 72_200, 400],
  ["ReleaseProjectDeployment", 72_600, 300],
];

/** The phases of a build that failed while compiling, which ran shorter. */
export const failedBuildPhases = () => mockPhases(...BUILD_PHASES.slice(0, 5), ["COMPILATION", 58_400, 31_500]);

/** The phases of a build that published its artifacts, ending now. */
export const readyBuildPhases = () => mockPhases(...BUILD_PHASES);

/** The phases of a deployment that went Live, ending now. */
export const deploymentPhases = () => mockPhases(...DEPLOYMENT_PHASES);
