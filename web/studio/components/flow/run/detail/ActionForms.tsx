// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2Action, FlowV2Definition } from "@/lib/api/flow/flow-definition";
import type { FlowDisplay } from "@/lib/api/flow/flow-types";
import { actionUserFields, visibleActions } from "./flow-actions";
import { RUN_COPY } from "./run-copy";
import { TypedValueInput } from "./TypedValueInput";
import { Button } from "@/components/ui/Button";
import { BLOCK, BLOCK_HEAD, FIELD, STATE } from "@/components/flow/frame/workspace-styles";

/** The Action field a QR code scan fills. */
export interface ActiveQRScanner {
  actionRPCName: string;
  fieldName: string;
  fieldDescription: string;
}

/** One form per Action the run offers now, with the fields a person fills in. */
export function ActionForms({
  definition,
  result,
  values,
  busyKey,
  onChange,
  onScan,
  onSubmit,
}: {
  definition: FlowV2Definition;
  result: FlowDisplay;
  values: Record<string, Record<string, string>>;
  busyKey: string;
  onChange: (rpcName: string, fieldName: string, value: string) => void;
  onScan: (scanner: ActiveQRScanner) => void;
  onSubmit: (action: FlowV2Action) => void;
}) {
  return (
    <div className={BLOCK}>
      <div className={BLOCK_HEAD}>{RUN_COPY.actions}</div>
      {visibleActions(definition.actions, result.eligibleActions).map((action) => (
        <form
          className="mb-3 flex flex-col items-start gap-1.5"
          key={action.rpcName}
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit(action);
          }}
        >
          {actionUserFields(action).map((field) => (
            <label className="flex w-full flex-col gap-1" key={field.fieldName}>
              <span className="text-xs text-ink-faint">{field.description}</span>
              <span className="flex w-full gap-1.5">
                <TypedValueInput
                  className={FIELD}
                  ariaLabel={field.description}
                  emptyLabel={field.required ? "Choose a value" : "Unset"}
                  required={field.required}
                  valueType={field.valueType}
                  value={values[action.rpcName]?.[field.fieldName] ?? ""}
                  onChange={(value) => onChange(action.rpcName, field.fieldName, value)}
                />
                {field.capture === "qr-code" && (
                  <Button
                    size="sm"
                    onClick={() =>
                      onScan({
                        actionRPCName: action.rpcName,
                        fieldName: field.fieldName,
                        fieldDescription: field.description,
                      })
                    }
                  >
                    Scan QR code
                  </Button>
                )}
              </span>
            </label>
          ))}
          <Button variant="primary" size="sm" disabled={!result.isActive || busyKey === action.rpcName} type="submit">
            {busyKey === action.rpcName ? "Working…" : action.label}
          </Button>
        </form>
      ))}
      {definition.actions.length > 0 && result.eligibleActions.length === 0 && (
        <p className={STATE}>{RUN_COPY.noActions}</p>
      )}
    </div>
  );
}
