// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useId, useState } from "react";
import type { ConnectionView, ConnectorUIBinding, ReleaseManifest } from "@/lib/api/flow/connector";
import { stringArrayField, stringField } from "@/lib/shared/json";
import { loadPickerOptions } from "./picker-options";
import { validateModelIDForRule } from "./model-id-rule";
import type {
  ConnectorStudioClient,
  ConnectorStudioConnection,
  ModelListing,
} from "@/components/flow/connector/native/model/model-listing";
import { Banner } from "@/components/ui/Banner";
import { cx } from "@/components/ui/cx";
import { Button } from "@/components/ui/Button";
import {
  BLOCK_HEAD,
  CHOICE,
  CHOICE_INPUT,
  FIELD_LABEL,
  NOTE,
  NOTICE,
  ROOMY_FIELD,
} from "@/components/flow/frame/workspace-styles";

/** Where a native unit's value is saved: a connection field, or the configuration of one Flow operation or trigger binding. */
export type NativeUnitTarget =
  | {
      kind: "connection";
      unitId?: string;
      bindings?: ConnectorUIBinding[];
      value?: Record<string, unknown>;
    }
  | {
      kind: "configurationUnit";
      use:
        | { kind: "operation"; operationId: string; flowType: string; stepType: string }
        | { kind: "trigger"; triggerName: string; bindingName: string };
      /** The use's whole saved configuration, which the unit's value is merged into. */
      configuration: Record<string, unknown>;
      instanceId: string;
      unitId: string;
      label: string;
      description?: string;
      required: boolean;
      bindings: ConnectorUIBinding[];
      value: Record<string, unknown>;
    };

/** A unit of one Flow operation's or trigger binding's settings. */
export type ConfigurationUnitTarget = Extract<NativeUnitTarget, { kind: "configurationUnit" }>;

type Command = (
  id: string,
  parameters: Record<string, string>,
  signal?: AbortSignal,
) => Promise<Record<string, unknown>>;
export type NativeUnitProps = {
  connection: ConnectionView;
  manifest: ReleaseManifest;
  target: NativeUnitTarget;
  execute: Command;
  /** Saves the unit's value: at once for a pick, after a pause for typing. A value the unit rejects is never saved. */
  onSave(value: Record<string, unknown>, immediate: boolean): void;
};

const OPTION_DETAIL = cx("text-[11px] text-ink-faint");

/** How each supported unit edits its value. */
interface UnitSpec {
  /** The value port the unit reads and writes. */
  port: string;
  /** A list of IDs, typed comma-separated, rather than one value. */
  multiple?: boolean;
  /** Offers options loaded from the provider through brokered commands. */
  picker?: boolean;
  /** Free text kept as typed, which may span lines. */
  multiline?: boolean;
}

export const UNIT_SPEC: Record<string, UnitSpec> = {
  modelPicker: { port: "model", picker: true },
  channelPicker: { port: "channelId", picker: true },
  memberPicker: { port: "memberIds", picker: true, multiple: true },
  spreadsheetPicker: { port: "spreadsheetId", picker: true },
  sheetTabPicker: { port: "tab", picker: true },
  textInput: { port: "text", multiline: true },
  emailListInput: { port: "emails", multiple: true },
  searchQueryInput: { port: "query", multiline: true },
};

/** These built-in controls consume metadata and brokered data, never Connector JavaScript. */
export function NativeConnectorUnit(props: NativeUnitProps) {
  const unitId = props.target.unitId;
  if (
    !unitId ||
    !Object.hasOwn(UNIT_SPEC, unitId) ||
    !props.manifest.spec.studio?.units?.some((unit) => unit.id === unitId)
  ) {
    return (
      <Banner role="alert" data-tone="attention" tone="busy">
        This connector release declares a configuration control that Studio does not support: {unitId ?? "unknown"}.
        Update Studio before configuring this release.
      </Banner>
    );
  }
  return <NativeUnit {...props} unitId={unitId} />;
}

function NativeUnit({ connection, manifest, target, execute, onSave, unitId }: NativeUnitProps & { unitId: string }) {
  const id = useId();
  const value = target.value ?? {};
  const { port, multiple = false, picker: isPicker = false, multiline = false } = UNIT_SPEC[unitId];
  const [draft, setDraft] = useState(() =>
    multiple ? stringArrayField(value[port]).join(", ") : stringField(value[port]),
  );
  const [resourceName, setResourceName] = useState(stringField(value.spreadsheetName));
  const [filter, setFilter] = useState("");
  const [showAll, setShowAll] = useState(false);
  const [loadRound, setLoadRound] = useState(0);
  const [listing, setListing] = useState<ModelListing>({ models: [] });
  const [loading, setLoading] = useState(isPicker);
  const [loadError, setLoadError] = useState("");
  const spreadsheetId = stringField(value.spreadsheetId);
  const provider = manifest.spec.provider;
  // The llm picker lists the saved provider in its saved region, never unsaved form values.
  const savedProvider = stringField(connection.configuration?.provider);
  const savedRegion = stringField(connection.configuration?.region);
  useEffect(() => {
    if (!isPicker) return;
    const controller = new AbortController();
    const client: ConnectorStudioClient = {
      executeProviderCommand: async (commandId, capability, parameters = {}) => {
        controller.signal.throwIfAborted();
        const command = manifest.spec.studio?.commands?.find((command) => command.id === commandId);
        const unit = manifest.spec.studio?.units?.find((unit) => unit.id === unitId);
        if (!command || command.capability !== capability || !unit?.backendCapabilities?.includes(capability))
          throw new Error("This connector release does not declare the requested data source.");
        return execute(commandId, parameters, controller.signal);
      },
    };
    const savedConnection: ConnectorStudioConnection = {
      configuration: { provider: savedProvider, region: savedRegion },
    };
    void loadPickerOptions({ unit: unitId, spreadsheetId, manifest }, client, savedConnection)
      .then((result) => {
        if (!controller.signal.aborted) {
          setListing(result);
          setLoading(false);
          setLoadError("");
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          setLoading(false);
          setLoadError(loadErrorMessage(error));
        }
      });
    return () => controller.abort();
  }, [isPicker, unitId, provider, spreadsheetId, savedProvider, savedRegion, execute, manifest, loadRound]);
  const title = target.kind === "configurationUnit" ? target.label : port === "model" ? "Default model" : port;
  const required = target.kind === "configurationUnit" && target.required;
  const selectedOf = (text: string) =>
    multiple
      ? text
          .split(",")
          .map((value) => value.trim())
          .filter(Boolean)
      : [];
  const selected = selectedOf(draft);
  const validationOf = (text: string) => {
    if (required && (multiple ? selectedOf(text).length === 0 : text.trim().length === 0))
      return "A value is required.";
    if (port === "model" && text.trim() !== "") {
      // llm checks every provider's model ID with the body rule; the Worker applies Gemini's stricter rule.
      const result = validateModelIDForRule(provider === "gemini" ? "pathSegment" : "body", text);
      if (!result.isValid) return result.message;
    }
    return "";
  };
  const validation = validationOf(draft);
  const filtered = listing.models.filter(
    (option) =>
      (showAll || !option.isHiddenByDefault) &&
      `${option.id} ${option.label ?? ""} ${option.detail ?? ""} ${option.badges?.join(" ") ?? ""}`
        .toLowerCase()
        .includes(filter.toLowerCase()),
  );
  /** Takes an edit and saves it unless the unit rejects it: a pick at once, typing after a pause. */
  function change(text: string, name: string, immediate: boolean) {
    setDraft(text);
    setResourceName(name);
    if (validationOf(text)) return;
    const next: Record<string, unknown> = {
      [port]: multiple ? [...new Set(selectedOf(text))] : multiline ? text : text.trim(),
    };
    if (unitId === "spreadsheetPicker") next.spreadsheetName = name;
    onSave(next, immediate);
  }
  return (
    <section className="grid gap-3 rounded-lg border border-line-soft p-3.5" aria-label={title}>
      <h4 className={BLOCK_HEAD}>
        {title}
        {required ? " *" : ""}
      </h4>
      {target.kind === "configurationUnit" && target.description && <p className={NOTE}>{target.description}</p>}
      {unitId === "sheetTabPicker" && spreadsheetId === "" && (
        <p className={NOTICE}>Choose a spreadsheet first to load its tabs.</p>
      )}
      {isPicker && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <label className={FIELD_LABEL} htmlFor={`${id}-filter`}>
              Search available {port === "model" ? "models" : "values"}
            </label>
            <input
              className={ROOMY_FIELD}
              id={`${id}-filter`}
              type="search"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
            <Button
              size="sm"
              disabled={loading}
              onClick={() => {
                setLoading(true);
                setLoadRound((round) => round + 1);
              }}
            >
              Refresh list
            </Button>
          </div>
          {port === "model" && (
            <label className={CHOICE}>
              <input
                className={CHOICE_INPUT}
                type="checkbox"
                checked={showAll}
                onChange={(event) => setShowAll(event.target.checked)}
              />
              Show all models
            </label>
          )}
          {loading && <p role="status">Loading provider data…</p>}
          {loadError && (
            <Banner role="status" data-tone="attention" tone="busy">
              {loadError}
            </Banner>
          )}
          {listing.notices?.map((notice, index) =>
            notice.tone === "attention" ? (
              <Banner data-tone={notice.tone} key={index} tone="busy">
                {notice.message}
              </Banner>
            ) : (
              <p className={NOTICE} data-tone={notice.tone} key={index}>
                {notice.message}
              </p>
            ),
          )}
          {listing.isTruncated && (
            <p className={NOTICE}>Only the first pages are shown. Enter an exact ID for a value not listed.</p>
          )}
          <div className="grid max-h-[280px] gap-1 overflow-auto" aria-label={`${title} options`}>
            {port === "model" && (
              <label className={CHOICE}>
                <input
                  className={CHOICE_INPUT}
                  type="radio"
                  name={`${id}-option`}
                  checked={draft === ""}
                  onChange={() => change("", resourceName, true)}
                />
                {target.kind === "connection"
                  ? "Use the connector's default model"
                  : "Use the connection's default model"}
              </label>
            )}
            {filtered.map((option) => (
              <label className={CHOICE} key={option.id}>
                <input
                  className={CHOICE_INPUT}
                  type={multiple ? "checkbox" : "radio"}
                  name={`${id}-option`}
                  checked={multiple ? selected.includes(option.id) : draft === option.id}
                  onChange={() =>
                    change(
                      multiple
                        ? (selected.includes(option.id)
                            ? selected.filter((value) => value !== option.id)
                            : [...selected, option.id]
                          ).join(", ")
                        : option.id,
                      unitId === "spreadsheetPicker" ? (option.label ?? "") : resourceName,
                      true,
                    )
                  }
                />
                <span className="grid min-w-0 gap-0.5">
                  <b className="text-[13px] font-semibold text-ink">{option.label ?? option.id}</b>
                  {option.label && option.label !== option.id && <small className={OPTION_DETAIL}>{option.id}</small>}
                  {option.detail && <small className={OPTION_DETAIL}>{option.detail}</small>}
                  {option.unavailableHint && <small className="text-[11px] text-busy">{option.unavailableHint}</small>}
                  {option.badges?.length ? <small className={OPTION_DETAIL}>{option.badges.join(" · ")}</small> : null}
                </span>
              </label>
            ))}
            {!loading && !loadError && filtered.length === 0 && (
              <p className={NOTE}>No matching values. You can enter an exact value below.</p>
            )}
          </div>
        </>
      )}
      <label className={cx(FIELD_LABEL, "grid gap-1.5")} htmlFor={`${id}-value`}>
        {isPicker
          ? multiple
            ? "Exact IDs, separated by commas"
            : "Exact ID or value"
          : port === "emails"
            ? "Email addresses, separated by commas"
            : title}
        {multiline ? (
          <textarea
            className={ROOMY_FIELD}
            id={`${id}-value`}
            rows={3}
            value={draft}
            onChange={(event) => change(event.target.value, resourceName, false)}
          />
        ) : (
          <input
            className={ROOMY_FIELD}
            id={`${id}-value`}
            type="text"
            autoComplete="off"
            value={draft}
            onChange={(event) => change(event.target.value, unitId === "spreadsheetPicker" ? "" : resourceName, false)}
            placeholder={port === "model" && provider === "llm" ? "model-id" : undefined}
          />
        )}
      </label>
      {unitId === "spreadsheetPicker" && (
        <label className={cx(FIELD_LABEL, "grid gap-1.5")} htmlFor={`${id}-name`}>
          Spreadsheet display name
          <input
            className={ROOMY_FIELD}
            id={`${id}-name`}
            value={resourceName}
            onChange={(event) => change(draft, event.target.value, false)}
          />
        </label>
      )}
      {validation && <p className={NOTE}>{validation}</p>}
    </section>
  );
}

// Show the provider's coded reason when the server supplied one.
export function loadErrorMessage(error: unknown): string {
  const retry = "Retry, or enter an exact value below.";
  const reason = error instanceof Error && error.message ? error.message : "The provider list is unavailable.";
  return `${reason} ${retry}`;
}
