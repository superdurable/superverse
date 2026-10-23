// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package history

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
)

const (
	// SearchPageSize is the hosted visibility store's largest page.
	SearchPageSize          = 100
	historyPageSize         = 500
	defaultMaxHistoryEvents = 2000
	defaultMaxBlobs         = 32
)

// Engine reads the runs of one Dex engine for an export.
type Engine struct {
	Flows dexpb.FlowServiceClient
	// MaxHistoryEvents bounds the events read per run; zero means 2,000.
	MaxHistoryEvents int
}

// Found is a run the search found; its engine run identity stays inside this package.
type Found struct {
	runIdentity
	runID string
}

// SearchPage returns one page of a Flow type's runs, newest start first within the page, and the next page's token.
func (e Engine) SearchPage(ctx context.Context, flowType, token string) ([]Found, string, error) {
	query, err := fdg.CompileQuery(flowType, nil, nil, fdg.V2Definition{})
	if err != nil {
		return nil, "", err
	}
	response, err := e.Flows.SearchFlows(ctx, &dexpb.SearchFlowsRequest{Query: fdg.EngineWorkflowSearchQuery(query), PageSize: SearchPageSize, NextPageToken: token})
	if err != nil {
		return nil, "", fmt.Errorf("search the runs of %s: %w", flowType, err)
	}
	found := make([]Found, 0, len(response.GetFlowRuns()))
	for _, entry := range response.GetFlowRuns() {
		run := runIdentity{flowType: entry.GetFlowType(), FlowID: entry.GetFlowId(), status: entry.GetFlowStatus(), StartedAt: entry.GetStartTime().AsTime().UTC()}
		if closed := entry.GetCloseTime(); closed != nil && entry.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
			at := closed.AsTime().UTC()
			run.closedAt = &at
		}
		found = append(found, Found{runIdentity: run, runID: entry.GetRunId()})
	}
	return found, response.GetNextPageToken(), nil
}

// InWindow reports whether a run belongs to an export whose window starts at from: open, or closed since.
func (f Found) InWindow(from time.Time) bool {
	return f.closedAt == nil || !f.closedAt.Before(from)
}

// Record reads a run's history and compacts it.
func (e Engine) Record(ctx context.Context, found Found, s Settings) (runexport.Record, error) {
	events, truncated, err := e.history(ctx, found)
	if err != nil {
		return runexport.Record{}, err
	}
	reader := &engineValues{ctx: ctx, flows: e.Flows, flowID: found.FlowID, budget: defaultMaxBlobs}
	record := compact(found.runIdentity, events, reader, s)
	record.HistoryTruncated = truncated
	if reader.err != nil {
		return runexport.Record{}, reader.err
	}
	return record, nil
}

// history reads a run's events in order, up to the event limit.
func (e Engine) history(ctx context.Context, found Found) ([]*dexpb.FlowHistoryEvent, bool, error) {
	limit := e.MaxHistoryEvents
	if limit <= 0 {
		limit = defaultMaxHistoryEvents
	}
	var events []*dexpb.FlowHistoryEvent
	after := int64(0)
	for {
		response, err := e.Flows.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{
			FlowId: found.FlowID, RunId: found.runID, StartInternalEventId: after + 1, EstimatePageSize: historyPageSize,
		})
		if err != nil {
			return nil, false, fmt.Errorf("read the history of %s: %w", found.FlowID, err)
		}
		for _, event := range response.GetEvents() {
			if len(events) == limit {
				return events, true, nil
			}
			events = append(events, event)
		}
		if len(response.GetNextPageToken()) == 0 {
			return events, false, nil
		}
		// The engine read up to NextInternalEventId, which may lie past the page's last event.
		after = response.GetNextInternalEventId() - 1
	}
}

// engineValues decodes values, loading stored values from the engine within a budget.
type engineValues struct {
	ctx    context.Context
	flows  dexpb.FlowServiceClient
	flowID string
	budget int
	err    error
}

func (v *engineValues) read(value *dexpb.Value) (any, bool) {
	switch value.GetKind().(type) {
	case *dexpb.Value_InternalBlobIdForStringValue, *dexpb.Value_InternalBlobIdForObjValue:
		if v.budget <= 0 || v.err != nil {
			return nil, false
		}
		v.budget--
		response, err := v.flows.LoadBlobs(v.ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: v.flowID, BlobValue: value}}})
		if err != nil {
			v.err = fmt.Errorf("load a stored value of %s: %w", v.flowID, err)
			return nil, false
		}
		for _, loaded := range response.GetValues() {
			return decode(loaded)
		}
		return nil, false
	}
	return decode(value)
}

// decode turns an inline Dex value into JSON-like Go values; numbers in objects stay json.Number.
func decode(value *dexpb.Value) (any, bool) {
	switch kind := value.GetKind().(type) {
	case *dexpb.Value_StringValue:
		return kind.StringValue, true
	case *dexpb.Value_IntValue:
		return kind.IntValue, true
	case *dexpb.Value_DoubleValue:
		if math.IsNaN(kind.DoubleValue) || math.IsInf(kind.DoubleValue, 0) {
			return nil, false
		}
		return kind.DoubleValue, true
	case *dexpb.Value_BoolValue:
		return kind.BoolValue, true
	case *dexpb.Value_ObjValue:
		var decoded any
		if kind.ObjValue.GetEncoding() == "json" && comm.DecodeStrictJSONNumbers(kind.ObjValue.GetPayload(), &decoded) == nil {
			return decoded, true
		}
	}
	return nil, false
}
