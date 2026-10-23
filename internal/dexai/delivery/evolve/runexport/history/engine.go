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
	"maps"
	"math"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	// maxChainReads bounds the history reads of the earlier runs a record walks
	// back through, so one record stays a short, bounded Live call however few
	// events each run of its chain holds.
	maxChainReads = 32
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
		run := runIdentity{flowType: entry.GetFlowType(), FlowID: entry.GetFlowId(), status: entry.GetFlowStatus(), StartedAt: entry.GetStartTime().AsTime().UTC(),
			indexed: entry.GetIndexedAttributes()}
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

// Record reads a run's history and compacts it. A run that continues earlier
// runs of its FlowID is read with them, back to the first run, within the
// event limit and maxChainReads; when a limit or the engine's retention stops
// that walk, the record starts at the oldest run read (HistoryFrom). A history longer than
// the limit is cut there, and the run's current allow-listed Attributes are
// then read from its state, so an Attribute outcome written later is not
// missed.
func (e Engine) Record(ctx context.Context, found Found, s Settings) (runexport.Record, error) {
	history, err := e.chain(ctx, found)
	if err != nil {
		return runexport.Record{}, err
	}
	if history.truncated {
		if history.state, err = e.currentAttributes(ctx, found, s); err != nil {
			return runexport.Record{}, err
		}
	}
	reader := &engineValues{ctx: ctx, flows: e.Flows, flowID: found.FlowID, budget: defaultMaxBlobs}
	record := compact(found.runIdentity, history, reader, s)
	record.HistoryTruncated = history.truncated
	if reader.err != nil {
		return runexport.Record{}, reader.err
	}
	return record, nil
}

// chain reads the found run's history and, while it continues an earlier run
// and the event limit and maxChainReads allow, that run's history before it.
// An earlier run that does not fit is left out whole, so the events read have
// no gap.
func (e Engine) chain(ctx context.Context, found Found) (runHistory, error) {
	limit := e.MaxHistoryEvents
	if limit <= 0 {
		limit = defaultMaxHistoryEvents
	}
	newest, err := e.history(ctx, found.FlowID, found.runID, limit, math.MaxInt)
	if err != nil {
		return runHistory{}, err
	}
	runs := []runRead{newest}
	remaining, reads := limit-len(newest.events), maxChainReads
	for previous := previousRun(newest.events); previous != "" && remaining > 0 && reads > 0; previous = previousRun(runs[len(runs)-1].events) {
		older, err := e.history(ctx, found.FlowID, previous, remaining, reads)
		if status.Code(err) == codes.NotFound {
			break
		}
		if err != nil {
			return runHistory{}, err
		}
		if older.truncated {
			break
		}
		runs = append(runs, older)
		remaining -= len(older.events)
		reads -= older.reads
	}
	history := runHistory{runs: len(runs), truncated: newest.truncated, unreadFrom: newest.unreadFrom}
	for i := len(runs) - 1; i >= 0; i-- {
		history.events = append(history.events, runs[i].events...)
	}
	return history, nil
}

// previousRun is the run that a history's first event says it continues, or "" for a run that started a FlowID.
func previousRun(events []*dexpb.FlowHistoryEvent) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].GetFlowStartedOrContinued().GetContinuedStart().GetPreviousRunId()
}

// currentAttributes reads the run's allow-listed Attributes and its trial variant from its state.
func (e Engine) currentAttributes(ctx context.Context, found Found, s Settings) ([]*dexpb.KV, error) {
	keys := append(slices.Sorted(maps.Keys(s.Fields)), variantAttribute)
	response, err := e.Flows.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: found.FlowID, RunId: found.runID, Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("read the current Attributes of %s: %w", found.FlowID, err)
	}
	return response.GetAttributes(), nil
}

// runRead is what history read of one run: its events in order, the pages it
// read, whether a limit cut them and, if so, the earliest event time among the
// unread events of the page it stopped in, since the engine's order is not
// time order.
type runRead struct {
	events     []*dexpb.FlowHistoryEvent
	reads      int
	truncated  bool
	unreadFrom time.Time
}

// history reads one run's events in order, up to limit events in at most
// pages pages; a run with more is truncated.
func (e Engine) history(ctx context.Context, flowID, runID string, limit, pages int) (runRead, error) {
	var read runRead
	after := int64(0)
	for {
		if read.reads == pages {
			read.truncated = true
			return read, nil
		}
		response, err := e.Flows.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{
			FlowId: flowID, RunId: runID, StartInternalEventId: after + 1, EstimatePageSize: historyPageSize,
		})
		if err != nil {
			return runRead{}, fmt.Errorf("read the history of %s: %w", flowID, err)
		}
		read.reads++
		for i, event := range response.GetEvents() {
			if len(read.events) == limit {
				read.truncated = true
				read.unreadFrom = event.GetEventTime().AsTime()
				for _, unread := range response.GetEvents()[i+1:] {
					if at := unread.GetEventTime().AsTime(); at.Before(read.unreadFrom) {
						read.unreadFrom = at
					}
				}
				return read, nil
			}
			read.events = append(read.events, event)
		}
		if len(response.GetNextPageToken()) == 0 {
			return read, nil
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
