// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState, type JSX } from "react";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { processName } from "@/lib/shared/format";
import { answeredBy, stepLabel, type FlowModel, type StepModel } from "@/components/flow/canvas/model/flow-model";
import { stepTitle } from "@/components/flow/canvas/layout/step-card";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { stepWords, type StepWords } from "./step-words";

const HEADING = cx("mt-3 mb-1 text-caption font-semibold tracking-wider text-ink-faint uppercase first:mt-0");
const PROSE = cx("text-reading text-ink-dim");
const NOTE = cx("mt-2 text-body text-ink-faint");

/**
 * The selected Step's definition: what it does, what it waits for and what happens next, in words,
 * with the engine's terms and full guard text behind More details. The run view explains a Step in
 * its own drawer instead.
 */

interface BranchRow {
  label: string;
  value: string;
  /** Raw guard text that must not be truncated. */
  pre: boolean;
}

export interface StepDefinition {
  title: string;
  stepType: string;
  words: StepWords;
  waitFor: string | null;
  waitConditions: string[];
  /** RPCs that publish to a Channel this Step waits on. */
  answeredBy: string[];
  branches: BranchRow[];
}

export function stepDefinition(flow: FlowModel, step: StepModel): StepDefinition {
  return {
    title: stepTitle(step),
    stepType: step.stepType,
    words: stepWords(flow, step),
    waitFor: step.waitFor === null ? null : `${step.waitFor.type} · ${step.waitFor.sentence}`,
    waitConditions: step.waitFor?.conditions.map((condition) => `${condition.label} (${condition.kind})`) ?? [],
    answeredBy: answeredBy(flow, step),
    branches: [
      ...step.execute.branches.map((branch) => {
        const targets = branch.targets.map((target) => stepLabel(flow, target.stepId)).join(", ");
        return {
          label: branch.decisionTypes.join("/") || "goTo",
          value: [
            branch.fullGuards.length > 0 ? branch.fullGuards.join("  ·  ") : "unconditional",
            targets.length > 0 ? `→ ${targets}` : "→ closes",
          ].join(" "),
          pre: branch.fullGuards.length > 0,
        };
      }),
      // A Connector Step's branches are its outcomes, which the definition draws as transitions.
      ...flow.transitions
        .filter((t) => t.fromStepId === step.id && t.outcomes.length > 0)
        .map((t) => ({ label: t.outcomes.join(" / "), value: `→ ${stepLabel(flow, t.toStepId)}`, pre: false })),
    ],
  };
}

/** Long branch lists start collapsed so the Step type and its wait stay in view. */
const COLLAPSED_BRANCH_LIMIT = 3;

/** The Step as the engine defines it: its type, its WaitFor and every Execute branch with its full guard. */
function EngineDetail({ model }: { model: StepDefinition }): JSX.Element {
  const [branchesExpanded, setBranchesExpanded] = useState(false);
  const branchCount = model.branches.length;
  const collapsible = branchCount > COLLAPSED_BRANCH_LIMIT;
  return (
    <div className="pt-1">
      <h3 className={HEADING}>Step type</h3>
      {/* The durable identifier, always reachable even when a display label covers it. */}
      <p className="font-mono text-caption wrap-anywhere text-ink-dim">{model.stepType}</p>
      <h3 className={HEADING}>WaitFor</h3>
      {model.waitFor === null ? (
        <p className={NOTE}>No WaitFor. Execute runs immediately.</p>
      ) : (
        <>
          <p className={cx(PROSE, "wrap-anywhere")}>{model.waitFor}</p>
          {model.waitConditions.length > 0 ? (
            <ul className="mt-1 pl-4.5 text-body wrap-anywhere text-ink-faint">
              {model.waitConditions.map((condition) => (
                <li key={condition}>{condition}</li>
              ))}
            </ul>
          ) : null}
          {model.answeredBy.length > 0 ? (
            <p className={NOTE}>
              Answered through <strong className="text-ink">{model.answeredBy.join(", ")}</strong>.
            </p>
          ) : null}
        </>
      )}
      <h3 className={HEADING}>Execute branches</h3>
      {branchCount === 0 ? (
        <p className={NOTE}>No Execute branches in the definition.</p>
      ) : collapsible && !branchesExpanded ? (
        <Button className="mt-1" size="sm" onClick={() => setBranchesExpanded(true)}>
          {branchCount} branches · expand
        </Button>
      ) : (
        <>
          <dl>
            {model.branches.map((row, index) => (
              <div
                className="grid grid-cols-[minmax(5rem,32%)_minmax(0,1fr)] gap-2 border-b border-line-faint py-0.75 text-body"
                key={`${row.label}-${index}`}
              >
                <dt className="min-w-0 wrap-anywhere text-ink-faint">{row.label}</dt>
                <dd
                  className={cx(
                    "min-w-0 wrap-anywhere text-ink-dim",
                    row.pre && "font-mono text-caption whitespace-pre-wrap",
                  )}
                >
                  {row.value}
                </dd>
              </div>
            ))}
          </dl>
          {collapsible ? (
            <Button className="mt-1" size="sm" onClick={() => setBranchesExpanded(false)}>
              Collapse branches
            </Button>
          ) : null}
        </>
      )}
    </div>
  );
}

export function DetailPanel({
  model,
  flowType,
  onClose,
}: {
  model: StepDefinition;
  flowType: string;
  onClose: () => void;
}): JSX.Element {
  const { askAgent } = useFlowHost();
  const { does, waits, ways } = model.words;

  return (
    // It occludes the canvas rather than resizing it, so opening it cannot change the zoom. Opaque,
    // unlike the other canvas chrome: edges showing through prose are worse than a few hidden ones.
    <aside
      className="absolute inset-y-0 right-0 z-6 flex w-[var(--v2-panel-w,290px)] min-w-0 animate-panel-in flex-col overflow-hidden border-l border-line-faint bg-bg motion-reduce:animate-none"
      aria-label={`Detail for ${model.title}`}
    >
      <header className="flex items-start justify-between gap-2 border-b border-line-faint px-3.5 pt-3 pb-2.5">
        <h2 className="m-0 min-w-0 text-title font-semibold wrap-anywhere">{model.title}</h2>
        <Button className="flex-none" size="sm" onClick={onClose} aria-label="Close panel">
          ✕
        </Button>
      </header>
      {askAgent && (
        <div className="border-b border-line-faint px-3.5 py-2">
          {/* The agent works in the code, so it is told the Step's type. */}
          <Button size="sm" onClick={() => askAgent(`${processName(flowType)} ▸ ${model.stepType}`)}>
            ✦ Ask the agent
          </Button>
        </div>
      )}
      <section className="min-h-0 flex-auto overflow-y-auto" aria-label="Step definition">
        <div className="px-3.5 py-3">
          <h3 className={HEADING}>What it does</h3>
          <p className={PROSE}>{does}</p>
          <h3 className={HEADING}>What it waits for</h3>
          <p className={PROSE}>{waits}</p>
          <h3 className={HEADING}>What happens next</h3>
          {ways.length === 0 ? (
            <p className={PROSE}>The definition shows no next step.</p>
          ) : (
            <ul className="grid gap-1">
              {ways.map((way, index) => (
                <li className={cx(PROSE, "wrap-anywhere")} key={`${way.lead}-${way.to}-${index}`}>
                  <span className={cx(way.failure && "text-ink-faint")}>{way.lead}</span>
                  {" → "}
                  <span className="text-ink">{way.to}</span>
                </li>
              ))}
            </ul>
          )}
          <MoreDetails className="mt-4" surface="steps.engine" hint="names in the code">
            <EngineDetail model={model} />
          </MoreDetails>
        </div>
      </section>
    </aside>
  );
}
