// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2StartInputSchema } from "@/lib/api/flow/flow-definition";
import { toDateTimeLocalInput } from "@/lib/shared/format";
import type { StartInputDraft, StartInputError } from "./start-input";
import { humanFieldName } from "@/components/flow/connector/manifest/manifest-fields";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { ROOMY_FIELD } from "@/components/flow/frame/workspace-styles";

/** The value of an optional enum that leaves the field out. */
const UNSET = "\u0000unset";

/** One field of the start input, as the form draws it. */
export interface FieldProps {
  draft: StartInputDraft;
  errors: StartInputError[];
  /** The field's name in words. */
  label: string;
  /** What the field is for, under its name. */
  hint?: string;
  /** The name the code gives the field, on hover. */
  name?: string;
  path: string;
  required: boolean;
  schema: FlowV2StartInputSchema;
  onChange: (next: StartInputDraft) => void;
}

/**
 * One value: an enum's names in words in a list, a date and time picker that stores RFC 3339, or text. Typing into
 * an optional field includes it, and clearing it leaves it out again.
 */
export function ScalarInput({
  id,
  describedBy,
  draft,
  label,
  path,
  required,
  schema,
  onChange,
}: FieldProps & { id: string; describedBy?: string }) {
  const omitted = draft.mode === "omitted";
  const set = (scalar: string) =>
    onChange({ ...draft, scalar, mode: !required && scalar === "" ? "omitted" : "value" });
  if (schema.enumValues)
    return (
      <Dropdown
        id={id}
        label={label}
        size="md"
        data-start-path={path}
        describedBy={describedBy}
        value={omitted ? UNSET : draft.scalar}
        onChange={(value) =>
          onChange(value === UNSET ? { ...draft, mode: "omitted" } : { ...draft, scalar: value, mode: "value" })
        }
        items={[
          ...(required ? [] : [{ value: UNSET, label: "Not set" }]),
          ...schema.enumValues.map((option) => ({
            value: option.value.toString(),
            label: humanFieldName(option.name),
          })),
        ]}
      />
    );
  if (schema.format === "date-time")
    return (
      <input
        aria-describedby={describedBy}
        className={ROOMY_FIELD}
        data-start-path={path}
        id={id}
        type="datetime-local"
        value={omitted ? "" : toDateTimeLocalInput(draft.scalar)}
        onChange={(event) => set(event.target.value ? new Date(event.target.value).toISOString() : "")}
      />
    );
  return (
    <input
      aria-describedby={describedBy}
      className={ROOMY_FIELD}
      data-start-path={path}
      id={id}
      inputMode={schema.kind === "integer" || schema.kind === "number" ? "decimal" : undefined}
      type="text"
      value={omitted ? "" : draft.scalar}
      onChange={(event) => set(event.target.value)}
    />
  );
}

export function FieldError({ errors, path }: { errors: StartInputError[]; path: string }) {
  const error = errors.find((candidate) => candidate.path === path);
  return error ? (
    <small className="text-caption text-fail" role="alert">
      {error.message}
    </small>
  ) : null;
}
