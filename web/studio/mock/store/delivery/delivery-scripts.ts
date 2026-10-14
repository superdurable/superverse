// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock Agent's delivery feedback: script phrases that stand in for a failed Live deployment or
// release, or a failed Preview start. Development only; no Release, build or deployment runs.
import { startTurn } from "@/mock/store/agent-scripts";
import { appendMessage, bumpWaitingRound, mockNotification, type MockAgent } from "@/mock/store/agent-state";
import { nowIso, randomHex } from "@/mock/store/clock";
import { mockConfigurationDraft } from "@/mock/store/configuration";
import { admittedStart, recordStart } from "@/mock/store/setup/preview-setup";
import { recordAgentCommit } from "@/mock/store/publishing";
import { SCENARIO_MARKERS } from "@/mock/store/scenarios/catalog";
import { mockMessage, mockToolResult } from "@/mock/store/seed";
import { deployPushedCommit, type MockSession, type MockStore } from "@/mock/store/store";

/** Exact phrases (trim + lower-case) that drive the delivery scripts. */
const MOCK_DELIVERY_PHRASES = {
  FAIL_LIVE_DEPLOYMENT: "fail live deployment",
  FAIL_LIVE_BUILD: "fail live build",
  FAIL_PREVIEW_START: "fail preview start",
} as const;

type MockDeliveryPhrase = (typeof MOCK_DELIVERY_PHRASES)[keyof typeof MOCK_DELIVERY_PHRASES];

/**
 * Runs the delivery script of a message, which follows the user's message; null when the message is
 * no delivery phrase, which the other scripts then answer.
 */
export function sendDeliveryScript(store: MockStore, projectId: string, content: string): boolean | null {
  const key = content.replace(SCENARIO_MARKERS, "").trim().toLowerCase();
  const phrase = Object.values(MOCK_DELIVERY_PHRASES).find(
    (candidate): candidate is MockDeliveryPhrase => candidate === key,
  );
  const agent = store.agents.get(projectId);
  const session = store.sessions.get(projectId);
  if (phrase === undefined || !agent || !session?.headCommitSha) return phrase === undefined ? null : false;
  appendMessage(agent, mockMessage("ROLE_USER", content));
  switch (phrase) {
    case MOCK_DELIVERY_PHRASES.FAIL_LIVE_DEPLOYMENT:
      failLiveDeployment(store, session, agent, session.headCommitSha);
      break;
    case MOCK_DELIVERY_PHRASES.FAIL_LIVE_BUILD:
      failLiveBuild(agent, session.headCommitSha);
      break;
    case MOCK_DELIVERY_PHRASES.FAIL_PREVIEW_START:
      failPreviewStart(store, session);
      break;
  }
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  bumpWaitingRound(agent);
  return true;
}

/** One assistant message with a completed call of `toolName`. */
function toolStep(agent: MockAgent, text: string, toolName: string, target: string) {
  const callId = `call-${toolName}-${agent.nextSequence}`;
  appendMessage(agent, mockMessage("ROLE_ASSISTANT", text, { toolCalls: [{ id: callId, name: toolName, target }] }));
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName }));
}

/**
 * The Live deployment of the Session's HEAD fails in a phase the application owns, as in the
 * scenario `live-failure-fix`: its notification opens an automatic turn that reads the delivery
 * status, pushes the fix and asks the user to publish it, because Live is a manual publish.
 */
function failLiveDeployment(store: MockStore, session: MockSession, agent: MockAgent, commit: string) {
  const deploymentId = `deploy-${randomHex(8)}`;
  const summary = `The Live deployment of ${commit.slice(0, 12)} ended with DEPLOYMENT_FAILED.`;
  const notification = mockNotification({
    id: `deployment/${deploymentId}/g1/r3`,
    kind: "KIND_DELIVERY",
    source: "SOURCE_DEPLOYMENT",
    summary,
    outcome: "OUTCOME_FAILURE",
    owner: "OWNER_APPLICATION",
    code: "DEPLOYMENT_FAILED",
    phase: "VerifyReadiness",
    commit,
    resourceId: deploymentId,
    message: "The application exited during startup: WELCOME_TEXT is not set.",
  });
  appendMessage(agent, mockMessage("ROLE_NOTIFICATION", summary, { notification }));
  startTurn(agent);
  toolStep(
    agent,
    "The Live deployment of the pushed commit failed. Reading the delivery status.",
    "read_delivery_status",
    "",
  );
  toolStep(
    agent,
    "The application exits at startup because WELCOME_TEXT is not set in Live. Removing the requirement.",
    "edit",
    "cmd/server/main.go",
  );
  toolStep(agent, "Pushing the fix.", "commit_and_push", "Start without WELCOME_TEXT");
  recordAgentCommit(store, session, "Start without WELCOME_TEXT");
  appendMessage(
    agent,
    mockMessage(
      "ROLE_ASSISTANT",
      "The fix is pushed. Live keeps serving what it served before; publish this commit to Live to replace the failed deployment.",
    ),
  );
  deployPushedCommit(store, session, agent);
}

/** A Live build of the Session's HEAD ends without an outcome: a platform problem, which only notifies. */
function failLiveBuild(agent: MockAgent, commit: string) {
  const buildRevisionId = `build-${randomHex(8)}`;
  const summary = `The Live release of ${commit.slice(0, 12)} ended with BUILD_UNKNOWN.`;
  const notification = mockNotification({
    id: `release/${commit}/${buildRevisionId}/r4`,
    kind: "KIND_DELIVERY",
    source: "SOURCE_RELEASE",
    summary,
    outcome: "OUTCOME_NOTICE",
    owner: "OWNER_PLATFORM",
    code: "BUILD_UNKNOWN",
    phase: "AwaitReleaseBuildRevision",
    commit,
    resourceId: buildRevisionId,
    message: "The build provider did not report the build's outcome.",
  });
  appendMessage(agent, mockMessage("ROLE_NOTIFICATION", summary, { notification }));
}

/**
 * A Preview start of the Session's HEAD fails at its configuration, whose remedy is Open Setup: the
 * Preview tab shows it failed with the problem panel, and the Preview stops.
 */
function failPreviewStart(store: MockStore, session: MockSession) {
  const draft = mockConfigurationDraft(store, session.projectId, "preview");
  recordStart(
    draft.setup,
    admittedStart({
      commandId: `start-preview-${randomHex(6)}`,
      sourceCommit: session.headCommitSha ?? "",
      configurationRevision: draft.revision,
      applied: true,
      admittedAt: nowIso(),
    }),
    "PREVIEW_CONFIGURATION_FAILED",
    nowIso(),
    "FAILED",
  );
  session.preview = { ...session.preview, state: "STOPPED", url: null, revision: session.preview.revision + 1 };
}
