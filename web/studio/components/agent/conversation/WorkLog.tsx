// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Badge, type BadgeTone } from "@/components/ui/badge/Badge";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import type { ActivityStreamEvent, AgentMessage, LiveTextEntry, SequencedMessage } from "@/lib/session/agent-types";
import { formatMonthDayTime, formatPaddedDuration } from "@/lib/shared/format";
import { useConversationClock } from "./ConversationClockProvider";
import { MarkdownContent } from "./MarkdownContent";
import {
  advanceConversationTurn,
  type ConversationOperationStatus,
  type ConversationToolWorkItem,
  type ConversationTurn,
  type ConversationWorkItem,
} from "./conversation-presentation";
import { MODEL_FAILURE_MARKER_PREFIX } from "./conversation-work-items";
import { workLogCalls } from "./conversation-timing";

/** One operation of the log: a model call, a tool call or a repeated tool group. */
const ITEM = cx("rounded-lg border border-line-soft bg-panel px-3 py-2.5");
const ROW = cx("flex flex-wrap items-center gap-2");
const SUMMARY = cx("cursor-pointer list-none [&::-webkit-details-marker]:hidden");
const META = cx("font-mono text-caption text-ink-faint");
/** What a tool call works on, on one line that keeps the rest of its row in view. */
const TARGET = cx("min-w-24 flex-1 truncate font-mono text-ink-dim");
/** The fold of every turn's work and of the activity no turn owns. */
const SURFACE = "chat.work-log";

/**
 * A turn's work as one plain line, how long the agent worked and how many steps it took, with the
 * full log one click away; a failed step keeps a line of its own. While the conversation is live and
 * the turn has live work, its durations advance each second.
 */
export function WorkLog({ turn, markers }: { turn: ConversationTurn; markers: readonly SequencedMessage[] }) {
  const nowMs = useConversationClock(turn.clock !== undefined);
  // Keep the same TurnWork and nested components mounted at completion
  // so an expanded disclosure is not reset when its clock subscription ends.
  return <TurnWork turn={nowMs === null ? turn : advanceConversationTurn(turn, nowMs)} markers={markers} />;
}

function TurnWork({ turn, markers }: { turn: ConversationTurn; markers: readonly SequencedMessage[] }) {
  const { models, tools } = workLogCalls(turn.operations);
  const notes = markers.length + turn.fallbacks.length + turn.historicalActivities.length + turn.liveActivities.length;
  if (models.length + tools.length + notes === 0) return null;
  const failure = failureNote(turn.operations, markers);
  return (
    <div className="flex flex-col gap-1 text-caption text-ink-faint" data-testid="work-log">
      <MoreDetails surface={SURFACE} label={models.length + tools.length > 0 ? workSummary(turn, tools) : undefined}>
        <WorkLogDetails turn={turn} markers={markers} />
      </MoreDetails>
      {failure && (
        <p className="flex items-center gap-1 text-fail" data-testid="work-log-failure">
          <Glyph name="alert" className="size-3.5" />
          {failure}
        </p>
      )}
    </div>
  );
}

/** "Worked 1 min 10 s · 8 steps": the turn's span less its waits for the person, and its tool calls. */
function workSummary(turn: ConversationTurn, tools: readonly ConversationToolWorkItem[]): string {
  const worked =
    turn.durationMs === null ? turn.workDurationMs : Math.max(0, turn.durationMs - (turn.waitDurationMs ?? 0));
  const time = spokenDuration(worked);
  const waiting =
    tools.some((item) => item.status === "waiting") || turn.questions.some((item) => item.status === "pending");
  const head = waiting
    ? "Waiting for you"
    : turn.clock?.running
      ? time
        ? `Working for ${time}`
        : "Working"
      : time === null
        ? "Worked"
        : time
          ? `Worked ${time}`
          : "Worked for a moment";
  return tools.length === 0 ? head : `${head} · ${tools.length} step${tools.length === 1 ? "" : "s"}`;
}

/** A duration as people say it: "45 s", "1 min 10 s", "2 h 3 min"; empty under a second, null when unknown. */
function spokenDuration(milliseconds: number | null): string | null {
  if (milliseconds === null || !Number.isFinite(milliseconds) || milliseconds < 0) return null;
  const seconds = Math.floor(milliseconds / 1_000);
  if (seconds < 1) return "";
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return seconds % 60 ? `${minutes} min ${seconds % 60} s` : `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  return minutes % 60 ? `${hours} h ${minutes % 60} min` : `${hours} h`;
}

/** The line a failed model or tool call keeps outside the fold, saying whether work went on after it. */
function failureNote(operations: readonly ConversationWorkItem[], markers: readonly SequencedMessage[]): string | null {
  const modelFailure = markers.findLast((entry) => entry.message.content.startsWith(MODEL_FAILURE_MARKER_PREFIX));
  if (modelFailure) return modelFailure.message.content;
  const statuses = operations.flatMap((operation) =>
    operation.kind === "model" ? [operation.item.status] : operation.group.calls.map((item) => item.status),
  );
  const last = statuses.lastIndexOf("failed");
  if (last < 0) return null;
  return last < statuses.length - 1 ? "A step failed; the agent carried on." : "A step failed.";
}

/** How each status of a model or tool call reads in the log, and its badge tone. */
const OPERATION_STATUS = {
  running: { label: "Running", tone: "accent" },
  waiting: { label: "Waiting", tone: "busy" },
  complete: { label: "Done", tone: "neutral" },
  failed: { label: "Failed", tone: "fail" },
  unknown: { label: "Unknown", tone: "neutral" },
} as const satisfies Record<ConversationOperationStatus, { label: string; tone: BadgeTone }>;

function OperationStatus({ value }: { value: ConversationOperationStatus }) {
  const { label, tone } = OPERATION_STATUS[value];
  return <Badge tone={tone}>{label}</Badge>;
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

/** A tool's name as its row shows it; a requirements card, which waits for nobody, reads as what it did. */
function ToolName({ name }: { name: string }) {
  return name === "confirm_requirements" ? <span>Recorded what to build</span> : <code>{name}</code>;
}

/**
 * One tool call as a single row: its name, target, attempt count once it retried, status, result
 * code and duration. The row never shows arguments, output or a diff; the Agent's history does not
 * carry them.
 */
function ToolCallRow({ item }: { item: ConversationToolWorkItem }) {
  return (
    <div className={cx(ITEM, ROW)}>
      <ToolName name={item.call.name} />
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

/** A system marker of the turn, such as a compaction or a context refresh, between two rules. */
function Marker({ entry }: { entry: SequencedMessage }) {
  return (
    <div
      className="flex items-center gap-3 text-ink-faint"
      data-message-sequence={entry.sequence}
      data-testid="conversation-marker"
      title={formatMonthDayTime(entry.message.createdAt)}
    >
      <span aria-hidden="true" className="h-px flex-1 bg-line-soft" />
      <span className="max-w-[80%] text-center wrap-anywhere">{entry.message.content}</span>
      <span aria-hidden="true" className="h-px flex-1 bg-line-soft" />
    </div>
  );
}

/** Activity events, numbered, under a label such as "Earlier activity". */
function ActivityList({
  entries,
  label,
}: {
  entries: readonly (ActivityStreamEvent | LiveTextEntry)[];
  label: string;
}) {
  if (entries.length === 0) return null;
  return (
    <div className={ITEM}>
      <p>
        {label} · {entries.length} event{entries.length === 1 ? "" : "s"}
      </p>
      <ol className="mt-1 list-decimal pl-5">
        {entries.map((entry, index) => (
          <li key={("resumeToken" in entry ? entry.resumeToken : entry.source) + String(index)}>
            {typeof entry.value === "string" ? entry.value : entry.value.message}
          </li>
        ))}
      </ol>
    </div>
  );
}

/**
 * The turn's full log: its markers, the counts and timers of its model and tool calls, each call with
 * its status, result code, duration and token metrics, the model fallbacks and the turn's activity.
 */
function WorkLogDetails({ turn, markers }: { turn: ConversationTurn; markers: readonly SequencedMessage[] }) {
  const { models, tools } = workLogCalls(turn.operations);
  const statuses = [...models, ...tools].map((item) => item.status);
  const count = (...values: ConversationOperationStatus[]) =>
    statuses.filter((status) => values.includes(status)).length;
  const failedCount = count("failed");
  const activeCount = count("running", "waiting");
  const unknownCount = count("unknown");
  return (
    <div className="ml-1.5 flex flex-col gap-2.5 border-l border-line-soft pl-3 text-caption text-ink-dim">
      {markers.map((entry) => (
        <Marker entry={entry} key={entry.sequence} />
      ))}
      {statuses.length > 0 && (
        <div className={ROW}>
          <strong className="font-semibold text-ink">Work log</strong>
          <span>
            {models.length} model call{models.length === 1 ? "" : "s"}
          </span>
          <span>{tools.length} tool calls</span>
          <span>{count("complete")} succeeded</span>
          <span className={cx(failedCount > 0 && "text-fail")}>{failedCount} failed</span>
          {activeCount > 0 ? <span>{activeCount} active</span> : null}
          {unknownCount > 0 ? <span>{unknownCount} unknown</span> : null}
          <span className="ml-auto flex flex-wrap gap-2.5 max-sm:ml-4 max-sm:w-full">
            <Duration value={turn.waitDurationMs} prefix="wait " />
            <Duration value={turn.workDurationMs} prefix="work " />
            <Duration value={turn.durationMs} prefix="turn " />
          </span>
        </div>
      )}
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
              <ToolName name={group.name} />
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
      {turn.fallbacks.map((event) => (
        <p key={event.resumeToken}>{event.value.message}</p>
      ))}
      <ActivityList entries={turn.historicalActivities} label="Historical activity" />
      <ActivityList entries={turn.liveActivities} label="Current activity" />
    </div>
  );
}

/** The activity no turn owns, folded with the work logs. */
export function EarlierActivity({ entries }: { entries: readonly (ActivityStreamEvent | LiveTextEntry)[] }) {
  if (entries.length === 0) return null;
  return (
    <MoreDetails surface={SURFACE} className="text-caption text-ink-faint" hint="earlier activity">
      <ActivityList entries={entries} label="Earlier activity" />
    </MoreDetails>
  );
}
