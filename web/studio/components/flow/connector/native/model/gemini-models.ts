// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  collectProviderPages,
  executeFirstAcceptedProviderCommand,
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  readProviderModelArray,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
} from "./model-listing";

/**
 * GeminiModelListCommands names the two Gemini model list commands a
 * connector declares under one capability: the native models.list, whose key
 * travels in the x-goog-api-key header, and the OpenAI-compatible list, whose
 * key is a bearer token.
 */
interface GeminiModelListCommands {
  /** capability is the manifest capability both commands declare, such as "gemini.models-list". */
  capability: string;
  /** nativeCommandId is the GET /v1beta/models command, which declares a pageToken query parameter. */
  nativeCommandId: string;
  /** openAICompatibleCommandId is the GET /v1beta/openai/models command. */
  openAICompatibleCommandId: string;
}

// Model IDs that name a non-text family stay behind "Show all models", even when they support generateContent.
const nonTextModelFragments = [
  "embedding",
  "imagen",
  "veo",
  "aqa",
  "tts",
  "image",
  "audio",
  "live",
  "lyria",
  "robotics",
] as const;

/**
 * loadGeminiModelListing lists Gemini models through the host broker and
 * projects them for ModelPicker. It tries the native list first, following
 * `nextPageToken` with its `pageToken` parameter; Dex Web through cli-v0.13.8
 * rejects its x-goog-api-key credential before sending, so the
 * OpenAI-compatible list, which accepts a bearer key, answers there instead.
 * It rejects with the last host error when both commands fail, so ModelPicker
 * offers manual model entry.
 */
export async function loadGeminiModelListing(
  client: ConnectorStudioClient,
  commands: GeminiModelListCommands,
): Promise<ModelListing> {
  const firstPage = await executeFirstAcceptedProviderCommand(client, commands.capability, [
    commands.nativeCommandId,
    commands.openAICompatibleCommandId,
  ]);
  if (!Array.isArray(firstPage.models)) return projectOpenAICompatibleGeminiModelList(firstPage);
  const pages = await collectProviderPages(async (cursor) => {
    const page =
      cursor === ""
        ? firstPage
        : await client.executeProviderCommand(commands.nativeCommandId, commands.capability, { pageToken: cursor });
    return {
      items: readProviderModelArray(page, "models"),
      nextCursor: typeof page.nextPageToken === "string" ? page.nextPageToken : "",
    };
  });
  return { models: projectNativeGeminiModels(pages.items), isTruncated: pages.isTruncated };
}

/**
 * projectNativeGeminiModels reads models.list items. The ID is the resource
 * name without its "models/" prefix; a model whose supportedGenerationMethods
 * lack generateContent, or whose ID names a non-text family, is hidden.
 */
function projectNativeGeminiModels(items: Record<string, unknown>[]): ModelOption[] {
  const seenIDs = new Set<string>();
  return items.flatMap((item) => {
    const id = typeof item.name === "string" ? item.name.trim().replace(/^models\//, "") : "";
    if (id === "" || seenIDs.has(id)) return [];
    seenIDs.add(id);
    const methods = Array.isArray(item.supportedGenerationMethods) ? item.supportedGenerationMethods : [];
    return [
      {
        id,
        label:
          typeof item.displayName === "string" && item.displayName.trim() !== "" ? item.displayName.trim() : undefined,
        detail: tokenLimitDetail(item),
        badges: item.thinking === true ? ["thinking"] : undefined,
        isHiddenByDefault: !methods.includes("generateContent") || hasAnyModelIDFragment(id, nonTextModelFragments),
      },
    ];
  });
}

/**
 * projectOpenAICompatibleGeminiModelList reads the OpenAI-compatible
 * `{data: [{id: "models/…"}]}` list, strips the "models/" prefix the connector
 * accepts either way, and hides non-text models, because that list has no
 * supportedGenerationMethods.
 */
function projectOpenAICompatibleGeminiModelList(value: Record<string, unknown>): ModelListing {
  const models = openAICompatibleModelOptions(value, {
    isHiddenByDefault: (id) => hasAnyModelIDFragment(id, nonTextModelFragments),
  }).map((model) => ({ ...model, id: model.id.replace(/^models\//, "") }));
  return {
    models: models.filter(
      (model, index) => model.id !== "" && models.findIndex((other) => other.id === model.id) === index,
    ),
  };
}

function tokenLimitDetail(item: Record<string, unknown>): string | undefined {
  const { inputTokenLimit, outputTokenLimit } = item;
  if (typeof inputTokenLimit !== "number" || typeof outputTokenLimit !== "number") return undefined;
  return `${inputTokenLimit.toLocaleString("en-US")} input / ${outputTokenLimit.toLocaleString("en-US")} output tokens`;
}
