// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { asRecordArray, type UnknownRecord } from "@/lib/shared/json";
import { ChannelMessages, DetailSection, Fields, KeyValues } from "@/components/flow/diagnostics/value/ValueFields";
import { RecordCard, Records, Subsection } from "@/components/flow/diagnostics/value/DetailRecords";

/** What an Execute method or RPC wrote: Attributes, recorded events, Step locals and Channel changes. */
export function EffectsSection({ value }: { value: UnknownRecord }) {
  return (
    <DetailSection title="Side effects">
      <EffectsContent value={value} />
    </DetailSection>
  );
}

export function EffectsContent({ value }: { value: UnknownRecord }) {
  const lists: Array<[title: string, entries: unknown]> = [
    ["Upsert attributes", value.upsertAttributes],
    ["Recorded events", value.recordEvents],
    ["Step locals", value.upsertStepExecutionLocals],
  ];
  const publishes = value.publishToChannel;
  const deletions = asRecordArray(value.deleteFromChannel);
  const nonEmpty = (entries: unknown) => Array.isArray(entries) && entries.length > 0;
  if (deletions.length === 0 && !nonEmpty(publishes) && !lists.some(([, entries]) => nonEmpty(entries))) return null;
  return (
    <>
      {lists
        .filter(([, entries]) => nonEmpty(entries))
        .map(([title, entries]) => (
          <Subsection title={title} key={title}>
            <KeyValues values={entries} />
          </Subsection>
        ))}
      {nonEmpty(publishes) && (
        <Subsection title="Channel publishes">
          <ChannelMessages values={publishes} />
        </Subsection>
      )}
      {deletions.length > 0 && (
        <Subsection title="Channel deletions">
          <Records>
            {deletions.map((deletion, index) => (
              <RecordCard key={`${String(deletion.messageId)}-${index}`}>
                <Fields
                  values={[
                    ["Channel", deletion.channelName],
                    ["Message ID", deletion.messageId],
                  ]}
                />
              </RecordCard>
            ))}
          </Records>
        </Subsection>
      )}
    </>
  );
}
