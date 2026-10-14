// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { openAICompatibleModelOptions, type ConnectorStudioClient, type ModelListing } from "./model-listing";

/** deepSeekModelsListCapability is the manifest capability of the listModels command. */
const deepSeekModelsListCapability = "deepseek.models-list";

const binaryMillionTokens = 1 << 20;
const binaryThousandTokens = 1 << 10;

/** loadDeepSeekModels lists DeepSeek models through the host broker and projects them for ModelPicker. */
export async function loadDeepSeekModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectDeepSeekModelList(await client.executeProviderCommand("listModels", deepSeekModelsListCapability));
}

/**
 * projectDeepSeekModelList reads the OpenAI-compatible `{data: [{id}]}` list in
 * the order DeepSeek returns it, labels each model with its display `name`,
 * and describes its context window and output limit. A model whose
 * `output_modalities` omit text cannot serve generateText, so it stays behind
 * "Show all models".
 */
export function projectDeepSeekModelList(value: Record<string, unknown>): ModelListing {
  return {
    models: openAICompatibleModelOptions(value, {
      isHiddenByDefault: (_id, item) =>
        Array.isArray(item.output_modalities) && !item.output_modalities.includes("text"),
      label: (item) => (typeof item.name === "string" && item.name.trim() !== "" ? item.name.trim() : undefined),
      detail: describeTokenLimits,
    }),
  };
}

function describeTokenLimits(item: Record<string, unknown>): string | undefined {
  const contextWindow = formatTokenCount(item.context_window);
  const maxOutputTokens = formatTokenCount(item.max_output_tokens);
  if (contextWindow !== undefined && maxOutputTokens !== undefined) {
    return `${contextWindow}-token context, up to ${maxOutputTokens} output tokens`;
  }
  if (contextWindow !== undefined) return `${contextWindow}-token context`;
  if (maxOutputTokens !== undefined) return `Up to ${maxOutputTokens} output tokens`;
  return undefined;
}

// formatTokenCount writes DeepSeek's binary limits as the docs do, such as 1048576 as "1M" and 393216 as "384K".
function formatTokenCount(value: unknown): string | undefined {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) return undefined;
  if (value % binaryMillionTokens === 0) return `${value / binaryMillionTokens}M`;
  if (value % binaryThousandTokens === 0) return `${value / binaryThousandTokens}K`;
  return value.toLocaleString("en-US");
}
