// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useId, type ReactNode } from "react";
import type { FlowV2StartInputSchema } from "@/lib/api/flow/flow-definition";
import { createMapEntry, createStartInputDraft, type StartInputDraft, type StartInputError } from "./start-input";
import { FieldError, ScalarInput, type FieldProps } from "./ScalarInput";
import { startFieldWords, type StartField } from "./start-labels";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { ROOMY_FIELD } from "@/components/flow/frame/workspace-styles";

/** An object, array or map inside the start input. */
const START_FIELDSET = cx("m-0 grid min-w-0 gap-3 rounded-lg border border-line-faint p-3");
/** The name of a field or a group of fields. */
const START_CAPTION = cx("text-caption font-semibold text-ink-dim");
/** One array item or map entry, ruled off from its neighbours. */
const START_ENTRY = cx("grid gap-2 rounded-md border border-line-faint p-2.25");
/** A quiet choice beside a field's name: Leave empty, or Include for an optional group. */
const QUIET_CHOICE = cx("inline-flex items-center gap-1.5 text-caption font-normal text-ink-faint");

/** The whole start input: an object's fields one under another, or its one value. */
export function StartFields({
  schema,
  draft,
  errors,
  onChange,
}: {
  schema: FlowV2StartInputSchema;
  draft: StartInputDraft;
  errors: StartInputError[];
  onChange: (next: StartInputDraft) => void;
}) {
  if (schema.kind !== "object" || draft.mode !== "value")
    return (
      <SchemaInput draft={draft} errors={errors} label="Input" path="$" required schema={schema} onChange={onChange} />
    );
  const fields = schema.fields ?? [];
  if (fields.length === 0) return <p className="text-body text-ink-faint">This process starts without any input.</p>;
  return (
    <div className="grid gap-3.5">
      {fields.map((field) => (
        <ChildInput key={field.name} field={field} parent={draft} path="$" errors={errors} onChange={onChange} />
      ))}
    </div>
  );
}

/** One field of an object, labelled in words. */
function ChildInput({
  field,
  parent,
  path,
  errors,
  onChange,
}: {
  field: StartField;
  parent: StartInputDraft;
  path: string;
  errors: StartInputError[];
  onChange: (next: StartInputDraft) => void;
}) {
  const words = startFieldWords(field);
  return (
    <SchemaInput
      draft={parent.fields[field.name]}
      errors={errors}
      label={words.label}
      hint={words.hint}
      name={field.name}
      path={`${path}.${field.name}`}
      required={field.required}
      schema={field.schema}
      onChange={(next) => onChange({ ...parent, fields: { ...parent.fields, [field.name]: next } })}
    />
  );
}

/** A field's name with "optional" when it may be left out, and its quiet choice beside it. */
function Caption({
  htmlFor,
  label,
  name,
  optional,
  children,
}: {
  htmlFor?: string;
  label: string;
  name?: string;
  optional: boolean;
  children?: ReactNode;
}) {
  const words = (
    <>
      {label}
      {optional && <span className="font-normal text-ink-faint"> (optional)</span>}
    </>
  );
  // A span, so the caption may head a fieldset as its legend.
  return (
    <span className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      {htmlFor ? (
        <label className={START_CAPTION} htmlFor={htmlFor} title={name && name !== label ? name : undefined}>
          {words}
        </label>
      ) : (
        <span className={START_CAPTION} title={name && name !== label ? name : undefined}>
          {words}
        </span>
      )}
      {children}
    </span>
  );
}

function SchemaInput(props: FieldProps) {
  const { draft, errors, label, hint, name, path, required, schema, onChange } = props;
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const empty = draft.mode === "null" || schema.kind === "null";
  // A nullable value may be left empty on purpose, which is not the same as leaving it out.
  const leaveEmpty = schema.nullable && draft.mode !== "omitted" && (
    <label className={QUIET_CHOICE}>
      <input
        checked={draft.mode === "null"}
        type="checkbox"
        onChange={(event) => onChange({ ...draft, mode: event.target.checked ? "null" : "value" })}
      />
      Leave empty
    </label>
  );
  const note = hint && (
    <small className="text-caption text-ink-faint" id={hintId}>
      {hint}
    </small>
  );
  if (schema.kind === "object" || schema.kind === "array" || schema.kind === "map") {
    const included = draft.mode !== "omitted";
    const include = !required && (
      <label className={QUIET_CHOICE}>
        <input
          checked={included}
          type="checkbox"
          onChange={(event) => onChange({ ...draft, mode: event.target.checked ? "value" : "omitted" })}
        />
        Include
      </label>
    );
    if (!included || empty)
      return (
        <div className="grid gap-1.5">
          <Caption label={label} name={name} optional={!required}>
            {include}
            {leaveEmpty}
          </Caption>
          {note}
        </div>
      );
    return (
      <fieldset className={START_FIELDSET} data-start-path={path} tabIndex={-1}>
        <legend className="px-1">
          <Caption label={label} name={name} optional={!required}>
            {include}
            {leaveEmpty}
          </Caption>
        </legend>
        {note}
        <GroupInput {...props} />
        <FieldError errors={errors} path={path} />
      </fieldset>
    );
  }
  if (schema.kind === "boolean")
    return (
      <div className="grid gap-1">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <label
            className="inline-flex items-center gap-2 text-body text-ink"
            title={name && name !== label ? name : undefined}
          >
            <input
              aria-describedby={hintId}
              checked={draft.mode === "value" && draft.booleanValue}
              data-start-path={path}
              disabled={empty}
              type="checkbox"
              onChange={(event) => onChange({ ...draft, mode: "value", booleanValue: event.target.checked })}
            />
            {label}
          </label>
          {leaveEmpty}
        </div>
        {note}
        <FieldError errors={errors} path={path} />
      </div>
    );
  return (
    <div className="grid gap-1.5">
      <Caption htmlFor={id} label={label} name={name} optional={!required}>
        {leaveEmpty}
      </Caption>
      {note}
      {schema.kind === "null" ? (
        <span className="text-body text-ink-faint">Always empty</span>
      ) : (
        !empty && <ScalarInput {...props} id={id} describedBy={hintId} />
      )}
      <FieldError errors={errors} path={path} />
    </div>
  );
}

/** The members of an object, the items of a list or the entries of a map. */
function GroupInput({ draft, errors, label, path, schema, onChange }: FieldProps) {
  if (schema.kind === "object")
    return (schema.fields ?? []).map((field) => (
      <ChildInput key={field.name} field={field} parent={draft} path={path} errors={errors} onChange={onChange} />
    ));
  const fixed = schema.kind === "array" && schema.fixedLength !== undefined;
  const itemSchema = schema.kind === "array" ? schema.items : schema.values;
  if (!itemSchema) return null;
  const remove = (index: number) =>
    schema.kind === "array"
      ? onChange({ ...draft, items: draft.items.filter((_, current) => current !== index) })
      : onChange({ ...draft, entries: draft.entries.filter((_, current) => current !== index) });
  return (
    <>
      {schema.kind === "array"
        ? draft.items.map((item, index) => (
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
              {!fixed && (
                <Button className="justify-self-start" size="sm" onClick={() => remove(index)}>
                  Remove
                </Button>
              )}
            </div>
          ))
        : draft.entries.map((entry, index) => (
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
                schema={itemSchema}
                onChange={(next) =>
                  onChange({
                    ...draft,
                    entries: draft.entries.map((entry, current) =>
                      current === index ? { ...entry, value: next } : entry,
                    ),
                  })
                }
              />
              <Button size="sm" onClick={() => remove(index)}>
                Remove
              </Button>
            </div>
          ))}
      {!fixed && (
        <Button
          className="justify-self-start"
          size="sm"
          onClick={() =>
            schema.kind === "array"
              ? onChange({ ...draft, items: [...draft.items, createStartInputDraft(itemSchema)] })
              : onChange({ ...draft, entries: [...draft.entries, createMapEntry(schema)] })
          }
        >
          {schema.kind === "array" ? "Add item" : "Add entry"}
        </Button>
      )}
    </>
  );
}
