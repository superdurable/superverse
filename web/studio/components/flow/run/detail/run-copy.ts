// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** Wording Runs, the Inbox and run search share, kept in tables so it can be audited in one place. */
export const RUN_COPY = {
  runsHeading: "Runs",
  previewRunsHeading: "Runs · Preview",
  liveRunsHeading: "Runs · Live",
  testRun: "Test run",
  liveRun: "Live run",
  clickStep: "Click a step for its details",
  noRuns: "No runs yet. Start a test run on Preview to try the latest version.",
  selectPrompt: "Select a run to see where it is and what it needs.",
  /** The step the canvas is showing, which is not always the one that needs somebody. */
  showingStep: "Showing",
  waitingAt: "Waiting at",
  noActions: "No Actions are available in the current state.",
  stop: "Stop",
  actionProgress: "Your actions",
  allActionsDone: "all done",
  actionsLeft(n: number): string {
    return n === 1 ? "1 left" : `${n} left`;
  },
  thisStep: "This step",
  actions: "Actions",
  stepOnly: "Step in this flow",
  noPurpose: "This Flow does not say what this Step is for.",
  close: "Close the run panel",
  expandList: "Show the run list",
  foldList: "Fold the run list",
  expandDrawer: "Show the run details",
  resizeList: "Resize the run list",
  resizeDrawer: "Resize the run drawer",
  elapsed: "Running for",
  closed: "Closed",
  started: "Started",
  runId: "Run ID",
  flowId: "Flow ID",
  diagnostics: "Diagnostics",
} as const;

/**
 * Nothing the Inbox says claims a person is needed. A Flow Definition Graph declares when an Action
 * may be offered, not who must act, so the Inbox says what it read and where it read it.
 */
export const INBOX_COPY = {
  appName: "Inbox",
  liveNote: "live",
  clear: "Nothing is waiting. Items appear here when a run reaches a step that waits for a person.",
  liveItem: "A real item on Live. If you act here, you act on the live application.",
  selectPrompt: "Select a run to see what it reports and which Actions are available.",
  seeProcess: "See the process",
  close: "Close this item",
  /** What a succeeded Action says; the response says nothing of what follows, so neither does this. */
  done(action: string): string {
    return `${action}: done`;
  },
} as const;

/** How fresh a run list or a run's Display is, in Runs and the Inbox alike. */
export const LIVENESS_COPY = {
  loading: "Asking the process…",
  unreachable: "Cannot reach the process, so this list is not the whole picture.",
  stale: "Showing the last answer: the process did not respond just now.",
  staleShort: "stale",
  /**
   * Dex routes a Flow RPC to the worker that owns the run, so this run is unreadable for
   * good while the server itself is fine.
   */
  stranded:
    "This one cannot be read or acted on: it was started by a worker that is no longer running, so nothing can reach it. The process itself is fine.",
  strandedRow: "cannot be reached",
} as const;

/** Control names, not field names: what can be searched comes from the Flow's own contract. */
export const SEARCH_COPY = {
  statusLabel: "Execution status",
  anyStatus: "Any status",
  sinceLabel: "Started within",
  advanced: "Advanced search",
  flowIdLabel: "Flow ID",
  flowIdPlaceholder: "Exact Flow ID",
  attributeLabel: "Attribute",
  anyAttribute: "Any attribute",
  operatorLabel: "Comparison",
  attributeValueLabel: "Attribute value",
  pickAttribute: "Choose an attribute first",
  value: "Value",
  anyValue: "Any",
  search: "Search",
  searching: "Searching…",
  clear: "Clear",
  filter: "Filter",
  flowTypeLabel: "Flow type",
} as const;
