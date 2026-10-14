// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Display labels for the Dex protobuf enums that appear in history and state payloads. A payload
 * may carry an enum as its number or as its full proto name, so each label function accepts both.
 */
function protoEnum(prefix: string, names: readonly string[], labels: readonly string[]): (value: unknown) => string {
  const byKey = new Map<string, string>();
  names.forEach((name, index) => {
    byKey.set(String(index), labels[index]);
    byKey.set(`${prefix}${name}`, labels[index]);
  });
  return (value) => {
    const key = String(value ?? 0);
    return byKey.get(key) ?? `Unknown (${key})`;
  };
}

export const durabilityLabel = protoEnum(
  "STEP_DURABILITY_",
  ["UNSPECIFIED", "SYNC", "ASYNC"],
  ["Unspecified", "sync", "async"],
);

export const waitingConditionTypeLabel = protoEnum(
  "WAITING_CONDITION_TYPE_",
  ["UNSPECIFIED", "ALL_COMPLETED", "ANY_COMPLETED", "ANY_COMBINATION_COMPLETED"],
  ["Unspecified", "All completed", "Any completed", "Any combination completed"],
);

export const pendingStepMethodPhaseLabel = protoEnum(
  "PENDING_STEP_METHOD_PHASE_",
  ["UNSPECIFIED", "SCHEDULED", "STARTED"],
  ["Unspecified", "Scheduled", "Started"],
);

export const conditionStatusLabel = protoEnum(
  "CONDITION_STATUS_",
  ["UNSPECIFIED", "WAITING", "COMPLETED"],
  ["Unspecified", "Waiting", "Completed"],
);

const FLOW_STATUS_NAMES = [
  "UNSPECIFIED",
  "RUNNING",
  "COMPLETED",
  "FAILED",
  "SERVER_SIDE_TIMEOUT_INTERNAL_ONLY",
  "TERMINATED",
  "CANCELED",
  "CONTINUED_AS_NEW",
];

export const flowStatusLabel = protoEnum("FLOW_STATUS_", FLOW_STATUS_NAMES, [
  "Unspecified",
  "Running",
  "Completed",
  "Failed",
  "Server-side timeout (internal only)",
  "Terminated",
  "Canceled",
  "Continued as new",
]);

/** The proto name without its prefix, such as `RUNNING`. */
export const subFlowStatusName = protoEnum("FLOW_STATUS_", FLOW_STATUS_NAMES, FLOW_STATUS_NAMES);

export const subFlowReusePolicyLabel = protoEnum(
  "SUB_FLOW_REUSE_POLICY_",
  ["UNSPECIFIED", "ATTACH", "RESTART_IF_PREVIOUS_EXITS_ABNORMALLY", "ALWAYS_RESTART"],
  ["Unspecified", "Attach", "Restart if previous exits abnormally", "Always restart"],
);

export const flowErrorTypeLabel = protoEnum(
  "FLOW_ERROR_TYPE_",
  [
    "UNSPECIFIED",
    "STEP_DECISION_FAILING_FLOW",
    "CLIENT_API_FAILING_FLOW",
    "WORKER_API_FAIL",
    "INVALID_USER_FLOW_CODE",
    "FLOW_TIMEOUT",
    "INTERNAL",
  ],
  [
    "Unspecified",
    "Step decision failed the flow",
    "Client API failed the flow",
    "Worker method failed",
    "Invalid flow code",
    "Flow timed out",
    "Internal error",
  ],
);

/** A failure's backend error: a FLOW_ERROR_TYPE_ name as its label, any other value as recorded. */
export function backendErrorLabel(value: unknown): unknown {
  return typeof value === "string" && value.startsWith("FLOW_ERROR_TYPE_") ? flowErrorTypeLabel(value) : value;
}

export const flowTimeoutPolicyLabel = protoEnum(
  "FLOW_TIMEOUT_POLICY_",
  ["UNSPECIFIED", "FAIL", "CANCEL", "HANDLER"],
  ["Unspecified", "Fail", "Cancel", "Handler"],
);

export function isHandlerTimeoutPolicy(value: unknown): boolean {
  const key = String(value ?? 0);
  return key === "3" || key === "FLOW_TIMEOUT_POLICY_HANDLER";
}

const CLOSE_DECISION_NAMES = [
  "UNSPECIFIED",
  "FORCE_COMPLETE_ON_CHANNELS_EMPTY",
  "GRACEFUL_COMPLETE",
  "FORCE_COMPLETE",
  "FORCE_FAIL",
  "DEAD_END",
];

export const closeDecisionTypeLabel = protoEnum("CLOSE_DECISION_TYPE_", CLOSE_DECISION_NAMES, [
  "Unspecified",
  "Complete when channels are empty",
  "Graceful complete",
  "Force complete",
  "Force fail",
  "Dead end",
]);

/** The SDK decision verb that produced a close decision, such as `forceFail`; empty when unspecified. */
export const closeDecisionVerb = protoEnum("CLOSE_DECISION_TYPE_", CLOSE_DECISION_NAMES, [
  "",
  "forceCompleteIfChannelsEmpty",
  "gracefulComplete",
  "forceComplete",
  "forceFail",
  "deadEnd",
]);

export const activeStepSearchModeLabel = protoEnum(
  "ACTIVE_STEP_SEARCH_MODE_",
  ["UNSPECIFIED", "ENABLED_FOR_ALL", "ENABLED_FOR_STEPS_WITH_WAIT_FOR", "DISABLED"],
  ["Unspecified", "All steps", "Steps with WaitFor", "Disabled"],
);

export const waitForFailurePolicyLabel = protoEnum(
  "WAIT_FOR_METHOD_FAILURE_POLICY_",
  ["UNSPECIFIED", "FAIL_FLOW_ON_FAILURE", "PROCEED_ON_FAILURE"],
  ["Unspecified", "Fail flow", "Proceed"],
);

export const executeFailurePolicyLabel = protoEnum(
  "EXECUTE_METHOD_FAILURE_POLICY_",
  ["UNSPECIFIED", "FAIL_FLOW_ON_EXECUTE_METHOD_FAILURE", "PROCEED_TO_CONFIGURED_STEP"],
  ["Unspecified", "Fail flow", "Proceed to configured step"],
);

const GRPC_STATUS_NAMES = [
  "OK",
  "CANCELLED",
  "UNKNOWN",
  "INVALID_ARGUMENT",
  "DEADLINE_EXCEEDED",
  "NOT_FOUND",
  "ALREADY_EXISTS",
  "PERMISSION_DENIED",
  "RESOURCE_EXHAUSTED",
  "FAILED_PRECONDITION",
  "ABORTED",
  "OUT_OF_RANGE",
  "UNIMPLEMENTED",
  "INTERNAL",
  "UNAVAILABLE",
  "DATA_LOSS",
  "UNAUTHENTICATED",
] as const;

/** `NOT_FOUND (5)`, from a numeric code or a `CODE_`/`STATUS_CODE_` prefixed or bare name. */
export function grpcStatusLabel(value: unknown): string {
  const numeric = typeof value === "number" ? value : Number(value);
  if (Number.isInteger(numeric) && numeric >= 0 && numeric < GRPC_STATUS_NAMES.length) {
    return `${GRPC_STATUS_NAMES[numeric]} (${numeric})`;
  }
  if (typeof value === "string") {
    const normalized = value.replace(/^CODE_/, "").replace(/^STATUS_CODE_/, "");
    const code = GRPC_STATUS_NAMES.indexOf(normalized as (typeof GRPC_STATUS_NAMES)[number]);
    if (code >= 0) return `${normalized} (${code})`;
  }
  return `Unknown (${String(value)})`;
}
