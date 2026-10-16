// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The Dex mark, Pipe D: the pipe operator |> set as a D, "pass this result to the next step".
 * The bar is what has run; the chevron is the hand-off to the next step.
 *
 * This is the small cut (32px and below: heavier strokes) of the canonical set in dex-ui
 * `logo/pipe-d`; copy its geometry from there rather than redrawing it. The two strokes are
 * custom properties, so the mark follows the theme and the chevron is the same token as the
 * primary button.
 */
export function DexMark({ size = 28 }: { size?: number }) {
  return (
    <svg aria-hidden="true" focusable="false" height={size} viewBox="0 0 64 64" width={size}>
      <path d="M 13.5 12 V 52" fill="none" stroke="var(--brand-track)" strokeLinecap="round" strokeWidth="12" />
      <path
        d="M 30.5 12 L 50.5 32 L 30.5 52"
        fill="none"
        stroke="var(--brand-head)"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="12"
      />
    </svg>
  );
}
