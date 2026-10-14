// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord } from "@/lib/shared/json";

/** ModelOption is one model a provider listed, projected by connector UI code. */
export interface ModelOption {
  /** id is the exact model ID sent to the provider. */
  id: string;
  /** label is a human-readable name; the ID is shown when it is absent. */
  label?: string;
  /** detail is one short line, such as a context window or a deprecation date. */
  detail?: string;
  /** badges are short capability tags, such as "structured output". */
  badges?: string[];
  /**
   * isHiddenByDefault marks a listed model that the connector's filter judged
   * unsuitable for this operation, such as an embedding model. "Show all
   * models" reveals it.
   */
  isHiddenByDefault?: boolean;
  /** Why choosing this option will not work yet, such as a Slack channel the app is not in. */
  unavailableHint?: string;
}

/** ModelListingNotice is one message a loader shows with the listed models. */
interface ModelListingNotice {
  /** tone is "attention" for a problem the user can act on, such as a provider that could not be listed, or "info". */
  tone: "attention" | "info";
  /** message is shown as written; it must not contain credential material. */
  message: string;
}

/** ModelListing is the result of one live model list. */
export interface ModelListing {
  models: ModelOption[];
  /** isTruncated reports that paging stopped before the provider's last page. */
  isTruncated?: boolean;
  /**
   * notices are shown in order above the models once the list loads, such as
   * one per source of a combined list that could not be listed. A listing
   * without notices renders as before.
   */
  notices?: ModelListingNotice[];
}

/** Studio injects only declared provider commands; no frame, URL or credential access. */
export interface ConnectorStudioClient {
  executeProviderCommand(
    commandId: string,
    capability: string,
    parameters?: Record<string, string>,
  ): Promise<Record<string, unknown>>;
}

/** ConnectorStudioConnection is the connection a loader lists for: its saved non-secret configuration, {} before the first save. */
export interface ConnectorStudioConnection {
  configuration: Record<string, unknown>;
}

/**
 * ProviderModelListCommand names one manifest-declared Studio provider
 * command that lists a provider's models, and the capability the command,
 * the unit, and `studio.setup` declare. Each connector passes its own IDs, so
 * one projection serves every connector that declares the same provider
 * request.
 */
export interface ProviderModelListCommand {
  /** capability is the command's manifest capability, such as "openai.models-list". */
  capability: string;
  /** commandId is the command's manifest ID, such as "listModels". */
  commandId: string;
}

/** ProviderPage is one page of provider items and the cursor for the next page, empty at the end. */
export interface ProviderPage<T> {
  items: T[];
  nextCursor: string;
}

/**
 * collectProviderPages fetches pages until nextCursor is empty, a cursor
 * repeats, or maxPages pages have been read, and returns every item in order.
 * isTruncated reports that maxPages stopped the loop before the last page.
 */
export async function collectProviderPages<T>(
  fetchPage: (cursor: string) => Promise<ProviderPage<T>>,
  maxPages = 20,
): Promise<{ items: T[]; isTruncated: boolean }> {
  const items: T[] = [];
  const seenCursors = new Set<string>();
  let cursor = "";
  for (let page = 0; page < maxPages; page++) {
    const result = await fetchPage(cursor);
    items.push(...result.items);
    if (result.nextCursor === "" || seenCursors.has(result.nextCursor)) return { items, isTruncated: false };
    seenCursors.add(result.nextCursor);
    cursor = result.nextCursor;
  }
  return { items, isTruncated: true };
}

/**
 * executeFirstAcceptedProviderCommand runs commandIds in order and returns the
 * first result the host accepts. A connector declares one command per fixed
 * provider host, such as a global and a China platform, because a key works on
 * only one of them; each host stays pinned in the manifest. The last error is
 * rethrown when every command fails.
 */
export async function executeFirstAcceptedProviderCommand(
  client: ConnectorStudioClient,
  capability: string,
  commandIds: string[],
  parameters: Record<string, string> = {},
): Promise<Record<string, unknown>> {
  let lastError: unknown = new Error("No provider command is declared");
  for (const commandId of commandIds) {
    try {
      return await client.executeProviderCommand(commandId, capability, parameters);
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError;
}

/** readProviderModelArray returns value[field] when it is an array of objects, else an empty array. */
export function readProviderModelArray(value: Record<string, unknown>, field = "data"): Record<string, unknown>[] {
  const items = value[field];
  return Array.isArray(items) ? items.filter(isRecord) : [];
}

/**
 * openAICompatibleModelOptions projects an OpenAI-style `{data: [{id}]}` list.
 * isHiddenByDefault marks models the connector's filter judges unsuitable,
 * such as embedding models; label and detail come from optional mappers.
 */
export function openAICompatibleModelOptions(
  value: Record<string, unknown>,
  projection: {
    isHiddenByDefault?(id: string, item: Record<string, unknown>): boolean;
    label?(item: Record<string, unknown>): string | undefined;
    detail?(item: Record<string, unknown>): string | undefined;
    badges?(item: Record<string, unknown>): string[];
  } = {},
): ModelOption[] {
  const seenIDs = new Set<string>();
  return readProviderModelArray(value).flatMap((item) => {
    const id = typeof item.id === "string" ? item.id.trim() : "";
    if (id === "" || seenIDs.has(id)) return [];
    seenIDs.add(id);
    return [
      {
        id,
        label: projection.label?.(item),
        detail: projection.detail?.(item),
        badges: projection.badges?.(item),
        isHiddenByDefault: projection.isHiddenByDefault?.(id, item) ?? false,
      },
    ];
  });
}

/**
 * hasAnyModelIDFragment reports whether id contains one of fragments, ignoring
 * case. Connectors use it to hide listed models that cannot serve chat, such
 * as "embedding", "tts", or "image", when the provider gives no capability field.
 */
export function hasAnyModelIDFragment(id: string, fragments: readonly string[]): boolean {
  const lowered = id.toLowerCase();
  return fragments.some((fragment) => lowered.includes(fragment));
}
