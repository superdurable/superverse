// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import { useConfigurationDraftQueue } from "@/lib/flow/FlowApiProvider";
import { connectionConfigurationSaveRequestBody } from "@/components/flow/connector/manifest/auth-methods";
import {
  CONNECTOR_REQUEST_FAILED,
  type CommandRunner,
  type ConnectionView,
  type ConnectorUIUnit,
  type ConnectionWriter,
  type ManifestField,
  type UISessionResponse,
} from "@/lib/api/flow/connector";
import { settingsDraftItem, type ProjectConnectionWriter } from "@/lib/api/flow/draft/scope-writes";
import { FOLLOW_PREVIEW, followsPreview, nextPublishSettingsItem } from "@/lib/api/flow/draft/next-publish-writes";
import {
  jsonPointerSegment,
  jsonPointerValue,
  mergeUnitValue,
} from "@/components/flow/connector/manifest/json-pointer";
import { isManifestFieldInputRequired, manifestFieldLabel } from "@/components/flow/connector/manifest/manifest-fields";
import { linkifiedDescription } from "@/components/flow/connector/manifest/ManifestFormField";
import { CompactConnectorUnit, type NextPublishChoice } from "./CompactConnectorUnit";
import { DraftItemNote, useDraftSnapshot } from "./DraftItemNote";
import { NativeConnectorUnit, type NativeUnitTarget, type ConfigurationUnitTarget } from "./NativeConnectorUnit";
import { FIELD_LABEL, NOTE } from "@/components/flow/frame/workspace-styles";

type HostProps = {
  connection: ConnectionView;
  session: UISessionResponse;
  /** One dropdown named `compact`, as in a row of settings, instead of the unit's full form. */
  compact?: string;
  /** What the compact dropdown's menu note adds, such as where its value comes from. */
  note?: string;
  /** The compact dropdown's colour beyond its own state: red when Setup misses it, amber worth a look. */
  tone?: "none" | "warn";
  onError: (message: string) => void;
};

/**
 * Wires a native unit to Studio: its provider reads run as commands of the connection's UI session, through
 * the connection's writer, and `save` takes each edit of its value. The unit mounts once per `generation`.
 */
function NativeConnectorUnitHost({
  writer,
  connection,
  session,
  target,
  save,
  compact,
  note,
  tone,
  nextPublish,
  generation = 0,
}: Omit<HostProps, "onError"> & {
  writer: CommandRunner;
  target: NativeUnitTarget;
  save: (value: Record<string, unknown>, immediate: boolean) => void;
  nextPublish?: NextPublishChoice;
  generation?: number;
}) {
  // NativeConnectorUnit already admits only commands its release declares for a capability the unit declares.
  const execute = useCallback(
    (commandId: string, parameters: Record<string, string>, signal?: AbortSignal) =>
      writer.invokeCommand(commandId, parameters, signal),
    [writer],
  );
  // Edits stay mounted while they save; only a new UI session or a change taken from elsewhere remounts.
  const key = `${session.sessionNonce}:${target.kind === "configurationUnit" ? target.instanceId : target.unitId}:${generation}`;
  const unit = { connection, manifest: session.manifest, target, execute, onSave: save };
  return compact ? (
    <CompactConnectorUnit key={key} {...unit} label={compact} note={note} tone={tone} nextPublish={nextPublish} />
  ) : (
    <NativeConnectorUnit key={key} {...unit} />
  );
}

/**
 * How a unit of Live's next publish reads: whether the server's item follows Preview, Preview's applied
 * value at the unit's port, and what current Live runs while the value waits for a publish.
 */
export interface NextPublishUnit {
  follows: boolean;
  previewValue: string;
  waiting?: { live: string; value: string };
}

/**
 * A native unit of one Flow operation's or trigger binding's settings in a Project scope, merged into that use's
 * `configuration` and saved through the scope's autosave queue. Units of one use share its newest value. On
 * Live's next publish (`nextPublish`) "Same as Preview" makes the whole use follow Preview again.
 */
export function ConfigurationUnit({
  writer,
  target,
  nextPublish,
  ...props
}: Omit<HostProps, "onError"> & {
  writer: ProjectConnectionWriter;
  target: ConfigurationUnitTarget;
  nextPublish?: NextPublishUnit;
}) {
  const queue = useConfigurationDraftQueue();
  useDraftSnapshot(queue);
  const [generation, setGeneration] = useState(0);
  const plain = settingsDraftItem(writer.catalog, writer.connection, target.use, target.configuration);
  const next =
    nextPublish &&
    nextPublishSettingsItem(writer.catalog, writer.connection, target.use, target.configuration, nextPublish.follows);
  const key = plain.key;
  const stored = queue.valueOf(key) ?? next?.base;
  const following = followsPreview(stored);
  // A follower shows what it resolves to, as the next publish reads it.
  const configuration = following || stored === undefined ? target.configuration : (stored as Record<string, unknown>);
  const current: ConfigurationUnitTarget = {
    ...target,
    configuration,
    value: Object.fromEntries(
      target.bindings.map((binding) => [binding.port, jsonPointerValue(configuration, binding.jsonPointer)]),
    ),
  };
  const merge = (latest: unknown, value: Record<string, unknown>) =>
    mergeUnitValue(
      followsPreview(latest) ? target.configuration : (latest as Record<string, unknown>),
      target.bindings,
      value,
    );
  return (
    <div className="grid min-w-0 gap-1">
      <NativeConnectorUnitHost
        {...props}
        writer={writer}
        target={current}
        generation={generation}
        nextPublish={
          next && nextPublish
            ? {
                following,
                previewValue: nextPublish.previewValue,
                waiting: nextPublish.waiting,
                onFollow: () => queue.save(next, () => FOLLOW_PREVIEW, true),
              }
            : undefined
        }
        save={(value, immediate) =>
          next
            ? queue.save(next, (latest) => merge(latest, value), immediate)
            : queue.save(plain, (latest) => merge(latest, value), immediate)
        }
      />
      <DraftItemNote queue={queue} itemKey={key} onTheirs={() => setGeneration((value) => value + 1)} />
    </div>
  );
}

/**
 * A connection configuration field that the release edits through one of its native units; a saved value
 * rewrites the connection and keeps its credentials.
 */
export function ConnectionStudioField({
  writer,
  connection,
  field,
  session,
  compact = false,
  onConfigured,
  onError,
}: {
  writer: ConnectionWriter;
  connection: ConnectionView;
  field: ManifestField;
  session: UISessionResponse;
  /** One dropdown beside the field's name, as in a setup form's row, instead of the unit's full form. */
  compact?: boolean;
  onConfigured: () => Promise<void>;
  onError: (message: string) => void;
}) {
  // A model is the connection's default model, as the full unit titles it; other fields keep their names.
  const name = field.studioUnit?.port === "model" ? "Default model" : manifestFieldLabel(field);
  const target = connectionFieldUnitTarget(field, connection);
  const save = useDebouncedSave(async (value: Record<string, unknown>) => {
    try {
      await writer.save(
        connectionConfigurationSaveRequestBody(
          connection,
          session.manifest,
          mergeUnitValue(connection.configuration ?? {}, target.bindings ?? [], value),
        ),
      );
      await onConfigured();
    } catch (error) {
      onError(errorMessage(error, CONNECTOR_REQUEST_FAILED));
    }
  });
  const host = { writer, connection, session, target, save };
  if (compact)
    return (
      <div className="grid min-w-0 gap-1">
        <span className={FIELD_LABEL}>{name}</span>
        <NativeConnectorUnitHost {...host} compact={name} />
        {field.description && <small className={NOTE}>{linkifiedDescription(field.description)}</small>}
      </div>
    );
  return (
    <div className="grid min-w-0 gap-1">
      <span className={FIELD_LABEL} title={field.name}>
        {manifestFieldLabel(field)}
        {isManifestFieldInputRequired(field) ? " *" : ""}
      </span>
      {field.description && <small className={NOTE}>{linkifiedDescription(field.description)}</small>}
      <NativeConnectorUnitHost {...host} />
    </div>
  );
}

/** A configuration field edited by a native unit the release actually declares. */
export function isStudioConnectionField(field: ManifestField, session: UISessionResponse): boolean {
  const studioUnit = field.studioUnit;
  return (
    studioUnit !== undefined && (session.manifest.spec.studio?.units ?? []).some((unit) => unit.id === studioUnit.unit)
  );
}

function connectionFieldUnitTarget(
  field: ManifestField,
  connection: ConnectionView,
): Extract<NativeUnitTarget, { kind: "connection" }> {
  const storedValue = connection.configuration?.[field.name];
  return {
    kind: "connection",
    unitId: field.studioUnit?.unit,
    bindings: [{ port: field.studioUnit?.port ?? field.name, jsonPointer: `/${jsonPointerSegment(field.name)}` }],
    value: storedValue === undefined ? {} : { [field.studioUnit?.port ?? field.name]: storedValue },
  };
}

/**
 * A Teamspace connection's setting is its own write, outside a Project scope's configuration: the newest
 * edit is written after a typing pause, or at once for a pick, and still written when the form closes.
 */
function useDebouncedSave(write: (value: Record<string, unknown>) => Promise<void>) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pending = useRef<Record<string, unknown> | null>(null);
  const latest = useRef(write);
  useEffect(() => {
    latest.current = write;
  });
  const flush = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    const value = pending.current;
    pending.current = null;
    if (value) void latest.current(value);
  }, []);
  useEffect(() => flush, [flush]);
  return (value: Record<string, unknown>, immediate: boolean) => {
    if (timer.current) clearTimeout(timer.current);
    pending.current = value;
    timer.current = setTimeout(flush, immediate ? 0 : 500);
  };
}

/** A configuration unit of one Flow operation or trigger binding, with the values it holds now. */
export function configurationUnitTarget(
  unit: ConnectorUIUnit,
  use: ConfigurationUnitTarget["use"],
  configuration: Record<string, unknown>,
): ConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    use,
    configuration,
    instanceId: unit.id,
    unitId: unit.unitId,
    label: unit.label,
    description: unit.description,
    required: unit.required,
    bindings: unit.bindings,
    value: Object.fromEntries(
      unit.bindings.map((binding) => [binding.port, jsonPointerValue(configuration, binding.jsonPointer)]),
    ),
  };
}
