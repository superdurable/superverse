// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The `slack` Setup fixture: a Slack connection name with a start and a reply trigger binding, each with
// a channel, so Setup's Triggers, the reply channel default and the not-in-channel hint can be seen.
// Development only; it adds to the configuration of the scope that enables it.
import type { ConfigurationItem } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import { MOCK_SLACK_CHANNELS, MOCK_SLACK_CONNECTOR, mockSlackManifest } from "@/mock/fixture/connector-manifest";

/** A scope's Slack values: the Teamspace connection its name selects and each binding's channel. */
export interface MockSlackValues {
  selectedConnectionId?: string;
  channels: Record<string, string>;
}

/** The fixture's bindings: the start binding and the reply binding, by binding name. */
const BINDINGS = [
  { triggerName: "channelThreadCreated", bindingName: "start" },
  { triggerName: "threadReplyCreated", bindingName: "reply" },
] as const;

const { connectorId, connectionName } = MOCK_SLACK_CONNECTOR;
const NONCE = "mock-slack-session";
const channelUnit = {
  id: "channel",
  unitId: "channelPicker",
  label: "Channel",
  required: true,
  bindings: [{ port: "channelId", jsonPointer: "/channelId" }],
};

/** The Slack connection name as the connections catalog shows it. */
export function slackConnectionView(values: MockSlackValues) {
  return {
    ...MOCK_SLACK_CONNECTOR,
    provider: "slack",
    status: values.selectedConnectionId ? "Ready" : "Missing",
    configuration: {},
    sharedConnectionId: values.selectedConnectionId,
    uses: [],
    triggerUses: BINDINGS.map((binding) => {
      const channelId = values.channels[binding.bindingName];
      return {
        ...binding,
        configuration: channelId ? { channelId } : {},
        configured: Boolean(channelId),
        configurationUI: { units: [channelUnit] },
      };
    }),
  };
}

/** What the Slack values lack: the connection, then each binding's channel. */
export function slackGaps(values: MockSlackValues): ConfigurationRequirementGap__Output[] {
  const gap = {
    connectorId,
    connectionName,
    triggerName: "",
    bindingName: "",
    environmentName: "",
    unitLabel: "",
    flowName: "",
    stepName: "",
    operationId: "",
    followRefusal: "CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED" as const,
  };
  if (!values.selectedConnectionId) return [{ ...gap, kind: "KIND_CONNECTION_REQUIRED" }];
  return BINDINGS.filter((binding) => !values.channels[binding.bindingName]).map((binding) => ({
    ...gap,
    ...binding,
    kind: "KIND_TRIGGER_BINDING_REQUIRED",
    unitLabel: channelUnit.label,
  }));
}

/** The channel another Project of the Teamspace already listens to, which Setup warns about. */
const SHARED_CHANNEL = { channelId: "C0SUPPORT", projectName: "Failed template app" };

/** The Slack items of a Preview draft against the applied values, by key. */
export function slackItems(values: MockSlackValues, applied: MockSlackValues | null): ConfigurationItem[] {
  const state = (now: string | undefined, before: string | undefined): ConfigurationItem["state"] =>
    applied === null || now !== before ? "STATE_CHANGED" : "STATE_UNCHANGED";
  return [
    {
      key: { kind: "KIND_CONNECTION", connectorId, connectionName },
      state: state(values.selectedConnectionId, applied?.selectedConnectionId),
    },
    ...BINDINGS.map((binding): ConfigurationItem => ({
      key: { kind: "KIND_TRIGGER_BINDING", connectorId, connectionName, ...binding },
      state: state(values.channels[binding.bindingName], applied?.channels[binding.bindingName]),
      ...(values.channels[binding.bindingName] === SHARED_CHANNEL.channelId
        ? { warnings: ["CONFIGURATION_ITEM_WARNING_CHANNEL_SHARED"], warningProjectName: SHARED_CHANNEL.projectName }
        : {}),
    })),
  ];
}

/** Answers a provider route of the Slack connection name's UI session, or undefined for another route. */
export function slackSessionRoute(key: string, method: string, body: { connectorId?: string }): object | undefined {
  if (key === "api/v2/connector-native-sessions" && method === "POST" && body.connectorId === connectorId)
    return { connectorId, connectionName, sessionNonce: NONCE, manifest: mockSlackManifest };
  if (key === `api/v2/connector-ui-sessions/${NONCE}/commands/listChannels` && method === "POST")
    return MOCK_SLACK_CHANNELS;
  return undefined;
}
