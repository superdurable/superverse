// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { V2ValueType } from "@/lib/api/flow/flow-definition";
import { toDateTimeLocalInput } from "@/lib/shared/format";
import { Dropdown } from "@/components/ui/menu/Dropdown";

/** A value type's form text and back: the control below edits text, the wire carries typed values. */

const INPUT_TYPE: Partial<Record<V2ValueType, string>> = {
  int64: "number",
  double: "number",
  datetime: "datetime-local",
};

/**
 * The form control a value type calls for, so a bool or a date cannot be typed as free text. A
 * bool is a dropdown whose empty option reads `emptyLabel`; every other type is an input.
 */
export function TypedValueInput({
  className,
  valueType,
  value,
  onChange,
  ariaLabel,
  emptyLabel,
  placeholder,
  disabled = false,
  required = false,
}: {
  className: string;
  /** Undefined until the value has a type, which renders a plain text input. */
  valueType: V2ValueType | undefined;
  value: string;
  onChange: (value: string) => void;
  ariaLabel: string;
  emptyLabel: string;
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
}) {
  if (valueType === "bool") {
    return (
      <Dropdown
        label={ariaLabel}
        disabled={disabled}
        required={required}
        value={value}
        onChange={onChange}
        items={[
          { value: "", label: emptyLabel },
          { value: "true", label: "True" },
          { value: "false", label: "False" },
        ]}
      />
    );
  }
  return (
    <input
      aria-label={ariaLabel}
      className={className}
      disabled={disabled}
      placeholder={placeholder}
      required={required}
      type={(valueType && INPUT_TYPE[valueType]) ?? "text"}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

/**
 * Form text as the value type the definition declares. An int64 stays a string, so integers above
 * 2^53 keep their exact digits on the wire.
 */
export function parseTypedValue(value: string, valueType: string, optional = false): unknown {
  if (optional && value === "") return null;
  if (valueType === "int64") return value;
  if (valueType === "double") return Number(value);
  if (valueType === "bool") return value === "true";
  if (valueType === "datetime") return new Date(value).toISOString();
  return value;
}

/** A stored value as the text its control edits; a datetime becomes the local input format. */
export function editableValue(value: unknown, valueType: V2ValueType): string {
  if (value === null || value === undefined) return "";
  const text = typeof value === "string" ? value : String(value);
  return valueType === "datetime" ? toDateTimeLocalInput(text) : text;
}
