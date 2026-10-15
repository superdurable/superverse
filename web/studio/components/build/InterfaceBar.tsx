// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState, type ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

/** An icon button of the address bar: reload, open in a new window, copy the link. */
export const BAR_BUTTON =
  "grid size-6.5 shrink-0 place-items-center rounded-md text-ink-faint hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none";

/**
 * The Interface's address bar over the app's page, as on Preview and Live alike: the environment with its
 * version and status dot, the address, then the page's own buttons and the environment's control.
 */
export function InterfaceBar({
  environment,
  dot,
  version,
  title,
  address,
  children,
}: {
  environment: string;
  /** The status dot's colours. */
  dot: string;
  /** "V12", or empty before a version runs. */
  version: string;
  /** The chip's hover hint: the environment's state and commit. */
  title: string;
  /** The address, or the environment's state while it has none. */
  address: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-1.5 rounded-[9px] border border-line-soft bg-panel py-1 pr-1.5 pl-2">
      <span
        className="inline-flex h-5 shrink-0 items-center gap-1.25 rounded-full border border-line-soft px-2 text-[11px] font-medium text-ink-dim"
        title={title}
        data-testid="interface-chip"
      >
        <span aria-hidden="true" className={cx("size-1.5 rounded-full border", dot)} />
        {environment}
        {version && ` ${version}`}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink-dim" title={address}>
        {address}
      </span>
      {children}
    </div>
  );
}

/** Copies the address, then says so for a moment. */
export function CopyAddress({ url, label }: { url: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label={copied ? `${label} copied` : `Copy the ${label}`}
      title={copied ? "Copied" : `Copy the ${label}`}
      className={BAR_BUTTON}
      onClick={() =>
        void navigator.clipboard.writeText(url).then(
          () => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1600);
          },
          () => undefined,
        )
      }
    >
      <Glyph name={copied ? "check" : "link"} className="size-3.5" />
    </button>
  );
}

/**
 * What sits under the page, folded under "Details": the commit, the history and the like. It is read only once
 * opened, and again each time it opens.
 */
export function InterfaceDetails({ children }: { children: () => ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="rounded-[10px] border border-line-soft bg-panel px-3.5 py-2.5 text-xs"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer text-[12.5px] text-ink-dim hover:text-ink">Details</summary>
      {open && <div className="mt-2.5 grid gap-3">{children()}</div>}
    </details>
  );
}
