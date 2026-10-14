// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The Dex mark, Branch D: the D drawn as a small workflow graph. Start and end nodes sit on the
 * stem, edges trace the outline, and the node on the bowl is the step running now.
 *
 * This is the small cut (32px and below: heavier edges, larger nodes) of the canonical set in
 * dex-ui `logo/branch-d`; copy its geometry from there rather than redrawing it. The two fills are
 * custom properties, so the mark follows the theme and the head is the same token as the primary
 * button.
 */
export function DexMark({ size = 28 }: { size?: number }) {
  return (
    <svg aria-hidden="true" focusable="false" height={size} viewBox="0 0 64 64" width={size}>
      <path
        d="M 17.25 16 V 48 M 17.25 16 H 29.25 A 16 16 0 0 1 45.25 32 A 16 16 0 0 1 29.25 48 H 17.25"
        fill="none"
        stroke="var(--brand-track)"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="8"
      />
      <circle cx="17.25" cy="16" fill="var(--brand-track)" r="8" />
      <circle cx="17.25" cy="48" fill="var(--brand-track)" r="8" />
      <circle cx="45.25" cy="32" fill="var(--brand-head)" r="10" />
    </svg>
  );
}
