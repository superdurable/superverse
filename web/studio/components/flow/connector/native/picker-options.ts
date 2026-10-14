// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReleaseManifest } from "@/lib/api/flow/connector";
import { loadClaudeModelListing } from "@/components/flow/connector/native/model/claude-models";
import { loadDeepSeekModels } from "@/components/flow/connector/native/model/deepseek-models";
import { loadGeminiModelListing } from "@/components/flow/connector/native/model/gemini-models";
import { loadGrokModels } from "@/components/flow/connector/native/model/grok-models";
import { loadKimiModels } from "@/components/flow/connector/native/model/kimi-models";
import { loadMetaModels } from "@/components/flow/connector/native/model/meta-models";
import { loadMistralModels } from "@/components/flow/connector/native/model/mistral-models";
import { loadOpenAIModelListing } from "@/components/flow/connector/native/model/openai-models";
import { loadQwenModels } from "@/components/flow/connector/native/model/qwen-models";
import { loadLLMModels } from "./llm-models";
import {
  collectProviderPages,
  type ConnectorStudioClient,
  type ConnectorStudioConnection,
  type ModelListing,
  type ModelOption,
} from "@/components/flow/connector/native/model/model-listing";
import { parseSlackChannelPage, parseSlackUserPage } from "./slack-resources";
import { parseSpreadsheetPage, parseSheetTabs } from "./sheet-resources";

/** The options a picker unit offers, loaded from the provider through the commands its release declares. */

/**
 * The model list each provider's model picker loads, keyed by manifest provider. Provider aliases
 * share a loader, and each connector keeps its own command IDs and capability. Only llm reads the
 * saved connection, whose provider and region select the list.
 */
const MODEL_LOADERS: Record<
  string,
  (client: ConnectorStudioClient, connection: ConnectorStudioConnection) => Promise<ModelListing>
> = {
  llm: loadLLMModels,
  openai: (client) => loadOpenAIModelListing(client, { commandId: "listModels", capability: "openai.models-list" }),
  claude: (client) => loadClaudeModelListing(client, { commandId: "listModels", capability: "claude.models-list" }),
  anthropic: (client) => loadClaudeModelListing(client, { commandId: "listModels", capability: "claude.models-list" }),
  gemini: (client) =>
    loadGeminiModelListing(client, {
      nativeCommandId: "listModels",
      openAICompatibleCommandId: "listOpenAICompatibleModels",
      capability: "gemini.models-list",
    }),
  deepseek: loadDeepSeekModels,
  grok: loadGrokModels,
  xai: loadGrokModels,
  kimi: loadKimiModels,
  meta: loadMetaModels,
  mistral: loadMistralModels,
  qwen: (client) => loadQwenModels(client),
};

/** How to let the Slack app into a channel it is not in, which it must be to hear the channel's messages. */
export function slackInviteHint(channel: string): string {
  return `The app is not in ${channel}: type /invite @app in Slack, then refresh`;
}

/** The options of picker `unit`; a sheet tab picker lists the tabs of `spreadsheetId`. */
export async function loadPickerOptions(
  { unit, spreadsheetId, manifest }: { unit: string; spreadsheetId: string; manifest: ReleaseManifest },
  client: ConnectorStudioClient,
  connection: ConnectorStudioConnection,
): Promise<ModelListing> {
  if (unit === "modelPicker") {
    const provider = manifest.spec.provider;
    const loadModels = Object.hasOwn(MODEL_LOADERS, provider) ? MODEL_LOADERS[provider] : undefined;
    if (!loadModels) throw new Error("No native model adapter exists for this provider");
    return loadModels(client, connection);
  }
  const command = (commandId: string, parameters: Record<string, string> = {}) => {
    const capability = manifest.spec.studio?.commands?.find((command) => command.id === commandId)?.capability;
    if (!capability) throw new Error("Data source unavailable");
    return client.executeProviderCommand(commandId, capability, parameters);
  };
  if (unit === "sheetTabPicker")
    return {
      models:
        spreadsheetId === "" ? [] : parseSheetTabs(await command("listTabs", { spreadsheetId })).map((id) => ({ id })),
    };
  const pages = await collectProviderPages<ModelOption>(async (cursor) => {
    if (unit === "channelPicker") {
      const result = parseSlackChannelPage(await command("listChannels", cursor ? { cursor } : {}));
      return {
        items: result.channels.map((channel) => ({
          id: channel.id,
          label: `#${channel.name}`,
          detail: channel.isPrivate ? "Private channel" : undefined,
          unavailableHint: channel.isMember ? undefined : slackInviteHint(`#${channel.name}`),
        })),
        nextCursor: result.nextCursor,
      };
    }
    if (unit === "memberPicker") {
      const result = parseSlackUserPage(await command("listUsers", cursor ? { cursor } : {}));
      return {
        items: result.users.map((user) => ({ id: user.id, label: user.displayName })),
        nextCursor: result.nextCursor,
      };
    }
    if (unit === "spreadsheetPicker") {
      const result = parseSpreadsheetPage(await command("listSpreadsheets", cursor ? { pageToken: cursor } : {}));
      return {
        items: result.files.map((file) => ({ id: file.id, label: file.name })),
        nextCursor: result.nextPageToken,
      };
    }
    return { items: [], nextCursor: "" };
  });
  const unique = pages.items.filter((option, index, all) => all.findIndex((item) => item.id === option.id) === index);
  return {
    // Channels the app is in come first; the others follow with how to invite it.
    models: [
      ...unique.filter((option) => !option.unavailableHint),
      ...unique.filter((option) => option.unavailableHint),
    ],
    isTruncated: pages.isTruncated,
  };
}
