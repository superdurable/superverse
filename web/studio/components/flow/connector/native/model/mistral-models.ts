// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord } from "@/lib/shared/json";
import { openAICompatibleModelOptions, type ConnectorStudioClient, type ModelListing } from "./model-listing";

/** mistralModelsListCapability is the manifest capability of the listModels command. */
const mistralModelsListCapability = "mistral.models-list";

/** loadMistralModels lists Mistral models through the host broker and projects them for ModelPicker. */
export async function loadMistralModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectMistralModelList(await client.executeProviderCommand("listModels", mistralModelsListCapability));
}

/**
 * projectMistralModelList reads Mistral's `{object: "list", data: [...]}` model
 * cards in the order Mistral returns them. A card without
 * `capabilities.completion_chat`, such as an embedding, OCR, or moderation
 * model, or an archived fine-tuned model, stays behind "Show all models".
 * Deprecated models stay visible with a badge and their deprecation date,
 * because they serve requests until retirement.
 */
export function projectMistralModelList(value: Record<string, unknown>): ModelListing {
  return {
    models: openAICompatibleModelOptions(value, {
      isHiddenByDefault: (_id, item) => readCapability(item, "completion_chat") !== true || item.archived === true,
      detail: deprecationDetail,
      badges: (item) => [
        ...(isDeprecated(item) ? ["deprecated"] : []),
        ...(readCapability(item, "function_calling") === true ? ["function calling"] : []),
        ...(readCapability(item, "vision") === true ? ["vision"] : []),
      ],
    }),
  };
}

function readCapability(item: Record<string, unknown>, name: string): unknown {
  const capabilities = item.capabilities;
  return isRecord(capabilities) ? capabilities[name] : undefined;
}

function isDeprecated(item: Record<string, unknown>): boolean {
  return typeof item.deprecation === "string" && item.deprecation.trim() !== "";
}

function deprecationDetail(item: Record<string, unknown>): string | undefined {
  if (!isDeprecated(item)) return undefined;
  const deprecation = String(item.deprecation).trim();
  const date = /^\d{4}-\d{2}-\d{2}/.exec(deprecation)?.[0] ?? deprecation;
  const replacement =
    typeof item.deprecation_replacement_model === "string" ? item.deprecation_replacement_model.trim() : "";
  return replacement === "" ? `Deprecation date ${date}.` : `Deprecation date ${date}. Replacement: ${replacement}.`;
}
