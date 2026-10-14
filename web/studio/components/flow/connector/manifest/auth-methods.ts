// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type {
  ConnectionView,
  ConnectionWrite,
  ManifestAuthMethod,
  ManifestField,
  ReleaseManifest,
} from "@/lib/api/flow/connector";
import { manifestFormFieldText, parseManifestFieldInput } from "./manifest-fields";

/** The authorization methods a release offers, and the connection write bodies built from the chosen one. */

/** The chosen method; a release without declared methods offers its top-level auth as one unnamed method. */
export function selectedManifestAuth(auth: ReleaseManifest["spec"]["auth"], methodId: string): ManifestAuthMethod {
  if (!auth.methods?.length) {
    return {
      id: "",
      displayName: "",
      description: "",
      type: auth.type ?? "none",
      // A connector that needs no sign-in, such as a public feed, declares no fields.
      fields: auth.fields ?? [],
      guide: auth.guide,
      oauth2: auth.oauth2,
    };
  }
  return (
    auth.methods.find((method) => method.id === methodId) ??
    auth.methods.find((method) => method.id === auth.defaultMethod) ??
    auth.methods[0]
  );
}

export function methodConfigurationFields(method: ManifestAuthMethod): ManifestField[] {
  return method.configuration?.fields ?? [];
}

// The server stamps the selected method into the stored credentials; blank stored credentials are kept.
// The connection names its module but no release, so a connector upgrade keeps it valid.
export function connectionWriteRequestBody(
  connection: ConnectionView,
  manifest: ReleaseManifest,
  authMethodId: string,
  values: Record<string, string>,
  storedValueFieldNames: ReadonlySet<string> = new Set(),
): ConnectionWrite {
  const method = selectedManifestAuth(manifest.spec.auth, authMethodId);
  const storedCredentialFields = new Set(connection.storedCredentialFields ?? []);
  const credentials: Record<string, string> = {};
  const keepCredentialFields: string[] = [];
  for (const { name } of method.fields) {
    const value = values[`credential:${name}`] ?? "";
    if (value !== "") credentials[name] = value;
    else if (storedCredentialFields.has(name)) keepCredentialFields.push(name);
  }
  return {
    modulePath: connection.modulePath,
    provider: manifest.spec.provider,
    authMethodId: method.id,
    configuration: connectionConfigurationValues(
      manifest,
      method,
      values,
      connection.configuration ?? {},
      storedValueFieldNames,
    ),
    credentials,
    keepCredentialFields,
    credentialExpiresAt: null,
  };
}

// A connection-field Studio unit saves one configuration field and keeps every stored credential.
export function connectionConfigurationSaveRequestBody(
  connection: ConnectionView,
  manifest: ReleaseManifest,
  configuration: Record<string, unknown>,
): ConnectionWrite {
  const method = selectedManifestAuth(manifest.spec.auth, connection.authMethodId ?? "");
  const declaredCredentialFields = new Set(method.fields.map((field) => field.name));
  return {
    modulePath: connection.modulePath,
    provider: manifest.spec.provider,
    authMethodId: connection.authMethodId ?? "",
    configuration,
    credentials: {},
    keepCredentialFields: (connection.storedCredentialFields ?? []).filter((name) =>
      declaredCredentialFields.has(name),
    ),
    credentialExpiresAt: connection.credentialExpiresAt ?? null,
  };
}

// Unchanged prefilled text sends the stored JSON value, so non-string values keep their type.
export function connectionConfigurationValues(
  manifest: ReleaseManifest,
  method: ManifestAuthMethod,
  values: Record<string, string>,
  storedConfiguration: Record<string, unknown>,
  storedValueFieldNames: ReadonlySet<string>,
): Record<string, unknown> {
  const configuration: Record<string, unknown> = {};
  for (const field of [...manifest.spec.configuration.fields, ...methodConfigurationFields(method)]) {
    const storedValue = storedConfiguration[field.name];
    if (storedValueFieldNames.has(field.name)) {
      if (storedValue !== undefined) configuration[field.name] = storedValue;
      continue;
    }
    const text = values[`configuration:${field.name}`] ?? "";
    if (text === "") continue;
    configuration[field.name] =
      storedValue !== undefined && text === manifestFormFieldText(field, storedValue)
        ? storedValue
        : parseManifestFieldInput(field, text);
  }
  return configuration;
}
