// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import {
  isBlobReferenceValue,
  isStoredValueUnavailable,
  storedValueJSONReplacer,
  VALUE_BLOB_UNAVAILABLE,
} from "@/lib/api/flow/flow-blobs";
import { asRecord, asRecordArray, isPresent, type UnknownRecord } from "@/lib/shared/json";
import { cx } from "@/components/ui/cx";
import { RecordDisclosure, RecordTitle, Records, WELL } from "./DetailRecords";
import { groupPersistenceEntries } from "./persistence-groups";

/** The building blocks every event view renders values with. */

/** A labelled value; `wide` spans both columns. Absent values are not shown. */
export type Field = [label: string, value: unknown, wide?: boolean];

/** A Dex Value message (string, int, double, bool, null or encoded object) as plain JSON. */
export function decodedValue(value: unknown): unknown {
  if (isBlobReferenceValue(value)) return "Loading stored value…";
  if (isStoredValueUnavailable(value)) return VALUE_BLOB_UNAVAILABLE;
  const message = asRecord(value);
  if ("stringValue" in message) return message.stringValue;
  if ("intValue" in message) return message.intValue;
  if ("doubleValue" in message) return message.doubleValue;
  if ("boolValue" in message) return message.boolValue;
  if ("nullValue" in message) return null;
  const object = asRecord(message.objValue);
  if (typeof object.payload !== "string") return value;
  try {
    const bytes = Uint8Array.from(atob(object.payload), (character) => character.charCodeAt(0));
    const decoded = new TextDecoder().decode(bytes);
    if (object.encoding === "json") {
      try {
        return JSON.parse(decoded) as unknown;
      } catch {
        return decoded;
      }
    }
    return decoded;
  } catch {
    return value;
  }
}

/** One line of text: strings, numbers and booleans as written, anything else as compact JSON. */
export function displayValue(value: unknown): string {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string" || typeof value === "number") return String(value);
  return JSON.stringify(value);
}

export function secondsText(value: unknown): string | undefined {
  return isPresent(value) ? `${String(value)}s` : undefined;
}

export function listText(value: unknown): string | undefined {
  return Array.isArray(value) && value.length > 0 ? value.map(String).join(", ") : undefined;
}

export function DetailSection({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2 rounded-lg border border-line-faint bg-raised p-2.5 text-ink-dim">
      {title && <h4 className="text-[11px] font-semibold text-ink">{title}</h4>}
      {children}
    </section>
  );
}

type FieldLayout = "regular" | "compact" | "stacked";

/** Columns fill the width they get, so the narrow sidebar lists one field per row and a wide card several. */
const FIELD_GRID = {
  regular: cx("grid-cols-[repeat(auto-fill,minmax(min(100%,11rem),1fr))]"),
  compact: cx("grid-cols-[repeat(auto-fill,minmax(min(100%,13rem),1fr))]"),
  stacked: cx("grid-cols-1"),
} satisfies Record<FieldLayout, string>;

const FIELD = {
  regular: cx("min-w-0"),
  compact: cx("grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-1.25"),
  stacked: cx("flex min-w-0 flex-col gap-0.5"),
} satisfies Record<FieldLayout, string>;

export function Fields({
  values,
  compact = false,
  stacked = false,
}: {
  values: Field[];
  compact?: boolean;
  stacked?: boolean;
}) {
  const visible = values.filter(([, value]) => isPresent(value));
  if (visible.length === 0) return null;
  const layout: FieldLayout = stacked ? "stacked" : compact ? "compact" : "regular";
  return (
    <dl className={cx("grid gap-2", FIELD_GRID[layout])}>
      {visible.map(([label, value, wide]) => (
        <div className={cx(FIELD[layout], wide && "col-span-full")} key={label}>
          <dt className={cx("text-[9px] text-ink-faint", layout === "regular" && "mb-0.5")}>{label}</dt>
          <dd className="overflow-hidden font-mono text-[10px] font-semibold wrap-anywhere text-ink">
            {displayValue(value)}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** A decoded object as indented JSON, with nested stored values shown by their state. */
export function ObjectJSON({ value }: { value: object }) {
  return <pre className={cx(WELL, "max-h-80")}>{JSON.stringify(value, storedValueJSONReplacer, 2)}</pre>;
}

/** A Dex Value decoded: an object as indented JSON, anything else as one line of code. */
export function DecodedValue({ value }: { value: unknown }) {
  const decoded = decodedValue(value);
  return decoded && typeof decoded === "object" ? (
    <ObjectJSON value={decoded} />
  ) : (
    <code className={WELL}>{displayValue(decoded)}</code>
  );
}

export function ValueBlock({
  label,
  value,
  showEmpty = false,
}: {
  label: string;
  value: unknown;
  showEmpty?: boolean;
}) {
  if (!isPresent(value) && !showEmpty) return null;
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-[9px] text-ink-faint">{label}</span>
      <DecodedValue value={value} />
    </div>
  );
}

/** The Details / Raw JSON switch above a payload; the caller owns the choice and the raw view. */
export function DetailsRawTabs({
  label,
  view,
  onView,
}: {
  label: string;
  view: "details" | "raw";
  onView: (view: "details" | "raw") => void;
}) {
  return (
    <div
      className="grid grid-cols-2 gap-1 rounded-lg border border-line-faint bg-raised p-1 text-ink-dim"
      role="tablist"
      aria-label={label}
    >
      {(["details", "raw"] as const).map((option) => (
        <button
          aria-selected={view === option}
          className="rounded-md px-2 py-1.5 text-[10px] font-bold text-ink-faint hover:text-ink aria-selected:bg-panel aria-selected:text-ink aria-selected:shadow-[0_1px_4px_rgba(28,51,45,0.1)]"
          key={option}
          onClick={() => onView(option)}
          role="tab"
          type="button"
        >
          {option === "details" ? "Details" : "Raw JSON"}
        </button>
      ))}
    </div>
  );
}

function CollapsedValueRecord({
  label,
  value,
  valueLabel,
  channel = false,
}: {
  label: string;
  value: unknown;
  valueLabel: string;
  channel?: boolean;
}) {
  return (
    <RecordDisclosure title={<RecordTitle channel={channel}>{label}</RecordTitle>}>
      <ValueBlock label={valueLabel} value={value} />
    </RecordDisclosure>
  );
}

/** Attribute-like `{key, value}` entries; AttributeMap instances group under their map name. */
export function KeyValues({ values, emptyLabel }: { values: unknown; emptyLabel?: string }) {
  const entries = asRecordArray(values);
  if (entries.length === 0) return emptyLabel ? <p className="text-xs text-ink-faint">{emptyLabel}</p> : null;
  return <PersistenceRecords entries={entries} keyOf={(entry) => entry.key} valueLabel="Value" />;
}

/** `{channelName, value}` messages; ChannelMap instances group under their map name. */
export function ChannelMessages({ values }: { values: unknown }) {
  const messages = asRecordArray(values);
  if (messages.length === 0) return null;
  return (
    <PersistenceRecords channel entries={messages} keyOf={(message) => message.channelName} valueLabel="Message" />
  );
}

/** Collapsed records, one per value, with Map instances nested under their map name. */
function PersistenceRecords({
  entries,
  keyOf,
  valueLabel,
  channel = false,
}: {
  entries: UnknownRecord[];
  keyOf: (entry: UnknownRecord) => unknown;
  valueLabel: string;
  channel?: boolean;
}) {
  const groups = groupPersistenceEntries(entries, (entry) => displayValue(keyOf(entry)));
  return (
    <Records>
      {groups.map((group) =>
        group.kind === "value" ? (
          <CollapsedValueRecord
            channel={channel}
            key={`${String(keyOf(group.entry))}-${group.index}`}
            label={displayValue(keyOf(group.entry))}
            value={group.entry.value}
            valueLabel={valueLabel}
          />
        ) : (
          <RecordDisclosure key={group.name} title={<RecordTitle channel={channel}>{group.name}</RecordTitle>}>
            {group.entries.map(({ entry, instance, index }) => (
              <CollapsedValueRecord
                channel={channel}
                key={`${instance}-${index}`}
                label={instance}
                value={entry.value}
                valueLabel={valueLabel}
              />
            ))}
          </RecordDisclosure>
        ),
      )}
    </Records>
  );
}
