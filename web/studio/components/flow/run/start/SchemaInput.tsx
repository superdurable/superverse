// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2StartInputSchema } from "@/lib/api/flow/flow-definition";
import { toDateTimeLocalInput } from "@/lib/shared/format";
import { createMapEntry, createStartInputDraft, type StartInputDraft, type StartInputError } from "./start-input";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { ROOMY_FIELD } from "@/components/flow/frame/workspace-styles";

/** An object, array or map of the start input, and the whole input around them. */
export const START_FIELDSET = cx("m-0 grid min-w-0 gap-3 rounded-lg border border-line-faint p-3");
/** The name of a field or a group of fields. */
export const START_CAPTION = cx("text-[11px] font-semibold text-ink-faint");
/** One array item or map entry, ruled off from its neighbours. */
const START_ENTRY = cx("grid gap-2 rounded-md border border-line-faint p-2.25");

export function SchemaInput({
  draft,
  errors,
  label,
  path,
  required,
  schema,
  onChange,
}: {
  draft: StartInputDraft;
  errors: StartInputError[];
  label: string;
  path: string;
  required: boolean;
  schema: FlowV2StartInputSchema;
  onChange: (next: StartInputDraft) => void;
}) {
  const isIncluded = draft.mode !== "omitted";
  const valueControls = (
    <>
      {!required && (
        <label className="inline-flex items-center gap-1.5 text-[11px] text-ink-faint">
          <input
            checked={isIncluded}
            type="checkbox"
            onChange={(event) => onChange({ ...draft, mode: event.target.checked ? "value" : "omitted" })}
          />
          Include
        </label>
      )}
      {isIncluded && schema.nullable && (
        <Dropdown
          label={`${label} value mode`}
          inline
          value={draft.mode === "null" ? "null" : "value"}
          onChange={(mode) => onChange({ ...draft, mode: mode === "null" ? "null" : "value" })}
          items={[
            { value: "value", label: "Value" },
            { value: "null", label: "Null" },
          ]}
        />
      )}
    </>
  );
  if (!isIncluded || draft.mode === "null" || schema.kind === "null") {
    return (
      <div className="grid gap-1.5">
        <span className={START_CAPTION}>{label}</span>
        {valueControls}
        {schema.kind === "null" && <code>null</code>}
      </div>
    );
  }
  if (schema.kind === "object") {
    return (
      <fieldset className={cx(START_FIELDSET, "ml-2")} data-start-path={path} tabIndex={-1}>
        <legend className={START_CAPTION}>{label}</legend>
        {valueControls}
        {(schema.fields ?? []).map((field) => (
          <SchemaInput
            draft={draft.fields[field.name]}
            errors={errors}
            key={field.name}
            label={field.name}
            path={`${path}.${field.name}`}
            required={field.required}
            schema={field.schema}
            onChange={(next) => onChange({ ...draft, fields: { ...draft.fields, [field.name]: next } })}
          />
        ))}
      </fieldset>
    );
  }
  if (schema.kind === "array") {
    const itemSchema = schema.items;
    if (!itemSchema) return null;
    return (
      <fieldset className={cx(START_FIELDSET, "ml-2")} data-start-path={path} tabIndex={-1}>
        <legend className={START_CAPTION}>{label}</legend>
        {valueControls}
        {draft.items.map((item, index) => (
          <div className={START_ENTRY} key={index}>
            <SchemaInput
              draft={item}
              errors={errors}
              label={`Item ${index + 1}`}
              path={`${path}[${index}]`}
              required
              schema={itemSchema}
              onChange={(next) =>
                onChange({ ...draft, items: draft.items.map((item, current) => (current === index ? next : item)) })
              }
            />
            {schema.fixedLength === undefined && (
              <Button
                className="justify-self-start"
                size="sm"
                onClick={() => onChange({ ...draft, items: draft.items.filter((_, current) => current !== index) })}
              >
                Remove
              </Button>
            )}
          </div>
        ))}
        {schema.fixedLength === undefined && (
          <Button
            className="justify-self-start"
            size="sm"
            onClick={() => onChange({ ...draft, items: [...draft.items, createStartInputDraft(itemSchema)] })}
          >
            Add item
          </Button>
        )}
        <FieldError errors={errors} path={path} />
      </fieldset>
    );
  }
  if (schema.kind === "map") {
    const valueSchema = schema.values;
    if (!valueSchema) return null;
    return (
      <fieldset className={cx(START_FIELDSET, "ml-2")} data-start-path={path} tabIndex={-1}>
        <legend className={START_CAPTION}>{label}</legend>
        {valueControls}
        {draft.entries.map((entry, index) => (
          <div
            className={cx(START_ENTRY, "items-start sm:grid-cols-[minmax(110px,0.35fr)_minmax(0,1fr)_auto]")}
            key={entry.id}
          >
            <input
              aria-label={`${label} key ${index + 1}`}
              className={ROOMY_FIELD}
              placeholder="Key"
              value={entry.key}
              onChange={(event) =>
                onChange({
                  ...draft,
                  entries: draft.entries.map((entry, current) =>
                    current === index ? { ...entry, key: event.target.value } : entry,
                  ),
                })
              }
            />
            <SchemaInput
              draft={entry.value}
              errors={errors}
              label="Value"
              path={`${path}[${JSON.stringify(entry.key)}]`}
              required
              schema={valueSchema}
              onChange={(next) =>
                onChange({
                  ...draft,
                  entries: draft.entries.map((entry, current) =>
                    current === index ? { ...entry, value: next } : entry,
                  ),
                })
              }
            />
            <Button
              size="sm"
              onClick={() => onChange({ ...draft, entries: draft.entries.filter((_, current) => current !== index) })}
            >
              Remove
            </Button>
          </div>
        ))}
        <Button
          className="justify-self-start"
          size="sm"
          onClick={() => onChange({ ...draft, entries: [...draft.entries, createMapEntry(schema)] })}
        >
          Add entry
        </Button>
        <FieldError errors={errors} path={path} />
      </fieldset>
    );
  }
  return (
    <label className="grid gap-1.5">
      <span className={START_CAPTION}>
        {label}
        {required ? " *" : ""}
      </span>
      {valueControls}
      <ScalarInput draft={draft} label={label} path={path} schema={schema} onChange={onChange} />
      <FieldError errors={errors} path={path} />
    </label>
  );
}

function ScalarInput({
  draft,
  label,
  path,
  schema,
  onChange,
}: {
  draft: StartInputDraft;
  label: string;
  path: string;
  schema: FlowV2StartInputSchema;
  onChange: (next: StartInputDraft) => void;
}) {
  if (schema.enumValues) {
    return (
      <Dropdown
        label={label}
        size="md"
        data-start-path={path}
        value={draft.scalar}
        onChange={(scalar) => onChange({ ...draft, scalar })}
        items={schema.enumValues.map((option) => ({
          value: option.value.toString(),
          label: `${option.name} (${option.value})`,
        }))}
      />
    );
  }
  if (schema.kind === "boolean") {
    return (
      <Dropdown
        label={label}
        size="md"
        data-start-path={path}
        value={draft.booleanValue ? "true" : "false"}
        onChange={(value) => onChange({ ...draft, booleanValue: value === "true" })}
        items={[
          { value: "false", label: "false" },
          { value: "true", label: "true" },
        ]}
      />
    );
  }
  if (schema.format === "date-time") {
    return (
      <input
        className={ROOMY_FIELD}
        data-start-path={path}
        type="datetime-local"
        value={toDateTimeLocalInput(draft.scalar)}
        onChange={(event) =>
          onChange({ ...draft, scalar: event.target.value ? new Date(event.target.value).toISOString() : "" })
        }
      />
    );
  }
  return (
    <input
      className={ROOMY_FIELD}
      data-start-path={path}
      inputMode={schema.kind === "integer" || schema.kind === "number" ? "decimal" : undefined}
      type="text"
      value={draft.scalar}
      onChange={(event) => onChange({ ...draft, scalar: event.target.value })}
    />
  );
}

function FieldError({ errors, path }: { errors: StartInputError[]; path: string }) {
  const error = errors.find((candidate) => candidate.path === path);
  return error ? (
    <small className="text-xs text-fail" role="alert">
      {error.message}
    </small>
  ) : null;
}
