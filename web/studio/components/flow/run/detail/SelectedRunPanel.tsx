// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useState, type ReactNode } from "react";
import type { FlowV2Action, FlowV2Definition, FlowV2Field } from "@/lib/api/flow/flow-definition";
import { formatDisplayValue } from "@/lib/shared/format";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { FlowDisplay, FlowSummary } from "@/lib/api/flow/flow-types";
import { getFlowSummary } from "@/lib/api/flow/flow-diagnostics";
import { getFlowDisplay, invokeFlowAction, updateFlowDisplayField } from "@/lib/api/flow/flow-run";
import { usesCurrentDefinition, unavailableDefinitionMessage } from "@/lib/flow/definition-binding";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { actionUserInput } from "./flow-actions";
import { LIVENESS_COPY, RUN_COPY } from "./run-copy";
import { absorb, classifyReadFailure, nothingHeld } from "./read-liveness";
import { leadFields } from "./ui-slots";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";
import { QrCodeScannerDialog } from "./QrCodeScannerDialog";
import { TypedValueInput, editableValue, parseTypedValue } from "./TypedValueInput";
import { ActionForms, type ActiveQRScanner } from "./ActionForms";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import {
  BLOCK,
  CASE,
  CASE_HEAD,
  CASE_STATUS,
  CASE_TITLE,
  ERROR,
  FACT,
  FACT_TERM,
  FACTS,
  FIELD,
  LINK,
  NOTE,
  STATE,
} from "@/components/flow/frame/workspace-styles";

/** Drawer or Inbox column alike: a capped reading column, centred rather than hard-left beside a void. */
const RUN_CASE = cx(CASE, "mx-auto max-w-[46rem]");

/** The Display field being edited and the callbacks that change it. */
interface FieldEditing {
  attributeKey: string;
  value: string;
  busyKey: string;
  errors: Record<string, string>;
  start: (field: FlowV2Field) => void;
  change: (value: string) => void;
  cancel: () => void;
  save: (field: FlowV2Field) => void;
}

/**
 * One run's Display fields and Actions, read for the run's own definition revision. Hosts key it by
 * Flow type and Flow ID, so a newly selected run never inherits the previous run's values.
 */
export function SelectedRunPanel({
  flowType,
  flowId,
  title,
  definition,
  definitionRevision,
  flowStatusCode,
  footer,
  reloadKey = 0,
  order,
  showHeading = true,
  onActed,
  onStranded,
}: {
  flowType: string;
  flowId: string;
  /** The run's name as its row says it, shown with the heading in place of the Flow ID. */
  title?: string;
  definition: FlowV2Definition;
  definitionRevision: string;
  /** From the search row, so a dead worker can be told apart from a closed run. */
  flowStatusCode?: number;
  footer?: ReactNode;
  /** Bumped by the view's shared clock, so the fields and the canvas move together. */
  reloadKey?: number;
  /**
   * Admin already knows the case, so Run puts the decision first. Whoever acts on an Inbox item
   * needs the evidence before the decision, so the Inbox reads the other way.
   */
  order: "actions-first" | "evidence-first";
  /** False when the host already names the run, so it is not named twice. */
  showHeading?: boolean;
  /**
   * An Action succeeded, so whatever the run was waiting for has moved on: the Action's label, and
   * whether the run, read again, still offers an Action.
   */
  onActed?: (outcome: { action: string; waiting: boolean }) => void;
  /** Reported up so the list can mark the row; a search cannot discover this. */
  onStranded?: (flowID: string) => void;
}) {
  const client = useFlowRequestScope(`${flowType}:${flowId}`);
  const { handleDefinitionError } = useFlowCatalog();
  const [binding, setBinding] = useState<FlowSummary | null>(null);
  const [held, setHeld] = useState(() => nothingHeld<FlowDisplay>());
  /** A write that failed is not a stale read, so it does not touch held. */
  const [actionError, setActionError] = useState("");
  const [busyKey, setBusyKey] = useState("");
  const [editingKey, setEditingKey] = useState("");
  const [editValue, setEditValue] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [actionValues, setActionValues] = useState<Record<string, Record<string, string>>>({});
  const [activeQRScanner, setActiveQRScanner] = useState<ActiveQRScanner | null>(null);

  /** Reads the run's Display again; what it read, or null when it read nothing. */
  const loadDisplay = useCallback(async (): Promise<FlowDisplay | null> => {
    try {
      const summary = await getFlowSummary(client, flowId);
      setBinding(summary);
      if (!usesCurrentDefinition(summary, definitionRevision)) {
        setHeld(nothingHeld<FlowDisplay>());
        return null;
      }
      const display = await getFlowDisplay(client, { flowType, flowId }, { definitionRevision });
      setHeld((prior) => absorb(prior, { state: "ok", value: display }));
      return display;
    } catch (loadError) {
      if (handleDefinitionError(loadError)) {
        setHeld(nothingHeld<FlowDisplay>());
        return null;
      }
      // Only knowable once somebody opens the run: a search says nothing about its worker.
      const outcome = classifyReadFailure<FlowDisplay>(loadError, flowStatusCode);
      if (outcome.state === "stranded") onStranded?.(flowId);
      setHeld((prior) => absorb(prior, outcome));
      return null;
    }
  }, [client, definitionRevision, flowId, flowStatusCode, flowType, handleDefinitionError, onStranded]);

  useEffect(() => {
    void Promise.resolve().then(loadDisplay);
  }, [loadDisplay, reloadKey]);

  const setActionValue = (rpcName: string, fieldName: string, value: string) => {
    setActionValues((current) => ({ ...current, [rpcName]: { ...current[rpcName], [fieldName]: value } }));
  };

  const setFieldError = (attributeKey: string, message: string) => {
    setFieldErrors((current) => ({ ...current, [attributeKey]: message }));
  };

  async function saveField(field: FlowV2Field) {
    setBusyKey(field.attributeKey);
    setActionError("");
    setFieldError(field.attributeKey, "");
    try {
      await updateFlowDisplayField(
        client,
        { flowType, flowId, attributeKey: field.attributeKey, value: parseTypedValue(editValue, field.valueType) },
        { definitionRevision },
      );
      setEditingKey("");
      await loadDisplay();
    } catch (saveError) {
      if (handleDefinitionError(saveError)) return;
      setFieldError(field.attributeKey, errorMessage(saveError, "Edit failed"));
    } finally {
      setBusyKey("");
    }
  }

  async function invokeAction(action: FlowV2Action, attributeSnapshot: Record<string, unknown>) {
    setBusyKey(action.rpcName);
    setActionError("");
    try {
      await invokeFlowAction(
        client,
        {
          flowType,
          flowId,
          rpcName: action.rpcName,
          input: actionUserInput(action, actionValues[action.rpcName] ?? {}),
          attributeSnapshot,
        },
        { definitionRevision },
      );
      const after = await loadDisplay();
      // A read that failed says nothing either way, so the run is taken to wait still.
      onActed?.({
        action: action.label,
        waiting: after === null || (after.isActive && after.eligibleActions.length > 0),
      });
    } catch (failure) {
      if (handleDefinitionError(failure)) return;
      setActionError(errorMessage(failure, String(failure)));
    } finally {
      setBusyKey("");
    }
  }

  if (binding && !usesCurrentDefinition(binding, definitionRevision)) {
    return (
      <div className={RUN_CASE}>
        <p role="status">{unavailableDefinitionMessage}</p>
        <FlowLink className={LINK} to={{ surface: "diagnostics", flowId, runId: binding.runId }}>
          Diagnostics
        </FlowLink>
        {footer}
      </div>
    );
  }

  const result = held.value;
  // A stranded run cannot be acted on, so only its last known status is shown.
  const shown = held.liveness === "stranded" ? null : result;
  const editing: FieldEditing = {
    attributeKey: editingKey,
    value: editValue,
    busyKey,
    errors: fieldErrors,
    start: (field) => {
      setEditingKey(field.attributeKey);
      setFieldError(field.attributeKey, "");
      setEditValue(editableValue(result?.display[field.attributeKey], field.valueType));
    },
    change: setEditValue,
    cancel: () => setEditingKey(""),
    save: (field) => void saveField(field),
  };
  const fields = shown && <DisplayFields definition={definition} result={shown} editing={editing} />;
  const actions = shown && (
    <ActionForms
      definition={definition}
      result={shown}
      values={actionValues}
      busyKey={busyKey}
      onChange={setActionValue}
      onScan={setActiveQRScanner}
      onSubmit={(action) => void invokeAction(action, shown.attributeSnapshot)}
    />
  );
  return (
    <div className={RUN_CASE}>
      {(showHeading || footer || held.liveness === "stale") && (
        <div className={CASE_HEAD}>
          {showHeading && (
            <span className={CASE_TITLE} title={flowId}>
              {title || flowId}
            </span>
          )}
          {showHeading && result && <span className={CASE_STATUS}>{result.flowStatus}</span>}
          {held.liveness === "stale" && (
            <span className="ml-auto text-[11px] text-fail">{LIVENESS_COPY.staleShort}</span>
          )}
          {footer}
        </div>
      )}
      {held.liveness === "stranded" && (
        <p className={STATE} data-liveness="stranded">
          {LIVENESS_COPY.stranded}
        </p>
      )}
      {held.liveness === "unreachable" && <p className={ERROR}>{held.reason}</p>}
      {held.liveness === "stale" && <p className={NOTE}>{held.reason}</p>}
      {held.liveness === "loading" && <p className={STATE}>{LIVENESS_COPY.loading}</p>}
      {actionError && <p className={ERROR}>{actionError}</p>}
      {order === "actions-first" ? (
        <>
          {actions}
          {fields}
        </>
      ) : (
        <>
          {fields}
          {actions}
        </>
      )}
      {showHeading && (
        <MoreDetails surface="runs.run-ids" className={BLOCK}>
          <dl className={FACTS}>
            <div className={FACT}>
              <dt className={FACT_TERM}>{RUN_COPY.flowId}</dt>
              <dd className="min-w-0 font-mono text-caption wrap-anywhere text-ink-dim">{flowId}</dd>
            </div>
          </dl>
        </MoreDetails>
      )}
      {activeQRScanner && (
        <QrCodeScannerDialog
          fieldDescription={activeQRScanner.fieldDescription}
          onCancel={() => setActiveQRScanner(null)}
          onScan={(value) => {
            setActionValue(activeQRScanner.actionRPCName, activeQRScanner.fieldName, value);
            setActiveQRScanner(null);
          }}
        />
      )}
    </div>
  );
}

/** The Display fields: the slotted lead fields first, then the rest, editable while the run is active. */
function DisplayFields({
  definition,
  result,
  editing,
}: {
  definition: FlowV2Definition;
  result: FlowDisplay;
  editing: FieldEditing;
}) {
  const lead = leadFields(definition);
  const detail = definition.display.fields.filter((field) => !lead.includes(field));
  const fact = (field: FlowV2Field) => (
    <FieldFact editing={editing} field={field} key={field.attributeKey} result={result} />
  );
  return (
    <div className={BLOCK}>
      {/* The slotted fields, read first, are set off by a rule: the slots already say what they are. */}
      {lead.length > 0 && (
        <dl className={cx(FACTS, "group mb-2 border-b border-line-faint pb-2")} data-lead="true">
          {lead.map(fact)}
        </dl>
      )}
      {detail.length > 0 && <dl className={FACTS}>{detail.map(fact)}</dl>}
    </div>
  );
}

function FieldFact({ field, result, editing }: { field: FlowV2Field; result: FlowDisplay; editing: FieldEditing }) {
  const error = editing.errors[field.attributeKey];
  return (
    <div className={FACT}>
      <dt className="text-xs text-ink-faint">{field.description}</dt>
      <dd className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1 text-[13px] text-ink group-data-[lead=true]:font-medium">
        {editing.attributeKey === field.attributeKey ? (
          <>
            <TypedValueInput
              className={FIELD}
              ariaLabel={field.description}
              emptyLabel="Unset"
              valueType={field.valueType}
              value={editing.value}
              onChange={editing.change}
            />
            <Button
              variant="primary"
              size="sm"
              disabled={editing.busyKey === field.attributeKey}
              onClick={() => editing.save(field)}
            >
              Save
            </Button>
            <Button size="sm" onClick={editing.cancel}>
              Cancel
            </Button>
            {error && <small className={ERROR}>{error}</small>}
          </>
        ) : (
          <>
            <span>{formatDisplayValue(result.display[field.attributeKey])}</span>
            {field.editable && result.isActive && (
              <Button size="sm" onClick={() => editing.start(field)}>
                Edit
              </Button>
            )}
          </>
        )}
      </dd>
    </div>
  );
}
