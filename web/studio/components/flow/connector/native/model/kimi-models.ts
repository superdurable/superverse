// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  executeFirstAcceptedProviderCommand,
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
} from "./model-listing";

/** kimiModelsListCapability is the manifest capability of both list commands. */
const kimiModelsListCapability = "kimi.models-list";

/**
 * kimiModelsListCommandIDs are the manifest commands for the global and the
 * China platform, in the order they are tried. A key works on only one of them.
 */
const kimiModelsListCommandIDs = ["listModels", "listModelsChina"];

// Kimi retired these families, and chat requests for them return 404 model not found.
const retiredModelFragments = ["moonshot-v1", "kimi-k2-", "kimi-k2.5", "kimi-latest", "kimi-thinking-preview"] as const;

/**
 * loadKimiModels lists Kimi models through the host broker, trying the global
 * platform and then the China platform, and projects them for ModelPicker.
 * It rejects with the last command error when neither platform accepts the key.
 */
export async function loadKimiModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectKimiModelList(
    await executeFirstAcceptedProviderCommand(client, kimiModelsListCapability, kimiModelsListCommandIDs),
  );
}

/**
 * projectKimiModelList reads the OpenAI-compatible `{data: [{id}]}` list in the
 * order Kimi returns it. Each model shows its context window and a reasoning
 * badge from Kimi's capability flags; retired families stay behind "Show all
 * models".
 */
export function projectKimiModelList(value: Record<string, unknown>): ModelListing {
  return {
    models: openAICompatibleModelOptions(value, {
      isHiddenByDefault: (id) => hasAnyModelIDFragment(id, retiredModelFragments),
      detail: (item) => describeContextLength(item.context_length),
      badges: (item) => (item.supports_reasoning === true ? ["reasoning"] : []),
    }),
  };
}

/** describeContextLength renders a positive token count such as 262144 as "256K-token context". */
function describeContextLength(contextLength: unknown): string | undefined {
  if (typeof contextLength !== "number" || !Number.isSafeInteger(contextLength) || contextLength <= 0) return undefined;
  const mebi = 1024 * 1024;
  if (contextLength % mebi === 0) return `${contextLength / mebi}M-token context`;
  if (contextLength % 1024 === 0) return `${contextLength / 1024}K-token context`;
  return `${contextLength}-token context`;
}
