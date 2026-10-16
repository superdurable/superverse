// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge/Badge";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { connectorName } from "@/components/flow/canvas/layout/step-card";
import type { BriefCard, BriefLine, BriefProcess, BriefRule, StepWait } from "./brief-model";

const HEADING = "text-caption font-semibold tracking-wider text-ink-faint uppercase";

/** One part of the Brief under its heading; a region, so its heading names it. */
export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="grid content-start gap-2">
      <h3 className={HEADING}>{title}</h3>
      {children}
    </section>
  );
}

/** One short part of the summary grid, in a cell of its own. */
export function GridPart({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section
      aria-label={title}
      className="grid min-w-0 content-start gap-2 rounded-lg border border-line-faint bg-raised px-3 py-2.5"
    >
      <h3 className={HEADING}>{title}</h3>
      {children}
    </section>
  );
}

/** Lines that each lead with who or what they are about. */
export function LineList({ lines }: { lines: readonly BriefLine[] }) {
  return (
    <ul className="grid gap-2">
      {lines.map((line) => (
        <li className="grid gap-0.5 wrap-anywhere" key={line.key}>
          <strong className="font-semibold text-ink">{line.lead}</strong>
          {line.text && <span>{line.text}</span>}
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

const WAIT_BADGE: Record<StepWait, { tone: "busy" | "neutral"; text: string }> = {
  person: { tone: "busy", text: "A person acts" },
  message: { tone: "neutral", text: "Waits for a message" },
  time: { tone: "neutral", text: "Waits for a time" },
};

/** The Steps of each process in reading order, for a design the Brief does not draw. */
export function StepsPart({ processes }: { processes: readonly BriefProcess[] }) {
  return (
    <Section title="Steps">
      {processes.map((process) => (
        <div className="grid gap-1.5" key={process.flowType}>
          {processes.length > 1 && <h4 className="text-body font-semibold text-ink">{process.name}</h4>}
          <ol className="grid list-decimal gap-2 pl-5 marker:text-ink-faint">
            {process.steps.map((step) => (
              <li className="wrap-anywhere" key={step.key}>
                <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                  <strong className="font-semibold text-ink">{step.title}</strong>
                  {step.waits && <Badge tone={WAIT_BADGE[step.waits].tone}>{WAIT_BADGE[step.waits].text}</Badge>}
                </span>
                {step.explanation && <span className="block">{step.explanation}</span>}
              </li>
            ))}
          </ol>
        </div>
      ))}
    </Section>
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

/** The apps the card connects to, as chips of the names people know; whether each is new, what it is for and its operations behind More details. */
export function AppsPart({ connections }: { connections: BriefCard["connections"] }) {
  return (
    <GridPart title="Apps">
      <ul className="flex flex-wrap gap-1.5" aria-label="Apps">
        {connections.map((connection) => (
          <li key={`${connection.connector}:${connection.need}`}>
            <Badge className="text-body">{connectorName(connection.connector)}</Badge>
          </li>
        ))}
      </ul>
      <MoreDetails surface="brief.apps">
        <ul className="grid gap-2">
          {connections.map((connection) => (
            <li className="grid gap-0.5 wrap-anywhere" key={`${connection.connector}:${connection.need}`}>
              <span className="flex flex-wrap items-center gap-2">
                <strong className="font-semibold text-ink">{connectorName(connection.connector)}</strong>
                {connection.status === "new" ? (
                  <Badge tone="busy">New connection</Badge>
                ) : (
                  <Badge>Existing connection</Badge>
                )}
              </span>
              <span>{connection.need}</span>
              {connection.userSetup !== "" && (
                <span className="text-caption text-ink-faint">You set up: {connection.userSetup}</span>
              )}
              {connection.operations.length > 0 && (
                <span className="font-mono text-caption text-ink-faint">
                  {connection.connector}: {connection.operations.join(", ")}
                </span>
              )}
            </li>
          ))}
        </ul>
      </MoreDetails>
    </GridPart>
  );
}
