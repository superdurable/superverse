// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useId } from "react";
import type { ManifestField } from "@/lib/api/flow/connector";
import { descriptionParts, isManifestFieldInputRequired, manifestFieldDefaultText } from "./manifest-fields";
import { cx } from "@/components/ui/cx";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { FIELD, FIELD_LABEL, LINK, NOTE } from "@/components/flow/frame/workspace-styles";

/** The form controls the connection form and the trigger forms draw release-manifest fields with. */

const storedCredentialPlaceholder = "Stored - leave blank to keep";

/** One manifest field; a stored credential may be left blank to keep it. */
export function ManifestFormField({
  field,
  prefix,
  stored = false,
  values,
  setValues,
}: {
  field: ManifestField;
  prefix: string;
  stored?: boolean;
  values: Record<string, string>;
  setValues: (next: Record<string, string>) => void;
}) {
  return (
    <FormField
      label={field.name}
      name={`${prefix}:${field.name}`}
      placeholder={stored ? storedCredentialPlaceholder : undefined}
      required={!stored && isManifestFieldInputRequired(field)}
      secret={field.type === "secretString"}
      options={field.type === "enum" ? field.enum : undefined}
      description={field.description}
      defaultText={manifestFieldDefaultText(field)}
      values={values}
      setValues={setValues}
    />
  );
}

export function FormField({
  inputId,
  label,
  name,
  placeholder,
  required,
  secret,
  options,
  description,
  defaultText,
  values,
  setValues,
}: {
  inputId?: string;
  label: string;
  name: string;
  placeholder?: string;
  required: boolean;
  secret: boolean;
  /** options lists an enum field's values, which a dropdown offers instead of free text. */
  options?: string[];
  description?: string;
  defaultText?: string;
  values: Record<string, string>;
  setValues: (next: Record<string, string>) => void;
}) {
  const noteId = useId();
  const describedBy = [description && `${noteId}-description`, defaultText && `${noteId}-default`]
    .filter(Boolean)
    .join(" ");
  // Notes sit outside the label, so they describe the input instead of lengthening its name.
  return (
    <div className="grid min-w-0 gap-1">
      <label className="grid min-w-0 gap-1">
        <span className={FIELD_LABEL}>
          {label}
          {required ? " *" : ""}
        </span>
        {options ? (
          <Dropdown
            id={inputId}
            label={`${label}${required ? " *" : ""}`}
            required={required}
            value={values[name] ?? ""}
            placeholder={required ? "Choose a value" : "Not set"}
            onChange={(value) => setValues({ ...values, [name]: value })}
            describedBy={describedBy || undefined}
            items={[
              ...(required ? [] : [{ value: "", label: "Not set" }]),
              ...options.map((option) => ({ value: option, label: option })),
            ]}
          />
        ) : (
          <input
            autoComplete="off"
            className={FIELD}
            id={inputId}
            placeholder={placeholder}
            required={required}
            type={secret ? "password" : "text"}
            value={values[name] ?? ""}
            onChange={(event) => setValues({ ...values, [name]: event.target.value })}
            aria-describedby={describedBy || undefined}
          />
        )}
      </label>
      {description && (
        <small className={NOTE} id={`${noteId}-description`}>
          {linkifiedDescription(description)}
        </small>
      )}
      {defaultText && (
        <small className={cx(NOTE, "italic")} id={`${noteId}-default`}>
          {defaultText}
        </small>
      )}
    </div>
  );
}

/** A manifest description with its http(s) URLs as links that open in a new tab. */
export function linkifiedDescription(description: string) {
  return descriptionParts(description).map((part, index) =>
    part.url ? (
      <a className={LINK} href={part.url} key={`${part.text}:${index}`} rel="noreferrer" target="_blank">
        {part.text}
      </a>
    ) : (
      part.text
    ),
  );
}
