// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo, type ReactNode } from "react";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { RunningMark } from "@/components/ui/wait/LongWait";
import type { FlowDesign } from "@/components/flow/design/design-graph";
import { DesignHeading } from "@/components/flow/design/DesignHeading";
import { designBrief, setupLines, type BriefCard } from "./brief-model";
import {
  BriefFoot,
  BulletList,
  ChangeLines,
  FeatureList,
  LineList,
  OutlineLines,
  RuleList,
  Section,
} from "./BriefSections";

export interface ProcessBriefProps {
  card: BriefCard;
  /** The agent builds from the card; or its last turn ended short of building it. */
  status: "building" | "stopped";
  /** The design's outline in Go names, which the card carries; absent for a card without a design. */
  outline?: string;
  /** The design's body; null while it loads, absent for a card without a design. */
  design?: FlowDesign | null;
  /** What the design changes against the current version; empty when nothing compares. */
  changes: readonly string[];
  /** The drawn design, which carries its own heading; absent for a card without a design. */
  drawing?: ReactNode;
  /** What the canvas says in place of a drawing, for a card without a design. */
  waiting?: string;
  /** A small line under the summary, such as how to answer a card an old Session waits at. */
  note?: string;
}

/**
 * What the agent builds, for the person it builds for, while its design is built: the drawn design fills the
 * Flow tab with the process title and its status on the canvas's corner, and under the canvas the agent's own
 * one-line summary of the card and one More details that holds the rest in this order, each part only when it
 * has content: what the person sets up, who decides, what starts it, the rules, the features, how it is
 * accepted, and behind a nested More details the Go names with what the design changes.
 */
export function ProcessBrief({
  card,
  status,
  outline = "",
  design,
  changes,
  drawing,
  waiting,
  note,
}: ProcessBriefProps) {
  const brief = useMemo(() => (design ? designBrief(design) : null), [design]);
  const failures = brief?.failureSteps ?? 0;
  const setup = setupLines(card);
  const parts = [
    (setup.length > 0 || card.userSetup.length > 0) && (
      <Section title="What you'll set up" key="setup">
        {setup.length > 0 && <LineList lines={setup} />}
        {card.userSetup.length > 0 && <BulletList items={card.userSetup} />}
      </Section>
    ),
    brief && brief.people.length > 0 && (
      <Section title="Who decides" key="people">
        <LineList lines={brief.people} />
      </Section>
    ),
    brief && brief.startsWhen.length > 0 && (
      <Section title="Starts when" key="start">
        <LineList lines={brief.startsWhen} />
      </Section>
    ),
    brief && (brief.rules.length > 0 || failures > 0) && (
      <Section title="Rules and exceptions" key="rules">
        <RuleList rules={brief.rules} failures={failures} />
      </Section>
    ),
    card.features.length > 0 && (
      <Section title="What it must do" key="features">
        <FeatureList features={card.features} />
      </Section>
    ),
    card.acceptance.length > 0 && (
      <Section title="How it's accepted" key="acceptance">
        <BulletList items={card.acceptance} />
      </Section>
    ),
    (outline !== "" || changes.length > 0) && (
      <MoreDetails surface="brief.code" hint="code names" key="code">
        <div className="grid gap-3.5 py-1">
          {outline !== "" && <OutlineLines outline={outline} />}
          {changes.length > 0 && <ChangeLines changes={changes} />}
        </div>
      </MoreDetails>
    ),
  ].filter(Boolean);
  const stopped = status === "stopped";
  return (
    <section
      aria-label="Brief"
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-panel"
      data-brief-state={status}
    >
      {drawing ? (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">{drawing}</div>
      ) : (
        <div className="relative flex min-h-0 flex-1 items-center justify-center bg-canvas px-6 pt-14 pb-6">
          <DesignHeading
            title={card.title}
            status={stopped ? "Not built yet" : "Building"}
            tone={stopped ? "busy" : "accent"}
          />
          <div className="flex flex-col items-center gap-4">
            {/* Building takes minutes; a card that stopped waits on the person, so its mark stays still in place. */}
            <RunningMark size="page" still={stopped} />
            <p
              className="max-w-[36rem] text-center text-body text-ink-dim"
              role="status"
              data-card-brief-state={stopped ? "stalled" : "building"}
            >
              {waiting}
            </p>
          </div>
        </div>
      )}
      <BriefFoot summary={card.summary} more={parts.length > 0 ? parts : null} note={note} />
    </section>
  );
}
