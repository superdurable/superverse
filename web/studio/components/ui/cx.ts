// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Joins class lists and drops the falsy ones. Conditional classes are separate arguments, never
 * concatenated strings: the Prettier Tailwind plugin sorts each literal passed here and once
 * trimmed the separating space inside a concatenated literal, gluing two classes together.
 */
export function cx(...classes: (string | false | null | undefined)[]): string {
  return classes.filter(Boolean).join(" ");
}
