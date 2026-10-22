// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentModelChoice__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModelChoice";

/** Development metadata only; real acceptance uses the authenticated server catalog. */
export const mockAgentModels: AgentModelChoice__Output[] = [
  {
    model: "AGENT_MODEL_OPUS_5_5",
    provider: "MODEL_PROVIDER_ANTHROPIC",
    mockEndpoint: false,
    label: "Claude Opus 5.5",
    providerName: "Anthropic",
    keyHint: "Anthropic API key",
  },
  {
    model: "AGENT_MODEL_SONNET_5_5",
    provider: "MODEL_PROVIDER_ANTHROPIC",
    mockEndpoint: false,
    label: "Claude Sonnet 5.5",
    providerName: "Anthropic",
    keyHint: "Anthropic API key",
  },
  {
    model: "AGENT_MODEL_HAIKU_4_5",
    provider: "MODEL_PROVIDER_ANTHROPIC",
    mockEndpoint: false,
    label: "Claude Haiku 4.5",
    providerName: "Anthropic",
    keyHint: "Anthropic API key",
  },
  {
    model: "AGENT_MODEL_MOCK",
    provider: "MODEL_PROVIDER_ANTHROPIC",
    mockEndpoint: false,
    label: "Mock",
    providerName: "Anthropic",
    keyHint: "Anthropic API key",
  },
  {
    model: "AGENT_MODEL_GPT_6_ASTRA",
    provider: "MODEL_PROVIDER_OPENAI",
    mockEndpoint: true,
    label: "GPT-6 Astra",
    providerName: "OpenAI",
    keyHint: "OpenAI API key",
  },
  {
    model: "AGENT_MODEL_GROK_4_7",
    provider: "MODEL_PROVIDER_XAI",
    mockEndpoint: true,
    label: "Grok 4.7",
    providerName: "xAI Grok",
    keyHint: "xAI API key",
  },
  {
    model: "AGENT_MODEL_GEMINI_4_ARGON",
    provider: "MODEL_PROVIDER_GEMINI",
    mockEndpoint: true,
    label: "Gemini 4 Argon",
    providerName: "Google Gemini",
    keyHint: "Gemini API key",
  },
];
