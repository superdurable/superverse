// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The fixture's native configuration views, as the configuration host answers them: the connection list
// and the application environment of the values a scope shows. Development only.
import { MOCK_CONNECTOR } from "@/mock/fixture/connector-manifest";
import { MOCK_DEFINITION_REVISION } from "@/mock/fixture/flow-definitions";
import type { MockNativeValues } from "./preview-setup";
import { slackConnectionView } from "./slack-fixture";
import { stripeConnectionView } from "./stripe-fixture";

/** The fixture's CSRF token, which every write repeats. */
export const CSRF = "studio-mock-csrf";
const { connectorId: connectionID, connectionName } = MOCK_CONNECTOR;

/** The environment settings the mock application declares. */
export const environmentFields = [
  { name: "APP_LABEL", required: false, secret: false, minLength: 0, enum: [] },
  { name: "MOCK_BUILD_OUTCOME", required: false, secret: false, minLength: 0, enum: ["ready", "failed"] },
  { name: "MOCK_TOKEN", required: false, secret: true, minLength: 0, enum: [] },
];

/** The connection list of a scope at `revision` with `values`, `ready` when they leave no gap. */
export function fixtureConnections(
  revision: string,
  ready: boolean,
  values: MockNativeValues & { configuration?: object },
) {
  return {
    configurationRevision: revision,
    configurationState: ready ? "Ready to deploy" : "Draft",
    definitionRevision: MOCK_DEFINITION_REVISION,
    appManifestRevision: 1,
    csrfToken: CSRF,
    connections: [
      {
        connectorId: connectionID,
        connectionName,
        displayName: MOCK_CONNECTOR.displayName,
        modulePath: MOCK_CONNECTOR.modulePath,
        moduleVersion: MOCK_CONNECTOR.version,
        versionLine: MOCK_CONNECTOR.versionLine,
        provider: "openai",
        status: values.selectedConnectionId ? "Ready" : "Missing",
        configuration: values.configuration,
        sharedConnectionId: values.selectedConnectionId,
        uses: [
          {
            flowName: "TextSummaryFlow",
            stepId: "step:GenerateSummary",
            stepName: "GenerateSummary",
            operationId: "generateText",
            operationKind: "query",
            configured: Boolean(values.useConfiguration.model),
            configuration: values.useConfiguration,
            configurationUI: {
              units: [
                {
                  id: "model",
                  unitId: "modelPicker",
                  label: "Fixture operation model",
                  required: false,
                  bindings: [{ port: "model", jsonPointer: "/model" }],
                },
              ],
            },
          },
        ],
        triggerUses: [],
      },
      ...(values.slack ? [slackConnectionView(values.slack)] : []),
      ...(values.stripe ? [stripeConnectionView(values.stripe)] : []),
    ],
  };
}

/** The application environment form of a scope at `revision` with `values`. */
export function environmentSnapshot(revision: string, values: MockNativeValues) {
  return {
    appManifestRevision: 1,
    configurationRevision: Number(revision),
    csrfToken: CSRF,
    obsolete: [],
    fields: environmentFields.map((field) => ({
      ...field,
      configured: field.secret
        ? values.configuredSecrets.includes(field.name)
        : Object.hasOwn(values.environment, field.name),
      ...(field.secret ? {} : { value: values.environment[field.name] }),
    })),
  };
}
