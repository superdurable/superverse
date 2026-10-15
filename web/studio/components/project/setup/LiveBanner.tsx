// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

/**
 * The Live tab's banner over Setup's table: that these are Live's settings and the version Live runs. What
 * waits for a publish is under Live's name in the table; the bar's Publish publishes it. It is dark, without
 * a coloured frame.
 */
export function LiveBanner({ version }: { /** The version Live runs, "V12". */ version: string }) {
  return (
    <section
      aria-label="You're changing Live"
      className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-t-[10px] bg-ink px-3.5 py-2.25 text-[13px] text-panel"
      data-testid="live-banner"
    >
      <b className="font-semibold">You&apos;re changing Live</b>
      <span className="opacity-75" data-testid="live-banner-version">
        {version}
      </span>
    </section>
  );
}
