// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock SessionService over the in-memory store and scripted Agent; development only.
import { httpError } from "@/lib/server/request-body";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { advanceMockDraft, mockRequirementsDraft } from "@/mock/store/agent-scripts";
import { archivedMessages, recentActivity } from "@/mock/store/agent-state";
import { nowIso } from "@/mock/store/clock";
import { mockSessionSnapshot } from "@/mock/store/resource-snapshots";
import { getSession, keepWorkspaceRunning, type MockStore } from "@/mock/store/store";
import type { MockResponse, MockService } from "./mock-backend";
import { pollStore } from "./store-watch";

/** The Project's one Session; a Project without one, or a missing Project, reads NOT_FOUND. */
export function projectSession(store: MockStore, projectId: string) {
  const session = getSession(store, projectId);
  if (!session) throw httpError(404, "Session not found");
  return session;
}

// The resume cursor names the Project and how many activity events the reader has seen.
function resumeCursor(projectId: string, position: number): string {
  return Buffer.from(JSON.stringify({ projectId, position })).toString("base64url");
}

function resumedPosition(cursor: string, projectId: string): number {
  try {
    if (!cursor || cursor.length > 2048) return 0;
    const value: unknown = JSON.parse(Buffer.from(cursor, "base64url").toString("utf8"));
    if (!value || typeof value !== "object" || !("position" in value)) return 0;
    const { position } = value;
    return "projectId" in value &&
      value.projectId === projectId &&
      Number.isSafeInteger(position) &&
      (position as number) >= 0
      ? (position as number)
      : 0;
  } catch {
    return 0;
  }
}

export const sessionService: MockService<"SessionService"> = {
  GetSession: ({ projectId }, { store }) => {
    projectSession(store, projectId);
    advanceMockDraft(store, projectId);
    return { session: mockSessionSnapshot(store, projectId) };
  },
  // Each changed snapshot, then the activity events after the resumed position and the card's changed draft.
  WatchSession: ({ projectId, resumeCursor: cursor }, { store }, signal) => {
    let position = resumedPosition(cursor, projectId);
    let fingerprint = "";
    let draftFingerprint = "";
    return pollStore(signal, () => {
      if (!getSession(store, projectId)) return null;
      advanceMockDraft(store, projectId);
      const frames: MockResponse<"WatchSession">[] = [];
      const snapshot = mockSessionSnapshot(store, projectId);
      const text = JSON.stringify(snapshot);
      if (text !== fingerprint) {
        fingerprint = text;
        frames.push({ snapshot, resumeCursor: resumeCursor(projectId, position) });
      }
      const events = recentActivity(store, projectId);
      if (position > events.length) position = 0;
      for (const event of events.slice(position)) {
        position += 1;
        frames.push({
          progress: {
            activity: event.value,
            createdAt: isoToTimestamp(event.createdAt),
            source: event.source,
            taskId: event.taskId ?? "",
          },
          resumeCursor: resumeCursor(projectId, position),
        });
      }
      const draft = mockRequirementsDraft(store, projectId);
      const draftText = JSON.stringify(draft);
      if (draft && draftText !== draftFingerprint)
        frames.push({
          progress: { requirementsDraft: draft, createdAt: isoToTimestamp(nowIso()), source: "mock-draft", taskId: "" },
          resumeCursor: resumeCursor(projectId, position),
        });
      draftFingerprint = draftText;
      return frames;
    });
  },
  RenewWorkspace: ({ projectId }, { store }) => {
    projectSession(store, projectId);
    return { renewed: keepWorkspaceRunning(store, projectId) };
  },
  ListArchivedMessages: ({ projectId, beforeSequence }, { store }) => {
    projectSession(store, projectId);
    const page = archivedMessages(store, projectId, Number(beforeSequence));
    if (!page) throw httpError(404, "Archived messages not found");
    return { history: page };
  },
};
