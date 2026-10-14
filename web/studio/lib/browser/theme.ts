// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readStoredPreference, STORAGE_KEYS, writeStoredPreference } from "./storage";

export type ThemePreference = "light" | "dark" | "auto";
/** The order the theme control steps through. */
export const THEME_PREFERENCES: readonly ThemePreference[] = ["light", "dark", "auto"];

export function resolveTheme(preference: ThemePreference, prefersDark: boolean): "light" | "dark" {
  return preference === "auto" ? (prefersDark ? "dark" : "light") : preference;
}

export function readStoredThemePreference(): ThemePreference {
  const value = readStoredPreference(STORAGE_KEYS.theme);
  return THEME_PREFERENCES.find((preference) => preference === value) ?? "auto";
}

export function writeStoredThemePreference(preference: ThemePreference): void {
  writeStoredPreference(STORAGE_KEYS.theme, preference);
}
