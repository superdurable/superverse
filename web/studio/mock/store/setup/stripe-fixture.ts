// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The `endpoint-bound` Setup fixture: a Stripe connection name `payments`, tied to one address, with a trigger
// that has no settings. Live's next publish never follows Preview's connection of it. Development only.
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import { MOCK_STRIPE_CONNECTOR, mockStripeManifest } from "@/mock/fixture/connector-manifest";

/** A scope's Stripe values: the TeamSpace connection its name selects. */
export interface MockStripeValues {
  selectedConnectionId?: string;
}

const { connectorId, connectionName } = MOCK_STRIPE_CONNECTOR;

/** The Stripe connection name as the connections catalog shows it. */
export function stripeConnectionView(values: MockStripeValues) {
  return {
    ...MOCK_STRIPE_CONNECTOR,
    provider: "stripe",
    status: values.selectedConnectionId ? "Ready" : "Missing",
    configuration: {},
    sharedConnectionId: values.selectedConnectionId,
    uses: [],
    triggerUses: [
      {
        triggerName: "paymentSucceeded",
        bindingName: "start",
        configuration: {},
        configured: true,
        configurationUI: { units: [] },
      },
    ],
  };
}

/** What the Stripe values lack: their own connection, which Live's next publish never takes from Preview. */
export function stripeGaps(values: MockStripeValues, live: boolean): ConfigurationRequirementGap__Output[] {
  if (values.selectedConnectionId) return [];
  return [
    {
      kind: "KIND_CONNECTION_REQUIRED",
      connectorId,
      connectionName,
      triggerName: "",
      bindingName: "",
      environmentName: "",
      unitLabel: "",
      flowName: "",
      stepName: "",
      operationId: "",
      followRefusal: live ? "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND" : "CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED",
    },
  ];
}

/** The UI session a working Stripe connection opens, which offers no unit. */
export function stripeSession(nonce: string) {
  return { connectorId, connectionName, sessionNonce: nonce, manifest: mockStripeManifest };
}
