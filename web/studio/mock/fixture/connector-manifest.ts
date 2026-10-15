// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development data only: the one connector of mock mode, which no provider backs.

/** The fixture connector, the connection name the mock Project declares it under, and its release. */
export const MOCK_CONNECTOR = {
  connectorId: "openai",
  connectionName: "default",
  displayName: "OpenAI (local fixture)",
  modulePath: "mock/openai",
  version: "fixture",
  versionLine: "v0",
} as const;

/** The fixture release manifest; provider lists are fixed and entered credential values are discarded. */
export const mockConnectorManifest = {
  metadata: {
    displayName: "OpenAI (local fixture)",
    description: "Mock development only. Provider lists are fixed and entered credential values are discarded.",
  },
  spec: {
    provider: "openai",
    configuration: {
      fields: [
        {
          name: "model",
          type: "string",
          description: "Fixture default model.",
          required: false,
          studioUnit: { unit: "modelPicker", port: "model" },
        },
      ],
    },
    auth: {
      type: "apiKey",
      fields: [
        {
          name: "api_key",
          type: "secretString",
          required: true,
          description: "Any entered value is discarded; only a configured marker is retained in this local fixture.",
        },
      ],
    },
    studio: {
      setup: { backendCapabilities: ["openai.models-list", "use.configuration.write"] },
      commands: [{ id: "listModels", capability: "openai.models-list" }],
      units: [
        {
          id: "modelPicker",
          description: "Fixed fixture models.",
          backendCapabilities: ["openai.models-list"],
          outputs: [{ name: "model", type: "string" }],
        },
      ],
    },
  },
};

/** The models the fixture's listModels command answers with. */
export const MOCK_MODELS = { data: [{ id: "gpt-4.1-mini" }, { id: "gpt-4.1" }] };

/** The Slack fixture connector, which the `slack` Setup fixture declares under the connection name `support`. */
export const MOCK_SLACK_CONNECTOR = {
  connectorId: "slack",
  connectionName: "support",
  displayName: "Slack (local fixture)",
  modulePath: "mock/slack",
  version: "fixture",
  versionLine: "v1",
} as const;

/**
 * The Slack fixture's release manifest: the connectors library's Slack sign-in (an OAuth sign-in that stores the
 * bot and user tokens, the app's own client, and the app-level token pasted once for Socket Mode), its two
 * triggers in the connector's words and its channel picker.
 */
export const mockSlackManifest = {
  metadata: { displayName: "Slack (local fixture)", description: "Mock development only. Channels are fixed." },
  spec: {
    provider: "slack",
    configuration: { fields: [] },
    auth: {
      type: "oauth2",
      fields: [
        {
          name: "oauth_client_id",
          type: "string",
          required: false,
          description: "Client ID from the Slack app's Basic Information > App Credentials.",
        },
        {
          name: "oauth_client_secret",
          type: "secretString",
          required: false,
          description: "Client Secret from the Slack app's Basic Information > App Credentials.",
        },
        { name: "bot_token", type: "secretString", required: true, description: "Stored by the Slack sign-in." },
        { name: "user_token", type: "secretString", required: true, description: "Stored by the Slack sign-in." },
        {
          name: "app_token",
          type: "secretString",
          required: true,
          description:
            "The xapp- token from Basic Information > App-Level Tokens, with connections:write. Any entered value is discarded.",
        },
      ],
      oauth2: {
        authorizationEndpoint: "https://slack.com/oauth/v2/authorize",
        tokenEndpoint: "https://slack.com/api/oauth.v2.access",
        clientIDCredential: "oauth_client_id",
        clientSecretCredential: "oauth_client_secret",
        scopes: ["channels:history", "groups:history", "channels:read", "groups:read", "users:read", "chat:write"],
        credentialMappings: [
          { credential: "bot_token", source: "access_token" },
          { credential: "user_token", source: "authed_user.access_token" },
        ],
      },
    },
    triggers: [
      { name: "channelThreadCreated", description: "Receive a matching top-level channel message." },
      { name: "threadReplyCreated", description: "Receive a matching thread reply." },
    ],
    studio: {
      setup: { backendCapabilities: ["slack.channels-list", "use.configuration.write"] },
      commands: [{ id: "listChannels", capability: "slack.channels-list" }],
      units: [
        {
          id: "channelPicker",
          description: "The channels the app can see.",
          backendCapabilities: ["slack.channels-list"],
          outputs: [{ name: "channelId", type: "string" }],
        },
      ],
    },
  },
};

/** The channels the fixture's listChannels answers with, as Slack's conversations.list does: one the app is not in. */
export const MOCK_SLACK_CHANNELS = {
  ok: true,
  channels: [
    { id: "C0SUPPORT", name: "support", is_member: true },
    { id: "C0ESCALATE", name: "escalations", is_member: true },
    { id: "C0GENERAL", name: "general", is_member: false },
  ],
  response_metadata: { next_cursor: "" },
};

/**
 * The Stripe fixture connector, which the `endpoint-bound` Setup fixture declares under the connection name
 * `payments`: Stripe signs each endpoint's events with that endpoint's own secret, so a connection serves one
 * address and Live's next publish never follows Preview's.
 */
export const MOCK_STRIPE_CONNECTOR = {
  connectorId: "stripe",
  connectionName: "payments",
  displayName: "Stripe (local fixture)",
  modulePath: "mock/stripe",
  version: "fixture",
  versionLine: "v1",
} as const;

/** Why a Stripe connection serves one address, as the server's endpoint-bound list says it. */
export const MOCK_STRIPE_ENDPOINT_REASON =
  "Stripe signs each webhook endpoint's events with that endpoint's own signing secret, so a connection receives events at one address.";

/** The Stripe fixture's release manifest: an API key and the endpoint's signing secret, and one trigger. */
export const mockStripeManifest = {
  metadata: { displayName: "Stripe (local fixture)", description: "Mock development only. No events arrive." },
  spec: {
    provider: "stripe",
    configuration: { fields: [] },
    auth: {
      type: "apiKey",
      fields: [
        { name: "api_key", type: "secretString", required: true, description: "Any entered value is discarded." },
        {
          name: "webhook_signing_secret",
          type: "secretString",
          required: true,
          description: "The signing secret of the endpoint at this environment's address.",
        },
      ],
    },
    triggers: [{ name: "paymentSucceeded", description: "Receive a succeeded payment." }],
  },
};
