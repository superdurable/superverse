// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Engine diagnostics inspect and repair one Flow execution through the Dex
// engine, independent of the definitions. They are the only operations that
// accept a RunID.

// diagnosticLongPollTimeout leaves five seconds for the 35-second Studio BFF
// deadline and browser bound to deliver an idle read's 408 document.
const diagnosticLongPollTimeout = 30 * time.Second

// FlowRun names a Flow and, optionally, one of its runs.
type FlowRun struct{ FlowID, RunID string }

// Summary reads the engine summary of a Flow run and its definition binding.
func (f *FlowAPI) Summary(ctx context.Context, run FlowRun) Document {
	if run.FlowID == "" {
		return errorDocument(http.StatusBadRequest, "flowId is required")
	}
	result, err := f.client.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: run.FlowID, RunId: run.RunID})
	if err != nil {
		return grpcErrorDocument(err, "GetFlowSummary")
	}
	return JSONDocument(http.StatusOK, mapSummary(result, f.bundle.Revision))
}

// State reads the engine state of a Flow run.
func (f *FlowAPI) State(ctx context.Context, run FlowRun) Document {
	if run.FlowID == "" || run.RunID == "" {
		return errorDocument(http.StatusBadRequest, "flowId and runId are required")
	}
	result, err := f.client.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: run.FlowID, RunId: run.RunID})
	if err != nil {
		return grpcErrorDocument(err, "GetFlowState")
	}
	mapped, err := mapFlowState(result)
	if err != nil {
		return errorDocument(http.StatusBadGateway, err.Error())
	}
	return JSONDocument(http.StatusOK, mapped)
}

// HistoryRequest pages through the history events of a Flow run after AfterEventID.
type HistoryRequest struct {
	FlowRun
	AfterEventID  int64
	PageSize      int32
	NextPageToken string
}

// History reads one page of a Flow run's history events.
func (f *FlowAPI) History(ctx context.Context, request HistoryRequest) Document {
	if request.FlowID == "" || request.RunID == "" {
		return errorDocument(http.StatusBadRequest, "flowId and runId are required")
	}
	if request.AfterEventID < 0 || request.PageSize < 0 {
		return errorDocument(http.StatusBadRequest, "startInternalEventId and estimatePageSize must be non-negative integers")
	}
	pageToken, err := base64.StdEncoding.DecodeString(request.NextPageToken)
	if err != nil {
		return errorDocument(http.StatusBadRequest, "nextPageToken must be base64")
	}
	result, err := f.client.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{
		FlowId: request.FlowID, RunId: request.RunID, StartInternalEventId: request.AfterEventID,
		EstimatePageSize: request.PageSize, NextPageToken: pageToken,
	})
	if err != nil {
		return grpcErrorDocument(err, "GetHistoryEvents")
	}
	events := make([]historyEvent, 0, len(result.GetEvents()))
	for _, event := range result.GetEvents() {
		mapped, mapErr := mapHistoryEvent(event)
		if mapErr != nil {
			return errorDocument(http.StatusBadGateway, mapErr.Error())
		}
		events = append(events, mapped)
	}
	return JSONDocument(http.StatusOK, historyPage{
		Events: events, NextPageToken: base64.StdEncoding.EncodeToString(result.GetNextPageToken()),
		NextInternalEventID: result.GetNextInternalEventId(),
	})
}

// WaitHistory long-polls until the Flow run records the event after AfterEventID.
func (f *FlowAPI) WaitHistory(ctx context.Context, request HistoryRequest) Document {
	if request.FlowID == "" || request.RunID == "" || request.AfterEventID <= 0 {
		return errorDocument(http.StatusBadRequest, "flowId, runId, and positive nextInternalEventId are required")
	}
	// Dex has no wait parameter for history, so the window ends on this
	// server's clock and Dex keeps the caller's later deadline.
	waitContext, endWait := comm.WithLongPollWindow(ctx, diagnosticLongPollTimeout)
	defer endWait()
	result, err := f.client.WaitForHistoryEvent(waitContext, &dexpb.WaitForHistoryEventRequest{
		FlowId: request.FlowID, RunId: request.RunID, NextInternalEventId: request.AfterEventID,
	})
	if err != nil {
		if comm.LongPollWindowEnded(waitContext) {
			err = status.Error(codes.DeadlineExceeded, "history long poll exceeded the deadline")
		}
		return grpcErrorDocument(err, "WaitForHistoryEvent")
	}
	mapped, err := protoMap(result)
	if err != nil {
		return errorDocument(http.StatusBadGateway, err.Error())
	}
	return JSONDocument(http.StatusOK, mapped)
}

// StreamRequest reads the next message of a Flow Stream after ResumeToken.
type StreamRequest struct {
	FlowID, FlowType, StreamName, ResumeToken string
}

// ReadStream reads the next message of a Flow Stream.
func (f *FlowAPI) ReadStream(ctx context.Context, request StreamRequest) Document {
	streamName := strings.TrimSpace(request.StreamName)
	if request.FlowID == "" || request.FlowType == "" || streamName == "" {
		return errorDocument(http.StatusBadRequest, "flowId, flowType, and streamName are required")
	}
	result, err := f.client.ReadStream(ctx, &dexpb.ReadStreamRequest{
		FlowId: request.FlowID, FlowType: request.FlowType, StreamName: streamName, ResumeToken: request.ResumeToken,
		WaitTimeSeconds: int32(diagnosticLongPollTimeout / time.Second),
	})
	if err != nil {
		return grpcErrorDocument(err, "ReadStream")
	}
	if result.GetMessage() == nil {
		return errorDocument(http.StatusBadGateway, "ReadStream returned no message")
	}
	return JSONDocument(http.StatusOK, mapStreamMessage(result.GetMessage()))
}

// BlobsRequest loads the values behind blob references of one Flow.
type BlobsRequest struct {
	FlowID string
	Values []BlobReference
}

// BlobReference names one blob-backed string or object value.
type BlobReference struct{ ID, Kind string }

// LoadBlobs loads blob-backed values, keyed by kind and ID.
func (f *FlowAPI) LoadBlobs(ctx context.Context, request BlobsRequest) Document {
	if request.FlowID == "" {
		return errorDocument(http.StatusBadRequest, "flowId is required")
	}
	entries := make([]*dexpb.LoadBlobRequestEntry, 0, len(request.Values))
	seen := make(map[BlobReference]bool, len(request.Values))
	for _, reference := range request.Values {
		var value *dexpb.Value
		switch {
		case reference.ID == "":
		case reference.Kind == "string":
			value = &dexpb.Value{Kind: &dexpb.Value_InternalBlobIdForStringValue{InternalBlobIdForStringValue: reference.ID}}
		case reference.Kind == "object":
			value = &dexpb.Value{Kind: &dexpb.Value_InternalBlobIdForObjValue{InternalBlobIdForObjValue: reference.ID}}
		}
		if value == nil {
			return errorDocument(http.StatusBadRequest, "blob id and kind (string or object) are required")
		}
		if !seen[reference] {
			seen[reference] = true
			entries = append(entries, &dexpb.LoadBlobRequestEntry{FlowId: request.FlowID, BlobValue: value})
		}
	}
	result, err := f.client.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: entries})
	if err != nil {
		// Keep only the code: a blob error can name storage internals.
		return grpcErrorDocument(status.Error(status.Code(err), "Value blob unavailable"), "LoadBlobs")
	}
	mapped := make(map[string]any, len(result.GetValues()))
	for _, reference := range request.Values {
		if value, loaded := result.GetValues()[reference.ID]; loaded {
			mapped[reference.Kind+":"+reference.ID] = dexValue(value)
		}
	}
	return JSONDocument(http.StatusOK, loadBlobsResponse{Values: mapped})
}

// StopRequest cancels, terminates or fails a Flow run.
type StopRequest struct {
	FlowRun
	StopType int32
	Reason   string
}

// Stop cancels, terminates or fails a Flow run; terminate and fail need a reason.
func (f *FlowAPI) Stop(ctx context.Context, request StopRequest) Document {
	if request.FlowID == "" || request.StopType == 0 {
		return errorDocument(http.StatusBadRequest, "flowId and stopType are required")
	}
	stopType := dexpb.StopType(request.StopType)
	switch stopType {
	case dexpb.StopType_STOP_TYPE_CANCEL:
	case dexpb.StopType_STOP_TYPE_TERMINATE, dexpb.StopType_STOP_TYPE_FAIL:
		if strings.TrimSpace(request.Reason) == "" {
			return errorDocument(http.StatusBadRequest, "reason is required for terminate and fail")
		}
	default:
		return errorDocument(http.StatusBadRequest, "stopType must be cancel, terminate, or fail")
	}
	if _, err := f.client.StopFlow(ctx, &dexpb.StopFlowRequest{
		FlowId: request.FlowID, RunId: request.RunID, Reason: request.Reason, StopType: stopType,
	}); err != nil {
		return grpcErrorDocument(err, "StopFlow")
	}
	return JSONDocument(http.StatusOK, map[string]struct{}{})
}

// TimeTravelRequest resets a Flow run to an earlier point of its history.
type TimeTravelRequest struct {
	FlowRun
	TimeTravelType   int32
	Reason           string
	StepType         string
	StepExecutionID  string
	HistoryEventTime string
	StepMethod       int32
}

// TimeTravel resets a Flow run and returns the run that continues it.
func (f *FlowAPI) TimeTravel(ctx context.Context, request TimeTravelRequest) Document {
	if request.FlowID == "" || request.RunID == "" || request.TimeTravelType == 0 || strings.TrimSpace(request.Reason) == "" {
		return errorDocument(http.StatusBadRequest, "flowId, runId, timeTravelType, and reason are required")
	}
	result, err := f.client.ResetFlow(ctx, &dexpb.ResetFlowRequest{
		FlowId: request.FlowID, RunId: request.RunID, ResetType: dexpb.FlowResetType(request.TimeTravelType),
		Reason: request.Reason, StepType: request.StepType, StepExecutionId: request.StepExecutionID,
		HistoryEventTime: request.HistoryEventTime, StepMethod: dexpb.FlowResetStepMethod(request.StepMethod),
	})
	if err != nil {
		return grpcErrorDocument(err, "TimeTravel")
	}
	return JSONDocument(http.StatusOK, timeTravelFlowResponse{RunID: result.GetRunId()})
}

// ChannelMessageRef names one pending message of a Flow run's Channel.
type ChannelMessageRef struct {
	FlowRun
	ChannelName, MessageID string
}

// DeleteChannelMessage deletes one pending Channel message.
func (f *FlowAPI) DeleteChannelMessage(ctx context.Context, message ChannelMessageRef) Document {
	if message.FlowID == "" || message.ChannelName == "" || message.MessageID == "" {
		return errorDocument(http.StatusBadRequest, "flowId, channelName, and messageId are required")
	}
	if _, err := f.client.DeleteChannelMessage(ctx, &dexpb.DeleteChannelMessageRequest{
		FlowId: message.FlowID, RunId: message.RunID, ChannelName: message.ChannelName,
		MessageId: message.MessageID, RequestId: uuid.NewString(),
	}); err != nil {
		return grpcErrorDocument(err, "DeleteChannelMessage")
	}
	return JSONDocument(http.StatusOK, map[string]bool{"deleted": true})
}
