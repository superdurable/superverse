// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import type { BriefCard, BriefLine, BriefRule } from "./brief-model";

const HEADING = "text-caption font-semibold tracking-wider text-ink-faint uppercase";

/** One part of the Brief under its heading; a region, so its heading names it. */
export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="grid content-start gap-1.5">
      <h3 className={HEADING}>{title}</h3>
      {children}
    </section>
  );
}

/** Lines that each lead with who or what they are about. */
export function LineList({ lines }: { lines: readonly BriefLine[] }) {
  return (
    <ul className="grid gap-1.5">
      {lines.map((line) => (
        <li className="wrap-anywhere" key={line.key}>
          <strong className="font-semibold text-ink">{line.lead}</strong>
          {line.text && <span> · {line.text}</span>}
        </li>
      ))}
    </ul>
  );
}

/** Each condition of an ordinary way out with where it leads, then the count of failure-handling Steps. */
export function RuleList({ rules, failures }: { rules: readonly BriefRule[]; failures: number }) {
  return (
    <>
      {rules.length > 0 && (
        <ul className="grid gap-1">
          {rules.map((rule) => (
            <li className="wrap-anywhere" key={rule.key}>
              {rule.when} <span className="text-ink-faint">→</span>{" "}
              <strong className="font-semibold text-ink">{rule.to}</strong>
            </li>
          ))}
        </ul>
      )}
      {failures > 0 && (
        <p className="text-caption text-ink-faint">
          +{failures} failure-handling step{failures === 1 ? "" : "s"}
        </p>
      )}
    </>
  );
}

export function BulletList({ items }: { items: readonly string[] }) {
  return (
    <ul className="grid list-disc gap-1 pl-5">
      {items.map((item) => (
        <li className="wrap-anywhere" key={item}>
          {item}
        </li>
      ))}
    </ul>
  );
}

/** The features, each under its ID. */
export function FeatureList({ features }: { features: BriefCard["features"] }) {
  return (
    <ol className="grid gap-1.5">
      {features.map((feature, index) => (
        <li className="flex gap-2" key={`${feature.id}:${index}`}>
          <strong className="shrink-0 font-semibold text-ink">{feature.id}</strong>
          <span className="wrap-anywhere">{feature.text}</span>
        </li>
      ))}
    </ol>
  );
}

/** What the design changes against the current version, one line per Flow. */
export function ChangeLines({ changes }: { changes: readonly string[] }) {
  return (
    <Section title="Compared with the current version">
      <ul className="grid gap-1" aria-label="What the design changes">
        {changes.map((line) => (
          <li className="wrap-anywhere" key={line}>
            {line}
          </li>
        ))}
      </ul>
    </Section>
  );
}

/** The design's outline in Go names. */
export function OutlineLines({ outline }: { outline: string }) {
  return (
    <Section title="Outline">
      <ul className="grid gap-1 font-mono text-caption text-ink-dim">
        {outline.split("\n").map((line) => (
          <li className="wrap-anywhere" key={line}>
            {line}
          </li>
        ))}
      </ul>
    </Section>
  );
}

/**
 * Under the canvas: the agent's own one-line summary of the card, or `pending` until it arrives, one More
 * details with the rest (`more`, absent when there is nothing more) and an optional small note. It keeps to
 * the lower part and scrolls on its own, so the drawing always shows.
 */
export function BriefFoot({
  summary,
  pending,
  more,
  note,
}: {
  summary: string;
  pending?: string;
  more: ReactNode;
  note?: string;
}) {
  return (
    <div className="grid max-h-[45%] min-h-0 shrink-0 content-start gap-1 overflow-x-hidden overflow-y-auto overscroll-contain border-t border-line-soft bg-panel px-4 py-2.5">
      {summary ? (
        <p className="text-body wrap-anywhere text-ink" data-testid="brief-summary">
          {summary}
        </p>
      ) : (
        pending && <p className="text-body text-ink-faint">{pending}</p>
      )}
      {more && (
        <MoreDetails surface="brief.more">
          <div className="grid max-w-[48rem] gap-3.5 py-1 text-body text-ink-dim">{more}</div>
        </MoreDetails>
      )}
      {note && <p className="text-caption text-ink-faint">{note}</p>}
    </div>
  );
}
