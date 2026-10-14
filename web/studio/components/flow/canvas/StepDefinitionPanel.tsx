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
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

const HEADING = cx("mt-2.5 mb-1 text-xs font-semibold tracking-wider text-ink-faint uppercase first:mt-0");
const PROSE = cx("text-sm/[1.45] text-ink-dim");
const NOTE = cx("mt-2 text-[13px]/normal text-ink-faint");

/**
 * The selected Step's definition: the place where values crowded off the canvas appear, such as
 * full guard text. The run view explains a Step in its own drawer instead.
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
  explanation: string | null;
  waitFor: string | null;
  waitConditions: string[];
  /** RPCs that publish to a Channel this Step waits on. */
  answeredBy: string[];
  branches: BranchRow[];
}

export function stepDefinition(flow: FlowModel, step: StepModel): StepDefinition {
  return {
    title: step.label,
    stepType: step.stepType,
    explanation: step.explanation ?? null,
    waitFor: step.waitFor === null ? null : `${step.waitFor.type} · ${step.waitFor.sentence}`,
    waitConditions: step.waitFor?.conditions.map((condition) => `${condition.label} (${condition.kind})`) ?? [],
    answeredBy: answeredBy(flow, step),
    branches: step.execute.branches.map((branch) => {
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
  };
}

/** Long branch lists start collapsed so the WaitFor and explanation stay in view. */
const COLLAPSED_BRANCH_LIMIT = 3;

export function DetailPanel({
  model,
  flowType,
  onClose,
}: {
  model: StepDefinition;
  flowType: string;
  onClose: () => void;
}): JSX.Element {
  const [branchesExpanded, setBranchesExpanded] = useState(false);
  const { askAgent } = useFlowHost();
  const branchCount = model.branches.length;
  const collapsible = branchCount > COLLAPSED_BRANCH_LIMIT;

  return (
    // It occludes the canvas rather than resizing it, so opening it cannot change the zoom. Opaque,
    // unlike the other canvas chrome: edges showing through prose are worse than a few hidden ones.
    <aside
      className="absolute inset-y-0 right-0 z-6 flex w-[var(--v2-panel-w,290px)] min-w-0 animate-panel-in flex-col overflow-hidden border-l border-line-faint bg-bg motion-reduce:animate-none"
      aria-label={`Detail for ${model.stepType}`}
    >
      <header className="flex items-start justify-between gap-2 border-b border-line-faint px-3.5 pt-3 pb-2.5">
        <div>
          <h2 className="m-0 text-[17px] font-semibold">{model.title}</h2>
          {/* The durable identifier, always reachable even when a display label covers it. */}
          <p className="mt-0.5 font-mono text-[12.5px] text-ink-faint">{model.stepType}</p>
        </div>
        <Button className="flex-none" size="sm" onClick={onClose} aria-label="Close panel">
          ✕
        </Button>
      </header>
      {askAgent && (
        <div className="border-b border-line-faint px-3.5 py-2">
          <Button size="sm" onClick={() => askAgent(`${processName(flowType)} ▸ ${model.stepType}`)}>
            ✦ Ask the agent
          </Button>
        </div>
      )}
      {/* The only place values appear: full guard text and every branch, wrapped rather than clipped. */}
      <section className="min-h-0 flex-auto overflow-y-auto" aria-label="Step definition">
        <div className="px-3.5 py-3">
          <h3 className={HEADING}>Explanation</h3>
          {model.explanation === null ? (
            <p className={NOTE}>No dex:explanation on this Step.</p>
          ) : (
            <p className={PROSE}>{model.explanation}</p>
          )}
          <h3 className={HEADING}>WaitFor</h3>
          {model.waitFor === null ? (
            <p className={NOTE}>No WaitFor — Execute runs immediately.</p>
          ) : (
            <>
              <p className={PROSE}>{model.waitFor}</p>
              {model.waitConditions.length > 0 ? (
                <ul className="mt-1 pl-4.5 text-[13px] text-ink-faint">
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
                    className="grid grid-cols-[minmax(5rem,32%)_minmax(0,1fr)] gap-2 border-b border-line-faint py-0.75 text-sm"
                    key={`${row.label}-${index}`}
                  >
                    <dt className="min-w-0 wrap-anywhere text-ink-faint">{row.label}</dt>
                    <dd
                      className={cx(
                        "min-w-0 wrap-anywhere text-ink-dim",
                        row.pre && "font-mono text-[12.5px]/normal whitespace-pre-wrap",
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
      </section>
    </aside>
  );
}
