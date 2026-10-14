// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useSyncExternalStore, type ReactElement } from "react";
import { cx } from "@/components/ui/cx";
import { SIDEBAR_ROW } from "./sidebar-styles";
import { subscribeStoredPreferences } from "@/lib/browser/storage";
import {
  THEME_PREFERENCES,
  readStoredThemePreference,
  resolveTheme,
  writeStoredThemePreference,
  type ThemePreference,
} from "@/lib/browser/theme";

function applyResolvedTheme(preference: ThemePreference) {
  const resolved = resolveTheme(preference, window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = resolved;
  document.documentElement.style.colorScheme = resolved;
}

const LABEL: Record<ThemePreference, string> = { light: "Light", dark: "Dark", auto: "Auto" };

/**
 * Sun, crescent, half-moon — drawn as paths, not typed, because the glyphs fall
 * back to colour emoji on some platforms and tofu on others. Same three icons as
 * dex-web's theme control.
 */
const ICON: Record<ThemePreference, ReactElement> = {
  light: (
    <>
      <circle cx="8" cy="8" r="3.25" />
      <path
        d="M8 1v1.6M8 13.4V15M1 8h1.6M13.4 8H15M3.05 3.05l1.13 1.13M11.82 11.82l1.13 1.13M12.95 3.05l-1.13 1.13M4.18 11.82l-1.13 1.13"
        strokeLinecap="round"
      />
    </>
  ),
  // One path: a crescent cut by a second disc needs a mask, and a mask is one
  // more thing that can fail to paint.
  dark: <path d="M13.2 10.1A5.6 5.6 0 0 1 5.9 2.8 5.7 5.7 0 1 0 13.2 10.1Z" />,
  auto: (
    <>
      <circle cx="8" cy="8" r="5.6" />
      <path d="M8 2.4a5.6 5.6 0 0 1 0 11.2Z" fill="currentColor" stroke="none" />
    </>
  ),
};

/**
 * The theme control, a row of the sidebar's footer: its icon names the current preference, and its
 * label shows once the sidebar is open.
 *
 * The stored preference is external mutable state, so it is read through a store rather than during
 * render. The server snapshot is "auto" because storage does not exist there; reading storage while
 * rendering would be a hydration mismatch, not a warning.
 */
export function ThemeToggle({ labelClassName }: { labelClassName?: string }) {
  const preference = useSyncExternalStore(subscribeStoredPreferences, readStoredThemePreference, () => "auto" as const);

  // The inline bootstrap in app/layout.tsx already painted the stored theme, so
  // this only follows later changes — to the preference, or to the OS in auto.
  useEffect(() => {
    applyResolvedTheme(preference);
    if (preference !== "auto") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyResolvedTheme("auto");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [preference]);

  const next = THEME_PREFERENCES[(THEME_PREFERENCES.indexOf(preference) + 1) % THEME_PREFERENCES.length];

  return (
    <button
      // The label names this state and the next, because an icon-only control
      // gives a screen reader nothing and hides that there is a third stop.
      aria-label={`Color theme: ${LABEL[preference]}. Activate for ${LABEL[next]}.`}
      className={SIDEBAR_ROW}
      onClick={() => writeStoredThemePreference(next)}
      title={`Theme: ${LABEL[preference]} — switch to ${LABEL[next]}`}
      type="button"
    >
      <svg
        aria-hidden="true"
        className="size-4.5 shrink-0"
        fill="none"
        focusable="false"
        stroke="currentColor"
        strokeWidth="1.3"
        viewBox="0 0 16 16"
        xmlns="http://www.w3.org/2000/svg"
      >
        {ICON[preference]}
      </svg>
      <span className={cx("min-w-0 truncate", labelClassName)}>Theme: {LABEL[preference]}</span>
    </button>
  );
}
