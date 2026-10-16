// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit in-memory development fixtures. No provider calls or credential values; a connection name selects
// a Teamspace connection of the mock TeamSpaceConnectionService by its ID.
// Project configuration drafts, and the /dex Configuration routes that read and change them.
import { createHash } from "node:crypto";
import type { Struct } from "@/lib/generated/protobuf/google/protobuf/Struct";
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import type { ProjectConfigurationSnapshot } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import type { ProjectConfigurationValidation__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationValidation";
import { httpError, readJSONObject } from "@/lib/server/request-body";
import {
  MOCK_CONNECTOR,
  MOCK_MODELS,
  MOCK_STRIPE_CONNECTOR,
  mockConnectorManifest,
} from "@/mock/fixture/connector-manifest";
import { nowIso, randomHex } from "./clock";
import {
  admittedStart,
  applyValues,
  newPreviewSetup,
  recordStart,
  previewItems,
  unappliedChangeCount,
  type MockNativeValues,
  type MockPreviewSetup,
} from "./setup/preview-setup";
import {
  liveItems,
  markOwn,
  newLiveSetup,
  resolveLayer,
  writeMockItem,
  type MockDeclared,
  type MockItemBody,
  type MockLiveSetup,
} from "./setup/next-publish";
import { slackGaps, slackSessionRoute } from "./setup/slack-fixture";
import { stripeGaps, stripeSession } from "./setup/stripe-fixture";
import { CSRF, environmentFields, environmentSnapshot, fixtureConnections } from "./setup/native-views";
import type { MockStore } from "./store";

export type MockConfigurationScope = "preview" | "live";

export interface MockAppManifest {
  sourceCommit: string | null;
  revision: string;
  environment: Struct;
}

/** The native controls' values; the fixture connection name's selected Teamspace connection holds the credential. */
export interface MockNativeConfiguration extends MockNativeValues {
  configuration: { model?: string };
}

/**
 * The Project's Preview, Live's next-publish layer (a Live draft without a release) or one Live commit's
 * frozen copy; the GetProjectConfiguration fields plus its native values.
 */
export interface MockConfigurationDraft {
  projectId: string;
  scope: MockConfigurationScope;
  environment: MockConfigurationScope;
  releaseId: string;
  sourceCommit: string | null;
  revision: string;
  appManifest: MockAppManifest;
  native: MockNativeConfiguration;
  validation?: ProjectConfigurationValidation__Output;
  /** What the Preview applied and its starts; unused by a Live draft. */
  setup: MockPreviewSetup;
  /** The layer's own items, or what a copy was built from; unused by the Preview's. */
  live: MockLiveSetup;
}

interface NativeConfigurationBody extends MockItemBody {
  values?: Record<string, string>;
  secrets?: Record<string, string>;
  remove?: string[];
  connectorId?: string;
  connectionName?: string;
}

const { connectorId: connectionID, connectionName } = MOCK_CONNECTOR;

export function mockConfigurationDraft(
  store: MockStore,
  projectId: string,
  scope: MockConfigurationScope,
  releaseId = "",
): MockConfigurationDraft {
  const key = `${projectId}:${scope}:${scope === "preview" ? "" : releaseId}`;
  let draft = store.configurations.get(key);
  if (!draft) {
    // Live's next-publish layer is checked against the Preview's source, a copy against its Release's.
    const sourceCommit =
      scope === "preview" || !releaseId ? (store.sessions.get(projectId)?.headCommitSha ?? "") : releaseId;
    draft = {
      projectId,
      scope,
      environment: scope,
      releaseId,
      sourceCommit,
      revision: "1",
      appManifest: { sourceCommit, revision: "1", environment: {} },
      native: {
        configuration: { model: "gpt-4.1-mini" },
        useConfiguration: { model: "gpt-4.1-mini" },
        environment: { APP_LABEL: "Studio fixture", MOCK_BUILD_OUTCOME: "ready" },
        configuredSecrets: [],
      },
      setup: newPreviewSetup(),
      live: newLiveSetup(),
    };
    store.configurations.set(key, draft);
  }
  return draft;
}

/**
 * What a draft lacks for its source: until a Teamspace connection is selected, the fixture connection,
 * so Setup, the Preview panel and a Preview notice show a gap, then the fixtures' own gaps; on Live a
 * connection tied to one address says it never follows. Go reports the real gaps.
 */
function mockRequirementGaps(
  draft: Pick<MockConfigurationDraft, "native">,
  live = false,
): ConfigurationRequirementGap__Output[] {
  const fixtures = [
    ...(draft.native.slack ? slackGaps(draft.native.slack) : []),
    ...(draft.native.stripe ? stripeGaps(draft.native.stripe, live) : []),
  ];
  if (draft.native.selectedConnectionId) return fixtures;
  return [
    ...fixtures,
    {
      kind: "KIND_CONNECTION_REQUIRED",
      connectionName,
      connectorId: connectionID,
      triggerName: "",
      bindingName: "",
      environmentName: "",
      unitLabel: "",
      flowName: "",
      stepName: "",
      operationId: "",
      followRefusal: "CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED",
    },
  ];
}

/**
 * What a Preview start lacks: an applying start (`applying`, or a first start) checks the draft, any
 * other start the configuration the Preview applied.
 */
export function mockStartGaps(draft: MockConfigurationDraft, applying: boolean): ConfigurationRequirementGap__Output[] {
  const applied = draft.setup.applied;
  return mockRequirementGaps(applying || !applied ? draft : { native: { ...draft.native, ...applied.values } });
}

/**
 * Records the Preview start of a pushed commit, which applies nothing: it serves the applied configuration,
 * or applies the draft on a first start; refused, it lacks what the applied configuration does not hold.
 */
export function recordPushedStart(draft: MockConfigurationDraft, commit: string, served: boolean) {
  const applied = draft.setup.applied;
  const start = admittedStart({
    commandId: `deploy-preview-${randomHex(6)}`,
    sourceCommit: commit,
    configurationRevision: applied?.revision ?? draft.revision,
    applied: !applied,
    admittedAt: nowIso(),
  });
  if (served && !applied) applyValues(draft.setup, draft.native, draft.revision, commit, nowIso(), "");
  recordStart(draft.setup, start, served ? null : "PREVIEW_CONFIGURATION_INCOMPATIBLE", nowIso());
}

/** Whether a draft validates, as GetProjectConfiguration decides on each read in Go: it has no gap. */
export function mockConfigurationReady(draft: Pick<MockConfigurationDraft, "native">): boolean {
  return mockRequirementGaps(draft).length === 0;
}

/**
 * The draft as GetProjectConfiguration reports it: READY when it has no gap, as in Go; a Preview draft
 * also with its items against the applied configuration and how many differ.
 */
export function mockConfigurationSnapshot(
  store: MockStore,
  draft: MockConfigurationDraft,
  nextPublish = false,
  source = "",
): ProjectConfigurationSnapshot {
  const layer = mockNextPublish(store, draft.projectId);
  const resolved = { native: mockNextPublishValues(store, draft.projectId, source) };
  const live = draft.scope === "live";
  const items = live
    ? liveItems(
        mockDeclared(store, draft.projectId, source),
        { values: layer.native, setup: layer.live },
        mockLiveValues(store, draft.projectId),
        mockPreviewProposals(store, draft.projectId),
      )
    : previewItems(MOCK_CONNECTOR, draft.native, draft.setup.applied?.values ?? null);
  const ready = mockConfigurationReady(live ? resolved : draft);
  return {
    projectId: draft.projectId,
    scope: live ? "PROJECT_SCOPE_LIVE" : "PROJECT_SCOPE_PREVIEW",
    status: ready ? "CONFIGURATION_STATUS_READY" : "CONFIGURATION_STATUS_NEEDS_REVIEW",
    revision: nextPublish ? layer.revision : draft.revision,
    releaseId: draft.releaseId,
    sourceCommit: draft.sourceCommit ?? "",
    validationErrors: ready ? [] : ["Review the current source configuration and credentials before deployment."],
    appManifest: { ...draft.appManifest, sourceCommit: draft.appManifest.sourceCommit ?? "" },
    validation: draft.validation ?? null,
    requirementGaps: mockRequirementGaps(live ? resolved : draft, live),
    items,
    unappliedChangeCount: unappliedChangeCount(items, draft.setup),
    isStale: live && !nextPublish && draft.live.copiedFrom !== mockCopyBasis(store, draft.projectId),
    nextPublishRevision: live ? layer.revision : "0",
    previewConfigurationRevision: live ? mockPreviewRevision(store, draft.projectId) : "0",
  };
}

/** Live's next-publish layer: a Live draft without a release. */
export function mockNextPublish(store: MockStore, projectId: string): MockConfigurationDraft {
  return mockConfigurationDraft(store, projectId, "live", "");
}

/**
 * What a source declares: the fixtures it enabled and the application's environment. The Release Live runs
 * declares what its frozen copy holds; any other source, what the Preview's draft enabled.
 */
function mockDeclared(store: MockStore, projectId: string, source = ""): MockDeclared {
  const live = source && store.liveDeployments.get(projectId)?.releaseId === source;
  const values = live ? mockLiveValues(store, projectId) : store.configurations.get(`${projectId}:preview:`)?.native;
  return { slack: Boolean(values?.slack), stripe: Boolean(values?.stripe), environment: environmentFields };
}

/**
 * The next publish's values, checked against `source` (a Release, or the Preview's source), resolved from
 * the layer, Preview's applied values and current Live.
 */
export function mockNextPublishValues(store: MockStore, projectId: string, source = ""): MockNativeConfiguration {
  const layer = mockNextPublish(store, projectId);
  const values = { values: layer.native, setup: layer.live };
  const live = mockLiveValues(store, projectId);
  return {
    ...layer.native,
    ...resolveLayer(mockDeclared(store, projectId, source), values, live, mockPreviewProposals(store, projectId)),
  };
}

/** The revision of the configuration Preview applied, "0" before the first Apply. */
function mockPreviewRevision(store: MockStore, projectId: string): string {
  return store.configurations.get(`${projectId}:preview:`)?.setup.applied?.revision ?? "0";
}

/** What a frozen copy built now is built from: the layer, Preview's applied version and current Live. */
export function mockCopyBasis(store: MockStore, projectId: string): string {
  const live = store.liveDeployments.get(projectId)?.buildRevisionId ?? "";
  return `${mockNextPublish(store, projectId).revision}:${mockPreviewRevision(store, projectId)}:${live}`;
}

/** The values current Live runs: its deployment's frozen configuration; null before the first publish. */
function mockLiveValues(store: MockStore, projectId: string): MockNativeValues | null {
  const live = store.liveDeployments.get(projectId);
  const release = live && store.releases.get(`${projectId}:${live.releaseId}`);
  return (
    release?.buildRevisions.find((build) => build.buildRevisionId === live?.buildRevisionId)?.frozenConfiguration ??
    null
  );
}

/** What Live proposes from Preview: the values its last Apply froze, else none. */
function mockPreviewProposals(store: MockStore, projectId: string): MockNativeValues | null {
  return store.configurations.get(`${projectId}:preview:`)?.setup.applied?.values ?? null;
}

/** A changed draft takes a new revision, and its validation receipt names the old one. */
export function invalidateConfigurationDraft(draft: MockConfigurationDraft) {
  draft.revision = String(Number(draft.revision) + 1);
  delete draft.validation;
}

export async function nativeConfigurationFixture(
  request: Request,
  store: MockStore,
  {
    projectId,
    preview = false,
    releaseId = "",
    nextPublish = false,
    path,
  }: { projectId: string; preview?: boolean; releaseId?: string; nextPublish?: boolean; path: string[] },
): Promise<object> {
  const scope = preview ? "preview" : "live";
  if (!store.projects.has(projectId) || ((preview || nextPublish) && !store.sessions.has(projectId)))
    throw httpError(404, "Mock resource not found");
  if (!preview && releaseId && !store.releases.has(`${projectId}:${releaseId}`))
    throw httpError(409, "Prepare this mock commit first");
  // Every Live write lands in the next-publish layer; a version's copy is only read.
  const draft = mockConfigurationDraft(store, projectId, scope, nextPublish ? "" : releaseId);
  const shown = nextPublish ? mockNextPublishValues(store, projectId, releaseId) : draft.native;
  const own = (keys: Parameters<typeof markOwn>[1], follow: boolean) =>
    nextPublish && markOwn(draft.live, keys, !follow);
  const key = path.join("/");
  const targetDigest = createHash("sha256").update(`${projectId}:${scope}:${releaseId}`).digest("hex");
  const nonce = `mock-${targetDigest.slice(0, 24)}`;
  const body = (await readJSONObject(request)) as NativeConfigurationBody;
  const writes = ["PUT", "DELETE"].includes(request.method);
  if (request.method !== "GET" && request.method !== "HEAD" && request.headers.get("x-dex-csrf-token") !== CSRF)
    throw httpError(403, "Invalid mock CSRF token");
  if (writes && request.headers.get("x-dex-configuration-revision") !== draft.revision)
    throw Object.assign(httpError(409, "Project configuration changed; reload before continuing"), {
      code: "PROJECT_CONFIGURATION_REVISION_CONFLICT",
    });
  const session = slackSessionRoute(key, request.method, body);
  if (session) return session;
  if (key === "api/v2/connector-connections" && ["GET", "HEAD"].includes(request.method))
    return fixtureConnections(draft.revision, mockConfigurationReady({ native: shown }), shown);
  if (key === "api/v2/application-environment") {
    if (request.method === "PUT") {
      const values = body.values ?? {};
      // Validate the complete ordinary patch before changing the fixture draft.
      for (const field of environmentFields.filter((field) => !field.secret)) {
        if (Object.hasOwn(values, field.name)) {
          const value = values[field.name];
          if (typeof value !== "string" || (field.enum.length && !field.enum.includes(value)))
            throw httpError(400, "Invalid fixture environment value");
        }
      }
      const follows = Array.isArray(body.followPreview) ? (body.followPreview as unknown[]) : [];
      if (
        follows.length &&
        (!nextPublish || environmentFields.some((field) => field.secret && follows.includes(field.name)))
      )
        throw Object.assign(httpError(409, "This setting cannot follow Preview"), {
          code: "CONFIGURATION_ITEM_CANNOT_FOLLOW",
        });
      const changed = environmentFields.filter(
        (field) =>
          Object.hasOwn(values, field.name) ||
          Object.hasOwn(body.secrets ?? {}, field.name) ||
          body.remove?.includes(field.name),
      );
      own(
        changed.map((field) => ({ kind: "KIND_ENVIRONMENT", environmentName: field.name })),
        false,
      );
      own(
        follows.map((name) => ({ kind: "KIND_ENVIRONMENT", environmentName: String(name) })),
        true,
      );
      for (const field of environmentFields) {
        if (body.remove?.includes(field.name)) {
          delete draft.native.environment[field.name];
          draft.native.configuredSecrets = draft.native.configuredSecrets.filter((name) => name !== field.name);
        } else if (field.secret && Object.hasOwn(body.secrets ?? {}, field.name))
          draft.native.configuredSecrets = [...new Set([...draft.native.configuredSecrets, field.name])];
        else if (!field.secret && Object.hasOwn(values, field.name)) {
          draft.native.environment[field.name] = values[field.name];
        }
      }
      invalidateConfigurationDraft(draft);
    }
    return environmentSnapshot(draft.revision, nextPublish ? shown : draft.native);
  }
  if (key === "api/v2/connector-native-sessions" && request.method === "POST") {
    if (body.connectorId === MOCK_STRIPE_CONNECTOR.connectorId) return stripeSession(nonce);
    if (body.connectorId !== connectionID || body.connectionName !== connectionName)
      throw httpError(404, "Fixture connection not found");
    return { connectorId: connectionID, connectionName, sessionNonce: nonce, manifest: mockConnectorManifest };
  }
  if (key === `api/v2/connector-ui-sessions/${nonce}/commands/listModels` && request.method === "POST")
    return MOCK_MODELS;
  if (key === "api/v2/connector-usages/release-unused" && request.method === "POST") return {};
  if (request.method === "PUT") {
    // Every Live write lands in the next-publish layer, whose items the Preview's source declares.
    const declared = nextPublish
      ? mockDeclared(store, projectId, releaseId)
      : { slack: Boolean(draft.native.slack), stripe: Boolean(draft.native.stripe), environment: environmentFields };
    const answer = writeMockItem(draft.native, nextPublish ? draft.live : null, declared, key, body);
    if (answer) {
      invalidateConfigurationDraft(draft);
      return { ...answer, configurationRevision: draft.revision };
    }
  }
  throw httpError(404, "Unsupported native configuration fixture route; OAuth and provider access are unavailable");
}
