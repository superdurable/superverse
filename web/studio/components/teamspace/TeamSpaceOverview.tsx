// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useAccount } from "@/lib/account/AccountProvider";
import { deleteTeamSpace, getTeamSpaceOverview, renameTeamSpace } from "@/lib/api/tenancy/team-space";
import { errorMessage, isRevisionConflict, newRequestId } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import type { TeamSpaceOverview__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceOverview";
import type { TeamSpaceSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceSnapshot";
import { teamSpaceRoleOf } from "@/lib/teamspace/team-space-roles";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dialog } from "@/components/ui/Dialog";

const FIELD = cx(
  "w-full rounded-lg border border-line bg-panel px-3 py-1.5 text-base text-ink focus:border-accent focus:shadow-(--focus-ring) focus:outline-none",
);

/** What a fenced write that found the TeamSpace changed says; the page then shows the TeamSpace as it is. */
const CHANGED = "Someone changed this TeamSpace meanwhile, so nothing was saved. This is it now; try again.";

function count(value: number, one: string, many: string) {
  return `${value} ${value === 1 ? one : many}`;
}

/**
 * A TeamSpace's Home header: its name, which its admins rename, and how many members, projects and Live
 * projects it has. The platform admin deletes the TeamSpace from here, after typing its name.
 */
export function TeamSpaceOverview() {
  const account = useAccount();
  const { teamSpace, accept, reload } = useTeamSpace();
  const [overview, setOverview] = useState<TeamSpaceOverview__Output | null>(null);
  const [error, setError] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const teamSpaceId = teamSpace?.teamSpaceId ?? "";
  const revision = teamSpace?.revision;

  useEffect(() => {
    if (!teamSpaceId) return;
    const controller = new AbortController();
    getTeamSpaceOverview(teamSpaceId, controller.signal).then(
      (value) => !controller.signal.aborted && setOverview(value),
      (reason: unknown) =>
        !controller.signal.aborted && setError(errorMessage(reason, "Could not count the TeamSpace")),
    );
    return () => controller.abort();
  }, [teamSpaceId, revision]);

  if (!teamSpace) return null;
  const manages = teamSpaceRoleOf(teamSpace.callerRole) === "admin" || Boolean(account?.isAdmin);
  const shown = overview?.teamSpace?.teamSpaceId === teamSpaceId ? overview : null;
  return (
    <section aria-label="TeamSpace" className="shrink-0 border-b border-line-soft px-8 pt-6 pb-5 max-sm:px-4">
      {renaming ? (
        <RenameForm
          teamSpace={teamSpace}
          onDone={(renamed) => {
            if (renamed) accept(renamed);
            setRenaming(false);
          }}
          onChanged={reload}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h2 className="min-w-0 truncate text-[26px] font-semibold tracking-[-0.01em] text-ink">{teamSpace.name}</h2>
          {manages && (
            <Button size="sm" variant="quiet" onClick={() => setRenaming(true)}>
              Rename
            </Button>
          )}
          {account?.isAdmin && (
            <Button size="sm" variant="danger" className="ml-auto" onClick={() => setDeleting(true)}>
              Delete TeamSpace…
            </Button>
          )}
        </div>
      )}
      <p className="mt-1 text-[13.5px] text-ink-faint" data-testid="team-space-counts">
        {shown
          ? `${count(shown.memberCount, "member", "members")} · ${count(shown.projectCount, "project", "projects")} · ${shown.liveProjectCount} Live · ${count(shown.connectionCount, "connection", "connections")}`
          : error || "Counting…"}
      </p>
      {deleting && (
        <DeleteDialog
          teamSpace={teamSpace}
          projectCount={shown?.projectCount ?? null}
          onClose={(deleted) => {
            if (deleted) accept(deleted);
            setDeleting(false);
          }}
        />
      )}
    </section>
  );
}

function RenameForm({
  teamSpace,
  onDone,
  onChanged,
}: {
  teamSpace: TeamSpaceSnapshot__Output;
  onDone: (renamed: TeamSpaceSnapshot__Output | null) => void;
  /** The TeamSpace changed since the page read it: read it again. */
  onChanged: () => void;
}) {
  const [name, setName] = useState(teamSpace.name);
  const action = useSingleFlight<"rename">();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || trimmed === teamSpace.name) return onDone(null);
    void action.run("rename", async (signal) => {
      try {
        const input = { teamSpaceId: teamSpace.teamSpaceId, expectedRevision: teamSpace.revision, name: trimmed };
        onDone(await renameTeamSpace(input, signal));
      } catch (reason) {
        if (!isRevisionConflict(reason)) throw reason;
        onChanged();
        throw new Error(CHANGED);
      }
    });
  };
  return (
    <form className="grid max-w-md gap-2" onSubmit={submit} aria-label="Rename the TeamSpace">
      <label className="grid gap-1 text-sm font-medium text-ink-dim">
        TeamSpace name
        <input
          className={FIELD}
          value={name}
          maxLength={80}
          autoFocus
          onChange={(event) => setName(event.target.value)}
        />
      </label>
      {action.error && <Banner role="alert">{action.error}</Banner>}
      <div className="flex gap-2">
        <Button type="submit" size="sm" variant="strong" disabled={action.busy !== null}>
          Save
        </Button>
        <Button type="button" size="sm" onClick={() => onDone(null)} disabled={action.busy !== null}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** The second confirmation of a TeamSpace's deletion: its name, typed. */
function DeleteDialog({
  teamSpace,
  projectCount,
  onClose,
}: {
  teamSpace: TeamSpaceSnapshot__Output;
  projectCount: number | null;
  onClose: (deleted: TeamSpaceSnapshot__Output | null) => void;
}) {
  const [typed, setTyped] = useState("");
  const requestId = useRef(newRequestId());
  const action = useSingleFlight<"delete">();
  const confirmed = typed.trim() === teamSpace.name;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!confirmed) return;
    void action.run("delete", async (signal) => {
      onClose(await deleteTeamSpace({ teamSpaceId: teamSpace.teamSpaceId, requestId: requestId.current }, signal));
    });
  };
  return (
    <Dialog onDismiss={() => action.busy === null && onClose(null)} aria-label={`Delete ${teamSpace.name}`}>
      <form className="grid gap-3 p-5" onSubmit={submit}>
        <h2 className="text-[17px] font-semibold text-ink">Delete {teamSpace.name}?</h2>
        <p className="text-sm text-ink-dim">
          {projectCount === null ? "Its projects" : `Its ${count(projectCount, "project", "projects")}`} and their Live
          apps are deleted with it, its connections too, and its members lose access. This cannot be undone.
        </p>
        <label className="grid gap-1 text-sm font-medium text-ink-dim">
          Type the TeamSpace&apos;s name to confirm
          <input
            className={FIELD}
            value={typed}
            autoFocus
            onChange={(event) => setTyped(event.target.value)}
            aria-describedby="delete-team-space-name"
          />
        </label>
        <p id="delete-team-space-name" className="font-mono text-xs text-ink-faint">
          {teamSpace.name}
        </p>
        {action.error && <Banner role="alert">{action.error}</Banner>}
        <div className="flex gap-2">
          <Button type="submit" size="sm" variant="danger" disabled={!confirmed || action.busy !== null}>
            {action.busy ? "Deleting…" : "Delete TeamSpace"}
          </Button>
          <Button type="button" size="sm" onClick={() => onClose(null)} disabled={action.busy !== null}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
