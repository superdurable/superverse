// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2Action } from "@/lib/api/flow/flow-definition";
import { parseTypedValue } from "./TypedValueInput";

/** The Flow v2 Actions a run offers right now: the server names the eligible RPCs per run. */
export function visibleActions(actions: FlowV2Action[], eligibleRPCNames: string[]) {
  const eligible = new Set(eligibleRPCNames);
  return actions.filter((action) => eligible.has(action.rpcName));
}

/** The input fields a person fills in; the rest come from the run's Attributes. */
export function actionUserFields(action: FlowV2Action) {
  return (action.input.fields ?? []).filter((field) => field.source === "user");
}

export function actionUserInput(action: FlowV2Action, values: Record<string, string>) {
  return Object.fromEntries(
    actionUserFields(action).map((field) => [
      field.fieldName,
      parseTypedValue(values[field.fieldName] ?? "", field.valueType, !field.required),
    ]),
  );
}
