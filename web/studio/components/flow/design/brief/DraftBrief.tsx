// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { DesignHeading } from "@/components/flow/design/DesignHeading";
import { processName, stepName } from "@/lib/shared/format";
import { RunningMark } from "@/components/ui/wait/LongWait";
import { BriefFoot, FeatureList, Section } from "./BriefSections";

/** What a draft of the requirements card holds so far, as the agent writes it. */
export interface BriefDraft {
  title: string;
  summary: string;
  features: readonly { id: string; text: string }[];
  flows: readonly { flowType: string; stepTypes: readonly string[]; failureHandlingStepTypes: readonly string[] }[];
}

function mainStorySteps({ stepTypes, failureHandlingStepTypes }: BriefDraft["flows"][number]): string[] {
  const failureHandling = new Set(failureHandlingStepTypes);
  return stepTypes.filter((stepType) => !failureHandling.has(stepType));
}

/**
 * The names of the designed main-story Steps written so far, per process, as a plain row of dashed boxes: like
 * the drawn design, the row leaves out the Steps only failure routes reach.
 */
function DraftedSteps({ flows }: { flows: BriefDraft["flows"] }) {
  // Until the first Step streams in the canvas is empty, so the running mark holds it; the boxes take over after.
  if (flows.length === 0)
    return (
      // No status region: the brief is aria-busy while it drafts, and the words are already there.
      <div className="m-auto flex flex-col items-center gap-3 text-center">
        <RunningMark size="page" />
        <p className="text-body text-ink-faint">The steps show here as the agent writes them.</p>
      </div>
    );
  return (
    <section aria-label="Drafted steps" className="m-auto grid w-full max-w-[64rem] gap-5">
      {flows.map((flow) => (
        <div className="grid gap-2" key={flow.flowType}>
          {flows.length > 1 && (
            <h3 className="text-center text-caption font-semibold text-ink-dim">{processName(flow.flowType)}</h3>
          )}
          <ol aria-label={processName(flow.flowType)} className="flex flex-wrap items-center justify-center gap-2">
            {mainStorySteps(flow).map((stepType, index) => (
              <li className="flex items-center gap-2" key={`${stepType}:${index}`}>
                {index > 0 && (
                  <span aria-hidden="true" className="text-ink-faint">
                    →
                  </span>
                )}
                <span className="rounded-lg border border-dashed border-line bg-panel px-3 py-2 text-body text-ink-dim">
                  {stepName(stepType)}
                </span>
              </li>
            ))}
          </ol>
        </div>
      ))}
    </section>
  );
}

/**
 * The Flow tab while the agent writes its requirements card, before it is recorded: the canvas area shows the
 * title with "Drafting" on its corner and the names of the designed Steps as they arrive, and the summary line
 * under it fills in, with the features behind More details. Each update replaces the whole draft; the drawn
 * design takes the place of the boxes once the card is recorded.
 */
export function DraftBrief({ draft }: { draft: BriefDraft }) {
  return (
    <section
      aria-label="Brief"
      aria-busy
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-panel"
      data-brief-state="drafting"
    >
      <div className="relative flex min-h-0 flex-1 overflow-y-auto bg-canvas px-6 pt-14 pb-6">
        <DesignHeading title={draft.title || "Your process"} status="Drafting" />
        <DraftedSteps flows={draft.flows} />
      </div>
      <BriefFoot
        summary={draft.summary}
        pending="The agent is writing the summary…"
        more={
          draft.features.length > 0 ? (
            <Section title="What it must do">
              <FeatureList features={draft.features} />
            </Section>
          ) : null
        }
      />
    </section>
  );
}
