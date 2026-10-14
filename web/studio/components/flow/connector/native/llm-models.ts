// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { loadClaudeModelListing } from "@/components/flow/connector/native/model/claude-models";
import { projectDeepSeekModelList } from "@/components/flow/connector/native/model/deepseek-models";
import { loadGeminiModelListing } from "@/components/flow/connector/native/model/gemini-models";
import { projectGrokModelList } from "@/components/flow/connector/native/model/grok-models";
import { projectKimiModelList } from "@/components/flow/connector/native/model/kimi-models";
import { projectMetaModelList } from "@/components/flow/connector/native/model/meta-models";
import { projectMistralModelList } from "@/components/flow/connector/native/model/mistral-models";
import type {
  ConnectorStudioClient,
  ConnectorStudioConnection,
  ModelListing,
} from "@/components/flow/connector/native/model/model-listing";
import { loadOpenAIModelListing } from "@/components/flow/connector/native/model/openai-models";
import { loadQwenModels, type QwenModelListRegion } from "@/components/flow/connector/native/model/qwen-models";

/** llmModelsListCapability is the manifest capability every llm model-list command, the unit, and the setup declare. */
const llmModelsListCapability = "llm.models-list";

/** LLMProvider is one value of the connection's provider field. */
type LLMProvider = "openai" | "anthropic" | "gemini" | "qwen" | "deepseek" | "meta" | "mistral" | "kimi" | "xai";

/** LLMRegion is one value of the connection's region field; a blank region is global. */
type LLMRegion = "global" | "us" | "eu" | "china" | "hong-kong";

/** LLMModelSource lists one provider's models for the picker. */
interface LLMModelSource {
  /** label names the provider in messages, such as "Claude". */
  label: string;
  /** load lists the provider's models in region through the Studio broker. */
  load(client: ConnectorStudioClient, region: LLMRegion): Promise<ModelListing>;
}

const llmProviders: readonly LLMProvider[] = [
  "openai",
  "anthropic",
  "gemini",
  "qwen",
  "deepseek",
  "meta",
  "mistral",
  "kimi",
  "xai",
];
const llmRegions: readonly LLMRegion[] = ["global", "us", "eu", "china", "hong-kong"];

const unsavedProviderMessage =
  "Save the connection with its provider and api_key first; the picker then lists that provider's models. Until then, enter the provider's model ID.";

const qwenChinaMessage =
  "Model Studio China (Beijing) has no model list here. Enter the model ID from the Model Studio console, such as qwen3.7-plus.";

/** qwenSingaporeModelLists are the commands of the global region, Model Studio Singapore. */
const qwenSingaporeModelLists: QwenModelListRegion = {
  textGenerationCommandId: "listQwenModels",
  reasoningCommandId: "listQwenReasoningModels",
};

/** qwenHongKongModelLists are the commands of the hong-kong region, Model Studio China (Hong Kong). */
const qwenHongKongModelLists: QwenModelListRegion = {
  textGenerationCommandId: "listQwenModelsHongKong",
  reasoningCommandId: "listQwenReasoningModelsHongKong",
};

/**
 * llmModelSources declares each provider's list with the native projection of
 * that provider. Each loader runs only its own provider's manifest commands
 * for the connection's region, so the broker sends api_key only to that
 * provider's hosts.
 */
const llmModelSources: Record<LLMProvider, LLMModelSource> = {
  openai: {
    label: "OpenAI",
    load: (client) =>
      loadOpenAIModelListing(client, { capability: llmModelsListCapability, commandId: "listOpenAIModels" }),
  },
  anthropic: {
    label: "Claude",
    load: (client) =>
      loadClaudeModelListing(client, { capability: llmModelsListCapability, commandId: "listAnthropicModels" }),
  },
  gemini: {
    label: "Gemini",
    load: (client) =>
      loadGeminiModelListing(client, {
        capability: llmModelsListCapability,
        nativeCommandId: "listGeminiModels",
        openAICompatibleCommandId: "listGeminiOpenAICompatibleModels",
      }),
  },
  qwen: {
    label: "Qwen",
    load: (client, region) =>
      region === "china"
        ? Promise.resolve({ models: [], notices: [{ tone: "info", message: qwenChinaMessage }] })
        : loadQwenModels(
            client,
            [region === "hong-kong" ? qwenHongKongModelLists : qwenSingaporeModelLists],
            llmModelsListCapability,
          ),
  },
  deepseek: {
    label: "DeepSeek",
    load: async (client) =>
      projectDeepSeekModelList(await client.executeProviderCommand("listDeepSeekModels", llmModelsListCapability)),
  },
  meta: {
    label: "Meta",
    load: async (client) =>
      projectMetaModelList(await client.executeProviderCommand("listMetaModels", llmModelsListCapability)),
  },
  mistral: {
    // The global catalog also lists the models of the eu and us regions.
    label: "Mistral",
    load: async (client) =>
      projectMistralModelList(await client.executeProviderCommand("listMistralModels", llmModelsListCapability)),
  },
  kimi: {
    // platform.kimi.com keys, which the china region names, list from the China platform.
    label: "Kimi",
    load: async (client, region) =>
      projectKimiModelList(
        await client.executeProviderCommand(
          region === "china" ? "listKimiModelsChina" : "listKimiModels",
          llmModelsListCapability,
        ),
      ),
  },
  xai: {
    label: "xAI",
    load: async (client, region) =>
      projectGrokModelList(
        await client.executeProviderCommand(
          region === "us" ? "listXAIModelsUS" : "listXAIModels",
          llmModelsListCapability,
        ),
      ),
  },
};

/**
 * loadLLMModels is the llm ModelPicker loader: the live list of the
 * connection's saved provider in its saved region, with model IDs exactly as
 * the provider names them. Without a saved provider it runs no command, so the
 * key never reaches a provider the connection does not name, and resolves with
 * a notice; the picker still offers the default option and model ID entry. A
 * failed list rejects with a message that names the provider.
 */
export async function loadLLMModels(
  client: ConnectorStudioClient,
  connection: ConnectorStudioConnection,
): Promise<ModelListing> {
  const provider = readConfiguredProvider(connection.configuration);
  if (provider === undefined) return { models: [], notices: [{ tone: "attention", message: unsavedProviderMessage }] };
  const source = llmModelSources[provider];
  try {
    return await source.load(client, readConfiguredRegion(connection.configuration));
  } catch (error) {
    const reason = error instanceof Error ? error.message : "Unknown error";
    throw new Error(`The ${source.label} model list failed (${reason}). Check the connection's api_key and region.`);
  }
}

function readConfiguredProvider(configuration: Record<string, unknown>): LLMProvider | undefined {
  const provider = configuration.provider;
  return llmProviders.find((candidate) => candidate === provider);
}

function readConfiguredRegion(configuration: Record<string, unknown>): LLMRegion {
  const region = configuration.region;
  return llmRegions.find((candidate) => candidate === region) ?? "global";
}
