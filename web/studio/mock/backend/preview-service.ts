// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock PreviewService over the in-memory store; development only.
import { httpError } from "@/lib/server/request-body";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { nowIso } from "@/mock/store/clock";
import { mockConfigurationDraft, mockStartGaps } from "@/mock/store/configuration";
import { applyValues, recordStart, type MockPreviewFailureCode } from "@/mock/store/setup/preview-setup";
import { mockFlowDefinitions, mockPreviewSnapshot } from "@/mock/store/resource-snapshots";
import { preparePreviewSource, startPreview, stopPreview, type MockSession, type MockStore } from "@/mock/store/store";
import type { MockService } from "./mock-backend";
import { projectSession } from "./session-service";

interface PreviewFence {
  projectId: string;
  expectedRevision: string;
  generation: string;
  workspaceRequestId: string;
}

/**
 * Like Go, a command names the Preview it observed with the Sandbox instance the Preview acquired,
 * which every mock Preview has: an observation without a generation is invalid; a command without
 * an instance, another instance or a different nonzero Preview revision is stale.
 */
function fenced(store: MockStore, fence: PreviewFence): MockSession {
  const session = projectSession(store, fence.projectId);
  if (fence.generation === "0") throw httpError(400, "The observed Preview instance is required");
  if (
    fence.workspaceRequestId !== session.workspace.workspaceRequestId ||
    fence.generation !== String(session.workspace.generation) ||
    (fence.expectedRevision !== "0" && fence.expectedRevision !== String(session.preview.revision))
  )
    throw httpError(409, "Preview changed; reload");
  return session;
}

function preview(store: MockStore, projectId: string) {
  return { preview: mockPreviewSnapshot(store, projectId) };
}

export const previewService: MockService<"PreviewService"> = {
  GetPreview: ({ projectId }, { store }) => {
    projectSession(store, projectId);
    return preview(store, projectId);
  },
  // Like PreviewFlow, an applying start (Apply, or the first start) freezes exactly the revision the
  // person saw; any other start revalidates the applied configuration. A refusal is recorded, not thrown.
  StartPreview: (request, { store }) => {
    const session = fenced(store, request);
    const head = session.headCommitSha ?? "";
    if (session.preview.preparedSource?.sourceCommit !== head) throw httpError(409, "Prepare source first");
    const draft = mockConfigurationDraft(store, session.projectId, "preview");
    const { setup } = draft;
    const applying = request.applyConfiguration || setup.applied === null;
    const start = {
      commandId: request.requestId,
      actorAccountId: request.caller?.accountId ?? "",
      sourceCommit: head,
      configurationRevision: request.applyConfiguration
        ? request.configurationRevision
        : (setup.applied?.revision ?? draft.revision),
      applied: applying,
      admittedAt: nowIso(),
      phaseTimings: [],
    };
    let failure: MockPreviewFailureCode | null = null;
    if (request.applyConfiguration && request.configurationRevision !== draft.revision)
      failure = "PREVIEW_CONFIGURATION_CHANGED";
    else if (mockStartGaps(draft, applying).length > 0) failure = "PREVIEW_CONFIGURATION_FAILED";
    if (failure) {
      recordStart(setup, start, failure, nowIso());
      session.preview.revision += 1;
      return preview(store, session.projectId);
    }
    if (applying) applyValues(setup, draft.native, draft.revision, head, nowIso(), start.actorAccountId);
    const started = startPreview(store, session.projectId);
    const phaseTimings = (started?.phaseTimings ?? []).map((timing) => ({
      ...timing,
      startedAt: isoToTimestamp(timing.startedAt),
    }));
    recordStart(setup, { ...start, phaseTimings }, null, nowIso());
    return preview(store, session.projectId);
  },
  StopPreview: (request, { store }) => {
    stopPreview(store, fenced(store, request).projectId);
    return preview(store, request.projectId);
  },
  PreparePreviewSource: (request, { store }) => {
    if (!preparePreviewSource(store, fenced(store, request).projectId, request.requestId))
      throw httpError(409, "Session is not active");
    return {};
  },
  // Like Go, the switch is an Agent command of an ACTIVE Session, which a repeat leaves unchanged.
  UpdatePreviewAutoDeploy: ({ projectId, isEnabled }, { store }) => {
    const session = projectSession(store, projectId);
    if (isEnabled === undefined) throw httpError(400, "is_enabled is required");
    const description = store.agents.get(projectId)?.description;
    if (session.state !== "ACTIVE" || !description)
      throw httpError(409, "command conflicts with current session state");
    description.isPreviewAutoDeployEnabled = isEnabled;
    return {};
  },
  GetPreviewDefinitions: async ({ projectId }, { store }) => {
    const session = projectSession(store, projectId);
    if (session.preview.preparedSource?.status !== "PREVIEW_SOURCE_STATUS_READY")
      throw httpError(409, "Prepare mock Preview source first");
    return { ...preview(store, projectId), flowDefinitions: await mockFlowDefinitions() };
  },
};
