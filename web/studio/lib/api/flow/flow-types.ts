// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionGraph, FlowV2Definition } from "./flow-definition";
import type { FlowStatus } from "./flow-status";

interface KeyValue {
  key: string;
  value: unknown;
}

export interface FlowSummary {
  definitionBinding?: "CURRENT" | "DIFFERENT" | "UNAVAILABLE";
  definitionRevision?: string;
  flowId: string;
  runId: string;
  firstRunId: string;
  requestId: string;
  flowType: string;
  flowStatus: FlowStatus;
  flowStatusCode: number;
  startTime: string | null;
  closeTime: string | null;
}

type HistoryEventType =
  | "FlowStartedOrContinued"
  | "FlowClosed"
  | "StepWaitForCompleted"
  | "StepWaitForFailed"
  | "StepWaitForPending"
  | "StepExecuteCompleted"
  | "StepExecuteFailed"
  | "StepExecutePending"
  | "RpcExecutionCompleted"
  | "ChannelExternalPublish"
  | "TimeTravelFork";

export interface FlowHistoryEvent {
  eventId: number;
  eventTime: string | null;
  type: HistoryEventType;
  payload: Record<string, unknown>;
}

export interface HistoryPage {
  events: FlowHistoryEvent[];
  nextPageToken: string;
  nextInternalEventId: number;
}

export interface StreamMessage {
  value: unknown;
  resumeToken: string;
  createdTime: string | null;
  source: string;
}

export interface ActiveStepExecution {
  stepExecutionId: string;
  fromStepExecutionId: string;
  stepType: string;
  phase: "Active" | "Waiting" | "Unspecified";
  movement?: Record<string, unknown>;
  waitingCondition?: Record<string, unknown>;
  completedConditions?: Record<string, unknown>;
  stepExecutionLocals: KeyValue[];
  timers: Record<string, unknown>[];
  lastFailureInfo?: Record<string, unknown>;
}

export interface FlowState {
  flowConfig: Record<string, unknown>;
  attributes: KeyValue[];
  activeStepExecutions: ActiveStepExecution[];
  queuedSteps: Record<string, unknown>[];
  pendingChannelMessages: Record<string, unknown>;
  completedSteps: Record<string, unknown>[];
}

interface FlowDefinitionEntry {
  id: string;
  file: string;
  schemaVersion: "1.0" | "2.0";
  flowName: string;
  sourceLanguage: string;
  sourcePath: string;
  valid: boolean;
  graph: FlowDefinitionGraph;
}

export interface FlowRuntimeCatalogEntry {
  flowType: string;
  definition: FlowV2Definition;
}

export interface FlowRuntimeCatalog {
  enabled: boolean;
  flows: FlowRuntimeCatalogEntry[];
  definitionRevision: string;
  startFlow?: StartFlowCapability;
}

export interface StartFlowCapability {
  enabled: boolean;
  targetRevision?: string;
}

export interface FlowSearchRow {
  definitionBinding?: "CURRENT" | "DIFFERENT" | "UNAVAILABLE";
  definitionRevision?: string;
  flowId: string;
  flowType: string;
  flowStatus: FlowStatus;
  flowStatusCode: number;
  startTime: string | null;
  closeTime: string | null;
  indexedAttributes: Record<string, unknown>;
  summary?: Record<string, unknown>;
  summaryError?: string;
}

export interface FlowSearchResult {
  flows: FlowSearchRow[];
  nextPageToken: string;
}

export interface FlowDisplay {
  flowId: string;
  flowType: string;
  flowStatus: FlowStatus;
  flowStatusCode: number;
  isActive: boolean;
  display: Record<string, unknown>;
  attributeSnapshot: Record<string, unknown>;
  eligibleActions: string[];
}

export interface FlowDefinitionCatalog {
  configured: boolean;
  directory?: string;
  source?: string;
  definitionRevision?: string;
  definitionCount: number;
  definitions: FlowDefinitionEntry[];
}
