// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { buttonStyle } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";

/** The person views Live: the commit Live serves with its version name, and the way back to Preview. */
export type LiveViewing = { commit: string | null; version: string | null; previewHref: string };

/**
 * A change asked for while the person views Live. The view never switches by itself: once the agent
 * waits again with a commit pushed since the message was sent, the chat offers See it on Preview.
 * Leaving Live or following the offer drops it.
 */
export function useLiveChange(live: LiveViewing | undefined, headCommit: string, idle: boolean) {
  // The head commit when the message was sent.
  const [askedAt, setAskedAt] = useState<string | null>(null);
  if (askedAt !== null && !live) setAskedAt(null);
  const viewingLive = live !== undefined;
  const markSent = useCallback(() => {
    if (viewingLive) setAskedAt(headCommit);
  }, [headCommit, viewingLive]);
  return {
    markSent,
    offered: live !== undefined && askedAt !== null && idle && headCommit !== askedAt,
    dismiss: () => setAskedAt(null),
  };
}

/** After a change asked for on Live: it is built on Preview, and Live still serves its version. */
export function SeeOnPreview({ live, onFollow }: { live: LiveViewing; onFollow: () => void }) {
  return (
    <div
      className="flex flex-wrap items-center gap-2 rounded-[10px] border border-line-soft bg-panel px-2.5 py-2 text-[12.5px] text-ink-dim"
      data-testid="see-on-preview"
    >
      <Glyph name="spark" className="size-3.5 text-accent" />
      <span className="min-w-0 flex-1">
        The change is on Preview; Live still serves {live.version ?? "its version"}.
      </span>
      <Link href={live.previewHref} onClick={onFollow} className={buttonStyle("secondary", "sm")}>
        See it on Preview
      </Link>
    </div>
  );
}

/** Above the composer while the person views Live: changes are built on Preview, never on Live. */
export function LiveNote({ live }: { live: LiveViewing }) {
  return (
    <div
      className="mx-3 mt-2 flex items-start gap-2 rounded-[10px] border border-line-soft bg-panel px-2.5 py-2 text-[12.5px] leading-snug text-ink-dim"
      data-testid="live-note"
    >
      <span aria-hidden="true" className="mt-1 size-2 shrink-0 rounded-full bg-pass" />
      <span>
        You&apos;re viewing <b className="text-ink">Live{live.version ? ` ${live.version}` : ""}</b>. What you ask for
        is built on <b className="text-ink">Preview</b> and reaches Live only when someone publishes.{" "}
        <Link href={live.previewHref} className="text-accent hover:underline">
          Back to Preview
        </Link>
      </span>
    </div>
  );
}
