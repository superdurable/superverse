// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactElement } from "react";
import { cx } from "./cx";

/*
 * The line icons of the Builder frame: one 24-unit grid, a 1.6 stroke in the text colour, drawn as
 * paths so no platform substitutes an emoji or tofu. A glyph is decoration: the control that holds
 * it carries the name (aria-label or visible text).
 */
const PATHS = {
  home: <path d="M3.5 10.5 12 3.5l8.5 7V20h-5.5v-5.5h-6V20H3.5z" />,
  plus: <path d="M12 5v14M5 12h14" />,
  plusCircle: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 8.5v7M8.5 12h7" />
    </>
  ),
  minusCircle: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M8.5 12h7" />
    </>
  ),
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m20 20-4.2-4.2" />
    </>
  ),
  flow: (
    <>
      <rect x="3" y="4" width="6.5" height="5" rx="1.2" />
      <rect x="14.5" y="4" width="6.5" height="5" rx="1.2" />
      <rect x="8.8" y="15" width="6.5" height="5" rx="1.2" />
      <path d="M9.5 6.5h5M6.3 9v3h5.7v3M17.8 9v3H12" />
    </>
  ),
  chat: <path d="M3.5 4.5h17v11.5H11l-5 4v-4H3.5z" />,
  runs: (
    <>
      <path d="M4 6h9M4 12h6M4 18h9" />
      <path d="m15.5 9 5 3-5 3z" />
    </>
  ),
  inbox: (
    <>
      <path d="M3 13h5l1.5 2.5h5L16 13h5" />
      <path d="M5.5 5h13L21 13v6H3v-6z" />
    </>
  ),
  window: (
    <>
      <rect x="3" y="4.5" width="18" height="15" rx="2" />
      <path d="M3 9h18" />
    </>
  ),
  tools: <path d="M14.7 6.3a4 4 0 0 0-5.2 5.2l-6 6 2.9 2.9 6-6a4 4 0 0 0 5.2-5.2l-2.4 2.4-2.6-.4-.4-2.6z" />,
  upload: (
    <>
      <path d="M12 15.5V4M7.5 8.5 12 4l4.5 4.5" />
      <path d="M4.5 14.5V19a1 1 0 0 0 1 1h13a1 1 0 0 0 1-1v-4.5" />
    </>
  ),
  link: (
    <>
      <path d="M10 14a4 4 0 0 0 5.7 0l3-3A4 4 0 0 0 13 5.3l-1 1" />
      <path d="M14 10a4 4 0 0 0-5.7 0l-3 3A4 4 0 0 0 11 18.7l1-1" />
    </>
  ),
  chevronDown: <path d="m6.5 9.5 5.5 5.5 5.5-5.5" />,
  chevronRight: <path d="m9.5 6 6 6-6 6" />,
  chevronLeft: <path d="m14.5 6-6 6 6 6" />,
  key: (
    <>
      <circle cx="8" cy="15" r="4" />
      <path d="m11 12 8.5-8.5M16 7l2.5 2.5" />
    </>
  ),
  more: (
    <>
      <circle cx="12" cy="5.5" r=".9" />
      <circle cx="12" cy="12" r=".9" />
      <circle cx="12" cy="18.5" r=".9" />
    </>
  ),
  plug: (
    <>
      <path d="M9 3.5V8M15 3.5V8M6.5 8h11v3a5.5 5.5 0 0 1-11 0z" />
      <path d="M12 16.5v4" />
    </>
  ),
  shield: <path d="M12 3.5 5 6.3v5.5c0 4.2 3 7.4 7 8.7 4-1.3 7-4.5 7-8.7V6.3z" />,
  sliders: (
    <>
      <path d="M4 6.5h9M17 6.5h3M4 12h3M11 12h9M4 17.5h11M19 17.5h1" />
      <circle cx="15" cy="6.5" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="17.5" r="2" />
    </>
  ),
  close: <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" />,
  external: (
    <>
      <path d="M14 4h6v6M20 4l-9 9" />
      <path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </>
  ),
  check: <path d="m5 12.5 4.5 4.5L19 7.5" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5v5.5" />
      <circle cx="12" cy="16.3" r=".6" />
    </>
  ),
  spark: (
    <path d="M12 3.5v4M12 16.5v4M3.5 12h4M16.5 12h4M6.2 6.2l2.6 2.6M15.2 15.2l2.6 2.6M6.2 17.8l2.6-2.6M15.2 8.8l2.6-2.6" />
  ),
  folder: <path d="M3.5 7V18a1 1 0 0 0 1 1h15a1 1 0 0 0 1-1V9a1 1 0 0 0-1-1h-7.5l-2-2.5h-5.5a1 1 0 0 0-1 1z" />,
  person: (
    <>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M5 20c1-3.8 3.7-6 7-6s6 2.2 7 6" />
    </>
  ),
  door: (
    <>
      <path d="M14 4h4a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4" />
      <path d="M4 12h10M10 8l4 4-4 4" />
    </>
  ),
  cpu: (
    <>
      <rect x="6" y="6" width="12" height="12" rx="2" />
      <path d="M9.5 2.8v3M14.5 2.8v3M9.5 18.2v3M14.5 18.2v3M2.8 9.5h3M2.8 14.5h3M18.2 9.5h3M18.2 14.5h3" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  /** A wait the analyser could not name: the clock's circle, broken, with no hands. */
  wait: (
    <>
      <circle cx="12" cy="12" r="8.5" strokeDasharray="3 2.4" />
      <path d="M12 8v4.5" />
    </>
  ),
  /** Where a Flow begins, as n8n and Zapier mark a trigger. */
  bolt: <path d="M13 3 5 13.5h6L10 21l8-10.5h-6z" />,
  child: (
    <>
      <rect x="3.5" y="4" width="8" height="6" rx="1.2" />
      <rect x="12.5" y="14" width="8" height="6" rx="1.2" />
      <path d="M7.5 10v7h5" />
    </>
  ),
  eye: (
    <>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z" />
      <circle cx="12" cy="12" r="2.8" />
    </>
  ),
  arrowUp: <path d="M12 19V5.5M6.5 11 12 5.5 17.5 11" />,
  arrowDown: <path d="M12 5v13.5M6.5 13l5.5 5.5 5.5-5.5" />,
  mic: (
    <>
      <rect x="9" y="3.5" width="6" height="11" rx="3" />
      <path d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v2.5" />
    </>
  ),
  pick: (
    <>
      <path d="M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M4 15v4a1 1 0 0 0 1 1h4" strokeDasharray="2.2 2.2" />
      <path d="m12.5 12.5 7.5 2.7-3.2 1.1-1.1 3.2z" />
    </>
  ),
  pin: (
    <>
      <path d="M9 3.5h6l-1 6 3 3H7l3-3z" />
      <path d="M12 12.5V20" />
    </>
  ),
  refresh: (
    <>
      <path d="M19.5 11a7.5 7.5 0 1 0-2.2 5.3" />
      <path d="M19.5 4.5V11H13" />
    </>
  ),
  logout: (
    <>
      <path d="M10 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h5" />
      <path d="M15 8l4 4-4 4M19 12H9" />
    </>
  ),
  users: (
    <>
      <circle cx="9" cy="8.5" r="3" />
      <path d="M3.5 19c.8-3.2 3-5 5.5-5s4.7 1.8 5.5 5" />
      <path d="M15.5 5.8a3 3 0 0 1 0 5.4M17.5 14.4c1.5.7 2.6 2.2 3 4.6" />
    </>
  ),
  stop: <rect x="6.5" y="6.5" width="11" height="11" rx="1.5" />,
  play: <path d="M8 5.5v13l10.5-6.5z" />,
} satisfies Record<string, ReactElement>;

export type GlyphName = keyof typeof PATHS;

export function Glyph({ name, className }: { name: GlyphName; className?: string }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={cx("size-4 shrink-0", className)}
    >
      {PATHS[name]}
    </svg>
  );
}
