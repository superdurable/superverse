// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo, type ReactNode } from "react";
import { Badge } from "@/components/ui/badge/Badge";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { cx } from "@/components/ui/cx";
import type { FlowDesign } from "@/components/flow/design/design-graph";
import { designBrief, type BriefCard } from "./brief-model";
import { BriefActions, type BriefActionsProps } from "./BriefActions";
import {
  AppsPart,
  BulletList,
  ChangeLines,
  GridPart,
  LineList,
  OutlineLines,
  RuleList,
  Section,
  StepsPart,
} from "./BriefSections";

export interface ProcessBriefProps {
  card: BriefCard;
  /** The version of the requirements the card is, which the agent recorded when it showed the card. */
  version: number;
  /**
   * The agent builds from the card, or built its design and the processes are being built from it; or its
   * last turn ended short of building it, which the Brief's host says.
   */
  status: "building" | "built" | "stopped";
  /** The design's summary and outline, which the card carries; absent for a card without a design. */
  designSummary?: string;
  outline?: string;
  /** The design's body; null while it loads, absent for a card without a design. */
  design?: FlowDesign | null;
  /** What the design changes against the current version; empty when nothing compares. */
  changes: readonly string[];
  /** The drawn design under the summary; absent for a card without a design. */
  drawing?: ReactNode;
  /** The Brief's foot; absent when it offers nothing to do. */
  actions?: BriefActionsProps;
  /** A line in place of the actions, such as who sends a change on the mock model. */
  note?: string;
}

/** Wide, the summary grid has three columns, then two, then one as its container narrows. */
const GRID = cx("grid grid-cols-1 gap-2.5 @min-[30rem]/brief:grid-cols-2 @min-[46rem]/brief:grid-cols-3");

/**
 * What the agent builds, in business words, for the person it builds for: a header, a grid of short parts
 * (what starts it, who acts, the rules, the apps) and the drawn design below it, read left to right. What it
 * does, what to prepare, when it is done, the features and the Go names wait behind More details. Each part
 * shows only when it has content.
 */
export function ProcessBrief({
  card,
  version,
  status,
  designSummary,
  outline = "",
  design,
  changes,
  drawing,
  actions,
  note,
}: ProcessBriefProps) {
  const brief = useMemo(() => (design ? designBrief(design) : null), [design]);
  const failures = brief?.failureSteps ?? 0;
  const summaries = [card.summary, designSummary].filter((text, index, all) => text && all.indexOf(text) === index);
  const more = [
    summaries.length > 0 && "what it does",
    card.userSetup.length > 0 && "to prepare",
    card.acceptance.length > 0 && "done when",
    card.features.length > 0 && `${card.features.length} feature${card.features.length === 1 ? "" : "s"}`,
    changes.length > 0 && "changes",
    outline !== "" && "outline",
  ].filter((part): part is string => part !== false);
  return (
    <section
      aria-label="Brief"
      aria-busy={actions?.submitting ?? false}
      className="@container/brief flex h-full min-h-0 min-w-0 flex-1 flex-col bg-panel"
    >
      {/* With a drawing the summary keeps to the top and scrolls on its own, so the drawing always shows. */}
      <div
        className={cx(
          "min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain px-5 pt-4 pb-3",
          drawing ? "max-h-[55%] shrink-0" : "flex-1",
        )}
      >
        <header className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <h2 className="text-title font-semibold wrap-anywhere text-ink">{card.title}</h2>
          {status === "building" && <Badge tone="accent">Building from this brief · version {String(version)}</Badge>}
          {status === "built" && <Badge tone="pass">Built from this brief · version {String(version)}</Badge>}
          {status === "stopped" && (
            <span className="text-caption text-ink-faint">Brief · version {String(version)}</span>
          )}
        </header>
        <div className={cx(GRID, "mt-3 text-body text-ink-dim")}>
          {brief && brief.startsWhen.length > 0 && (
            <GridPart title="Starts when">
              <LineList lines={brief.startsWhen} />
            </GridPart>
          )}
          {brief && brief.people.length > 0 && (
            <GridPart title="People">
              <LineList lines={brief.people} />
            </GridPart>
          )}
          {brief && (brief.rules.length > 0 || failures > 0) && (
            <GridPart title="Rules and exceptions">
              <RuleList rules={brief.rules} failures={failures} />
            </GridPart>
          )}
          {card.connections.length > 0 && <AppsPart connections={card.connections} />}
          {/* "I assumed" and "Needs you · N" take the cells after Apps once the card carries them. */}
        </div>
        {brief && !drawing && (
          <div className="mt-4 text-body text-ink-dim">
            <StepsPart processes={brief.processes} />
          </div>
        )}
        {more.length > 0 && (
          <MoreDetails surface="brief.more" hint={more.join(" · ")} className="mt-3">
            <div className="grid max-w-[48rem] gap-4 py-1 text-body text-ink-dim">
              {summaries.length > 0 && (
                <Section title="What it does">
                  {summaries.map((text, index) => (
                    <p className={cx("wrap-anywhere", index === 0 && "text-reading text-ink")} key={text}>
                      {text}
                    </p>
                  ))}
                </Section>
              )}
              {card.userSetup.length > 0 && (
                <Section title="You need to prepare">
                  <BulletList items={card.userSetup} />
                </Section>
              )}
              {card.acceptance.length > 0 && (
                <Section title="Done when">
                  <BulletList items={card.acceptance} />
                </Section>
              )}
              {card.features.length > 0 && (
                <Section title="Features">
                  <ol className="grid gap-1.5">
                    {card.features.map((feature) => (
                      <li className="flex gap-2" key={feature.id}>
                        <strong className="shrink-0 font-semibold text-ink">{feature.id}</strong>
                        <span className="wrap-anywhere">{feature.text}</span>
                      </li>
                    ))}
                  </ol>
                </Section>
              )}
              {changes.length > 0 && <ChangeLines changes={changes} />}
              {outline !== "" && <OutlineLines outline={outline} />}
            </div>
          </MoreDetails>
        )}
      </div>
      {drawing && <div className="flex min-h-48 min-w-0 flex-1 flex-col border-t border-line-soft">{drawing}</div>}
      {actions ? (
        <BriefActions {...actions} />
      ) : (
        note && <p className="border-t border-line-soft bg-panel px-5 py-3 text-caption text-ink-dim">{note}</p>
      )}
    </section>
  );
}
