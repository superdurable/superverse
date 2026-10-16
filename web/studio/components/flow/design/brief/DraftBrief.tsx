// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Badge } from "@/components/ui/badge/Badge";
import { processName, stepName } from "@/lib/shared/format";
import { BulletList, Section } from "./BriefSections";

/** What a draft of the requirements card holds so far, as the agent writes it. */
export interface BriefDraft {
  title: string;
  summary: string;
  features: readonly { id: string; text: string }[];
  connections: readonly { need: string; connector: string }[];
  designSummary: string;
  flows: readonly { flowType: string; stepTypes: readonly string[] }[];
}

/**
 * The Brief while the agent writes its requirements card: the title, summary, features, apps and the names
 * of the designed Steps as they arrive, with no drawing, which appears once the card is recorded. Each
 * update replaces the whole draft.
 */
export function DraftBrief({ draft }: { draft: BriefDraft }) {
  const summaries = [draft.summary, draft.designSummary].filter(
    (text, index, all) => text && all.indexOf(text) === index,
  );
  return (
    <section
      aria-label="Brief"
      aria-busy
      className="@container/brief flex h-full min-h-0 min-w-0 flex-1 flex-col bg-panel"
      data-brief-state="drafting"
    >
      <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto overscroll-contain px-5 pt-4 pb-3">
        <header className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <h2 className="text-title font-semibold wrap-anywhere text-ink">{draft.title || "Your process"}</h2>
          <Badge tone="accent">Brief · drafting</Badge>
        </header>
        <div className="mt-3 grid max-w-[48rem] gap-4 text-body text-ink-dim">
          {summaries.map((text, index) => (
            <p className={index === 0 ? "text-reading wrap-anywhere text-ink" : "wrap-anywhere"} key={text}>
              {text}
            </p>
          ))}
          {draft.features.length > 0 && (
            <Section title="Features">
              <ol className="grid gap-1.5">
                {draft.features.map((feature, index) => (
                  <li className="flex gap-2" key={`${feature.id}:${index}`}>
                    <strong className="shrink-0 font-semibold text-ink">{feature.id}</strong>
                    <span className="wrap-anywhere">{feature.text}</span>
                  </li>
                ))}
              </ol>
            </Section>
          )}
          {draft.flows.length > 0 && (
            <Section title="Steps">
              {draft.flows.map((flow) => (
                <div className="grid gap-1" key={flow.flowType}>
                  <strong className="font-semibold text-ink">{processName(flow.flowType)}</strong>
                  <BulletList items={flow.stepTypes.map(stepName)} />
                </div>
              ))}
            </Section>
          )}
          {draft.connections.length > 0 && (
            <Section title="Apps">
              <BulletList
                items={draft.connections.map(({ need, connector }) => (connector ? `${connector}: ${need}` : need))}
              />
            </Section>
          )}
        </div>
      </div>
      <p className="border-t border-line-soft bg-panel px-5 py-3 text-caption text-ink-dim" role="status">
        The agent is writing this brief. The drawing appears when it records the card.
      </p>
    </section>
  );
}
