// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { asRecord, isRecord } from "@/lib/shared/json";
import {
  collectProviderPages,
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
  type ProviderModelListCommand,
} from "./model-listing";

/**
 * loadClaudeModelListing lists Claude models through the host broker with
 * command, a GET of https://api.anthropic.com/v1/models that declares an
 * `afterId` query parameter for `after_id`. It follows `has_more` and
 * `last_id` with that parameter and projects them for ModelPicker in the
 * order Claude returns them, newest first. It rejects with the host's error
 * when a page fails, so ModelPicker offers manual model entry.
 */
export async function loadClaudeModelListing(
  client: ConnectorStudioClient,
  command: ProviderModelListCommand,
): Promise<ModelListing> {
  const { items, isTruncated } = await collectProviderPages(async (cursor) => {
    const page = await client.executeProviderCommand(
      command.commandId,
      command.capability,
      cursor === "" ? {} : { afterId: cursor },
    );
    return { items: projectClaudeModelPage(page), nextCursor: readNextClaudeModelCursor(page) };
  });
  const models: ModelOption[] = [];
  const seenIDs = new Set<string>();
  for (const model of items) {
    if (seenIDs.has(model.id)) continue;
    seenIDs.add(model.id);
    models.push(model);
  }
  return { models, isTruncated };
}

/**
 * projectClaudeModelPage reads one `GET /v1/models` page. Every listed Claude
 * model serves the Messages API, so none is hidden. The label is the
 * `display_name`, the detail is the context window and output limit, and the
 * badges come from `capabilities`.
 */
function projectClaudeModelPage(value: Record<string, unknown>): ModelOption[] {
  return openAICompatibleModelOptions(value, {
    label: (item) =>
      typeof item.display_name === "string" && item.display_name.trim() !== "" ? item.display_name.trim() : undefined,
    detail: describeTokenLimits,
    badges: readCapabilityBadges,
  });
}

/** readNextClaudeModelCursor returns `last_id` while `has_more` is true, else an empty cursor. */
function readNextClaudeModelCursor(value: Record<string, unknown>): string {
  return value.has_more === true && typeof value.last_id === "string" ? value.last_id : "";
}

function describeTokenLimits(item: Record<string, unknown>): string | undefined {
  const limits = [
    isPositiveNumber(item.max_input_tokens) ? `${formatTokenCount(item.max_input_tokens)} context` : "",
    isPositiveNumber(item.max_tokens) ? `${formatTokenCount(item.max_tokens)} max output` : "",
  ].filter((limit) => limit !== "");
  return limits.length > 0 ? limits.join(" · ") : undefined;
}

function readCapabilityBadges(item: Record<string, unknown>): string[] {
  const capabilities = asRecord(item.capabilities);
  const badges: string[] = [];
  if (isSupported(capabilities.structured_outputs)) badges.push("structured output");
  if (isSupported(capabilities.effort)) badges.push("effort");
  if (isSupported(capabilities.thinking)) badges.push("thinking");
  return badges;
}

function isSupported(capability: unknown): boolean {
  return isRecord(capability) && capability.supported === true;
}

function formatTokenCount(count: number): string {
  if (count >= 1_000_000) return `${Number((count / 1_000_000).toFixed(1))}M`;
  if (count >= 1_000) return `${Math.round(count / 1_000)}K`;
  return String(count);
}

function isPositiveNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}
