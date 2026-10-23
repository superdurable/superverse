// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, useState } from "react";
import { cx } from "@/components/ui/cx";
import { PopoverCard, usePopover } from "@/components/ui/Popover";
import type { ModelProvider } from "@/lib/api/model-credential";
import { agentModelLabel, type AgentModel } from "@/lib/session/agent-types";
import { agentModelInput, agentModelProvider } from "@/lib/session/model/agent-model";
import type { useAgentModels } from "@/lib/session/model/agent-model-catalog";
import { useModelCredential } from "@/lib/session/credentials/use-model-credential";
import type { StartingCredential } from "@/lib/session/credentials/starting-credential";
import { ProviderModelChoices } from "@/components/session/executor/ProviderModelPicker";
import { ModelCredentialEntry } from "@/components/session/executor/ModelCredentialEntry";

/** Reuses the composer picker; pre-Project keys stay in the start form's memory. */
export function StartingModelPicker({
  model,
  catalog,
  disabled,
  onSelect,
  onCredential,
  className,
}: {
  model: AgentModel | null;
  catalog: ReturnType<typeof useAgentModels>;
  disabled: boolean;
  onSelect: (model: AgentModel) => void;
  onCredential: (credential: StartingCredential | null) => void;
  className?: string;
}) {
  const [locked, setLocked] = useState(false);
  const [opened, setOpened] = useState(false);
  const [provider, setProvider] = useState<ModelProvider>(
    agentModelProvider(model ? agentModelInput(model) : undefined),
  );
  const credential = useModelCredential("", provider, false, undefined);
  const anchor = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  return (
    <div ref={anchor} className={cx("bg-panel px-3 py-2 text-[13px] text-ink-faint", className)}>
      <button
        type="button"
        aria-label="Model"
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        disabled={disabled}
        onClick={() => {
          setOpened(true);
          popover.toggle();
        }}
        className="text-ink-dim"
      >
        Model · {model ? agentModelLabel(model) : "Loading models…"}
      </button>
      {(popover.open || opened) && (
        <PopoverCard
          anchorRef={anchor}
          cardRef={card}
          label="Choose model"
          autoFocus={popover.open}
          className={`w-80 max-w-[90vw] p-2 ${popover.open ? "" : "hidden"}`}
        >
          <ProviderModelChoices
            choiceRole="option"
            model={model}
            catalog={catalog}
            provider={provider}
            chooseProvider={(next) => {
              setProvider(next);
              onCredential(null);
            }}
            canManage
            disabled={disabled || locked}
            credentialLocked={locked}
            onSelect={(value) => {
              onSelect(value);
              popover.close();
            }}
            credentialEntry={(selected, choices) => (
              <ModelCredentialEntry
                key={selected.provider}
                canManage
                onLocked={setLocked}
                credential={credential}
                providerName={selected.providerName}
                keyHint={selected.keyHint}
                mockEndpoint={selected.mockEndpoint}
                allowCustom={choices.some((choice) => choice.model.endsWith("_CUSTOM"))}
                stage={{ provider: selected.provider, onSave: onCredential }}
              />
            )}
          />
        </PopoverCard>
      )}
    </div>
  );
}
