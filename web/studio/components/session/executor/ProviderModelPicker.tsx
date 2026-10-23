// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { ModelProvider } from "@/lib/api/model-credential";
import { useModelCredential } from "@/lib/session/credentials/use-model-credential";
import { POPOVER_ITEM } from "@/components/ui/Popover";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { agentModelView } from "@/lib/session/model/agent-model";
import type { AgentModel } from "@/lib/session/agent-types";
import { ModelCredentialEntry } from "./ModelCredentialEntry";

/** Provider groups come only from the authenticated server catalog. A Builder
 * can enter a provider key before selecting its first model for this Project. */
export function ProviderModelPicker({
  model,
  canManage,
  disabled,
  onSelect,
}: {
  model: AgentModel | null;
  canManage: boolean;
  disabled: boolean;
  onSelect: (model: AgentModel) => void;
}) {
  const session = useSessionResource();
  const [provider, chooseProvider] = useState(session?.credential.provider ?? ModelProvider.MODEL_PROVIDER_ANTHROPIC);
  const credential = useModelCredential(
    session?.projectId ?? "",
    provider,
    session?.status === "SESSION_STATUS_ACTIVE",
    session?.resource?.agent?.description?.modelCredentialVersion,
  );
  if (!session) return null;
  const catalog = session.modelCatalog;
  if (!catalog)
    return (
      <p role="status" className="px-2.25 text-xs text-ink-faint">
        Loading models…
      </p>
    );
  if (catalog.error)
    return (
      <p role="alert" className="px-2.25 text-xs text-fail">
        {catalog.error}
      </p>
    );
  const providers = catalog.models.filter((option) => agentModelView(option.model) !== "mock");
  const groups = [...new Map(providers.map((option) => [option.provider, option])).values()];
  const selected = groups.find((option) => option.provider === provider);
  const choices = providers.filter((option) => option.provider === selected?.provider);
  const mock = catalog.models.find((option) => agentModelView(option.model) === "mock");
  return (
    <>
      <div className="flex flex-wrap gap-1 px-2.25" aria-label="Model providers">
        {groups.map((option) => (
          <button
            type="button"
            key={option.provider}
            aria-pressed={option.provider === selected?.provider}
            className="rounded px-2 py-1 text-xs text-ink-dim hover:bg-hover aria-pressed:bg-accent-wash aria-pressed:text-ink"
            onClick={() => chooseProvider(option.provider)}
          >
            {option.providerName}
          </button>
        ))}
      </div>
      {selected && (
        <ModelCredentialEntry
          key={`${session.projectId}/${selected.provider}`}
          canManage={canManage}
          credential={credential}
          providerName={selected.providerName}
          keyHint={selected.keyHint}
          mockEndpoint={selected.mockEndpoint}
          allowCustom={choices.some((choice) => choice.model.endsWith("_CUSTOM"))}
        />
      )}
      {choices.map((option) => {
        const value = agentModelView(option.model);
        if (!value) return null;
        return (
          <button
            key={value}
            type="button"
            className={POPOVER_ITEM}
            aria-current={value === model ? "page" : undefined}
            disabled={!canManage || disabled}
            onClick={() => onSelect(value)}
          >
            {option.label}
          </button>
        );
      })}
      {mock && (
        <button
          type="button"
          className={POPOVER_ITEM}
          aria-current={model === "mock" ? "page" : undefined}
          disabled={!canManage || disabled}
          onClick={() => onSelect("mock")}
        >
          {mock.label}
          <span className="text-xs text-ink-faint">Testing, no model cost</span>
        </button>
      )}
    </>
  );
}
