// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  readProviderModelArray,
  type ConnectorStudioClient,
  type ModelListing,
  type ProviderModelListCommand,
} from "./model-listing";

// The list has no capability field; "search" also hides deep research, which requires a search tool.
const nonTextModelFragments = [
  "embedding",
  "tts",
  "whisper",
  "transcribe",
  "audio",
  "realtime",
  "image",
  "dall-e",
  "sora",
  "moderation",
  "search",
  "babbage",
  "davinci",
  "computer-use",
  "gpt-live",
] as const;

/**
 * loadOpenAIModelListing lists OpenAI models through the host broker with
 * command, a GET of https://api.openai.com/v1/models, and projects them for
 * ModelPicker with projectOpenAIModelList. now defaults to the time the list
 * returns. It rejects with the host's error when the command fails, so
 * ModelPicker offers manual model entry.
 */
export async function loadOpenAIModelListing(
  client: ConnectorStudioClient,
  command: ProviderModelListCommand,
  now?: Date,
): Promise<ModelListing> {
  const list = await client.executeProviderCommand(command.commandId, command.capability);
  return projectOpenAIModelList(list, now ?? new Date());
}

/**
 * projectOpenAIModelList reads the `{data: [{id, created, shutdown_date}]}`
 * list newest first, because OpenAI documents no order. It hides non-text
 * families and models whose `shutdown_date` is on or before `now`, and notes
 * every announced shutdown date.
 */
function projectOpenAIModelList(value: Record<string, unknown>, now: Date): ModelListing {
  const today = now.toISOString().slice(0, 10);
  const newestFirst = [...readProviderModelArray(value)].sort((left, right) => readCreated(right) - readCreated(left));
  return {
    models: openAICompatibleModelOptions(
      { data: newestFirst },
      {
        isHiddenByDefault: (id, item) => {
          const shutdownDate = readShutdownDate(item);
          return (
            hasAnyModelIDFragment(readModelFamilyID(id), nonTextModelFragments) ||
            (shutdownDate !== undefined && shutdownDate <= today)
          );
        },
        detail: (item) => {
          const shutdownDate = readShutdownDate(item);
          if (shutdownDate === undefined) return undefined;
          return shutdownDate <= today ? `Shut down on ${shutdownDate}.` : `Shuts down on ${shutdownDate}.`;
        },
      },
    ),
  };
}

/** readModelFamilyID returns the base model of an `ft:<base>:<organization>:<suffix>:<id>` ID, ignoring its chosen suffix. */
function readModelFamilyID(id: string): string {
  if (!id.toLowerCase().startsWith("ft:")) return id;
  const baseModelEnd = id.indexOf(":", 3);
  return baseModelEnd === -1 ? id.slice(3) : id.slice(3, baseModelEnd);
}

/** readShutdownDate returns the ISO calendar date OpenAI announced, or undefined when there is none. */
function readShutdownDate(item: Record<string, unknown>): string | undefined {
  const value = item.shutdown_date;
  return typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : undefined;
}

function readCreated(item: Record<string, unknown>): number {
  return typeof item.created === "number" && Number.isFinite(item.created) ? item.created : 0;
}
