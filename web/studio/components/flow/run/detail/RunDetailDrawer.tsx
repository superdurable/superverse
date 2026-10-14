// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2Definition } from "@/lib/api/flow/flow-definition";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { SelectedRunPanel } from "./SelectedRunPanel";
import { RUN_COPY } from "./run-copy";
import { RunHeader } from "./RunHeader";
import { StepActions } from "@/components/flow/run/step/StepActions";
import { hasMultipleActions, type ActionableStep } from "@/components/flow/canvas/model/actionable-steps";
import type { StepContextView } from "@/components/flow/canvas/model/step-context";
import type { SelectedStepStatus } from "@/components/flow/canvas/FlowCanvas";
import type { Reason } from "@/components/flow/canvas/model/run-overlay";
import { cx } from "@/components/ui/cx";
import {
  BLOCK_HEAD,
  CLOSE,
  FACT,
  FACT_TERM,
  FACT_VALUE,
  FACTS,
  LABEL,
  LINK,
  PURPOSE,
  STEP_MARK,
  STEP_NAME,
} from "@/components/flow/frame/workspace-styles";

/** The band names why the Step stopped in the hue of who must act; a healthy Step stays grey. */
const BAND_LABEL_TONE = {
  blocked: cx("text-busy"),
  failed: cx("text-fail"),
  terminal: cx("text-ink-dim"),
} satisfies Record<Reason["tone"], string>;

/**
 * Run facts and Actions stay put; only the top band follows the canvas. Actions are never
 * hidden behind a tab or a scroll, because they are the reason an Admin opened the run.
 */
export function RunDetailDrawer({
  flowType,
  flowId,
  definition,
  definitionRevision,
  summary,
  flowStatusCode,
  selectedStepStatus,
  actionable,
  stepContext,
  reloadKey,
  onStranded,
  onStopped,
  onClose,
}: {
  flowType: string;
  /** Empty when nothing is selected: the drawer then explains the Step alone. */
  flowId: string;
  definition: FlowV2Definition;
  definitionRevision: string;
  summary: FlowSummary | null;
  flowStatusCode?: number;
  selectedStepStatus: SelectedStepStatus | null;
  actionable: readonly ActionableStep[];
  stepContext: StepContextView | null;
  reloadKey: number;
  onStranded?: (flowID: string) => void;
  onStopped: () => void;
  onClose: () => void;
}) {
  const hasRun = flowId !== "";
  return (
    <section
      aria-label={hasRun ? flowId : (stepContext?.stepType ?? RUN_COPY.thisStep)}
      className="flex min-h-0 min-w-0 animate-panel-in flex-col overflow-y-auto border-l border-line-faint bg-panel motion-reduce:animate-none dark:bg-transparent"
      data-step-only={hasRun ? undefined : "true"}
    >
      {hasRun ? (
        <RunHeader key={flowId} flowId={flowId} summary={summary} onStopped={onStopped} />
      ) : (
        <div className="flex items-baseline gap-2 border-b border-line-faint px-4 pt-3 pb-2.5">
          <span className={LABEL}>{RUN_COPY.stepOnly}</span>
          <button aria-label={RUN_COPY.close} className={cx(CLOSE, "ml-auto")} onClick={onClose} type="button">
            ✕
          </button>
        </div>
      )}
      {hasRun && hasMultipleActions(actionable) && <ActionProgress steps={actionable} />}
      {hasRun && selectedStepStatus && (
        <div
          className="grid gap-0.5 border-b border-line-faint bg-panel px-4 py-2.5"
          data-tone={selectedStepStatus.reason?.tone}
        >
          <span
            className={cx(
              "text-[10px] tracking-wider uppercase",
              selectedStepStatus.reason ? BAND_LABEL_TONE[selectedStepStatus.reason.tone] : "text-ink-faint",
            )}
          >
            {selectedStepStatus.isBlocking ? RUN_COPY.waitingAt : RUN_COPY.showingStep}
          </span>
          <span className="font-mono text-[13px] text-ink">{selectedStepStatus.stepType}</span>
          {selectedStepStatus.reason && (
            <span className="text-[11px]/[1.45] text-ink-faint">{selectedStepStatus.reason.text}</span>
          )}
        </div>
      )}
      {hasRun && selectedStepStatus && (
        <StepActions
          flowType={flowType}
          flowId={flowId}
          summary={summary}
          stepType={selectedStepStatus.stepType}
          waitsForPerson={
            selectedStepStatus.isBlocking &&
            actionable.some((step) => step.stepType === selectedStepStatus.stepType && step.state === "current")
          }
          failure={selectedStepStatus.reason?.tone === "failed" ? selectedStepStatus.reason.text : null}
        />
      )}
      {hasRun && (
        <SelectedRunPanel
          key={`${flowType}:${flowId}`}
          definition={definition}
          definitionRevision={definitionRevision}
          flowId={flowId}
          flowStatusCode={flowStatusCode}
          flowType={flowType}
          order="actions-first"
          showHeading={false}
          reloadKey={reloadKey}
          onActed={() => {
            // Closing was right when one Action ended the run. With another gate pending,
            // advancing is the point: the reader should not have to find it again.
            if (actionable.filter((step) => step.state !== "done").length <= 1) onClose();
          }}
          onStranded={onStranded}
        />
      )}
      {stepContext !== null && <StepContextBlock view={stepContext} ruled={hasRun} />}
    </section>
  );
}

/**
 * What the selected Step is for, and where it sits. Flow-level: no run values here. Below a run it
 * is ruled off; standalone, the Step is all the drawer has to say.
 */
function StepContextBlock({ view, ruled }: { view: StepContextView; ruled: boolean }) {
  return (
    <div className={cx("mt-4 px-4 pt-3.5 pb-5", ruled && "border-t border-line-faint")}>
      <div className={BLOCK_HEAD}>{RUN_COPY.thisStep}</div>
      <p className={STEP_NAME}>{view.stepType}</p>
      <p className={PURPOSE}>{view.explanation ?? RUN_COPY.noPurpose}</p>
      {view.connector && (
        <div className="my-2.5 grid gap-1 rounded-md border border-line-soft p-2.5 text-xs text-ink-dim">
          <b>{view.connector.connectorId}</b>
          <span>
            {view.connector.operationId} · {view.connector.operationKind}
          </span>
          <span>Connection: {view.connector.connectionName || "Unnamed"}</span>
          <span>Version: {view.connector.moduleVersion || "Unsupported"}</span>
          <FlowLink className={cx(LINK, "font-semibold")} to={{ surface: "configuration" }}>
            Configure connection
          </FlowLink>
        </div>
      )}
      <dl className={cx(FACTS, "mt-2.5")}>
        {view.facts.map((fact) => (
          <div className={FACT} key={fact.label}>
            <dt className={FACT_TERM}>{fact.label}</dt>
            <dd className={FACT_VALUE}>{fact.value}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

/**
 * Progress through a Flow's human gates. Only one is ever open, so this reads as a sequence
 * rather than a list of things that could be done now.
 */
function ActionProgress({ steps }: { steps: readonly ActionableStep[] }) {
  const current = steps.findIndex((step) => step.state === "current");
  const remaining = steps.filter((step) => step.state !== "done").length;
  return (
    <div className="border-b border-line-faint px-4 py-2.5">
      <div className="flex items-baseline gap-2">
        <span className={LABEL}>{RUN_COPY.actionProgress}</span>
        <span className="ml-auto text-[10px] text-ink-faint">
          {current < 0 ? RUN_COPY.allActionsDone : RUN_COPY.actionsLeft(remaining)}
        </span>
      </div>
      <ol className="mt-1.75 flex flex-col gap-0.75">
        {steps.map((step, index) => (
          <li className="flex items-center gap-1.75" data-state={step.state} key={step.stepId}>
            <span className={STEP_MARK} data-state={step.state} aria-hidden="true">
              {step.state === "done" ? "✓" : index + 1}
            </span>
            {/* The open gate is the one thing here worth emphasis. */}
            <span
              className={cx(
                "font-mono text-[11px] wrap-anywhere",
                step.state === "current" ? "font-semibold text-ink" : "text-ink-faint",
              )}
            >
              {step.stepType}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
