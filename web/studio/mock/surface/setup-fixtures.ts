// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Named Setup fixtures a mock page can ask for with `?setupFixture=`, so each Setup state can be seen
// without clicking through a publish first. Development only.
import { MOCK_CONNECTOR } from "@/mock/fixture/connector-manifest";
import { nowIso, randomHex } from "@/mock/store/clock";
import {
  invalidateConfigurationDraft,
  mockConfigurationDraft,
  mockNextPublish,
  mockNextPublishValues,
} from "@/mock/store/configuration";
import { buildMockRelease, prepareMockRelease } from "@/mock/store/publishing";
import { SEED_HEAD_COMMIT } from "@/mock/store/seed";
import { preparePreviewSource, type MockStore } from "@/mock/store/store";
import { markOwn } from "@/mock/store/setup/next-publish";
import { admittedStart, applyValues, recordStart, type MockPreviewSetup } from "@/mock/store/setup/preview-setup";

/** The TeamSpace connection the seed TeamSpace holds for the fixture connector. */
const SEED_CONNECTION = "c0000000seed";
/** The seed Project's older main commit, which the later publish puts on Live. */
const OLDER_COMMIT = "dddddddddddddddddddddddddddddddddddddddd";

/** A commit that is not on the seed Project's main branch, whose starts fill the Preview's record. */
const OTHER_COMMIT = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee";

/** Records a READY Preview start of `commit`, as a start that served it does. */
function readyStart(setup: MockPreviewSetup, commit: string, revision: string) {
  const at = nowIso();
  recordStart(
    setup,
    admittedStart({
      commandId: `start-${randomHex(8)}`,
      sourceCommit: commit,
      configurationRevision: revision,
      applied: false,
      admittedAt: at,
    }),
    null,
    at,
  );
}

/** Preview applied the seed connection and a label, and ran the seed head on them. */
function previewRan(store: MockStore, projectId: string) {
  const preview = mockConfigurationDraft(store, projectId, "preview");
  if (preview.setup.applied) return preview;
  preview.native.selectedConnectionId = SEED_CONNECTION;
  preview.native.environment.APP_LABEL = "Support desk";
  invalidateConfigurationDraft(preview);
  applyValues(preview.setup, preview.native, preview.revision, SEED_HEAD_COMMIT, nowIso(), "");
  readyStart(preview.setup, SEED_HEAD_COMMIT, preview.revision);
  return preview;
}

const uuid = () => `${randomHex(8)}-${randomHex(4)}-4${randomHex(3)}-8${randomHex(3)}-${randomHex(12)}`;

/**
 * A later publish: Live runs the older commit with a connection, a model and a setting the newer commit
 * no longer declares; Preview applied its own values; Live's next publish follows Preview except for
 * its own model, which waits, and drops the old setting.
 */
function laterPublish(store: MockStore, projectId: string) {
  if (store.liveDeployments.has(projectId)) return;
  previewRan(store, projectId);

  const older = prepareMockRelease(store, projectId, OLDER_COMMIT);
  const running = mockConfigurationDraft(store, projectId, "live", OLDER_COMMIT);
  running.native.selectedConnectionId = SEED_CONNECTION;
  running.native.useConfiguration = { model: "gpt-4.1-mini" };
  running.native.environment = { MOCK_BUILD_OUTCOME: "ready", LEGACY_GREETING: "Hello from V0" };
  invalidateConfigurationDraft(running);
  buildMockRelease(store, projectId, {
    requestId: `setup-fixture-${randomHex(6)}`,
    releaseId: OLDER_COMMIT,
    buildRevisionId: `build-${uuid()}`,
    configurationRevision: running.revision,
    expectedRevision: older.revision,
    autoDeploy: true,
    expectedLiveRevision: "0",
    previewSkipReason: "Seeded fixture: published before the Preview record began",
  });

  prepareMockRelease(store, projectId, SEED_HEAD_COMMIT);
  const next = mockNextPublish(store, projectId);
  next.native.useConfiguration = { model: "gpt-4.1" };
  markOwn(
    next.live,
    [
      {
        kind: "KIND_STEP_SETTING",
        connectorId: MOCK_CONNECTOR.connectorId,
        connectionName: MOCK_CONNECTOR.connectionName,
        operationId: "generateText",
        flowType: "TextSummaryFlow",
        stepType: "GenerateSummary",
      },
    ],
    true,
  );
  invalidateConfigurationDraft(next);
}

/**
 * A first publish: Live never ran, Preview applied its values and ran the seed head, so the head is
 * validated and every item of Live's next publish follows Preview.
 */
function firstPublish(store: MockStore, projectId: string) {
  previewRan(store, projectId);
}

/**
 * A start the record dropped: Preview ran the seed head, then fifty starts of another commit pushed it out
 * of the bounded record, so the head reads "not in the Preview record" and needs a reason to publish.
 */
function evictedStart(store: MockStore, projectId: string) {
  const preview = previewRan(store, projectId);
  if (preview.setup.starts.some((start) => start.sourceCommit === OTHER_COMMIT)) return;
  for (let index = 0; index < 50; index++) readyStart(preview.setup, OTHER_COMMIT, preview.revision);
}

/** A later publish whose build fails, so the wizard's progress shows the failure as a problem. */
function buildFailure(store: MockStore, projectId: string) {
  laterPublish(store, projectId);
  const next = mockNextPublish(store, projectId);
  if (next.native.environment.MOCK_BUILD_OUTCOME === "failed") return;
  next.native.environment.MOCK_BUILD_OUTCOME = "failed";
  markOwn(next.live, [{ kind: "KIND_ENVIRONMENT", environmentName: "MOCK_BUILD_OUTCOME" }], true);
  invalidateConfigurationDraft(next);
}

/** A Slack connection name with a start and a reply trigger, each needing a channel, on Preview. */
function slack(store: MockStore, projectId: string) {
  const preview = mockConfigurationDraft(store, projectId, "preview");
  if (preview.native.slack) return;
  preview.native.slack = { channels: {} };
  invalidateConfigurationDraft(preview);
}

/** The seed TeamSpace's Slack and Stripe connections, which the next-publish fixtures select on Preview. */
const SLACK_CONNECTION = "c000000slack";
const STRIPE_CONNECTION = "c00000stripe";

/** Prepares the Preview's source of the head once, so Setup opens without Build processes. */
function prepared(store: MockStore, projectId: string) {
  const session = store.sessions.get(projectId);
  if (session && session.preview.preparedSource?.sourceCommit !== session.headCommitSha)
    preparePreviewSource(store, projectId, `setup-fixture-${randomHex(6)}`);
}

/** Preview applies its draft as it stands and runs the seed head on it. */
function applyPreview(store: MockStore, projectId: string) {
  const preview = mockConfigurationDraft(store, projectId, "preview");
  invalidateConfigurationDraft(preview);
  applyValues(preview.setup, preview.native, preview.revision, SEED_HEAD_COMMIT, nowIso(), "");
  readyStart(preview.setup, SEED_HEAD_COMMIT, preview.revision);
}

/**
 * Live's next publish just after a publish: Preview applied the seed connection, a label and a Slack
 * connection name listening in #escalations, and Live runs the seed head with exactly the next publish, so
 * every item follows Preview, nothing waits and Slack's Next publish channel is Preview's too.
 */
function nextPublish(store: MockStore, projectId: string) {
  prepared(store, projectId);
  if (store.liveDeployments.has(projectId)) return;
  const preview = mockConfigurationDraft(store, projectId, "preview");
  preview.native.selectedConnectionId = SEED_CONNECTION;
  preview.native.environment.APP_LABEL = "Support desk";
  preview.native.slack = {
    selectedConnectionId: SLACK_CONNECTION,
    channels: { start: "C0ESCALATE", reply: "C0ESCALATE" },
  };
  applyPreview(store, projectId);
  const release = prepareMockRelease(store, projectId, SEED_HEAD_COMMIT);
  const copy = mockConfigurationDraft(store, projectId, "live", SEED_HEAD_COMMIT);
  copy.native = structuredClone(mockNextPublishValues(store, projectId));
  invalidateConfigurationDraft(copy);
  buildMockRelease(store, projectId, {
    requestId: `setup-fixture-${randomHex(6)}`,
    releaseId: SEED_HEAD_COMMIT,
    buildRevisionId: `build-${uuid()}`,
    configurationRevision: copy.revision,
    expectedRevision: release.revision,
    autoDeploy: true,
    expectedLiveRevision: "0",
    previewSkipReason: "",
  });
}

/**
 * Two settings wait for a publish: Preview applied a new label, which the next publish follows, and the
 * next publish has its own model.
 */
function waiting(store: MockStore, projectId: string) {
  nextPublish(store, projectId);
  const preview = mockConfigurationDraft(store, projectId, "preview");
  if (preview.native.environment.APP_LABEL === "Support desk v2") return;
  preview.native.environment.APP_LABEL = "Support desk v2";
  applyPreview(store, projectId);
  const next = mockNextPublish(store, projectId);
  next.native.useConfiguration = { model: "gpt-4.1" };
  markOwn(
    next.live,
    [
      {
        kind: "KIND_STEP_SETTING",
        connectorId: MOCK_CONNECTOR.connectorId,
        connectionName: MOCK_CONNECTOR.connectionName,
        operationId: "generateText",
        flowType: "TextSummaryFlow",
        stepType: "GenerateSummary",
      },
    ],
    true,
  );
  invalidateConfigurationDraft(next);
}

/**
 * Preview applied a Stripe connection name, tied to one address: the next publish cannot follow it and
 * needs a connection of its own.
 */
function endpointBound(store: MockStore, projectId: string) {
  nextPublish(store, projectId);
  const preview = mockConfigurationDraft(store, projectId, "preview");
  if (preview.native.stripe) return;
  preview.native.stripe = { selectedConnectionId: STRIPE_CONNECTION };
  applyPreview(store, projectId);
}

/** Waiting settings and a connection tied to one address, which the mock page draws in a narrow tool panel. */
function narrow(store: MockStore, projectId: string) {
  waiting(store, projectId);
  endpointBound(store, projectId);
}

/** The Setup fixtures by name. */
const SETUP_FIXTURES = {
  "first-publish": firstPublish,
  "evicted-start": evictedStart,
  "later-publish": laterPublish,
  "build-failure": buildFailure,
  slack,
  "next-publish": nextPublish,
  waiting,
  "endpoint-bound": endpointBound,
  narrow,
} as const satisfies Record<string, (store: MockStore, projectId: string) => void>;

/** The fixture that draws Setup's tool panel narrow, below the 780px at which its rows show chips. */
export const NARROW_SETUP_FIXTURE = "narrow";

/** Seeds the named Setup fixture into a Project once; an unknown name does nothing. */
export function seedSetupFixture(store: MockStore, projectId: string, name: string | undefined) {
  if (name && Object.hasOwn(SETUP_FIXTURES, name))
    SETUP_FIXTURES[name as keyof typeof SETUP_FIXTURES](store, projectId);
}
