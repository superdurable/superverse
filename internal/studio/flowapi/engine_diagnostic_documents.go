// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

// These are the engine diagnostic documents Studio reads.

type keyValue struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type loadBlobsResponse struct {
	Values map[string]any `json:"values"`
}

type flowSummary struct {
	DefinitionBinding  string  `json:"definitionBinding"`
	DefinitionRevision string  `json:"definitionRevision,omitempty"`
	FlowID             string  `json:"flowId"`
	RunID              string  `json:"runId"`
	FirstRunID         string  `json:"firstRunId"`
	RequestID          string  `json:"requestId"`
	FlowType           string  `json:"flowType"`
	FlowStatus         string  `json:"flowStatus"`
	FlowStatusCode     int32   `json:"flowStatusCode"`
	StartTime          *string `json:"startTime"`
	CloseTime          *string `json:"closeTime"`
}

type historyPage struct {
	Events              []historyEvent `json:"events"`
	NextPageToken       string         `json:"nextPageToken"`
	NextInternalEventID int64          `json:"nextInternalEventId"`
}

type historyEvent struct {
	EventID   int64          `json:"eventId"`
	EventTime *string        `json:"eventTime"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

type flowState struct {
	FlowConfig             map[string]any        `json:"flowConfig"`
	Attributes             []keyValue            `json:"attributes"`
	ActiveStepExecutions   []activeStepExecution `json:"activeStepExecutions"`
	QueuedSteps            []any                 `json:"queuedSteps"`
	PendingChannelMessages map[string]any        `json:"pendingChannelMessages"`
	CompletedSteps         []any                 `json:"completedSteps"`
}

type streamMessage struct {
	Value       any     `json:"value"`
	ResumeToken string  `json:"resumeToken"`
	CreatedTime *string `json:"createdTime"`
	Source      string  `json:"source"`
}

type activeStepExecution struct {
	StepExecutionID     string         `json:"stepExecutionId"`
	FromStepExecutionID string         `json:"fromStepExecutionId"`
	StepType            string         `json:"stepType"`
	Phase               string         `json:"phase"`
	Movement            map[string]any `json:"movement,omitempty"`
	WaitingCondition    map[string]any `json:"waitingCondition,omitempty"`
	CompletedConditions map[string]any `json:"completedConditions,omitempty"`
	StepExecutionLocals []keyValue     `json:"stepExecutionLocals"`
	Timers              []any          `json:"timers"`
	LastFailureInfo     map[string]any `json:"lastFailureInfo,omitempty"`
}

type timeTravelFlowResponse struct {
	RunID string `json:"runId"`
}
