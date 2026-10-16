// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useId, useSyncExternalStore, type ReactNode } from "react";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

const OPEN = "open";

/**
 * The one way a surface keeps engine detail out of its default view: a "More details" text link
 * that opens the detail in place. `surface` names the fold; an opened fold stays open on that
 * surface in this browser until the person closes it, so a builder who wants the detail keeps it.
 */
export function MoreDetails({
  surface,
  hint,
  label = "More details",
  className,
  children,
}: {
  /** A stable name of this fold, such as "runs.events" or "chat.work-log". */
  surface: string;
  /** A short summary after the label, such as "41 actions · 6 min". */
  hint?: string;
  label?: string;
  className?: string;
  children: ReactNode;
}) {
  const key = STORAGE_KEYS.moreDetails(surface);
  const open =
    useSyncExternalStore(
      subscribeStoredPreferences,
      () => readStoredPreference(key),
      () => null,
    ) === OPEN;
  const toggle = useCallback(() => writeStoredPreference(key, open ? "" : OPEN), [key, open]);
  const panelId = useId();
  return (
    <div className={className} data-more-details={surface}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={toggle}
        className="inline-flex items-center gap-1 rounded text-caption text-ink-faint hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        <Glyph name={open ? "chevronDown" : "chevronRight"} className="size-3.5" />
        <span>{label}</span>
        {hint && <span className="text-ink-faint">· {hint}</span>}
      </button>
      <div id={panelId} hidden={!open} className={cx(open && "mt-1.5")}>
        {open && children}
      </div>
    </div>
  );
}
