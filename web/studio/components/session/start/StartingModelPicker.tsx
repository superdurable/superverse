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
import { AGENT_MODEL_OPTIONS, type AgentModel } from "@/lib/session/agent-types";

/** Chooses the model of a Project whose Superverse Session has not started yet. */
export function StartingModelPicker({
  model,
  mockSelectable,
  disabled,
  onSelect,
  className,
}: {
  model: AgentModel | null;
  mockSelectable: boolean;
  disabled: boolean;
  onSelect: (model: AgentModel) => void;
  className?: string;
}) {
  const selectId = useId();
  const options = AGENT_MODEL_OPTIONS.filter(({ value }) => value !== "mock" || mockSelectable || model === "mock");
  return (
    <div className={cx("bg-panel px-3 py-2 text-[13px] text-ink-faint", className)}>
      <div className="flex items-center gap-2">
        <label className="font-medium text-ink-dim" htmlFor={selectId}>
          Model
        </label>
        <Dropdown
          id={selectId}
          label="Model"
          inline
          disabled={disabled}
          value={model}
          placeholder="Not selected yet"
          onChange={onSelect}
          items={options.map((option) => ({ value: option.value, label: option.label, sub: option.description }))}
        />
      </div>
    </div>
  );
}
