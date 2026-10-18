// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, type KeyboardEvent, type Ref } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import { formatFileSize, type AttachmentKind } from "@/lib/session/attachments/attachment-files";
import {
  chipLabel,
  chipStatusText,
  chipWorking,
  type AttachmentChip,
} from "@/lib/session/attachments/attachment-chips";
import type { AttachmentTray as Tray } from "@/lib/session/attachments/use-attachment-tray";

const KIND_GLYPHS = { image: "image", document: "document", json: "braces" } as const satisfies Record<
  AttachmentKind,
  GlyphName
>;

const ATTACHMENT_DISCLOSURE = "Files are read by the AI model to understand your request.";

/** The progress ring around a working chip's icon: the upload's percent, else an open arc that turns. */
function ProgressRing({ chip }: { chip: AttachmentChip }) {
  const circumference = 2 * Math.PI * 17;
  const known = chip.phase === "uploading";
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 40 40"
      className={cx("pointer-events-none absolute inset-0 size-full text-accent", !known && "animate-spin")}
    >
      <circle cx="20" cy="20" r="17" fill="none" stroke="currentColor" strokeOpacity="0.15" strokeWidth="3" />
      <circle
        cx="20"
        cy="20"
        r="17"
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={circumference * (1 - (known ? chip.percent / 100 : 0.25))}
        transform="rotate(-90 20 20)"
      />
    </svg>
  );
}

function ChipIcon({ chip }: { chip: AttachmentChip }) {
  const failed = chip.phase === "failed" || chip.phase === "expired";
  return (
    <span
      className={cx(
        "relative grid size-9 shrink-0 place-items-center overflow-hidden rounded-md",
        failed ? "bg-fail-subtle text-fail" : "bg-band-1 text-ink-dim",
      )}
    >
      {chip.previewUrl && !failed ? (
        // A local object URL of the person's own file, which only this page holds.
        // eslint-disable-next-line @next/next/no-img-element
        <img alt="" src={chip.previewUrl} className="size-full object-cover" />
      ) : (
        <Glyph name={failed ? "alert" : KIND_GLYPHS[chip.kind]} />
      )}
      {chipWorking(chip) && <ProgressRing chip={chip} />}
    </span>
  );
}

function Chip({
  chip,
  onRemove,
  removable,
  buttonRef,
}: {
  chip: AttachmentChip;
  onRemove: () => void;
  /** False while a send carries the chip's attachment. */
  removable: boolean;
  buttonRef: Ref<HTMLButtonElement>;
}) {
  const failed = chip.phase === "failed" || chip.phase === "expired";
  const status = chipStatusText(chip);
  function removeOnKey(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "Delete" && event.key !== "Backspace") return;
    event.preventDefault();
    if (removable) onRemove();
  }
  return (
    <span
      className={cx(
        "flex max-w-64 min-w-0 items-center gap-2 rounded-lg border bg-panel py-1 pr-1 pl-1",
        failed ? "border-fail-rule" : "border-line-soft",
      )}
      data-attachment-chip={chip.phase}
      data-attachment-id={chip.attachmentId ?? undefined}
    >
      <button
        ref={buttonRef}
        type="button"
        className="flex min-w-0 items-center gap-2 rounded-md text-left focus-visible:outline-2 focus-visible:outline-accent"
        aria-label={chipLabel(chip)}
        aria-keyshortcuts="Delete Backspace"
        title={`${chip.name} · ${formatFileSize(chip.size)} · ${status}`}
        onKeyDown={removeOnKey}
      >
        <ChipIcon chip={chip} />
        <span className="grid min-w-0">
          <span className="truncate text-[12px] text-ink">
            {chip.name}
            <span className="text-ink-faint"> · {formatFileSize(chip.size)}</span>
          </span>
          <span className={cx("text-[11px]", failed ? "text-fail" : "truncate text-ink-faint")}>{status}</span>
        </span>
      </button>
      <button
        type="button"
        aria-label={`Remove ${chip.name}`}
        title={removable ? `Remove ${chip.name}` : `${chip.name} is being sent`}
        disabled={!removable}
        onClick={onRemove}
        className="grid size-6 shrink-0 place-items-center self-start rounded-md text-ink-faint hover:bg-hover hover:text-ink focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent"
      >
        <Glyph name="close" className="size-3.5" />
      </button>
    </span>
  );
}

/** What the polite live region says of the tray: how many files stand where, which changes as they move. */
function announcement(chips: readonly AttachmentChip[]): string {
  if (chips.length === 0) return "";
  const ready = chips.filter((chip) => chip.phase === "ready").length;
  const failed = chips.filter((chip) => chip.phase === "failed" || chip.phase === "expired").length;
  const working = chips.length - ready - failed;
  return [
    `${chips.length} ${chips.length === 1 ? "file" : "files"} attached`,
    ...(ready ? [`${ready} ready`] : []),
    ...(working ? [`${working} in progress`] : []),
    ...(failed ? [`${failed} could not be attached`] : []),
  ].join(", ");
}

/**
 * The composer's attachments above its text: one chip per file with its icon or thumbnail, name, size and
 * progress, then why it waits or failed. Each chip is a button; Delete or Backspace on it, or its ×, removes
 * it, except while a send carries it, and the focus moves to the next chip, else the previous one, else to
 * `returnFocus`, such as the composer's text. Under the chips, once per browser, the line that the AI model
 * reads the files, and the last refusal. The live region and the refusal's status stay mounted, empty or
 * not, so that a screen reader announces the first chip and a refusal on an empty tray.
 */
export function AttachmentTray({
  tray,
  className,
  returnFocus,
}: {
  tray: Tray;
  className?: string;
  returnFocus?: () => void;
}) {
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  // The chip whose removal moves the focus, and where to: a chip's key, or "" for returnFocus.
  const refocus = useRef<{ removed: string; target: string } | null>(null);
  const shown = tray.chips.length > 0 || tray.notice !== null;

  useEffect(() => {
    const pending = refocus.current;
    if (!pending || tray.chips.some((chip) => chip.key === pending.removed)) return;
    refocus.current = null;
    if (pending.target) buttons.current.get(pending.target)?.focus();
    else returnFocus?.();
  }, [returnFocus, tray.chips]);

  function remove(key: string) {
    const index = tray.chips.findIndex((chip) => chip.key === key);
    const neighbour = tray.chips[index + 1] ?? tray.chips[index - 1];
    refocus.current = { removed: key, target: neighbour?.key ?? "" };
    tray.remove(key);
  }

  return (
    <div
      className={cx(shown && "grid gap-1", shown && className)}
      role={shown ? "group" : undefined}
      aria-label={shown ? "Attached files" : undefined}
    >
      {tray.chips.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {tray.chips.map((chip) => (
            <li key={chip.key} className="min-w-0">
              <Chip
                chip={chip}
                onRemove={() => remove(chip.key)}
                removable={!tray.sending.has(chip.key)}
                buttonRef={(button) => {
                  if (button) buttons.current.set(chip.key, button);
                  else buttons.current.delete(chip.key);
                }}
              />
            </li>
          ))}
        </ul>
      )}
      {tray.disclosure && <p className="text-[11px] text-ink-faint">{ATTACHMENT_DISCLOSURE}</p>}
      <p className={tray.notice ? "text-[11px] text-ink-dim" : "sr-only"} role="status">
        {tray.notice ?? ""}
      </p>
      <span className="sr-only" aria-live="polite">
        {announcement(tray.chips)}
      </span>
    </div>
  );
}
