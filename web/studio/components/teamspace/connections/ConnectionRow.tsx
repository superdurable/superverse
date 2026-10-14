// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState, type ReactNode } from "react";
import type { TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { failureProblem } from "@/lib/shared/problem/problem";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { connectionDetail, connectionStatus, projectUses, reachText, type ProjectUse } from "./connection-words";

/** A Project that uses the connection, with P and L for its Preview and Live; it opens the Project's Connectors. */
function UseChip({ use, name }: { use: ProjectUse; name: string }) {
  const sides = [use.preview && "Preview", use.live && "Live"].filter(Boolean).join(" and ");
  return (
    <a
      className="inline-flex max-w-44 items-center gap-1 rounded-md border border-line-soft bg-panel py-px pr-1 pl-1.5 text-[11.5px] text-ink-dim hover:border-mark hover:text-ink"
      href={studioRoutes.previewConfiguration(use.projectId)}
      title={`${name} uses it on ${sides}`}
    >
      <span className="truncate">{name}</span>
      {use.preview && <span className="rounded-sm bg-raised px-1 text-[10px] font-semibold text-ink-faint">P</span>}
      {use.live && <span className="rounded-sm bg-accent-subtle px-1 text-[10px] font-semibold text-accent">L</span>}
    </a>
  );
}

/**
 * One connection of an app: its name, how it signs in, the Projects that use it on Preview and Live, and its
 * status. Opening it puts the editor in the row, with what a change reaches and Delete, which stays blocked
 * while a Project uses it. A member who manages no connection sees the row without its actions.
 */
export function ConnectionRow({
  connection,
  versionTag,
  projectName,
  canManage,
  open,
  busy,
  onOpen,
  onDelete,
  editor,
}: {
  connection: TeamSpaceConnection;
  /** The major version, shown when the app's connections span several. */
  versionTag?: string;
  projectName: (projectId: string) => string;
  canManage: boolean;
  open: boolean;
  busy: boolean;
  onOpen: () => void;
  onDelete: () => void;
  editor: () => ReactNode;
}) {
  const [confirming, setConfirming] = useState(false);
  const status = connectionStatus(connection);
  const uses = projectUses(connection);
  const detail = connectionDetail(connection);
  const reach = reachText(uses, projectName);
  return (
    <li
      className={cx("border-t border-line-faint px-4 py-3 first:border-t-0", open && "bg-raised/60")}
      data-testid="team-space-connection"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="min-w-40 flex-1">
          <p className="truncate text-[13.5px] font-medium text-ink">
            {connection.name}
            {versionTag && (
              <span className="ml-1.5 font-mono text-[11px] font-normal text-ink-faint">{versionTag}</span>
            )}
          </p>
          {detail && <p className="truncate text-xs text-ink-faint">{detail}</p>}
        </div>
        <div
          className="flex max-w-full flex-wrap items-center gap-1.5"
          aria-label={`Projects using ${connection.name}`}
        >
          {uses.length === 0 ? (
            <span className="text-xs text-ink-faint">Not used</span>
          ) : (
            uses.map((use) => <UseChip key={use.projectId} use={use} name={projectName(use.projectId)} />)
          )}
        </div>
        <span
          className={cx(
            "inline-flex items-center gap-1.5 text-xs whitespace-nowrap",
            status.needsYou ? "rounded-full bg-busy-subtle px-2 py-0.5 font-medium text-busy" : "text-ink-dim",
          )}
        >
          {!status.needsYou && <span aria-hidden="true" className="size-1.5 rounded-full bg-pass" />}
          {status.words}
        </span>
        {canManage && !open && (
          <Button size="sm" variant={status.needsYou ? "accent" : "quiet"} disabled={busy} onClick={onOpen}>
            {status.action}
          </Button>
        )}
      </div>
      {connection.lastFailure && (
        <ProblemPanel
          className="mt-1.5"
          problem={failureProblem(connection.lastFailure, { operation: "Authorizing the connection" })}
          actions={canManage ? { FAILURE_REMEDY_REAUTHORIZE: { onClick: onOpen } } : undefined}
        />
      )}
      {connection.unroutedEvents && (
        <p className="mt-1.5 text-xs text-busy" data-testid="connection-unrouted-events">
          {connection.unroutedEvents.count} Slack {connection.unroutedEvents.count === "1" ? "event" : "events"} reached
          this workspace but no environment is registered to receive them. Counted since the platform last restarted.
        </p>
      )}
      {open && (
        <div className="mt-3 grid gap-3">
          {editor()}
          <div className="flex flex-wrap items-start justify-between gap-3 border-t border-line-faint pt-3">
            <p className="max-w-xl text-xs/relaxed text-ink-faint">
              {reach ? (
                <>
                  A new key or sign-in takes effect right away in <b className="font-medium text-ink-dim">{reach}</b>.
                </>
              ) : (
                "No Project uses this connection yet."
              )}{" "}
              A settings change reaches Preview at its next start and Live with its next release.
            </p>
            {confirming ? (
              <span className="flex items-center gap-2 text-xs text-ink-dim">
                Delete {connection.name}?
                <Button size="sm" variant="danger" disabled={busy} onClick={onDelete}>
                  Delete
                </Button>
                <Button size="sm" variant="quiet" onClick={() => setConfirming(false)}>
                  Keep
                </Button>
              </span>
            ) : (
              <Button
                size="sm"
                variant="quiet"
                className="text-fail"
                disabled={busy || uses.length > 0}
                title={uses.length > 0 ? `${reach} use it. Choose another connection there first.` : undefined}
                onClick={() => setConfirming(true)}
              >
                {uses.length > 0
                  ? `Delete · used by ${uses.length === 1 ? projectName(uses[0].projectId) : `${uses.length} Projects`}`
                  : "Delete"}
              </Button>
            )}
          </div>
        </div>
      )}
    </li>
  );
}
