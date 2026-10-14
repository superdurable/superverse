// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ConnectorCatalogEntry, TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { timestampToISO } from "@/lib/shared/protobuf";

// What the Connections pages say about a connection: its status, how it signs in, which Projects use it and what a
// change reaches. One word set, so the list, the editor and the catalog agree.

export type ConnectionStatusWords = { words: string; needsYou: boolean; action: string };

/** A connection's status; anything but READY needs someone, and its action says what to do. */
export function connectionStatus(connection: TeamSpaceConnection): ConnectionStatusWords {
  switch (connection.credentialStatus) {
    case "CREDENTIAL_STATUS_READY":
      return { words: "Connected", needsYou: false, action: "Edit" };
    case "CREDENTIAL_STATUS_REAUTHORIZATION_REQUIRED":
      return { words: "Sign in again", needsYou: true, action: "Sign in again" };
    case "CREDENTIAL_STATUS_AUTHORIZING":
    case "CREDENTIAL_STATUS_UNSPECIFIED":
      // Only a connection copied to a new major version, or one made before sign-in created connections, is here.
      return { words: "Sign-in not finished", needsYou: true, action: "Finish sign-in" };
  }
}

function day(timestamp: TeamSpaceConnection["updatedAt"]): string {
  const iso = timestampToISO(timestamp);
  return iso ? new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "";
}

/** The line under a connection's name: whose app it signs in with, and when it ended or last changed. */
export function connectionDetail(connection: TeamSpaceConnection): string {
  const parts: string[] = [];
  if (connection.credentialStatus === "CREDENTIAL_STATUS_REAUTHORIZATION_REQUIRED" && connection.credentialExpiresAt)
    parts.push(`Ended ${day(connection.credentialExpiresAt)}`);
  else if (connection.updatedAt) parts.push(`Changed ${day(connection.updatedAt)}`);
  if (connection.clientSource === "CONNECTION_CLIENT_SOURCE_PLATFORM") parts.push("Superverse's app");
  return parts.join(" · ");
}

/** One Project that uses a connection, with the sides that use it. */
export type ProjectUse = { projectId: string; preview: boolean; live: boolean };

/** The Projects that use a connection, each once, with its Preview and Live. */
export function projectUses(connection: TeamSpaceConnection): ProjectUse[] {
  const uses = new Map<string, ProjectUse>();
  for (const usage of connection.usages) {
    const use = uses.get(usage.projectId) ?? { projectId: usage.projectId, preview: false, live: false };
    if (usage.scope === "PROJECT_SCOPE_LIVE") use.live = true;
    else use.preview = true;
    uses.set(usage.projectId, use);
  }
  return [...uses.values()];
}

/** "Refunds · Live and Newsletter · Preview": where a changed key or sign-in takes effect at once. */
export function reachText(uses: ProjectUse[], projectName: (projectId: string) => string): string {
  const items = uses.map(
    (use) =>
      `${projectName(use.projectId)} · ${[use.preview && "Preview", use.live && "Live"].filter(Boolean).join(" and ")}`,
  );
  return items.length <= 1 ? (items[0] ?? "") : `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

/** The first free name for another connection of an app: its name, then "<name> 2", "<name> 3", and so on. */
export function freeName(base: string, connections: TeamSpaceConnection[]): string {
  const taken = new Set(connections.map((connection) => connection.name));
  if (!taken.has(base)) return base;
  for (let index = 2; ; index++) if (!taken.has(`${base} ${index}`)) return `${base} ${index}`;
}

/** An app's name as the catalog gives it, else its connector ID. */
export function appName(connectorId: string, catalog: ConnectorCatalogEntry[]): string {
  return catalog.find((entry) => entry.connectorId === connectorId)?.displayName || connectorId;
}

/** Apps a TeamSpace often adds first, offered under Add more apps when the catalog lists them. */
export const SUGGESTED_APPS = ["slack", "gmail", "google-sheets", "notion", "hubspot", "stripe", "openai", "github"];
