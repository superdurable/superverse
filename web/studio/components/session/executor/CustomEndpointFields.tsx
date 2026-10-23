// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { RefObject } from "react";
import type { CredentialInfo, ModelProvider } from "@/lib/api/model-credential";

/** Custom endpoint draft fields contain only bounded configuration, never a saved key. */
export function CustomEndpointFields({
  endpointField,
  modelField,
  contextField,
  outputField,
  info,
  busy,
  models,
  discoveryNote,
  provider,
  discover,
  clearDiscovery,
  chooseModel,
  editContext,
}: {
  endpointField: RefObject<HTMLInputElement | null>;
  modelField: RefObject<HTMLInputElement | null>;
  contextField: RefObject<HTMLInputElement | null>;
  outputField: RefObject<HTMLInputElement | null>;
  info: CredentialInfo | null;
  busy: string | null;
  models: string[];
  discoveryNote: string;
  provider: ModelProvider;
  discover: () => void;
  clearDiscovery: () => void;
  chooseModel: (model: string) => void;
  editContext: () => void;
}) {
  return (
    <>
      <p className="text-ink-faint">
        Protocol:{" "}
        {provider === "MODEL_PROVIDER_ANTHROPIC"
          ? "Anthropic Messages"
          : provider === "MODEL_PROVIDER_OPENAI" || provider === "MODEL_PROVIDER_XAI"
            ? "OpenAI Responses"
            : "OpenAI Chat Completions"}
      </p>
      <label className="grid gap-1">
        Endpoint API base URL
        <input
          ref={endpointField}
          type="url"
          maxLength={2048}
          defaultValue={info?.baseUrl ?? ""}
          disabled={busy !== null}
          className="h-8 rounded border border-line bg-panel px-2 text-ink"
          onChange={clearDiscovery}
        />
      </label>
      <label className="grid gap-1">
        Model ID (optional)
        <input
          ref={modelField}
          maxLength={256}
          defaultValue={info?.customModelId ?? ""}
          list={`model-ids-${provider}`}
          disabled={busy !== null}
          className="h-8 rounded border border-line bg-panel px-2 text-ink"
          onChange={(event) => chooseModel(event.target.value)}
        />
      </label>
      {models.length > 1 && (
        <label className="grid gap-1">
          Discovered models
          <select
            aria-label="Discovered models"
            defaultValue=""
            disabled={busy !== null}
            className="h-8 rounded border border-line bg-panel px-2 text-ink"
            onChange={(event) => {
              if (modelField.current) modelField.current.value = event.target.value;
              chooseModel(event.target.value);
            }}
          >
            <option value="">Choose a model</option>
            {models.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </select>
        </label>
      )}
      <datalist id={`model-ids-${provider}`}>
        {models.map((id) => (
          <option key={id} value={id} />
        ))}
      </datalist>
      <label className="grid gap-1">
        Context window (tokens)
        <input
          ref={contextField}
          type="number"
          min={32768}
          max={1000000}
          step={1}
          required
          defaultValue={info?.contextWindow && info.contextWindow !== "0" ? info.contextWindow : "32768"}
          disabled={busy !== null}
          onChange={editContext}
          className="h-8 rounded border border-line bg-panel px-2 text-ink"
        />
      </label>
      <p className="text-ink-faint">
        Enter the supported endpoint capacity: 32,768 to 1,000,000 tokens. Discovery suggests a default; it does not
        verify capacity.
      </p>
      <label className="grid gap-1">
        Maximum output tokens
        <input
          ref={outputField}
          type="number"
          min={1}
          max={65536}
          step={1}
          placeholder="4096"
          defaultValue={info?.maxOutputTokens && info.maxOutputTokens !== "0" ? info.maxOutputTokens : ""}
          disabled={busy !== null}
          className="h-8 rounded border border-line bg-panel px-2 text-ink"
        />
      </label>
      <p className="text-ink-faint">
        Optional; defaults to 4,096. At most 65,536 tokens or one eighth of the context window.
      </p>
      <button type="button" disabled={busy !== null} onClick={discover} className="text-left text-accent">
        {busy === "discover" ? "Discovering…" : "Discover models"}
      </button>
      <p className="text-ink-faint">
        Leave the model ID empty to use the only discovered model. Text and serial tools only. Your endpoint receives
        the conversation.
      </p>
      {discoveryNote && <p role="status">{discoveryNote}</p>}
    </>
  );
}

/** Validate declared budgets before making any credential request. */
export function customBudgetError(contextWindow: string, maxOutputTokens: string) {
  const context = Number(contextWindow),
    output = Number(maxOutputTokens);
  if (!/^\d+$/.test(contextWindow) || !Number.isSafeInteger(context) || context < 32768 || context > 1000000)
    return "Context window must contain 32768 to 1000000 tokens.";
  if (
    !/^\d+$/.test(maxOutputTokens) ||
    !Number.isSafeInteger(output) ||
    output < 1 ||
    output > Math.min(65536, Math.floor(context / 8))
  )
    return "Maximum output tokens must be from 1 to the smaller of 65536 and one eighth of the context window.";
  return "";
}
