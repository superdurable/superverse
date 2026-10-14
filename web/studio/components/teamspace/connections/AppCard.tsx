// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import type { TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { AppLogo } from "./AppLogo";
import { ConnectionRow } from "./ConnectionRow";
import { connectionStatus } from "./connection-words";

/** The connection open in its editor: an existing one, read with its stored credential field names, or a new one. */
export type Editing =
  | { kind: "existing"; connection: TeamSpaceConnection; storedCredentialFields: string[] }
  | { kind: "new"; connectorId: string };

/**
 * One app of the TeamSpace with its connections, those that need someone first. "Add another" opens a new
 * connection's editor at the top of the card, under the next free name.
 */
export function AppCard({
  name,
  logoSvg,
  connections,
  projectName,
  canManage,
  editing,
  busy,
  onOpen,
  onAdd,
  onDelete,
  editor,
  newEditor,
}: {
  name: string;
  logoSvg: string;
  connections: TeamSpaceConnection[];
  projectName: (projectId: string) => string;
  canManage: boolean;
  editing: Editing | null;
  busy: boolean;
  onOpen: (connection: TeamSpaceConnection) => void;
  onAdd: () => void;
  onDelete: (connection: TeamSpaceConnection) => void;
  editor: (read: Extract<Editing, { kind: "existing" }>) => ReactNode;
  newEditor: () => ReactNode;
}) {
  const needsYou = connections.filter((connection) => connectionStatus(connection).needsYou).length;
  const lines = new Set(connections.map((connection) => connection.versionLine));
  const adding = editing?.kind === "new" && editing.connectorId === connections[0]?.connectorId;
  return (
    <section
      aria-label={name}
      className={cx("overflow-hidden rounded-xl border bg-panel", needsYou ? "border-busy-rule" : "border-line-soft")}
      data-testid="team-space-app"
    >
      <header className="flex flex-wrap items-center gap-3 border-b border-line-faint px-4 py-3">
        <AppLogo name={name} svg={logoSvg} />
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{name}</h3>
        <span className={cx("text-xs", needsYou ? "font-medium text-busy" : "text-ink-faint")}>
          {needsYou
            ? `${needsYou} need${needsYou === 1 ? "s" : ""} you`
            : `${connections.length} connection${connections.length === 1 ? "" : "s"}`}
        </span>
        {canManage && (
          <Button size="sm" variant="quiet" disabled={busy || adding} onClick={onAdd}>
            + Add another
          </Button>
        )}
      </header>
      {adding && (
        <div className="border-b border-line-faint bg-raised/60 px-4 py-3" data-testid="team-space-new-connection">
          {newEditor()}
        </div>
      )}
      <ul aria-label={`${name} connections`}>
        {connections.map((connection) => (
          <ConnectionRow
            key={connection.connectionId}
            connection={connection}
            versionTag={lines.size > 1 ? connection.versionLine : undefined}
            projectName={projectName}
            canManage={canManage}
            open={editing?.kind === "existing" && editing.connection.connectionId === connection.connectionId}
            busy={busy}
            onOpen={() => onOpen(connection)}
            onDelete={() => onDelete(connection)}
            editor={() => (editing?.kind === "existing" ? editor(editing) : null)}
          />
        ))}
      </ul>
    </section>
  );
}
