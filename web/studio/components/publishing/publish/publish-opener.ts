// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef } from "react";

/**
 * What a Publish entry asks the bar's popover to open on: a version (the newest ahead of Live unless one
 * is named), or Live's own version again with its waiting settings.
 */
export type PublishOpening = { mode: "version"; commit?: string } | { mode: "live" };

const listeners = new Set<(opening: PublishOpening) => void>();

/**
 * Opens the bar's Publish popover from anywhere in the Builder, such as Tools ▸ Publishing's "Publish
 * V13…" or the Live tab's "Publish this change…" banner, which passes `{ mode: "live" }`.
 */
export function openPublishPopover(opening: PublishOpening): void {
  for (const listener of listeners) listener(opening);
}

/** Calls `onOpen` whenever a page asks the popover to open; the bar's Publish button listens. */
export function usePublishOpenings(onOpen: (opening: PublishOpening) => void) {
  const latest = useRef(onOpen);
  useEffect(() => {
    latest.current = onOpen;
  });
  useEffect(() => {
    const listener = (opening: PublishOpening) => latest.current(opening);
    listeners.add(listener);
    return () => void listeners.delete(listener);
  }, []);
}
