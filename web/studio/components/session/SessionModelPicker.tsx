// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useId } from "react";
import { cx } from "@/components/ui/cx";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { AGENT_MODEL_OPTIONS, agentModelLabel, type AgentModel } from "@/lib/session/agent-types";

/**
 * The model of a Session: a selector for the model its next turn runs on, the model and effort its
 * current context runs on, and a note while the two differ. Changing the model is allowed at any
 * time; the Agent takes it up when its next turn starts. The mock model is listed only where the
 * server lets this account select it; the server still authorizes every change.
 *
 * The creation page shows the same picker for a Session that does not exist yet; the Session
 * applies the chosen model once it starts.
 */
export function SessionModelPicker({
  model,
  contextModel = null,
  effort = "",
  mockSelectable,
  disabled,
  onSelect,
  className = "border-t border-line",
}: {
  /** The selected model, or null before the Agent has one or when none was chosen. */
  model: AgentModel | null;
  /** The model the current context segment runs on, or null before its first turn. */
  contextModel?: AgentModel | null;
  /** The effort of the current context, empty when unknown or not applicable. */
  effort?: string;
  /** Whether ListMockScenarios lets this account select the mock model here. */
  mockSelectable: boolean;
  disabled: boolean;
  onSelect: (model: AgentModel) => void;
  /** The frame around the picker; the Session docks it above its composer. */
  className?: string;
}) {
  const selectId = useId();
  const noteId = useId();
  const options = AGENT_MODEL_OPTIONS.filter(({ value }) => value !== "mock" || mockSelectable || model === "mock");
  const current = contextModel ?? model;
  const switchPending = model !== null && contextModel !== null && model !== contextModel;
  return (
    <div className={cx("shrink-0 bg-panel px-3 py-2 text-[13px] text-ink-faint", className)}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="flex min-w-0 items-center gap-2">
          <label className="font-medium text-ink-dim" htmlFor={selectId}>
            Model
          </label>
          <Dropdown
            id={selectId}
            label="Model"
            inline
            describedBy={switchPending ? noteId : undefined}
            disabled={disabled}
            value={model}
            placeholder="Not selected yet"
            onChange={onSelect}
            items={options.map((option) => ({ value: option.value, label: option.label, sub: option.description }))}
          />
        </div>
        {current === null ? null : (
          <p data-testid="session-model-line">
            {agentModelLabel(current)}
            {effort ? `, ${effort} effort` : ""}
          </p>
        )}
      </div>
      {switchPending ? (
        <p className="mt-1" id={noteId}>
          Takes effect at the start of the next turn
        </p>
      ) : null}
    </div>
  );
}
