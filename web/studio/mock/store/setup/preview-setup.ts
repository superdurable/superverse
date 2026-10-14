// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's Setup · Preview state: the configuration the last applying start froze, the bounded
// record of starts and the Preview's failure, as PreviewFlow keeps them. Development only.
import type { ConfigurationItem } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { PhaseTiming } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import type { PreviewSnapshot } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { MOCK_STRIPE_CONNECTOR } from "@/mock/fixture/connector-manifest";
import { slackItems, type MockSlackValues } from "./slack-fixture";
import type { MockStripeValues } from "./stripe-fixture";

/** The native values of a scope's configuration that a Preview start freezes. */
export interface MockNativeValues {
  selectedConnectionId?: string;
  /** The `slack` Setup fixture's connection name and channels, when a fixture added it. */
  slack?: MockSlackValues;
  /** The `endpoint-bound` Setup fixture's connection name, tied to one address, when a fixture added it. */
  stripe?: MockStripeValues;
  useConfiguration: { model?: string };
  environment: Record<string, string>;
  configuredSecrets: string[];
}

const MOCK_STRIPE_NAME = {
  connectorId: MOCK_STRIPE_CONNECTOR.connectorId,
  connectionName: MOCK_STRIPE_CONNECTOR.connectionName,
};

/** The fixture connection the items name. */
export interface MockSetupConnection {
  connectorId: string;
  connectionName: string;
}

interface MockStart {
  commandId: string;
  actorAccountId: string;
  sourceCommit: string;
  configurationRevision: string;
  applied: boolean;
  admittedAt: string;
  endedAt: string;
  outcome: "READY" | "FAILED" | "REFUSED";
  failureCode: string;
  phaseTimings: PhaseTiming[];
}

/** What a Preview keeps of Setup across stops: the applied configuration, the newest starts and its failure. */
export interface MockPreviewSetup {
  applied: {
    revision: string;
    values: MockNativeValues;
    sourceCommit: string;
    appliedAt: string;
    appliedBy: string;
  } | null;
  starts: MockStart[];
  failure: Failure__Output | null;
}

/** PreviewFlow keeps the newest 50 starts. */
const START_LIMIT = 50;

/** The failures the mock's starts end with, each with its server-declared remedy and owner. */
const MOCK_PREVIEW_FAILURES = {
  PREVIEW_CONFIGURATION_CHANGED: {
    phase: "configuration",
    message: "Setup changed after you pressed Apply: review it and Apply again.",
    remedy: "FAILURE_REMEDY_APPLY_AGAIN",
  },
  PREVIEW_CONFIGURATION_INCOMPATIBLE: {
    phase: "configuration",
    message: "The applied configuration lacks what this version declares. Complete Setup, then Apply.",
    remedy: "FAILURE_REMEDY_OPEN_SETUP",
  },
  PREVIEW_CONFIGURATION_FAILED: {
    phase: "configuration",
    message: "The configuration is incomplete. Complete Setup · Preview, then Apply.",
    remedy: "FAILURE_REMEDY_OPEN_SETUP",
  },
} as const satisfies Record<string, Pick<Failure__Output, "phase" | "message" | "remedy">>;
export type MockPreviewFailureCode = keyof typeof MOCK_PREVIEW_FAILURES;

export function newPreviewSetup(): MockPreviewSetup {
  return { applied: null, starts: [], failure: null };
}

const copy = (values: MockNativeValues): MockNativeValues => JSON.parse(JSON.stringify(values)) as MockNativeValues;

/** An admitted start, before it ends. */
export function admittedStart(
  start: Pick<MockStart, "commandId" | "sourceCommit" | "configurationRevision" | "applied" | "admittedAt"> &
    Partial<Pick<MockStart, "actorAccountId">>,
): Omit<MockStart, "outcome" | "failureCode" | "endedAt"> {
  return { actorAccountId: "", phaseTimings: [], ...start };
}

/** Records an admitted start, dropping the oldest beyond the bound; a failure ends it with that failure. */
export function recordStart(
  setup: MockPreviewSetup,
  start: Omit<MockStart, "outcome" | "failureCode" | "endedAt">,
  failure: MockPreviewFailureCode | null,
  endedAt: string,
  failedOutcome: "FAILED" | "REFUSED" = "REFUSED",
) {
  setup.failure = failure && {
    code: failure,
    isRecoverable: true,
    owner: "FAILURE_OWNER_CONFIGURATION",
    ...MOCK_PREVIEW_FAILURES[failure],
  };
  const outcome: MockStart["outcome"] = failure ? failedOutcome : "READY";
  setup.starts = [...setup.starts, { ...start, endedAt, outcome, failureCode: failure ?? "" }].slice(-START_LIMIT);
}

/** Freezes `values` at `revision` as the applied configuration. */
export function applyValues(
  setup: MockPreviewSetup,
  values: MockNativeValues,
  revision: string,
  sourceCommit: string,
  at: string,
  by: string,
) {
  setup.applied = { revision, values: copy(values), sourceCommit, appliedAt: at, appliedBy: by };
}

/**
 * The Preview items of the head against the applied configuration, by item key: the connection
 * selection, the Step setting and each environment value, a secret by presence only.
 */
export function previewItems(
  connection: MockSetupConnection,
  head: MockNativeValues,
  applied: MockNativeValues | null,
): ConfigurationItem[] {
  const state = (now: unknown, before: unknown): ConfigurationItem["state"] =>
    applied === null
      ? "STATE_ADDED"
      : before === undefined
        ? now === undefined
          ? "STATE_UNCHANGED"
          : "STATE_ADDED"
        : now === undefined
          ? "STATE_REMOVED"
          : JSON.stringify(now) === JSON.stringify(before)
            ? "STATE_UNCHANGED"
            : "STATE_CHANGED";
  const names = [
    ...new Set([
      ...Object.keys(head.environment),
      ...head.configuredSecrets,
      ...Object.keys(applied?.environment ?? {}),
      ...(applied?.configuredSecrets ?? []),
    ]),
  ];
  const environmentValue = (values: MockNativeValues | null, name: string) =>
    values?.configuredSecrets.includes(name) ? "set" : values?.environment[name];
  return [
    {
      key: { kind: "KIND_CONNECTION", ...connection },
      state: state(head.selectedConnectionId, applied?.selectedConnectionId),
    },
    {
      key: {
        kind: "KIND_STEP_SETTING",
        ...connection,
        operationId: "generateText",
        flowType: "TextSummaryFlow",
        stepType: "GenerateSummary",
      },
      state: state(head.useConfiguration.model, applied?.useConfiguration.model),
    },
    ...names.map((name): ConfigurationItem => ({
      key: { kind: "KIND_ENVIRONMENT", environmentName: name },
      state: state(environmentValue(head, name), environmentValue(applied, name)),
    })),
    ...(head.slack ? slackItems(head.slack, applied ? (applied.slack ?? { channels: {} }) : null) : []),
    ...(head.stripe
      ? [
          {
            key: { kind: "KIND_CONNECTION" as const, ...MOCK_STRIPE_NAME },
            state: state(head.stripe.selectedConnectionId, applied?.stripe?.selectedConnectionId),
          },
        ]
      : []),
  ];
}

/** How many items differ from the applied configuration; none before the first Apply is counted. */
export function unappliedChangeCount(items: ConfigurationItem[], setup: MockPreviewSetup): number {
  if (!setup.applied) return 0;
  return items.filter((item) => item.state !== "STATE_UNCHANGED").length;
}

/** The PreviewSnapshot fields of Setup. */
export function previewSetupSnapshot(
  setup: MockPreviewSetup,
): Pick<PreviewSnapshot, "appliedConfiguration" | "recentStarts" | "failure"> {
  return {
    failure: setup.failure,
    appliedConfiguration: setup.applied && {
      configurationRevision: setup.applied.revision,
      sourceCommit: setup.applied.sourceCommit,
      appliedAt: isoToTimestamp(setup.applied.appliedAt),
      appliedByAccountId: setup.applied.appliedBy,
    },
    recentStarts: setup.starts.map((start) => ({
      ...start,
      admittedAt: isoToTimestamp(start.admittedAt),
      endedAt: isoToTimestamp(start.endedAt),
      outcome: `START_OUTCOME_${start.outcome}`,
    })),
  };
}
