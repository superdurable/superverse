// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { openAICompatibleModelOptions, type ConnectorStudioClient, type ModelListing } from "./model-listing";

/** metaModelsListCapability is the manifest capability of the listModels command. */
const metaModelsListCapability = "meta.models-list";

/** loadMetaModels lists Meta models through the host broker and projects them for ModelPicker. */
export async function loadMetaModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectMetaModelList(await client.executeProviderCommand("listModels", metaModelsListCapability));
}

/**
 * projectMetaModelList reads the OpenAI-compatible `{data: [{id}]}` list in
 * the order Meta returns it. Only Muse Spark models serve Chat Completions, so
 * Muse Image, Muse Voice Transcribe, and SAM stay behind "Show all models".
 * Contributor variants are flagged because Meta may train on their prompts and
 * completions.
 */
export function projectMetaModelList(value: Record<string, unknown>): ModelListing {
  return {
    models: openAICompatibleModelOptions(value, {
      isHiddenByDefault: (id) => !id.startsWith("muse-spark-"),
      detail: (item) =>
        typeof item.id === "string" && item.id.endsWith("-contributor")
          ? "Contributor tier: Meta may train on prompts and completions."
          : undefined,
    }),
  };
}
