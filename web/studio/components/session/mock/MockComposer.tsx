// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Fragment, useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import type { AgentDescription, RequirementsDecision, UserInputAnswer } from "@/lib/session/agent-types";
import {
  MOCK_ARGUMENT_VALUE,
  MOCK_CATEGORIES,
  MOCK_CATEGORY_LABELS,
  MOCK_MARK_LEGEND,
  mockScenarioMark,
  mockScenarioOffered,
  mockInputText,
  nextMockInput,
  receivedMockInputs,
  type MockInputChoice,
  type MockProgress,
  type MockScenario,
  type MockScenarioCatalog,
  type MockScenarioInput,
  withoutMockMarkers,
} from "@/lib/session/mock-scenarios";

type PendingInput = NonNullable<AgentDescription["pendingUserInput"]>;

const FIELD =
  "w-full min-w-0 rounded-md border border-line-soft bg-panel px-2 py-1 text-[13px] text-ink focus-visible:border-accent focus-visible:shadow-(--focus-ring) focus-visible:outline-none disabled:cursor-not-allowed disabled:text-mark";

/** Why the input cannot be sent now, as the pending input decides, or null when it can. */
function blockedReason(input: MockScenarioInput, pending: PendingInput | null): string | null {
  switch (input.kind) {
    case "KIND_MESSAGE":
      return pending === null ? null : "Answer the pending input first";
    case "KIND_REQUIREMENTS_CONFIRM":
    case "KIND_REQUIREMENTS_CHANGE":
      return pending?.kind === "requirements" ? null : "Waits for the requirements card";
    case "KIND_ANSWERS":
      return pending?.kind === "question" && pending.questions.length === input.answers.length
        ? null
        : "Waits for the agent's questions";
    case "KIND_UNSPECIFIED":
      return "This input has no kind";
  }
}

/**
 * The composer of a Session on the mock model, which answers only its scenarios' inputs, in
 * order: a category, a scenario of it and its arguments; Send dispatches the scenario's next input
 * as its kind arrives. A message or a request for changes carries the scenario's marker; a
 * confirmation or answers resolve the pending input. Which input is next is
 * what the newest mock reply reported (`progress`), so no input is ever chosen by hand.
 * While a turn runs, and from a send until the Session took the input, nothing can be sent, so a
 * second click cannot send an input twice. The creation page shows the same composer before the
 * Session exists, where the next input is the first one of the scenario.
 */
export function MockComposer({
  catalog,
  progress,
  turnRunning,
  tasksRunning = false,
  lastSequence,
  pendingInput,
  busy,
  disabledReason,
  initial = null,
  newProject = false,
  sendLabel = "Send next input",
  className = "border-t border-line",
  onSend,
  onDecide,
  onAnswer,
  onStop,
  stopDisabled = false,
  stopping = false,
}: {
  catalog: MockScenarioCatalog | null;
  /** Where the newest mock reply left its scenario (AgentDescription.mockProgress). */
  progress: MockProgress;
  /** Whether the Agent is working on a turn. */
  turnRunning: boolean;
  /** Whether a background task runs, whose notification continues the scenario. */
  tasksRunning?: boolean;
  /** The Session's last entry sequence, which an input the Session took raises. */
  lastSequence: number;
  pendingInput: PendingInput | null;
  busy: boolean;
  /** Why the Session takes no input now, or null. */
  disabledReason: string | null;
  /** The scenario and argument values the composer starts with, such as those of an unsent first message. */
  initial?: MockInputChoice | null;
  /** Whether the composer starts a new project, which offers the scenarios that change the source. */
  newProject?: boolean;
  /** The label of the send button. */
  sendLabel?: string;
  /** The frame around the composer; the Session docks it below its conversation. */
  className?: string;
  /** Sends a message input as its text, with the scenario and argument values behind the text. */
  onSend: (text: string, choice: MockInputChoice) => Promise<unknown>;
  onDecide: (callId: string, decision: RequirementsDecision) => Promise<unknown>;
  onAnswer: (callId: string, answers: readonly UserInputAnswer[]) => Promise<unknown>;
  /** Shown while a turn or a task runs; omitted otherwise. */
  onStop?: () => unknown;
  stopDisabled?: boolean;
  /** The Session admitted a Stop and its work is ending. */
  stopping?: boolean;
}) {
  const categoryId = useId();
  const scenarioId = useId();
  const argumentsId = useId();
  const statusId = useId();
  const [chosen, setChosen] = useState<string | null>(initial?.scenario ?? null);
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      Object.entries(initial?.arguments ?? {}).map(([name, value]) => [`${initial?.scenario}:${name}`, value]),
    ),
  );
  // The last entry sequence when an input was sent; the Session has taken it once it grew.
  const [sentAt, setSentAt] = useState<number | null>(null);
  const awaitingInput = sentAt !== null && lastSequence <= sentAt;
  const locked = busy || turnRunning || awaitingInput;
  // Listed by title, which the scenario select shows.
  const scenarios = [...(catalog?.scenarios ?? [])].sort((a, b) => a.title.localeCompare(b.title));
  const offered = (item: MockScenario) => mockScenarioOffered(item, progress, newProject);
  const scenario =
    scenarios.find(({ name }) => name === (chosen ?? progress?.scenario)) ??
    MOCK_CATEGORIES.map((value) => scenarios.find((item) => item.category === value && offered(item))).find(Boolean) ??
    null;
  const category = scenario?.category ?? null;
  const inCategory = scenarios.filter((item) => item.category === category);
  const next = scenario ? nextMockInput(progress, scenario) : 1;
  const received = scenario ? receivedMockInputs(progress, scenario) : 0;
  // The scenario received every input, so Send starts it again from input 1.
  const finished = scenario !== null && received === scenario.inputs.length;
  const input = scenario?.inputs[next - 1] ?? null;
  const argumentValues = Object.fromEntries(
    (scenario?.arguments ?? []).map(({ name, defaultValue }) => [
      name,
      values[`${scenario?.name}:${name}`] ?? defaultValue,
    ]),
  );
  const invalidArgument = Object.entries(argumentValues).find(([, value]) => !MOCK_ARGUMENT_VALUE.test(value));
  const reason =
    catalog === null
      ? "Reading the mock scenarios…"
      : (catalog.error ??
        (scenario === null || input === null
          ? "This environment lists no mock scenario"
          : (disabledReason ??
            (busy
              ? "A request is already running"
              : turnRunning || awaitingInput
                ? "The agent is working on the last input"
                : finished && tasksRunning
                  ? "Background tasks still run; their notifications continue the scenario"
                  : !offered(scenario)
                    ? scenario.testAction
                      ? "It needs a test's action, so it runs only in its Dex test"
                      : "It changes the project, so it starts only in a new project"
                    : invalidArgument
                      ? `${invalidArgument[0]}: letters, digits, space and ._:/- up to 128 characters`
                      : blockedReason(input, pendingInput)))));

  // Escape stops the running work, unless something else, such as a dialog, already took the key.
  useEffect(() => {
    if (!onStop || stopDisabled) return;
    const stopOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented || event.isComposing) return;
      event.preventDefault();
      void onStop();
    };
    window.addEventListener("keydown", stopOnEscape);
    return () => window.removeEventListener("keydown", stopOnEscape);
  }, [onStop, stopDisabled]);

  async function send() {
    if (reason !== null || input === null || scenario === null) return;
    const sentFrom = lastSequence;
    const callId = pendingInput?.callId ?? "";
    const questions = pendingInput?.questions ?? [];
    try {
      switch (input.kind) {
        case "KIND_MESSAGE":
          await onSend(mockInputText(input, argumentValues), { scenario: scenario.name, arguments: argumentValues });
          break;
        case "KIND_REQUIREMENTS_CHANGE":
          await onDecide(callId, { isConfirmed: false, requestedChanges: mockInputText(input, argumentValues) });
          break;
        case "KIND_REQUIREMENTS_CONFIRM":
          await onDecide(callId, { isConfirmed: true, requestedChanges: "" });
          break;
        case "KIND_ANSWERS":
          await onAnswer(
            callId,
            questions.map((question, index) => ({ questionId: question.id, answer: input.answers[index] ?? "" })),
          );
          break;
        case "KIND_UNSPECIFIED":
          break;
      }
      setSentAt(sentFrom);
    } catch {
      // The Session shows why the command failed; the scenario stays at the same input.
    }
  }

  return (
    <div className={cx("bg-panel p-3", className)}>
      <div className="grid min-w-0 gap-2 rounded-xl border border-line bg-well p-2 text-[13px]">
        <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5">
          <label className="font-medium text-ink-dim" htmlFor={categoryId}>
            Category
          </label>
          <select
            className={FIELD}
            id={categoryId}
            aria-describedby={statusId}
            disabled={locked || scenarios.length === 0}
            value={category ?? ""}
            onChange={(event) =>
              setChosen(scenarios.find((item) => item.category === event.target.value && offered(item))?.name ?? null)
            }
          >
            {MOCK_CATEGORIES.map((value) => {
              const count = scenarios.filter((item) => item.category === value).length;
              return (
                <option
                  key={value}
                  value={value}
                  disabled={!scenarios.some((item) => item.category === value && offered(item))}
                >
                  {MOCK_CATEGORY_LABELS[value]} ({count})
                </option>
              );
            })}
          </select>
          <label className="font-medium text-ink-dim" htmlFor={scenarioId}>
            Scenario
          </label>
          <select
            className={FIELD}
            id={scenarioId}
            aria-describedby={statusId}
            disabled={locked || scenarios.length === 0}
            value={scenario?.name ?? ""}
            onChange={(event) => setChosen(event.target.value)}
          >
            {inCategory.map((item) => (
              <option key={item.name} value={item.name} disabled={!offered(item)}>
                {mockScenarioMark(item)} {item.title}
              </option>
            ))}
          </select>
          {scenario?.arguments.map(({ name }) => (
            <Fragment key={`${scenario.name}:${name}`}>
              <label className="font-medium text-ink-dim" htmlFor={`${argumentsId}-${name}`}>
                {name}
              </label>
              <input
                className={FIELD}
                disabled={locked}
                id={`${argumentsId}-${name}`}
                value={argumentValues[name] ?? ""}
                onChange={(event) =>
                  setValues((current) => ({ ...current, [`${scenario.name}:${name}`]: event.target.value }))
                }
              />
            </Fragment>
          ))}
        </div>
        <p className="text-[12px] text-ink-faint">{MOCK_MARK_LEGEND}</p>
        {scenario && (
          <p className="wrap-anywhere text-ink-faint">
            <span className="font-mono">{scenario.name}</span> · {scenario.description}
            {scenario.testAction && ` Needs a test action: the test ${scenario.testAction}.`}
          </p>
        )}
        {input && (input.content || input.answers.length > 0) && (
          <p className="wrap-anywhere text-ink-dim">
            Sends: {input.kind === "KIND_ANSWERS" ? input.answers.join(" · ") : withoutMockMarkers(input.content)}
          </p>
        )}
        <div className="flex flex-wrap items-center justify-end gap-2">
          <span
            className={cx("mr-auto min-w-0 wrap-anywhere", reason === null ? "text-ink" : "text-ink-faint")}
            id={statusId}
            role="status"
            data-mock-next-input={scenario ? next : undefined}
          >
            {scenario === null || input === null
              ? reason
              : finished
                ? `${received === 1 ? "Its input was" : `All ${received} inputs were`} received${reason ? ` · ${reason}` : ""}`
                : reason !== null
                  ? `Input ${next} of ${scenario.inputs.length} · ${reason}`
                  : `Next input ${next} of ${scenario.inputs.length}: ${input.label}${onStop ? " · Esc to stop" : ""}`}
          </span>
          {onStop && (
            <Button disabled={stopDisabled || stopping} onClick={() => void onStop()} aria-keyshortcuts="Escape">
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          )}
          {(scenario === null || offered(scenario)) && (
            <Button variant="primary" disabled={reason !== null} onClick={() => void send()}>
              {finished ? "Start again" : sendLabel}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
