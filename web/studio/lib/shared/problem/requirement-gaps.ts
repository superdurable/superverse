// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";

/** What each gap kind lacks, in the server's words; the Record makes a new kind a compile error here. */
const GAP_PHRASES = {
  KIND_CONNECTION_REQUIRED: "connection required",
  KIND_AUTHORIZATION_REQUIRED: "authorization required",
  KIND_CONNECTION_INCOMPLETE: "configuration incomplete",
  KIND_TRIGGER_BINDING_REQUIRED: "trigger binding required",
  KIND_TRIGGER_BINDING_INVALID: "trigger binding invalid",
  KIND_OPERATION_CONFIGURATION_REQUIRED: "operation configuration required",
  KIND_ENVIRONMENT_REQUIRED: "environment required",
} as const satisfies Record<Exclude<ConfigurationRequirementGap__Output["kind"], "KIND_UNSPECIFIED">, string>;

/**
 * What a configuration gap concerns and what it lacks, named as the server names it in a
 * notification, such as "gmail (authorization required)", naming the required unit a trigger binding or a
 * Step's operation use lacks.
 * A gap never holds a value.
 */
export function requirementGapName(gap: ConfigurationRequirementGap__Output): string {
  const subject = gap.triggerName
    ? `${gap.connectionName} trigger ${gap.triggerName}/${gap.bindingName}`
    : gap.stepName
      ? `${gap.connectionName} step ${gap.flowName}/${gap.stepName} operation ${gap.operationId}`
      : gap.connectionName || (gap.environmentName ? `environment ${gap.environmentName}` : "application environment");
  if (gap.kind === "KIND_UNSPECIFIED") return subject;
  return gap.unitLabel
    ? `${subject} (${GAP_PHRASES[gap.kind]}: ${gap.unitLabel})`
    : `${subject} (${GAP_PHRASES[gap.kind]})`;
}
