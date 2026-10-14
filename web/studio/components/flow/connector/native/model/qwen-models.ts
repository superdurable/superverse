// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { asRecord, stringArrayField } from "@/lib/shared/json";
import {
  collectProviderPages,
  hasAnyModelIDFragment,
  readProviderModelArray,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
} from "./model-listing";

/** qwenModelsListCapability is the manifest capability of every model-list command. */
const qwenModelsListCapability = "qwen.models-list";

/**
 * QwenModelListRegion names one DashScope host's two model-list commands:
 * the `capabilities=TG` list and the `capabilities=Reasoning` list.
 */
export interface QwenModelListRegion {
  /** textGenerationCommandId lists models with the TG capability. */
  textGenerationCommandId: string;
  /** reasoningCommandId lists models with the Reasoning capability, including thinking-only models. */
  reasoningCommandId: string;
}

/**
 * qwenModelListRegions are the manifest's model-list hosts in the order the
 * bundle tries them: Singapore, then China (Hong Kong). A key works only in its
 * own region, so the first host whose text generation list accepts it serves
 * every later page and the Reasoning list.
 */
const qwenModelListRegions: readonly QwenModelListRegion[] = [
  { textGenerationCommandId: "listModels", reasoningCommandId: "listReasoningModels" },
  { textGenerationCommandId: "listModelsHongKong", reasoningCommandId: "listReasoningModelsHongKong" },
];

// The manifest's page_size is 100, so five pages list up to 500 models per list before isTruncated.
const maxModelListPages = 5;

// Fallback for entries without modality metadata: these families do not serve Chat Completions.
const nonChatModelFragments = ["realtime", "livetranslate", "tts", "asr", "embedding", "rerank"] as const;

/**
 * loadQwenModels lists Model Studio text generation and reasoning models
 * through the host broker, page by page, from the first of regions that
 * accepts the key, and projects them for ModelPicker. It rethrows the last
 * region's error when no region accepts the key, and any later page's error
 * as is. A connector with other command IDs, such as llm, which lists only
 * the connection's own region, passes its regions and capability.
 */
export async function loadQwenModels(
  client: ConnectorStudioClient,
  regions: readonly QwenModelListRegion[] = qwenModelListRegions,
  capability = qwenModelsListCapability,
): Promise<ModelListing> {
  const accepted = await findAcceptingModelListRegion(client, regions, capability);
  const textGenerationPages = await collectModelListPages((pageNumber) =>
    pageNumber === 1
      ? Promise.resolve(accepted.firstTextGenerationPage)
      : executeModelListPage(client, capability, accepted.region.textGenerationCommandId, pageNumber),
  );
  const reasoningPages = await collectModelListPages((pageNumber) =>
    executeModelListPage(client, capability, accepted.region.reasoningCommandId, pageNumber),
  );
  const listing = projectQwenModelList(...textGenerationPages.items, ...reasoningPages.items);
  return { models: listing.models, isTruncated: textGenerationPages.isTruncated || reasoningPages.isTruncated };
}

async function findAcceptingModelListRegion(
  client: ConnectorStudioClient,
  regions: readonly QwenModelListRegion[],
  capability: string,
): Promise<{ region: QwenModelListRegion; firstTextGenerationPage: Record<string, unknown> }> {
  let lastError: unknown = new Error("No model-list region is declared");
  for (const region of regions) {
    try {
      return {
        region,
        firstTextGenerationPage: await executeModelListPage(client, capability, region.textGenerationCommandId, 1),
      };
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError;
}

function collectModelListPages(
  fetchModelListPage: (pageNumber: number) => Promise<Record<string, unknown>>,
): Promise<{ items: Record<string, unknown>[]; isTruncated: boolean }> {
  return collectProviderPages(async (cursor) => {
    const pageNumber = cursor === "" ? 1 : Number(cursor);
    const page = await fetchModelListPage(pageNumber);
    return { items: [page], nextCursor: hasNextModelPage(page, pageNumber) ? String(pageNumber + 1) : "" };
  }, maxModelListPages);
}

function executeModelListPage(
  client: ConnectorStudioClient,
  capability: string,
  commandId: string,
  pageNumber: number,
): Promise<Record<string, unknown>> {
  return client.executeProviderCommand(commandId, capability, { pageNumber: String(pageNumber) });
}

/**
 * projectQwenModelList reads the native `GET /api/v1/models` pages
 * (`{output: {models: [{model, name, provider, ...}]}}`) in the order given,
 * keeping the first entry for a model ID that several lists return. Qwen
 * models with text input and output are shown; third-party models, models
 * without text input and output, and realtime models stay behind "Show all
 * models". A model with a scheduled offline time names it in its detail line.
 */
function projectQwenModelList(...pages: Record<string, unknown>[]): ModelListing {
  const seenIDs = new Set<string>();
  const models: ModelOption[] = [];
  for (const page of pages) {
    for (const item of readProviderModelArray(asRecord(page.output), "models")) {
      const id = typeof item.model === "string" ? item.model.trim() : "";
      if (id === "" || seenIDs.has(id)) continue;
      seenIDs.add(id);
      models.push({
        id,
        label: typeof item.name === "string" && item.name.trim() !== "" ? item.name.trim() : undefined,
        detail: modelDetail(item),
        badges: modelBadges(item),
        isHiddenByDefault: !isQwenChatModel(id, item),
      });
    }
  }
  return { models };
}

function hasNextModelPage(page: Record<string, unknown>, pageNumber: number): boolean {
  const output = asRecord(page.output);
  const total = typeof output.total === "number" ? output.total : 0;
  const pageSize = typeof output.page_size === "number" && output.page_size > 0 ? output.page_size : 0;
  return pageSize > 0 && readProviderModelArray(output, "models").length > 0 && pageNumber * pageSize < total;
}

function isQwenChatModel(id: string, item: Record<string, unknown>): boolean {
  if (item.provider !== "qwen" || hasAnyModelIDFragment(id, nonChatModelFragments)) return false;
  if (stringArrayField(item.capabilities).some((capability) => capability.startsWith("Realtime"))) return false;
  const metadata = asRecord(item.inference_metadata);
  const requestModalities = stringArrayField(metadata.request_modality);
  const responseModalities = stringArrayField(metadata.response_modality);
  return (
    (requestModalities.length === 0 || requestModalities.includes("Text")) &&
    (responseModalities.length === 0 || responseModalities.includes("Text"))
  );
}

function modelDetail(item: Record<string, unknown>): string | undefined {
  const offlineTime = asRecord(item.inference_offline_info).offline_time;
  if (typeof offlineTime === "string" && offlineTime.trim() !== "")
    return `Scheduled to go offline ${offlineTime.trim()}.`;
  const contextWindow = asRecord(item.model_info).context_window;
  return typeof contextWindow === "number" && contextWindow > 0
    ? `${contextWindow.toLocaleString("en-US")}-token context`
    : undefined;
}

function modelBadges(item: Record<string, unknown>): string[] {
  const badges: string[] = [];
  if (stringArrayField(item.capabilities).includes("Reasoning")) badges.push("reasoning");
  if (stringArrayField(item.features).includes("structured-outputs")) badges.push("structured output");
  return badges;
}
