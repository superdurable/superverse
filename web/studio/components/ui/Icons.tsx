// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "./cx";

// Small inline glyphs; an icon font or package would be more than two glyphs need.

export function Spinner({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      className={cx("animate-spin motion-reduce:animate-none", className)}
      aria-hidden
    >
      <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.75" opacity="0.25" />
      <path d="M14 8a6 6 0 00-6-6" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  );
}
