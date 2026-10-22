// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ModelProvider } from "@/lib/generated/protobuf/superverse/v2/ModelProvider";

import type { AgentModel__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModel";

/** The Agent UI literal of each selectable model; the Record type makes a new enum value a compile error here. */
const AGENT_MODEL_LITERALS = {
  AGENT_MODEL_OPUS_5_5: "opus-5.5",
  AGENT_MODEL_SONNET_5_5: "sonnet-5.5",
  AGENT_MODEL_HAIKU_4_5: "haiku-4.5",
  AGENT_MODEL_MOCK: "mock",
  AGENT_MODEL_GPT_6_ASTRA: "gpt-6-astra",
  AGENT_MODEL_DEEPSEEK_FLASH: "deepseek-flash",
  AGENT_MODEL_GROK_4_7: "grok-4.7",
  AGENT_MODEL_MUSE_WONDER_1_0: "muse-wonder-1.0",
  AGENT_MODEL_GEMINI_4_ARGON: "gemini-4-argon",
  AGENT_MODEL_KIMI_K3: "kimi-k3",
  AGENT_MODEL_GLM_5_3: "glm-5.3",
  AGENT_MODEL_QWEN_3_8_MAX: "qwen3.8-max",
  AGENT_MODEL_MINIMAX_M3_1_FLASH: "minimax-m3.1-flash",
  AGENT_MODEL_KIMI_K2_6: "kimi-k2.6",
  AGENT_MODEL_GLM_5_2: "glm-5.2",
  AGENT_MODEL_MINIMAX_M3: "minimax-m3",
  AGENT_MODEL_ANTHROPIC_CUSTOM: "anthropic/custom",
  AGENT_MODEL_OPENAI_CUSTOM: "openai/custom",
  AGENT_MODEL_XAI_CUSTOM: "xai/custom",
  AGENT_MODEL_GEMINI_CUSTOM: "gemini/custom",
  AGENT_MODEL_QWEN_CUSTOM: "qwen/custom",
} as const satisfies Record<Exclude<AgentModel__Output, "AGENT_MODEL_UNSPECIFIED">, string>;

type AgentModelName = keyof typeof AGENT_MODEL_LITERALS;
type AgentModelLiteral = (typeof AGENT_MODEL_LITERALS)[AgentModelName];

/** A model's UI literal; null while no model is set, as a context segment before its first turn. */
export function agentModelView(value: AgentModel__Output): AgentModelLiteral | null {
  return value === "AGENT_MODEL_UNSPECIFIED" ? null : AGENT_MODEL_LITERALS[value];
}

/** The UpdateSessionModel enum value of a model literal. */
export function agentModelInput(model: AgentModelLiteral): AgentModelName {
  return (Object.keys(AGENT_MODEL_LITERALS) as AgentModelName[]).find((name) => AGENT_MODEL_LITERALS[name] === model)!;
}

const modelProviders = {
  AGENT_MODEL_OPUS_5_5: ModelProvider.MODEL_PROVIDER_ANTHROPIC,
  AGENT_MODEL_SONNET_5_5: ModelProvider.MODEL_PROVIDER_ANTHROPIC,
  AGENT_MODEL_HAIKU_4_5: ModelProvider.MODEL_PROVIDER_ANTHROPIC,
  AGENT_MODEL_MOCK: ModelProvider.MODEL_PROVIDER_ANTHROPIC,
  AGENT_MODEL_GPT_6_ASTRA: ModelProvider.MODEL_PROVIDER_OPENAI,
  AGENT_MODEL_DEEPSEEK_FLASH: ModelProvider.MODEL_PROVIDER_DEEPSEEK,
  AGENT_MODEL_GROK_4_7: ModelProvider.MODEL_PROVIDER_XAI,
  AGENT_MODEL_MUSE_WONDER_1_0: ModelProvider.MODEL_PROVIDER_META,
  AGENT_MODEL_GEMINI_4_ARGON: ModelProvider.MODEL_PROVIDER_GEMINI,
  AGENT_MODEL_KIMI_K3: ModelProvider.MODEL_PROVIDER_KIMI,
  AGENT_MODEL_GLM_5_3: ModelProvider.MODEL_PROVIDER_GLM,
  AGENT_MODEL_QWEN_3_8_MAX: ModelProvider.MODEL_PROVIDER_QWEN,
  AGENT_MODEL_MINIMAX_M3_1_FLASH: ModelProvider.MODEL_PROVIDER_MINIMAX,
  AGENT_MODEL_KIMI_K2_6: ModelProvider.MODEL_PROVIDER_KIMI,
  AGENT_MODEL_GLM_5_2: ModelProvider.MODEL_PROVIDER_GLM,
  AGENT_MODEL_MINIMAX_M3: ModelProvider.MODEL_PROVIDER_MINIMAX,
  AGENT_MODEL_ANTHROPIC_CUSTOM: ModelProvider.MODEL_PROVIDER_ANTHROPIC,
  AGENT_MODEL_OPENAI_CUSTOM: ModelProvider.MODEL_PROVIDER_OPENAI,
  AGENT_MODEL_XAI_CUSTOM: ModelProvider.MODEL_PROVIDER_XAI,
  AGENT_MODEL_GEMINI_CUSTOM: ModelProvider.MODEL_PROVIDER_GEMINI,
  AGENT_MODEL_QWEN_CUSTOM: ModelProvider.MODEL_PROVIDER_QWEN,
} as const satisfies Record<AgentModelName, ModelProvider>;

/** The explicit credential namespace of a selected catalog model; mock keeps the existing key form. */
export function agentModelProvider(model: AgentModel__Output | undefined) {
  return model && model !== "AGENT_MODEL_UNSPECIFIED" ? modelProviders[model] : ModelProvider.MODEL_PROVIDER_ANTHROPIC;
}
