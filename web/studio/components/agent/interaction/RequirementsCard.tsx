// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import type { Requirements } from "@/lib/session/agent-types";
import { RequirementsSpec } from "./RequirementsSpec";

export interface RequirementsCardProps {
  requirements: Requirements;
  /** The version the card shows. */
  version: number;
}

/**
 * The card sits in the pending-action dock like a question batch: one grid whose middle row scrolls,
 * so a long card stays above its note.
 */
const CARD = cx(
  "grid max-h-[calc(min(68dvh,44rem)-1.5rem)] min-h-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden",
  "rounded-[10px] border border-busy-rule bg-busy-subtle p-2.5 text-[13px] text-ink-dim",
  "max-md:max-h-[calc(min(72dvh,36rem)-1.5rem)]",
);

/**
 * A requirements card that a Session still waits at from before cards stopped waiting, read only: the
 * person's next chat message is the reply that lets the agent build it or change it. A card the agent shows
 * now never waits; it is recorded as the current version, which the Process Brief shows.
 */
export function RequirementsCard({ requirements, version }: RequirementsCardProps) {
  return (
    <section className={CARD} aria-label="Requirements waiting for a reply">
      <div>
        <Eyebrow>What the agent will build</Eyebrow>
        <strong className="font-semibold wrap-anywhere text-ink">
          {requirements.title} · version {String(version)}
        </strong>
      </div>
      <div className="my-3 min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain px-0.5">
        <RequirementsSpec spec={requirements} />
      </div>
      <p className="-mx-2.5 -mb-2.5 border-t border-busy-rule bg-panel p-2.5 text-xs" role="status">
        Reply in the chat to build or change it.
      </p>
    </section>
  );
}
