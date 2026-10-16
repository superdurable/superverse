// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import type { FlowHistoryEvent, FlowState, FlowSummary } from "@/lib/api/flow/flow-types";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { asRecord, asRecordArray, stringField } from "@/lib/shared/json";
import { JsonView } from "@/components/flow/diagnostics/value/JsonView";
import { FailureDetails } from "@/components/flow/diagnostics/event/FailureDetails";
import { Subsection } from "@/components/flow/diagnostics/value/DetailRecords";
import { Button } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { cx } from "@/components/ui/cx";

const BLOCK = cx("mt-4.5 border-t border-line-faint pt-3.5");
const BLOCK_HEADING = cx("mb-1 flex items-start justify-between gap-3");
const MUTED = cx("text-xs text-ink-faint");

/** An active Step's phase pill: running in the accent, waiting in the timer hue. */
const PHASE_INK: Partial<Record<string, string>> = { active: cx("text-accent"), waiting: cx("text-clock") };

const sectionExpandByKey = new Map<string, boolean>();

/** A section's "Expand all" choice, kept across tab switches for this page session. */
function useSectionExpand(persistKey: string) {
  const [expanded, setExpanded] = useState(() => sectionExpandByKey.get(persistKey) ?? false);
  return {
    expanded,
    setExpanded: (next: boolean) => {
      sectionExpandByKey.set(persistKey, next);
      setExpanded(next);
    },
  };
}

function SectionExpandToggle({
  expanded,
  onChange,
  label,
}: {
  expanded: boolean;
  onChange: (next: boolean) => void;
  label: string;
}) {
  return (
    <label className="inline-flex shrink-0 cursor-pointer items-center gap-1.5 rounded-[5px] border border-line-soft bg-raised px-2 py-1 text-[11px] font-semibold whitespace-nowrap text-ink-dim select-none">
      <input
        className="m-0 accent-cta"
        type="checkbox"
        checked={expanded}
        onChange={(event) => onChange(event.target.checked)}
      />
      {expanded ? "Collapse all" : "Expand all"}
      <span className="sr-only">{label}</span>
    </label>
  );
}

/** The run's live state while it is active (Steps, Attributes, Channels, internals), then its close result. */
export function LiveFlowState({
  summary,
  state,
  closed,
  deletingChannelMessage,
  onDeleteChannelMessage,
}: {
  summary: FlowSummary;
  state: FlowState | null;
  closed: FlowHistoryEvent | undefined;
  deletingChannelMessage: string;
  onDeleteChannelMessage: (channelName: string, messageId: string) => Promise<void>;
}) {
  const [collapsedActiveStepIds, setCollapsedActiveStepIds] = useState<Set<string>>(() => new Set());
  // Deleting a queued message changes the run, so like Stop it is for those who build the Project.
  const { canIntervene } = useFlowHost();
  const attributesExpand = useSectionExpand("overview:expand:attributes");
  const channelsExpand = useSectionExpand("overview:expand:channels");
  const activeStepIds = state?.activeStepExecutions.map((step) => step.stepExecutionId) ?? [];
  const allActiveStepsExpanded =
    activeStepIds.length > 0 && activeStepIds.every((stepExecutionId) => !collapsedActiveStepIds.has(stepExecutionId));
  const pendingMessages = pendingChannelEntries(state?.pendingChannelMessages ?? {});

  const setAllActiveStepsExpanded = (expanded: boolean) => {
    setCollapsedActiveStepIds((current) => {
      const next = new Set(current);
      for (const stepExecutionId of activeStepIds) {
        if (expanded) next.delete(stepExecutionId);
        else next.add(stepExecutionId);
      }
      return next;
    });
  };

  const toggleActiveStep = (stepExecutionId: string) => {
    setCollapsedActiveStepIds((current) => {
      const next = new Set(current);
      if (next.has(stepExecutionId)) next.delete(stepExecutionId);
      else next.add(stepExecutionId);
      return next;
    });
  };

  return (
    <section className="min-w-0 rounded-lg border border-line-faint bg-panel p-4.5">
      <Eyebrow className="mb-1.5">{state ? "Current" : summary.flowStatus || "Unavailable"}</Eyebrow>
      <h2 className="m-0 text-sm font-semibold tracking-normal text-ink">Live Flow State</h2>
      {!state && <p className={cx(MUTED, "mt-2")}>Live state is queried only while this run is active.</p>}
      {state && (
        <>
          <div className="mt-3.5 grid grid-cols-[repeat(auto-fit,minmax(96px,1fr))] gap-2">
            {(
              [
                ["Attributes", state.attributes.length],
                ["Active steps", state.activeStepExecutions.length],
                ["Queued steps", state.queuedSteps.length],
                ["Completed outputs", state.completedSteps.length],
                ["Pending channels", Object.keys(state.pendingChannelMessages).length],
              ] as const
            ).map(([label, count]) => (
              <div className="flex flex-col gap-1.25 rounded-lg border border-line-faint bg-panel p-3" key={label}>
                <span className="text-[10px] text-ink-faint">{label}</span>
                <b className="text-[22px] text-ink">{count}</b>
              </div>
            ))}
          </div>

          <div className={BLOCK}>
            <div className={BLOCK_HEADING}>
              <Eyebrow>Active steps</Eyebrow>
              <SectionExpandToggle
                expanded={allActiveStepsExpanded}
                onChange={setAllActiveStepsExpanded}
                label="active steps"
              />
            </div>
            {state.activeStepExecutions.map((step) => (
              <details
                className="group mt-2.25 rounded-lg border border-line-faint bg-raised p-2.5 text-[11px]"
                key={step.stepExecutionId}
                open={!collapsedActiveStepIds.has(step.stepExecutionId)}
              >
                <summary
                  className="flex cursor-pointer list-none items-center justify-between gap-1.75 [&::-webkit-details-marker]:hidden"
                  onClick={(event) => {
                    event.preventDefault();
                    toggleActiveStep(step.stepExecutionId);
                  }}
                >
                  <span className="inline-flex min-w-0 items-center gap-1.5">
                    <span className="text-ink-faint transition-transform group-open:rotate-90" aria-hidden="true">
                      ▸
                    </span>
                    <b>{step.stepType}</b>
                  </span>
                  <span
                    className={cx(
                      "rounded-full bg-panel px-1.5 py-0.75 text-[9px] font-extrabold uppercase",
                      PHASE_INK[step.phase.toLowerCase()] ?? "text-ink-faint",
                    )}
                  >
                    {step.phase}
                  </span>
                </summary>
                <div className="mt-2 flex flex-col gap-1.5">
                  <code className="wrap-anywhere">{step.stepExecutionId}</code>
                  <span className="text-ink-faint">From {step.fromStepExecutionId || "—"}</span>
                  {step.waitingCondition && Object.keys(step.waitingCondition).length > 0 && (
                    <JsonView
                      value={step.waitingCondition}
                      label="Waiting condition"
                      persistKey={`overview:active-step:${step.stepType}:waiting-condition`}
                      parentFlowId={summary.flowId}
                      stepExecutionId={step.stepExecutionId}
                    />
                  )}
                  {step.timers.length > 0 && (
                    <JsonView
                      value={step.timers}
                      label={`${step.timers.length} timers`}
                      persistKey={`overview:active-step:${step.stepType}:timers`}
                    />
                  )}
                  {step.lastFailureInfo && Object.keys(step.lastFailureInfo).length > 0 && (
                    <Subsection title="Last failure">
                      {/* Pending backend attempts exclude local fallback attempts, so hide them until cumulative values are available. */}
                      <FailureDetails value={step.lastFailureInfo} stackInitiallyExpanded showAttempt={false} />
                    </Subsection>
                  )}
                </div>
              </details>
            ))}
            {state.activeStepExecutions.length === 0 && <p className={MUTED}>No active step executions.</p>}
          </div>

          <div className={BLOCK}>
            <div className={BLOCK_HEADING}>
              <div>
                <Eyebrow>Attributes</Eyebrow>
                <h3 className="mt-1 mb-0 text-xs font-semibold text-ink-dim">{state.attributes.length} values</h3>
              </div>
              <SectionExpandToggle
                expanded={attributesExpand.expanded}
                onChange={attributesExpand.setExpanded}
                label="attributes"
              />
            </div>
            <JsonView
              value={state.attributes}
              label="Attributes"
              initiallyOpen
              persistKey="overview:attributes"
              persistenceKind="attributes"
              forceOpen={attributesExpand.expanded}
            />
          </div>

          <div className={BLOCK}>
            <div className={BLOCK_HEADING}>
              <Eyebrow>Channels & Others Internals</Eyebrow>
              <SectionExpandToggle
                expanded={channelsExpand.expanded}
                onChange={channelsExpand.setExpanded}
                label="channels and other internals"
              />
            </div>
            <JsonView
              value={state.queuedSteps}
              label={`${state.queuedSteps.length} queued steps`}
              persistKey="overview:queued-steps"
              forceOpen={channelsExpand.expanded}
            />
            {pendingMessages.length > 0 && (
              <div className="mt-3 grid gap-3">
                {pendingMessages.map((message) => (
                  <div
                    className="flex items-start justify-between gap-4 rounded-xl border border-line-faint bg-raised p-3"
                    key={`${message.channelName}:${message.messageId}`}
                  >
                    <div className="min-w-0">
                      <b>{message.channelName}</b>
                      <code className="mt-1 block wrap-anywhere">{message.messageId}</code>
                      <JsonView value={message.value} label="Value" />
                    </div>
                    {canIntervene && (
                      <Button
                        variant="danger"
                        disabled={deletingChannelMessage === message.messageId}
                        onClick={() => void onDeleteChannelMessage(message.channelName, message.messageId)}
                      >
                        {deletingChannelMessage === message.messageId ? "Deleting…" : "Delete"}
                      </Button>
                    )}
                  </div>
                ))}
                <p className={MUTED}>
                  Temporal deletes are atomic. Cadence deletion is best effort and may race consumption.
                </p>
              </div>
            )}
            <JsonView
              value={state.pendingChannelMessages}
              label="Pending channel messages"
              persistKey="overview:pending-channels"
              persistenceKind="channels"
              forceOpen={channelsExpand.expanded}
            />
            <JsonView
              value={state.completedSteps}
              label="Completed outputs"
              persistKey="overview:completed-outputs"
              forceOpen={channelsExpand.expanded}
            />
          </div>

          {state.flowConfig && (
            <JsonView value={state.flowConfig} label="Flow config" persistKey="overview:flow-config" />
          )}
        </>
      )}
      {closed && (
        <JsonView value={closed.payload.results ?? []} label="Close result" persistKey="overview:close-result" />
      )}
    </section>
  );
}

interface PendingChannelEntry {
  channelName: string;
  messageId: string;
  value: unknown;
}

/** Every pending message that carries an ID, which is what a delete needs. */
function pendingChannelEntries(channels: Record<string, unknown>): PendingChannelEntry[] {
  return Object.entries(channels).flatMap(([channelName, entry]) =>
    asRecordArray(asRecord(entry).values).flatMap((message) => {
      const messageId = stringField(message.messageId);
      return messageId === "" ? [] : [{ channelName, messageId, value: message.value }];
    }),
  );
}
