// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Two runs of the template's TextSummaryFlow after its Agent added the summary approval, as the Flow API answered
 * Studio on Local Kind while the second run waited for its Approve summary Action and the first had completed. The
 * documents are sanitized Dex JSON, not protobuf, so the lib/api/flow document types describe them; mock/README.md
 * records how they were recorded and sanitized.
 */

import type { FlowDisplay, FlowHistoryEvent, FlowSearchRow, FlowState, FlowSummary } from "@/lib/api/flow/flow-types";
import type { DefinitionGraph, DefinitionInput } from "@/lib/shared/flow-definition-catalog";

/** One run as the Flow API answered for it at the recording moment. */
export interface RecordedFlowRun {
  summary: FlowSummary;
  /** The run's row in a run search. */
  row: FlowSearchRow;
  display: FlowDisplay;
  /** Live state; a closed run has none. */
  state: FlowState | null;
  /** The whole history, which fits one page. */
  history: { events: FlowHistoryEvent[]; nextInternalEventId: number };
}

export interface RecordedFlowRuns {
  runs: RecordedFlowRun[];
  /** Stored Value blobs, keyed `<kind>:<id>` as LoadFlowBlobs answers them. */
  blobs: Record<string, unknown>;
}

/** A Flow Definition as the analyzer wrote it for the recorded source. */
type RecordedDefinition = Required<DefinitionInput<DefinitionGraph>> & { graph: DefinitionGraph };

interface FlowRunRecording extends RecordedFlowRuns {
  /** When the documents were read. */
  recordedAt: string;
  /** The Flow Definitions the runs started under. */
  definitions: RecordedDefinition[];
}

// Loaded on demand, like the other fixtures, so the recording stays out of bundles that never serve it.
async function loadRecording(): Promise<FlowRunRecording> {
  const recording: { default: unknown } = await import("./flow-runs.json", { with: { type: "json" } });
  return recording.default as FlowRunRecording;
}

/** The mock Project's Flow Definitions, as its Releases and Preview carry them: those the recorded runs started under. */
export async function loadProjectFlowDefinitions(): Promise<RecordedDefinition[]> {
  return [...structuredClone((await loadRecording()).definitions), ...(await loadExtraFlowDefinitions())];
}

/**
 * Analyzer FDG 2.0 files that `SUPERVERSE_STUDIO_MOCK_FLOWS` names, colon-separated, joined to the mock Project for
 * canvas layout work against a real application's Flows. They have no recorded runs.
 */
async function loadExtraFlowDefinitions(): Promise<RecordedDefinition[]> {
  const paths = (process.env.SUPERVERSE_STUDIO_MOCK_FLOWS ?? "").split(":").filter(Boolean);
  if (paths.length === 0) return [];
  const { readFile } = await import("node:fs/promises");
  const { createHash } = await import("node:crypto");
  return Promise.all(
    paths.map(async (path) => {
      const text = await readFile(path, "utf8");
      const graph = JSON.parse(text) as DefinitionGraph & { flow: { name: string }; source?: { path?: string } };
      return {
        flowType: graph.flow.name,
        sourcePath: graph.source?.path ?? path,
        schemaVersion: "2.0",
        digest: `sha256:${createHash("sha256").update(text).digest("hex")}`,
        valid: true,
        graph,
      };
    }),
  );
}

/** The recorded runs, every time moved so that they were read at `epochMs`. */
export async function loadRecordedFlowRuns(epochMs: number): Promise<RecordedFlowRuns> {
  const { recordedAt, runs, blobs } = await loadRecording();
  // Whole seconds keep each time's recorded fractional digits.
  return shiftTimes({ runs, blobs }, Math.round((epochMs - Date.parse(recordedAt)) / 1000));
}

/** A UTC time as Dex writes it: whole seconds, then any number of fractional digits. */
const UTC_TIME = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?Z$/;

/** A deep copy of `value` with every UTC time moved by `seconds`. */
export function shiftTimes<T>(value: T, seconds: number): T {
  return JSON.parse(JSON.stringify(value), (_key, entry: unknown) => {
    const time = typeof entry === "string" ? UTC_TIME.exec(entry) : null;
    if (!time) return entry;
    const shifted = new Date(Date.parse(`${time[1]}Z`) + seconds * 1000).toISOString().slice(0, 19);
    return `${shifted}${time[2] ?? ""}Z`;
  }) as T;
}
