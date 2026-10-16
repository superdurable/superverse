// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef, useState, type RefObject } from "react";
import { useAccount } from "@/lib/account/AccountProvider";
import type { TeamSpaceSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceSnapshot";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { TEAM_SPACE_ROLES, teamSpaceRoleOf } from "@/lib/teamspace/team-space-roles";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { useTeamSpaceList } from "@/lib/teamspace/use-team-space-list";
import { cx } from "@/components/ui/cx";
import { Dialog } from "@/components/ui/Dialog";
import { Glyph } from "@/components/ui/Glyph";
import { POPOVER_ITEM, PopoverCard, usePopover } from "@/components/ui/Popover";
import { TeamSpaceCreateForm } from "./TeamSpaceCreateForm";

/** The caller's word for a Teamspace: its role there, or "platform admin" for the administrator outside it. */
function roleWord(teamSpace: TeamSpaceSnapshot__Output, isAdmin: boolean): string {
  const role = teamSpaceRoleOf(teamSpace.callerRole);
  return role ? TEAM_SPACE_ROLES[role].word : isAdmin ? "platform admin" : "";
}

function Tile({ name, current }: { name: string; current?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cx(
        "-ml-px grid size-5 shrink-0 place-items-center rounded-[5px] text-[11px] font-semibold",
        current ? "bg-accent text-panel" : "bg-band-1 text-ink",
      )}
    >
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  );
}

/**
 * The sidebar's Teamspace row: the current Teamspace with the person's role in it. It opens the person's
 * Teamspaces, every one for the platform admin, and New Teamspace. The sidebar passes its row and rail-label
 * classes, which this row shares.
 */
export function TeamSpaceSwitcher({ rowClassName, labelClassName }: { rowClassName: string; labelClassName?: string }) {
  const account = useAccount();
  const { teamSpace } = useTeamSpace();
  const anchor = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  const [creating, setCreating] = useState(false);
  const name = teamSpace?.name ?? "Teamspaces";
  const word = teamSpace ? roleWord(teamSpace, Boolean(account?.isAdmin)) : "";
  return (
    <div ref={anchor} className="relative">
      <button
        type="button"
        aria-expanded={popover.open}
        aria-haspopup="dialog"
        onClick={popover.toggle}
        aria-label={`${name}${word ? ` · ${word}` : ""}`}
        title={`${name}${word ? ` · ${word}` : ""} · switch Teamspace`}
        className={cx(rowClassName, "h-10")}
      >
        <Tile name={name} current />
        <span className={cx("grid min-w-0 flex-1 text-left leading-tight", labelClassName)}>
          <b className="truncate text-[13.5px] font-semibold text-ink">{name}</b>
          {word && <small className="truncate text-[11.5px] text-ink-faint">{word}</small>}
        </span>
        <Glyph name="chevronDown" className={cx("size-3.5 shrink-0", labelClassName)} />
      </button>
      {popover.open && (
        <TeamSpaceList
          anchorRef={anchor}
          currentId={teamSpace?.teamSpaceId ?? null}
          isAdmin={Boolean(account?.isAdmin)}
          onPick={popover.close}
          onCreate={() => {
            popover.close();
            setCreating(true);
          }}
        />
      )}
      {creating && (
        <Dialog onDismiss={() => setCreating(false)} aria-label="New Teamspace">
          <div className="grid gap-3 p-5">
            <h2 className="text-[17px] font-semibold text-ink">New Teamspace</h2>
            <TeamSpaceCreateForm onCreated={() => setCreating(false)} onCancel={() => setCreating(false)} />
          </div>
        </Dialog>
      )}
    </div>
  );
}

/** Mounted only while open, so opening reads the Teamspaces again. */
function TeamSpaceList({
  anchorRef,
  currentId,
  isAdmin,
  onPick,
  onCreate,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  currentId: string | null;
  isAdmin: boolean;
  onPick: () => void;
  onCreate: () => void;
}) {
  const list = useTeamSpaceList("switcher");
  return (
    <PopoverCard anchorRef={anchorRef} label="Switch Teamspace" className="max-h-[70vh] w-72">
      <p className="px-2.25 pt-1 pb-1.5 text-[11px] tracking-[0.07em] text-ink-faint uppercase">
        {isAdmin ? "Every Teamspace" : "Your Teamspaces"}
      </p>
      {list.items.map((item) => (
        <Link
          key={item.teamSpaceId}
          href={studioRoutes.teamSpace(item.teamSpaceId)}
          aria-current={item.teamSpaceId === currentId ? "page" : undefined}
          onClick={onPick}
          className={POPOVER_ITEM}
        >
          <Tile name={item.name} />
          <span className="grid min-w-0 flex-1 leading-tight">
            <span className="truncate">{item.name}</span>
            <small className="truncate text-[11.5px] text-ink-faint">
              {item.status === "TEAM_SPACE_STATUS_DELETING" ? "being deleted" : roleWord(item, isAdmin)}
            </small>
          </span>
          {item.teamSpaceId === currentId && <Glyph name="check" className="size-3.5 shrink-0" />}
        </Link>
      ))}
      {!list.loaded && <p className="px-2.25 py-1.75 text-ink-faint">Loading Teamspaces…</p>}
      {list.error && <p className="px-2.25 py-1.75 text-fail">{list.error}</p>}
      {list.nextPageToken && (
        <button
          type="button"
          disabled={list.loading}
          onClick={() => void list.loadMore()}
          className={cx(POPOVER_ITEM, "text-ink-dim")}
        >
          {list.loading ? "Loading…" : "Load more"}
        </button>
      )}
      <hr className="mx-0.5 my-1 border-line-faint" />
      <button type="button" onClick={onCreate} className={POPOVER_ITEM}>
        <Glyph name="plus" />
        New Teamspace
      </button>
    </PopoverCard>
  );
}
