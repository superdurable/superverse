// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState, type FormEvent, type ReactNode } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import {
  connectionConfigurationValues,
  connectionWriteRequestBody,
  methodConfigurationFields,
  selectedManifestAuth,
} from "@/components/flow/connector/manifest/auth-methods";
import {
  CONNECTOR_REQUEST_FAILED,
  connectorDisplayName,
  type ConnectionView,
  type ConnectionWrite,
  type ConnectionWriter,
  type ManifestAuthMethod,
  type ManifestField,
  type ReleaseManifest,
  type UISessionResponse,
} from "@/lib/api/flow/connector";
import {
  isManifestFieldInputRequired,
  manifestFormFieldText,
} from "@/components/flow/connector/manifest/manifest-fields";
import { FormField, ManifestFormField } from "@/components/flow/connector/manifest/ManifestFormField";
import {
  ConnectionStudioField,
  isStudioConnectionField,
} from "@/components/flow/connector/native/NativeConnectorUnitHost";
import { AuthorizationGuide } from "./AuthorizationGuide";
import { Disclosure, OAuthClientChoice, StepLegend } from "./FormSteps";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { CHOICE, CHOICE_INPUT, NOTE } from "@/components/flow/frame/workspace-styles";

/** A group of form fields under its legend. */
const FIELD_GROUP = cx("grid min-w-0 gap-2.5");

/** The connection form: authentication method, OAuth or credential fields, and connection configuration. */

type ManifestFormFieldEntry = { field: ManifestField; prefix: "configuration" | "credential" };

export function ConnectorForm({
  writer,
  connection,
  session,
  onAuthorized,
  onReload,
  onError,
  onCancel,
  lastStep,
  alsoSave,
  platformClientMethodIds = [],
}: {
  /** Where the form saves and starts OAuth, and the provider commands its native units run. */
  writer: ConnectionWriter;
  connection: ConnectionView;
  session: UISessionResponse;
  /** The credentials were saved; an OAuth method leaves the page for the provider instead. */
  onAuthorized: () => Promise<void>;
  /** A connection setting edited by a native unit was saved. */
  onReload: () => Promise<void>;
  onError: (message: string) => void;
  onCancel?: () => void;
  /** A last numbered step before Save, such as where the connection is used; told whether it signs in with OAuth. */
  lastStep?: (oauth: boolean) => { title: string; content: ReactNode } | null;
  /**
   * Another store's connection that gets the same entered values in the same save, as when one key serves
   * Preview and Live. Only typed values travel: a stored secret is never read back.
   */
  alsoSave?: { connection: ConnectionView; save: (write: ConnectionWrite) => Promise<void> };
  /** The methods for which the platform offers its own OAuth application, so no client is entered. */
  platformClientMethodIds?: string[];
}) {
  const manifest = session.manifest;
  const [values, setValues] = useState<Record<string, string>>(() => connectionFormInitialValues(manifest, connection));
  const [submitting, setSubmitting] = useState(false);
  const [authMethodId, setAuthMethodId] = useState(
    connection.authMethodId || manifest.spec.auth.defaultMethod || manifest.spec.auth.methods?.[0]?.id || "",
  );
  const auth = selectedManifestAuth(manifest.spec.auth, authMethodId);
  const oauth = auth.type === "oauth2";
  const platformOffered = oauth && platformClientMethodIds.includes(auth.id);
  const [usePlatform, setUsePlatform] = useState(true);
  const platform = platformOffered && usePlatform;
  const storedCredentialFields = new Set(oauth ? [] : (connection.storedCredentialFields ?? []));
  const storedValueFields = connectionStoredValueFieldNames(manifest, connection, session);
  const mappedCredentialNames = new Set([
    ...(auth.oauth2?.clientIDCredential ? [auth.oauth2.clientIDCredential] : []),
    ...(auth.oauth2?.clientSecretCredential ? [auth.oauth2.clientSecretCredential] : []),
    ...(auth.oauth2?.credentialMappings?.map((mapping) => mapping.credential) ?? ["access_token"]),
    ...(auth.oauth2?.credentialDerivations?.map((derivation) => derivation.credential) ?? []),
  ]);
  const visibleManifestFormFields: ManifestFormFieldEntry[] = [
    ...manifest.spec.configuration.fields.map((field) => ({ field, prefix: "configuration" }) as const),
    ...methodConfigurationFields(auth).map((field) => ({ field, prefix: "configuration" }) as const),
    ...auth.fields
      .filter((field) => !oauth || !mappedCredentialNames.has(field.name))
      .map((field) => ({ field, prefix: "credential" }) as const),
  ];
  const requiredManifestFormFields = visibleManifestFormFields.filter(({ field }) =>
    isManifestFieldInputRequired(field),
  );
  const optionalManifestFormFields = visibleManifestFormFields.filter(
    ({ field }) => !isManifestFieldInputRequired(field),
  );
  const renderFormField = ({ field, prefix }: ManifestFormFieldEntry) =>
    prefix === "configuration" ? (
      <ConnectionConfigurationField
        writer={writer}
        connection={connection}
        field={field}
        key={`${prefix}:${field.name}`}
        onConfigured={onReload}
        onError={onError}
        session={session}
        setValues={setValues}
        values={values}
      />
    ) : (
      <ManifestFormField
        field={field}
        key={`${prefix}:${field.name}`}
        prefix={prefix}
        stored={storedCredentialFields.has(field.name)}
        setValues={setValues}
        values={values}
      />
    );
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      if (oauth) {
        const configuration = connectionConfigurationValues(
          manifest,
          auth,
          values,
          connection.configuration ?? {},
          storedValueFields,
        );
        const credentialValues = fieldValues(
          auth.fields.filter((field) => field.type !== "secretString"),
          values,
          "credential",
        );
        const credentialSecrets = Object.fromEntries(
          auth.fields
            .filter((field) => field.type === "secretString" && !mappedCredentialNames.has(field.name))
            .map((field) => [field.name, values[`credential:${field.name}`] ?? ""]),
        );
        const result = await writer.startOAuth({
          clientSource: platform ? "platform" : "user",
          authMethodId,
          clientId: values.clientId ?? "",
          clientSecret: values.clientSecret ?? "",
          configuration,
          credentialValues,
          credentialSecrets,
        });
        window.location.assign(result.authorizationUrl);
        return;
      }
      const write = connectionWriteRequestBody(connection, manifest, authMethodId, values, storedValueFields);
      await writer.save(write);
      if (alsoSave)
        await alsoSave.save(
          connectionWriteRequestBody(alsoSave.connection, manifest, authMethodId, values, storedValueFields),
        );
      setValues(configurationFormValues);
      await onAuthorized();
    } catch (submitError) {
      onError(errorMessage(submitError, CONNECTOR_REQUEST_FAILED));
    } finally {
      setSubmitting(false);
    }
  };
  // A connection being set up reads as numbered steps; a working one folds its guide away.
  const settingUp = connection.status !== "Ready";
  const steps = [
    (manifest.spec.auth.methods?.length ?? 0) > 1 ? "method" : null,
    auth.guide && settingUp && !platform ? "guide" : null,
    oauth || requiredManifestFormFields.length > 0 ? "fields" : null,
    lastStep?.(oauth) ? "last" : null,
  ].filter(Boolean);
  const last = lastStep?.(oauth) ?? null;
  const stepNumber = (step: string) => (settingUp && steps.length > 1 ? steps.indexOf(step) + 1 : null);
  return (
    <form className="grid gap-3.5" id="connector-host-form" onSubmit={(event) => void submit(event)}>
      {(manifest.spec.auth.methods?.length ?? 0) > 1 && (
        <fieldset className={FIELD_GROUP}>
          <StepLegend number={stepNumber("method")}>
            {manifest.spec.auth.methodLabel?.trim() || "How to sign in"}
          </StepLegend>
          {manifest.spec.auth.methods?.map((method) => (
            <label className={CHOICE} key={method.id}>
              <input
                className={CHOICE_INPUT}
                checked={authMethodId === method.id}
                name="connector-auth-method"
                onChange={() => {
                  setAuthMethodId(method.id);
                  setValues(configurationFormValues);
                }}
                type="radio"
                value={method.id}
              />
              <span className="grid min-w-0 gap-0.5">
                <b className="text-[13px] font-semibold text-ink">{method.displayName}</b>
                {method.recommended && (
                  <em className="text-[10px] tracking-wider text-accent uppercase not-italic">Recommended</em>
                )}
                <small className="text-[11px] text-ink-faint">{method.description}</small>
              </span>
            </label>
          ))}
        </fieldset>
      )}
      {auth.guide &&
        !platform &&
        (settingUp ? (
          <div className={FIELD_GROUP}>
            <StepLegend number={stepNumber("guide")}>
              Follow {manifest.metadata.displayName || connectorDisplayName(connection)}&apos;s steps
            </StepLegend>
            <AuthorizationGuide guide={auth.guide} redirectURI={oauth ? session.oauthRedirectUri : undefined} />
          </div>
        ) : (
          <Disclosure label="How to connect">
            <AuthorizationGuide guide={auth.guide} redirectURI={oauth ? session.oauthRedirectUri : undefined} />
          </Disclosure>
        ))}
      {(oauth || requiredManifestFormFields.length > 0) && (
        <fieldset className={FIELD_GROUP}>
          <StepLegend number={stepNumber("fields")}>
            {platform ? "Sign in with the platform app" : oauth ? "Your app's client ID and secret" : "Fill in"}
          </StepLegend>
          {platformOffered && (
            <OAuthClientChoice connectorId={connection.connectorId} platform={usePlatform} onChange={setUsePlatform} />
          )}
          {oauth && !platform && (
            <>
              <FormField
                inputId="connector-oauth-client-id"
                label="OAuth client ID"
                name="clientId"
                required
                secret={false}
                description="From the provider application the steps above create. It is not secret."
                values={values}
                setValues={setValues}
              />
              <FormField
                label="OAuth client secret"
                name="clientSecret"
                required
                secret
                description="Stored write-only so access tokens can refresh, and never shown again."
                values={values}
                setValues={setValues}
              />
            </>
          )}
          {requiredManifestFormFields.map(renderFormField)}
        </fieldset>
      )}
      {optionalManifestFormFields.length > 0 && (
        <Disclosure label={`Optional settings (${optionalManifestFormFields.length})`}>
          <div className={FIELD_GROUP} data-optional="true">
            {optionalManifestFormFields.map(renderFormField)}
          </div>
        </Disclosure>
      )}
      {last && (
        <div className={FIELD_GROUP}>
          <StepLegend number={stepNumber("last")}>{last.title}</StepLegend>
          {last.content}
        </div>
      )}
      {oauth && <p className={NOTE}>{scopeNote(auth.oauth2)}</p>}
      <div className="flex items-center gap-1.5">
        <Button variant="strong" size="sm" disabled={submitting} type="submit">
          {oauth ? "Authorize" : "Save"}
        </Button>
        {onCancel && (
          <Button size="sm" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}

function ConnectionConfigurationField({
  writer,
  connection,
  field,
  session,
  values,
  setValues,
  onConfigured,
  onError,
}: {
  writer: ConnectionWriter;
  connection: ConnectionView;
  field: ManifestField;
  session: UISessionResponse;
  values: Record<string, string>;
  setValues: (next: Record<string, string>) => void;
  onConfigured: () => Promise<void>;
  onError: (message: string) => void;
}) {
  const presentation = connectionFieldPresentation(field, connection, session);
  if (presentation === "nativeUnit") {
    return (
      <ConnectionStudioField
        writer={writer}
        compact
        connection={connection}
        field={field}
        onConfigured={onConfigured}
        onError={onError}
        session={session}
      />
    );
  }
  return <ManifestFormField field={field} prefix="configuration" setValues={setValues} values={values} />;
}

// A native unit reads the provider with the connection's credential, so until the connection works its field
// is a plain input, which the first save stores.
function connectionFieldPresentation(
  field: ManifestField,
  connection: ConnectionView,
  session: UISessionResponse,
): "nativeUnit" | "input" {
  return isStudioConnectionField(field, session) && connection.status === "Ready" ? "nativeUnit" : "input";
}

function connectionStoredValueFieldNames(
  manifest: ReleaseManifest,
  connection: ConnectionView,
  session: UISessionResponse,
): ReadonlySet<string> {
  return new Set(
    manifest.spec.configuration.fields
      .filter((field) => connectionFieldPresentation(field, connection, session) !== "input")
      .map((field) => field.name),
  );
}

function configurationFormValues(values: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(values).filter(([name]) => name.startsWith("configuration:")));
}

function connectionFormInitialValues(manifest: ReleaseManifest, connection: ConnectionView): Record<string, string> {
  const configuration = connection.configuration ?? {};
  const fields = [
    ...manifest.spec.configuration.fields,
    ...(manifest.spec.auth.methods ?? []).flatMap(methodConfigurationFields),
  ];
  return Object.fromEntries(
    fields
      .filter((field) => configuration[field.name] !== undefined)
      .map((field) => [`configuration:${field.name}`, manifestFormFieldText(field, configuration[field.name])]),
  );
}

function fieldValues(fields: ManifestField[], values: Record<string, string>, prefix: string) {
  return Object.fromEntries(
    fields
      .map((field) => [field.name, values[`${prefix}:${field.name}`] ?? ""] as const)
      .filter(([, value]) => value !== ""),
  );
}

/** What an authorization asks for: the declared scopes, or those of the setting they come from. */
function scopeNote(oauth2: ManifestAuthMethod["oauth2"]): string {
  const scopes = oauth2?.scopes;
  const user = oauth2?.userScopes?.length ? `; for the signed-in person: ${oauth2.userScopes.join(", ")}` : "";
  if (Array.isArray(scopes)) return `Asks for: ${scopes.join(", ")}${user}`;
  return scopes ? `Asks for the scopes of the ${scopes.fromSetting} setting${user}` : "";
}
