// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useState } from "react";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import type { Requirements, RequirementsDecision } from "@/lib/session/agent-types";
import { RequirementsSpec, type DesignChanges } from "./RequirementsSpec";

export interface RequirementsCardProps {
  requirements: Requirements;
  /** The version a confirmation will have. */
  version: number;
  disabled?: boolean;
  isSubmitting?: boolean;
  /** Set when another control decides the card, which the card then names instead of its controls. */
  answeredBy?: string;
  /**
   * What its Flow design changes and the Flow tab whose Process Brief draws it, which the card's host knows.
   * With that Brief the card is only a pointer to it, so the decision has one place.
   */
  designChanges?: DesignChanges;
  onDecide: (decision: RequirementsDecision) => void;
}

/**
 * The card sits in the pending-action dock like a question batch: one grid whose middle row scrolls,
 * so a long card stays above its two actions.
 */
const CARD = cx(
  "grid max-h-[calc(min(68dvh,44rem)-1.5rem)] min-h-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden",
  "rounded-[10px] border border-busy-rule bg-busy-subtle p-2.5 text-[13px] text-ink-dim",
  "max-md:max-h-[calc(min(72dvh,36rem)-1.5rem)]",
);

/** In the dock while the Process Brief holds the decision: what waits, and the way to the Brief. */
function BriefPointer({
  requirements,
  version,
  isSubmitting,
  designChanges,
}: Pick<RequirementsCardProps, "requirements" | "version" | "isSubmitting"> & { designChanges: DesignChanges }) {
  return (
    <section
      className="grid gap-1 rounded-[10px] border border-busy-rule bg-busy-subtle p-2.5 text-body text-ink-dim"
      aria-label="Requirements to confirm"
      aria-busy={isSubmitting}
    >
      <Eyebrow>Waiting for you</Eyebrow>
      <strong className="font-semibold wrap-anywhere text-ink">
        {requirements.title} · version {String(version)}
      </strong>
      <p className="line-clamp-2 wrap-anywhere">{requirements.summary}</p>
      {designChanges.href ? (
        <Link className={cx(buttonStyle("accent", "sm"), "mt-1 justify-self-start")} href={designChanges.href}>
          Open the brief
        </Link>
      ) : (
        <p className="mt-1 text-caption text-ink-faint" role="status">
          Preparing the brief…
        </p>
      )}
    </section>
  );
}

/**
 * What the Agent will build, for the one confirmation before it writes code: the user confirms it, or
 * asks for changes in text, which the Agent answers with a revised card. A card whose Flow design the
 * Flow tab draws is decided in its Process Brief there, and the card points to it; another card keeps
 * its own controls.
 */
export function RequirementsCard({
  requirements,
  version,
  disabled = false,
  isSubmitting = false,
  answeredBy,
  designChanges,
  onDecide,
}: RequirementsCardProps) {
  const [changes, setChanges] = useState("");
  const requestedChanges = changes.trim();
  const requestChanges = () => {
    if (!disabled && requestedChanges !== "") onDecide({ isConfirmed: false, requestedChanges });
  };
  if (designChanges?.href || (requirements.design && designChanges?.loading))
    return (
      <BriefPointer
        requirements={requirements}
        version={version}
        isSubmitting={isSubmitting}
        designChanges={designChanges}
      />
    );
  return (
    <section className={CARD} aria-label="Requirements to confirm" aria-busy={isSubmitting}>
      <div>
        <Eyebrow>Confirm what the agent will build</Eyebrow>
        <strong className="font-semibold wrap-anywhere text-ink">
          {requirements.title} · version {String(version)}
        </strong>
      </div>
      <div className="my-3 min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain px-0.5">
        <RequirementsSpec spec={requirements} designChanges={designChanges} />
      </div>
      {answeredBy ? (
        <p className="-mx-2.5 -mb-2.5 border-t border-busy-rule bg-panel p-2.5 text-xs">{answeredBy}</p>
      ) : (
        <div className="-mx-2.5 -mb-2.5 grid gap-2 border-t border-busy-rule bg-panel p-2.5">
          <label className="grid gap-1 text-xs font-semibold">
            Changes you want
            <textarea
              className="min-h-14 w-full resize-y rounded-lg border border-line bg-panel px-3 py-2 text-[13px] font-normal text-ink placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              placeholder="Write what should change, then choose Request changes…"
              value={changes}
              disabled={disabled}
              onChange={(event) => {
                setChanges(event.target.value);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) requestChanges();
              }}
            />
          </label>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button disabled={disabled || requestedChanges === ""} onClick={requestChanges}>
              Request changes
            </Button>
            <Button
              variant="primary"
              disabled={disabled}
              onClick={() => {
                onDecide({ isConfirmed: true, requestedChanges: "" });
              }}
            >
              Confirm and build
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
