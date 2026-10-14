// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

/**
 * The Live tab's banner over Setup's table: that these are Live's settings, the version Live runs, how
 * many settings wait for a publish and the way to publish them, or to publish Live's version again when
 * nothing waits. It is dark, without a coloured frame.
 */
export function LiveBanner({
  version,
  waiting,
  onPublish,
}: {
  /** The version Live runs, "V12". */
  version: string;
  waiting: number;
  /** Opens the Publish popover on Live's own version with these settings; null where nothing opens it. */
  onPublish: (() => void) | null;
}) {
  return (
    <section
      aria-label="You're changing Live"
      className="flex max-w-240 flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-t-[10px] bg-ink px-3.5 py-2.25 text-[13px] text-panel"
      data-testid="live-banner"
    >
      <b className="font-semibold">You&apos;re changing Live</b>
      <span className="opacity-75" data-testid="live-banner-version">
        {version}
      </span>
      <span className="ml-auto" data-testid="live-waiting-count">
        {waiting > 0 ? `${waiting} ${waiting === 1 ? "change" : "changes"} waiting` : "Nothing waiting for a publish"}
      </span>
      {onPublish && (
        <button
          type="button"
          onClick={onPublish}
          className="inline-flex h-7 items-center rounded-[7px] border border-panel bg-panel px-2.5 text-[12.5px] font-medium text-ink hover:bg-band-0"
          data-testid="change-live-configuration"
        >
          {waiting === 0
            ? `Publish ${version} again…`
            : waiting === 1
              ? "Publish this change…"
              : "Publish these changes…"}
        </button>
      )}
    </section>
  );
}
