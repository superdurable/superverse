// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

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
