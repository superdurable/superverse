// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Chip } from "@/components/ui/Chip";
import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import type { UserInputAnswer, UserInputQuestion } from "@/lib/session/agent-types";

export interface PendingQuestionBatchProps {
  questions: readonly UserInputQuestion[];
  disabled?: boolean;
  isSubmitting?: boolean;
  /** Set when another control answers the questions, which the batch then names instead of its controls. */
  answeredBy?: string;
  onSubmit: (answers: readonly UserInputAnswer[]) => void;
}

interface QuestionDraft {
  selectedOption: string | null;
  detail: string;
  isOther: boolean;
}

/**
 * The batch sits in the pending-action dock: one grid whose middle row scrolls, so long questions
 * stay above their action row. Its height is the dock's less the dock's padding.
 */
const BATCH = cx(
  "grid max-h-[calc(min(68dvh,44rem)-1.5rem)] min-h-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden",
  "rounded-[10px] border border-busy-rule bg-busy-subtle p-2.5 text-[13px] text-ink-dim",
  "max-md:max-h-[calc(min(72dvh,36rem)-1.5rem)]",
);

const OPTION = cx(
  "grid w-full min-w-0 cursor-pointer gap-0.5 rounded-lg border px-3 py-2 text-left wrap-anywhere transition-colors",
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50",
);
const OPTION_CHOSEN = cx("border-accent bg-accent-subtle text-ink");
const OPTION_OPEN = cx("border-line-soft bg-panel text-ink-dim enabled:hover:border-mark");

export function PendingQuestionBatch({
  questions,
  disabled: isDisabled = false,
  isSubmitting = false,
  answeredBy,
  onSubmit,
}: PendingQuestionBatchProps) {
  const disabled = isDisabled || answeredBy !== undefined;
  const [currentIndex, setCurrentIndex] = useState(0);
  const [drafts, setDrafts] = useState<Record<string, QuestionDraft>>({});
  const question = questions[currentIndex];
  if (question === undefined) return null;
  const currentDraft = drafts[question.id];
  const hasAllAnswers = questions.every(({ id }) => questionAnswer(drafts[id]) !== "");
  const isLast = currentIndex === questions.length - 1;
  const chooseOption = (selectedOption: string) => {
    setDrafts((current) => ({
      ...current,
      [question.id]: {
        selectedOption,
        detail:
          current[question.id]?.isOther === false && current[question.id]?.selectedOption === selectedOption
            ? (current[question.id]?.detail ?? "")
            : "",
        isOther: false,
      },
    }));
  };
  const chooseOther = () => {
    setDrafts((current) => ({
      ...current,
      [question.id]: {
        selectedOption: null,
        detail: current[question.id]?.isOther === true ? (current[question.id]?.detail ?? "") : "",
        isOther: true,
      },
    }));
  };
  const setDetail = (detail: string) => {
    setDrafts((current) => {
      const draft = current[question.id];
      if (draft === undefined) return current;
      return { ...current, [question.id]: { ...draft, detail } };
    });
  };
  const submit = () => {
    if (!hasAllAnswers || disabled) return;
    onSubmit(
      questions.map(({ id }) => ({
        questionId: id,
        answer: questionAnswer(drafts[id]),
      })),
    );
  };

  return (
    <section className={BATCH} aria-label="Agent questions">
      <div className="flex flex-wrap items-start justify-between gap-2.5">
        <div>
          <Eyebrow>Agent needs your input</Eyebrow>
          <strong className="font-semibold text-ink">
            Question {String(currentIndex + 1)} of {String(questions.length)}
          </strong>
        </div>
        <div className="flex min-w-0 flex-[1_1_18rem] flex-wrap items-center gap-1.5" aria-label="Questions">
          {questions.map((candidate, index) => (
            <Chip
              on={index === currentIndex}
              className="flex max-w-full min-w-0 items-center gap-1"
              aria-current={index === currentIndex ? "step" : undefined}
              title={candidate.header}
              key={candidate.id}
              onClick={() => {
                setCurrentIndex(index);
              }}
            >
              <span className="truncate">{candidate.header}</span>
              {questionAnswer(drafts[candidate.id]) !== "" && (
                <span className="text-pass" aria-label="Answered">
                  ✓
                </span>
              )}
            </Chip>
          ))}
        </div>
      </div>
      <fieldset
        className="my-3 min-h-0 min-w-0 overflow-x-hidden overflow-y-auto overscroll-contain px-0.5"
        disabled={disabled}
      >
        <legend className="font-semibold text-ink">{question.header}</legend>
        <p className="mt-1 wrap-anywhere">{question.question}</p>
        <div className="mt-2.5 grid grid-cols-[repeat(auto-fit,minmax(min(100%,15rem),1fr))] gap-2 p-0.5">
          {question.options.map((option) => {
            const isSelected = currentDraft?.isOther === false && currentDraft.selectedOption === option.label;
            return (
              <button
                type="button"
                className={cx(OPTION, isSelected ? OPTION_CHOSEN : OPTION_OPEN)}
                key={option.label}
                onClick={() => {
                  chooseOption(option.label);
                }}
              >
                <strong className="font-semibold">{option.label}</strong>
                <small className="text-xs">{option.description}</small>
              </button>
            );
          })}
          <button
            type="button"
            className={cx(OPTION, currentDraft?.isOther === true ? OPTION_CHOSEN : OPTION_OPEN)}
            onClick={() => {
              chooseOther();
            }}
          >
            <strong className="font-semibold">Other</strong>
            <small className="text-xs">Enter a different answer.</small>
          </button>
        </div>
        {currentDraft !== undefined && (
          <label className="mt-2.5 grid gap-1.5 text-xs font-semibold">
            {currentDraft.isOther ? "Your answer" : "Add details (optional)"}
            <input
              aria-label={
                currentDraft.isOther
                  ? `Other answer for ${question.header}`
                  : `Additional details for ${question.header}`
              }
              className="w-full rounded-lg border border-line bg-panel px-3 py-2 text-[13px] font-normal text-ink placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              placeholder={currentDraft.isOther ? "Enter your answer…" : "Add dates, constraints, or context…"}
              value={currentDraft.detail}
              onChange={(event) => {
                setDetail(event.target.value);
              }}
            />
          </label>
        )}
      </fieldset>
      <div className="-mx-2.5 -mb-2.5 flex flex-wrap items-center justify-end gap-2 border-t border-busy-rule bg-panel p-2.5">
        {answeredBy && <p className="mr-auto text-xs">{answeredBy}</p>}
        <Button
          disabled={disabled || currentIndex === 0}
          onClick={() => {
            setCurrentIndex((index) => Math.max(0, index - 1));
          }}
        >
          Previous
        </Button>
        {!isLast && (
          <Button
            disabled={disabled || questionAnswer(currentDraft) === ""}
            onClick={() => {
              setCurrentIndex((index) => index + 1);
            }}
          >
            Next
          </Button>
        )}
        {isLast && (
          <Button variant="primary" disabled={disabled || !hasAllAnswers} onClick={submit}>
            {isSubmitting ? "Submitting answers…" : "Submit all"}
          </Button>
        )}
      </div>
    </section>
  );
}

function questionAnswer(draft: QuestionDraft | undefined): string {
  if (draft === undefined) return "";
  const detail = draft.detail.trim();
  if (draft.isOther) return detail;
  const selectedOption = draft.selectedOption?.trim() ?? "";
  if (selectedOption === "" || detail === "") return selectedOption;
  return `${selectedOption}: ${detail}`;
}
