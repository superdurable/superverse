// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { cx } from "@/components/ui/cx";
import type { AgentMessage } from "@/lib/session/agent-types";
import { formatPaddedDuration } from "@/lib/shared/format";
import { useConversationClock } from "./ConversationClockProvider";
import { MarkdownContent } from "./MarkdownContent";
import {
  advanceConversationTurn,
  type ConversationModelWorkItem,
  type ConversationToolWorkItem,
  type ConversationTurn,
} from "./conversation-presentation";
import { workLogCalls } from "./conversation-timing";

/** One operation of the log: a model call, a tool call or a repeated tool group. */
const ITEM = cx("rounded-lg border border-line-soft bg-panel px-3 py-2.5");
const ROW = cx("flex flex-wrap items-center gap-2");
const SUMMARY = cx("cursor-pointer list-none [&::-webkit-details-marker]:hidden");
const META = cx("font-mono text-[11px] text-ink-faint");
/** What a tool call works on, on one line that keeps the rest of its row in view. */
const TARGET = cx("min-w-24 flex-1 truncate font-mono text-ink-dim");

/** A turn's work log; while the conversation is live and the turn has live work, its durations advance each second. */
export function WorkLog({ turn }: { turn: ConversationTurn }) {
  const nowMs = useConversationClock(turn.clock !== undefined);
  // Keep the same WorkLogDetails and nested components mounted at completion
  // so an expanded disclosure is not reset when its clock subscription ends.
  return <WorkLogDetails turn={nowMs === null ? turn : advanceConversationTurn(turn, nowMs)} />;
}

export function OperationStatus({ value }: { value: string }) {
  return (
    <span
      className={cx(
        "rounded-full border px-1.5 py-0.5 text-[10px] uppercase",
        value === "failed" ? "border-current text-fail" : "border-line-soft",
      )}
    >
      {value}
    </span>
  );
}

function Duration({ value, prefix }: { value: number | null; prefix?: string }) {
  const label = formatPaddedDuration(value);
  return label === null ? null : (
    <span aria-hidden="true" className={META}>
      {prefix}
      {label}
    </span>
  );
}

/**
 * One tool call as a single row: its name, target, attempt count once it retried, status, result
 * code and duration. The row never shows arguments, output or a diff; the Agent's history does not
 * carry them.
 */
function ToolCallRow({ item }: { item: ConversationToolWorkItem }) {
  return (
    <div className={cx(ITEM, ROW)}>
      <code>{item.call.name}</code>
      {item.target ? (
        <span className={TARGET} title={item.target}>
          {item.target}
        </span>
      ) : null}
      {item.attemptCount !== null && item.attemptCount > 1 ? <span>{item.attemptCount} attempts</span> : null}
      <OperationStatus value={item.status} />
      {item.resultCode ? <code className={META}>{item.resultCode}</code> : null}
      <Duration value={item.durationMs} />
    </div>
  );
}

function tokenCount(label: string, value: number | null): string {
  return `${label} ${value === null ? "unknown" : value.toLocaleString("en-US")}`;
}

/** One model call's request: model, effort, token counters, timing and, after a fallback, the model that served it. */
function ModelCallMetrics({ metrics }: { metrics: NonNullable<AgentMessage["modelCall"]> }) {
  const servedBy = metrics.servedModel !== "" && metrics.servedModel !== metrics.model ? metrics.servedModel : null;
  return (
    <p className="mt-1">
      {metrics.model}
      {metrics.effort ? ` · ${metrics.effort} effort` : ""}
      {` · ${tokenCount("input", metrics.inputTokens)}`}
      {` · ${tokenCount("cache read", metrics.cacheReadInputTokens)}`}
      {` · ${tokenCount("cache write", metrics.cacheCreationInputTokens)}`}
      {` · ${tokenCount("output", metrics.outputTokens)}`}
      {` · ${tokenCount("thinking", metrics.thinkingTokens)} tokens`}
      {servedBy === null ? "" : ` · served by ${servedBy}`}
      <Duration value={metrics.durationMs} prefix=" · request " />
      <Duration value={metrics.firstEventMs} prefix=" · first event " />
    </p>
  );
}

/** What the collapsed log names: the call that runs now, else the first that failed, else the last tool call. */
function headlineOf(models: readonly ConversationModelWorkItem[], tools: readonly ConversationToolWorkItem[]) {
  const live = (status: string) => status === "running" || status === "waiting";
  const tool = tools.findLast((item) => live(item.status)) ?? tools.find((item) => item.status === "failed");
  if (tool) return { name: tool.call.name, target: tool.target, status: tool.status, durationMs: tool.durationMs };
  const model = models.findLast((item) => live(item.status));
  if (model) return { name: "model", target: model.summary, status: model.status, durationMs: model.durationMs };
  const last = tools.at(-1);
  return last ? { name: last.call.name, target: last.target, status: last.status, durationMs: last.durationMs } : null;
}

function WorkLogDetails({ turn }: { turn: ConversationTurn }) {
  // Open while work runs or after a failure, until the person opens or closes it themselves.
  const [chosenOpen, setChosenOpen] = useState<boolean | null>(null);
  const { models, tools } = workLogCalls(turn.operations);
  if (models.length + tools.length === 0) return null;
  const statuses = [...models, ...tools].map((item) => item.status);
  const succeededCount = statuses.filter((status) => status === "complete").length;
  const failedCount = statuses.filter((status) => status === "failed").length;
  const activeCount = statuses.filter((status) => status === "running" || status === "waiting").length;
  const unknownCount = statuses.filter((status) => status === "unknown").length;
  const failed = failedCount > 0;
  const running = activeCount > 0;
  const isOpen = chosenOpen ?? (running || failed);
  const headline = headlineOf(models, tools);
  return (
    <details
      className={cx(
        "rounded-lg border bg-panel text-xs/[1.45] text-ink-dim",
        failed ? "border-fail" : "border-line-soft",
      )}
      open={isOpen}
      onToggle={(event) => {
        if (event.currentTarget.open !== isOpen) setChosenOpen(event.currentTarget.open);
      }}
    >
      <summary className={cx(SUMMARY, ROW, "px-3 py-2.5 text-[11px]")}>
        {running ? (
          <span
            aria-hidden="true"
            className="size-2 animate-pulse-soft rounded-full bg-accent motion-reduce:animate-none"
          />
        ) : null}
        <strong className="text-xs font-semibold text-ink">Work log</strong>
        <span>
          {models.length} model call{models.length === 1 ? "" : "s"}
        </span>
        <span>{tools.length} tool calls</span>
        <span>{succeededCount} succeeded</span>
        <span className={cx(failed && "text-fail")}>{failedCount} failed</span>
        {activeCount > 0 ? <span>{activeCount} active</span> : null}
        {unknownCount > 0 ? <span>{unknownCount} unknown</span> : null}
        <span className="ml-auto flex flex-wrap gap-2.5 max-sm:ml-4 max-sm:w-full">
          <Duration value={turn.waitDurationMs} prefix="wait " />
          <Duration value={turn.workDurationMs} prefix="work " />
          <Duration value={turn.durationMs} prefix="turn " />
        </span>
        {headline && !isOpen ? (
          <span className={cx(ROW, "w-full min-w-0 text-ink-dim")}>
            <code>{headline.name}</code>
            {headline.target ? (
              <span className={TARGET} title={headline.target}>
                {headline.target}
              </span>
            ) : null}
            <OperationStatus value={headline.status} />
            <Duration value={headline.durationMs} />
          </span>
        ) : null}
      </summary>
      {isOpen ? (
        <div className="flex flex-col gap-2.5 border-t border-line-soft p-3">
          {turn.operations.map((operation) => {
            if (operation.kind === "model") {
              const model = operation.item;
              return (
                <div className={ITEM} key={operation.key}>
                  <div className={ROW}>
                    <strong>{model.summary}</strong>
                    {model.attemptCount !== null && model.attemptCount > 1 ? (
                      <span>{model.attemptCount} attempts</span>
                    ) : null}
                    <OperationStatus value={model.status} />
                    <Duration value={model.durationMs} />
                  </div>
                  {model.metrics ? <ModelCallMetrics metrics={model.metrics} /> : null}
                  {model.reasoning?.value ? (
                    <details className="mt-1" open={!model.reasoning.isComplete}>
                      <summary className="cursor-pointer">
                        Reasoning · {model.reasoning.isComplete ? "complete" : "streaming"}
                      </summary>
                      <MarkdownContent value={model.reasoning.value} />
                    </details>
                  ) : null}
                </div>
              );
            }
            const group = operation.group;
            const repeatLabel =
              group.repeatIndex !== null && group.repeatTotal !== null
                ? `Repeat ${String(group.repeatIndex)}/${String(group.repeatTotal)}`
                : null;
            return group.calls.length === 1 && group.calls[0] ? (
              <div key={operation.key}>
                {repeatLabel ? <span className="mb-1 ml-1 inline-block text-ink-faint">{repeatLabel}</span> : null}
                <ToolCallRow item={group.calls[0]} />
              </div>
            ) : (
              <details className={ITEM} key={operation.key}>
                <summary className={cx(SUMMARY, ROW)}>
                  <code>{group.name}</code>
                  {group.calls[0]?.target ? (
                    <span className={TARGET} title={group.calls[0].target}>
                      {group.calls[0].target}
                    </span>
                  ) : null}
                  <span>Repeated ×{group.calls.length}</span>
                </summary>
                <div className="mt-2 flex flex-col gap-2">
                  {group.calls.map((item) => (
                    <ToolCallRow item={item} key={item.call.id} />
                  ))}
                </div>
              </details>
            );
          })}
        </div>
      ) : null}
    </details>
  );
}
