// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { stringField } from "@/lib/shared/json";
import type {
  ConnectorStudioClient,
  ConnectorStudioConnection,
  ModelListing,
} from "@/components/flow/connector/native/model/model-listing";
import { FIELD } from "@/components/flow/frame/workspace-styles";
import { Dropdown, type DropdownItem } from "@/components/ui/menu/Dropdown";
import { validateModelIDForRule } from "./model-id-rule";
import { loadErrorMessage, NativeConnectorUnit, UNIT_SPEC, type NativeUnitProps } from "./NativeConnectorUnit";
import { loadPickerOptions, slackInviteHint } from "./picker-options";

/** The option that makes a next publish's setting follow Preview's; no provider value is this. */
const SAME_AS_PREVIEW = "\u0000same-as-preview";

/**
 * A unit of Live's next publish: "Same as Preview" first, and the blue dot while its value is not what
 * current Live runs.
 */
export interface NextPublishChoice {
  /** Whether the setting follows Preview now. */
  following: boolean;
  /** Preview's applied value, which "Same as Preview" names; empty when Preview has none. */
  previewValue: string;
  onFollow: () => void;
  /** While the value waits for a publish: Live's name, "Live V12", and its value there. */
  waiting?: { live: string; value: string };
}

/** How a compact unit is drawn beyond its own state: red when Setup misses it, amber when it deserves a look. */
type CompactTone = "none" | "warn";

/**
 * A unit as one dropdown: the provider's choices (and, for a model, the default), or another exact value
 * typed in. Picking saves at once; a typed value saves after a pause. A unit that takes several values or
 * free text keeps its full form. Why a choice will not work yet, such as a channel the app is not in, is
 * the option's tooltip and the menu note.
 * It is drawn neutral, amber when its value will not work yet: on the Connectors tab only a working
 * connection is green. A next publish's unit starts with "Same as Preview" (`nextPublish`).
 */
export function CompactConnectorUnit(
  props: NativeUnitProps & { label: string; note?: string; tone?: CompactTone; nextPublish?: NextPublishChoice },
) {
  const unitId = props.target.unitId ?? "";
  const spec = UNIT_SPEC[unitId];
  if (!spec?.picker || spec.multiple || !props.manifest.spec.studio?.units?.some((unit) => unit.id === unitId))
    return <NativeConnectorUnit {...props} />;
  return <CompactPicker {...props} unitId={unitId} port={spec.port} />;
}

function CompactPicker({
  connection,
  manifest,
  target,
  execute,
  onSave,
  label,
  note: outerNote,
  tone: outerTone,
  nextPublish,
  unitId,
  port,
}: NativeUnitProps & {
  label: string;
  note?: string;
  tone?: CompactTone;
  nextPublish?: NextPublishChoice;
  unitId: string;
  port: string;
}) {
  const value = target.value ?? {};
  const [current, setCurrent] = useState(() => stringField(value[port]));
  // A value set from outside the picker, such as a default Setup fills in, replaces what it shows.
  const external = stringField(value[port]);
  const [seen, setSeen] = useState(external);
  if (external !== seen) {
    setSeen(external);
    setCurrent(external);
  }
  const [listing, setListing] = useState<ModelListing | null>(null);
  const [loadError, setLoadError] = useState("");
  const [typing, setTyping] = useState(false);
  const [draft, setDraft] = useState(current);
  const spreadsheetId = stringField(value.spreadsheetId);
  const savedProvider = stringField(connection.configuration?.provider);
  const savedRegion = stringField(connection.configuration?.region);
  useEffect(() => {
    const controller = new AbortController();
    const client: ConnectorStudioClient = {
      executeProviderCommand: async (commandId, capability, parameters = {}) => {
        controller.signal.throwIfAborted();
        const command = manifest.spec.studio?.commands?.find((item) => item.id === commandId);
        const unit = manifest.spec.studio?.units?.find((item) => item.id === unitId);
        if (!command || command.capability !== capability || !unit?.backendCapabilities?.includes(capability))
          throw new Error("This connector release does not declare the requested data source.");
        return execute(commandId, parameters, controller.signal);
      },
    };
    const saved: ConnectorStudioConnection = { configuration: { provider: savedProvider, region: savedRegion } };
    loadPickerOptions({ unit: unitId, spreadsheetId, manifest }, client, saved).then(
      (result) => !controller.signal.aborted && setListing(result),
      (error: unknown) => !controller.signal.aborted && setLoadError(loadErrorMessage(error)),
    );
    return () => controller.abort();
  }, [execute, manifest, savedProvider, savedRegion, spreadsheetId, unitId]);

  const save = (next: string, immediate: boolean, name?: string) => {
    setCurrent(next);
    onSave({ [port]: next, ...(unitId === "spreadsheetPicker" ? { spreadsheetName: name ?? "" } : {}) }, immediate);
  };
  const options = (listing?.models ?? []).filter((option) => !option.isHiddenByDefault || option.id === current);
  const listed = options.some((option) => option.id === current);
  const withDefault = port === "model";
  const invalidOf = (text: string) => {
    if (port !== "model" || !text.trim()) return "";
    const result = validateModelIDForRule(manifest.spec.provider === "gemini" ? "pathSegment" : "body", text);
    return result.isValid ? "" : result.message;
  };
  const invalid = invalidOf(draft);
  // A stored channel that no page lists as one the app is in is flagged like a listed one it is not in.
  const unavailable =
    options.find((option) => option.id === current)?.unavailableHint ??
    (unitId === "channelPicker" && listing && current && !listed ? slackInviteHint("this channel") : "");
  const nameOf = (id: string) => options.find((option) => option.id === id)?.label ?? id;
  const following = Boolean(nextPublish?.following) && !typing;
  // A follower that needs a look shows the value it resolves to, Preview's, and its note says so first.
  const attention = following && (outerTone !== undefined || Boolean(unavailable));
  // Why the choice reads as it does goes in the menu note, never as lines under the dropdown.
  const note = [
    attention
      ? current
        ? `${nameOf(current)} is Preview's value, which the next publish follows`
        : "Preview has no value yet, so the next publish has none"
      : "",
    unavailable,
    outerNote,
    ...(listing?.notices ?? []).map((notice) => notice.message),
    listing?.isTruncated ? "Only the first pages are listed; choose Other value… for one not listed." : "",
  ]
    .filter((sentence): sentence is string => Boolean(sentence))
    .map((sentence) => (/[.!?…][”’")]?$/.test(sentence) ? sentence : `${sentence}.`))
    .join(" ");
  const waiting = nextPublish?.waiting
    ? `${nextPublish.waiting.live} runs ${nextPublish.waiting.value ? nameOf(nextPublish.waiting.value) : "nothing"} now. This goes out with the next publish.`
    : undefined;
  const items: DropdownItem<string>[] = [
    ...(nextPublish
      ? [
          {
            value: SAME_AS_PREVIEW,
            label: "Same as Preview",
            sub: nextPublish.previewValue ? nameOf(nextPublish.previewValue) : "Preview has none yet",
          },
          "separator" as const,
        ]
      : []),
    ...(withDefault
      ? [{ value: "", label: target.kind === "connection" ? "Connector default" : "Connection default" }]
      : []),
    ...(current && !listed ? [{ value: current, label: current }] : []),
    ...options.map((option) => ({
      value: option.id,
      label: option.label ?? option.id,
      sub: [option.unavailableHint ? "app not in channel" : "", option.detail ?? ""].filter(Boolean).join(" · "),
      tone: option.unavailableHint ? ("warn" as const) : undefined,
      title: option.unavailableHint ?? (option.label ? option.id : undefined),
    })),
    "separator",
    {
      action: () => {
        setDraft(current);
        setTyping(true);
      },
      label: "Other value…",
    },
  ];
  return (
    <div aria-label={label} className="grid min-w-0 gap-1.5" role="group">
      <Dropdown
        label={label}
        disabled={!listing && !loadError}
        tone={outerTone ?? (unavailable ? "warn" : following ? "same" : "neutral")}
        note={note || undefined}
        pending={waiting}
        display={attention ? (current ? nameOf(current) : "Not set") : undefined}
        align={nextPublish ? "end" : "start"}
        value={following ? SAME_AS_PREVIEW : typing ? null : current}
        placeholder={
          typing ? "Other value…" : listing || loadError ? `Choose ${label.toLowerCase()}…` : "Loading choices…"
        }
        onChange={(next) => {
          setTyping(false);
          if (next === SAME_AS_PREVIEW) return nextPublish?.onFollow();
          const option = options.find((item) => item.id === next);
          save(next, true, option?.label);
        }}
        items={items}
      />
      {(typing || (loadError && !listing)) && (
        <div className="flex min-w-0 items-center gap-1.5">
          <input
            aria-label={`${label}, exact value`}
            className={FIELD}
            value={draft}
            onChange={(event) => {
              const text = event.target.value;
              setDraft(text);
              // A value the unit rejects is not saved; the hint below says why.
              if (text.trim() && !invalidOf(text)) save(text.trim(), false);
            }}
          />
        </div>
      )}
      {(loadError || invalid) && <p className="text-[11px] text-ink-faint">{invalid || loadError}</p>}
    </div>
  );
}
