// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { conditionStatusLabel, durabilityLabel } from "@/lib/flow/enum-labels";
import { formatUnixSeconds } from "@/lib/shared/format";
import { asRecord, asRecordArray, type UnknownRecord } from "@/lib/shared/json";
import { cx } from "@/components/ui/cx";
import { groupPersistenceEntries } from "./persistence-groups";
import { RecordCard, RecordDisclosure, RecordTitle, Records } from "./DetailRecords";
import { DecodedValue, Fields, ObjectJSON, decodedValue, displayValue, secondsText } from "./ValueFields";
import { WaitingConditionContent } from "./WaitingCondition";

const MUTED = cx("text-xs text-ink-faint");

function isWaitingCondition(value: unknown): boolean {
  const record = asRecord(value);
  return (
    "waitingConditionType" in record ||
    "channelConditions" in record ||
    "timerConditions" in record ||
    "subFlowConditions" in record
  );
}

function isKeyValueList(value: unknown): boolean {
  const entries = asRecordArray(value);
  return entries.length > 0 && entries.every((entry) => "key" in entry);
}

function isTimerList(value: unknown): boolean {
  const entries = asRecordArray(value);
  return (
    entries.length > 0 && entries.every((entry) => "firingUnixTimestampSeconds" in entry || "durationSeconds" in entry)
  );
}

function KeyValueListStructured({ value }: { value: unknown }) {
  return (
    <PersistenceStructured entries={asRecordArray(value)} renderValue={(value) => <DecodedValue value={value} />} />
  );
}

function ChannelValues({ value }: { value: unknown }) {
  const values = asRecordArray(asRecord(value).values);
  if (values.length === 0) return <DecodedValue value={value} />;
  return (
    <Records>
      {values.map((message, index) => (
        <RecordCard key={index}>
          <RecordTitle>Message {index + 1}</RecordTitle>
          <DecodedValue value={message} />
        </RecordCard>
      ))}
    </Records>
  );
}

function PendingChannelsStructured({ value }: { value: UnknownRecord }) {
  return (
    <PersistenceStructured
      channel
      entries={Object.entries(value).map(([key, entry]) => ({ key, value: entry }))}
      renderValue={(value) => <ChannelValues value={value} />}
    />
  );
}

/** `{key, value}` entries as records, with Map instances as collapsed entries under their map name. */
function PersistenceStructured({
  entries,
  renderValue,
  channel = false,
}: {
  entries: UnknownRecord[];
  renderValue: (value: unknown) => ReactNode;
  channel?: boolean;
}) {
  const groups = groupPersistenceEntries(entries, (entry) =>
    typeof entry.key === "string" ? entry.key : displayValue(entry.key),
  );
  return (
    <Records>
      {groups.map((group) =>
        group.kind === "value" ? (
          <RecordCard key={`${String(group.entry.key)}-${group.index}`}>
            <RecordTitle channel={channel}>{displayValue(group.entry.key)}</RecordTitle>
            {renderValue(group.entry.value)}
          </RecordCard>
        ) : (
          <RecordCard key={group.name}>
            <RecordTitle channel={channel}>{group.name}</RecordTitle>
            {group.entries.map(({ entry, instance, index }) => (
              <RecordDisclosure key={`${instance}-${index}`} tone="entry" title={<RecordTitle>{instance}</RecordTitle>}>
                {renderValue(entry.value)}
              </RecordDisclosure>
            ))}
          </RecordCard>
        ),
      )}
    </Records>
  );
}

function TimerListStructured({ value }: { value: unknown }) {
  return (
    <Records>
      {asRecordArray(value).map((timer, index) => (
        <RecordCard key={`${String(timer.conditionId)}-${index}`}>
          <RecordTitle>Timer {index + 1}</RecordTitle>
          <Fields
            compact
            values={[
              ["Condition ID", timer.conditionId],
              ["Status", conditionStatusLabel(timer.status)],
              ["Fires at", formatUnixSeconds(timer.firingUnixTimestampSeconds)],
              ["Delay", secondsText(timer.durationSeconds)],
            ]}
          />
        </RecordCard>
      ))}
    </Records>
  );
}

function ObjectStructured({ value }: { value: UnknownRecord }) {
  const entries = Object.entries(value);
  if (entries.length === 0) return <p className={MUTED}>Empty object</p>;
  return (
    <dl className="grid min-w-0 grid-cols-1 gap-2">
      {entries.map(([key, entry]) => {
        const decoded = decodedValue(entry);
        const labeled = key === "stepDurability" || key === "waitForDurability" ? durabilityLabel(entry) : null;
        return (
          <div className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-1.25" key={key}>
            <dt className="text-[9px] text-ink-faint">{key}</dt>
            <dd className="min-w-0 font-mono text-[10px] font-semibold wrap-anywhere text-ink">
              {labeled ??
                (decoded && typeof decoded === "object" ? <ObjectJSON value={decoded} /> : displayValue(decoded))}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

function ArrayStructured({ value }: { value: unknown[] }) {
  if (value.length === 0) return <p className={MUTED}>Empty list</p>;
  return (
    <Records>
      {value.map((entry, index) => (
        <RecordCard key={index}>
          <RecordTitle>Item {index + 1}</RecordTitle>
          {entry && typeof entry === "object" ? (
            <ObjectStructured value={asRecord(entry)} />
          ) : (
            <code className="wrap-anywhere">{displayValue(entry)}</code>
          )}
        </RecordCard>
      ))}
    </Records>
  );
}

export function StructuredValue({
  value,
  parentFlowId = "",
  stepExecutionId = "",
  persistenceKind,
}: {
  value: unknown;
  parentFlowId?: string;
  stepExecutionId?: string;
  persistenceKind?: "attributes" | "channels";
}) {
  if (value === undefined || value === null) {
    return <p className={MUTED}>No value</p>;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <p className={MUTED}>Empty list</p>;
    if (persistenceKind === "attributes" && isKeyValueList(value)) return <KeyValueListStructured value={value} />;
    if (isTimerList(value)) return <TimerListStructured value={value} />;
    return <ArrayStructured value={value} />;
  }
  if (typeof value === "object") {
    if (Object.keys(value).length === 0) return <p className={MUTED}>Empty object</p>;
    if (persistenceKind === "channels") return <PendingChannelsStructured value={asRecord(value)} />;
    if (isWaitingCondition(value)) {
      return (
        <div className="flex min-w-0 flex-col gap-2">
          <WaitingConditionContent value={value} parentFlowId={parentFlowId} stepExecutionId={stepExecutionId} />
        </div>
      );
    }
    return <ObjectStructured value={asRecord(value)} />;
  }
  return <code className="wrap-anywhere">{displayValue(value)}</code>;
}
