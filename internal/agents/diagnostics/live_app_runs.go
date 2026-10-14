// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput/generated"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/studio/runs"
)

const (
	// maximumLiveLogBytes bounds the log tail of the Live application before
	// the result as a whole is fitted to maximumOutputBytes.
	maximumLiveLogBytes = 12 << 10
)

// liveSearchPage is one page of a Flow type's Live runs, newest first. A
// page with more runs names none of the rest: the next page filters on
// startTime before the oldest one shown.
type liveSearchPage struct {
	FlowType string    `json:"flow_type"`
	Runs     []liveRun `json:"runs"`
	More     bool      `json:"more,omitempty"`
}

type liveRun struct {
	FlowID            string         `json:"flow_id"`
	Status            string         `json:"status"`
	StartTime         string         `json:"start_time,omitempty"`
	CloseTime         string         `json:"close_time,omitempty"`
	IndexedAttributes map[string]any `json:"indexed_attributes,omitempty"`
}

// readLiveAppRuns reads the application runs or the application log of the
// Project's current Live deployment. Runs resolves the deployment with the
// exact checks of a Studio Live target; every string is redacted with the
// deployment's configured values and the workload grant environment of its
// application container, and no blob is loaded.
func (d *ProjectDiagnostics) readLiveAppRuns(ctx context.Context, identity agentloop.SessionIdentity, arguments string) ([]byte, error) {
	input, err := decodeArguments(toolinput.ReadLiveAppRuns.Decode, arguments)
	if err != nil {
		return nil, err
	}
	switch {
	case input.Operation == generated.ReadLiveAppRunsOperationSearch && !input.FlowType.IsSet():
		return nil, &callFailureError{code: toolfailure.InvalidArguments, message: "search requires flow_type"}
	case input.Operation != generated.ReadLiveAppRunsOperationSearch && input.Operation != generated.ReadLiveAppRunsOperationLogs && !input.FlowID.IsSet():
		return nil, &callFailureError{code: toolfailure.InvalidArguments, message: string(input.Operation) + " requires flow_id"}
	}
	var output []byte
	err = d.Runs.InvokeLive(ctx, identity.ProjectID, func(ctx context.Context, live runs.LiveEngine) error {
		var readErr error
		output, readErr = d.readLive(ctx, live, input)
		return readErr
	})
	if errors.Is(err, runs.ErrLiveNotReady) {
		return nil, &callFailureError{code: toolfailure.LiveNotReady, message: err.Error()}
	}
	return output, err
}

// readLive serves one operation on the resolved Live deployment.
func (d *ProjectDiagnostics) readLive(ctx context.Context, live runs.LiveEngine, input generated.ReadLiveAppRuns) ([]byte, error) {
	application, err := d.Serving.ReadApplication(ctx, live.Live.Deployment)
	if err != nil {
		return nil, applicationFailure(err)
	}
	redaction, err := d.liveRedaction(ctx, live, application)
	if err != nil {
		return nil, err
	}
	switch input.Operation {
	case generated.ReadLiveAppRunsOperationSearch:
		return searchLiveRuns(ctx, live, redaction, input)
	case generated.ReadLiveAppRunsOperationLogs:
		log, err := application.Log(ctx, input.TailLines.Or(100))
		if err != nil {
			return nil, err
		}
		full := redaction.text(log.Log)
		return redaction.fitted(maximumLiveLogBytes, func(logBytes int) any {
			fitted := log
			fitted.Log = comm.TailUTF8(full, logBytes)
			return fitted
		}), nil
	case generated.ReadLiveAppRunsOperationHistory:
		summary, err := readSummary(ctx, live.Flows, input.FlowID.Or(""))
		if err != nil {
			return nil, err
		}
		page, err := readHistory(ctx, live.Flows, redaction, summary, int64(input.AfterEventID.Or(0)), input.PageSize.Or(20))
		if err != nil {
			return nil, err
		}
		return fittedHistory(redaction, page), nil
	default:
		return readLiveRun(ctx, live.Flows, redaction, input.FlowID.Or(""), input.Operation == generated.ReadLiveAppRunsOperationState)
	}
}

// readLiveRun reads the summary or the state of one Live run.
func readLiveRun(ctx context.Context, flows dexpb.FlowServiceClient, redaction redactor, flowID string, withState bool) ([]byte, error) {
	summary, err := readSummary(ctx, flows, flowID)
	if err != nil {
		return nil, err
	}
	if !withState {
		document, readErr := summaryOf(ctx, flows, redaction, summary)
		if readErr != nil {
			return nil, readErr
		}
		return redaction.encode(document), nil
	}
	state, err := readState(ctx, flows, summary)
	if err != nil {
		return nil, err
	}
	document := stateDocument(redaction, summary, state, func(_ string, value any) (any, bool) { return value, true })
	keys := slices.Sorted(maps.Keys(document.Attributes))
	return redaction.fitted(len(keys), func(count int) any {
		fitted := document
		fitted.Attributes = make(map[string]any, count)
		for _, key := range keys[:count] {
			fitted.Attributes[key] = document.Attributes[key]
		}
		fitted.LeftOutAttributes = len(keys) - count
		return fitted
	}), nil
}

// searchLiveRuns reads one page of a Flow type's runs through its indexed
// Attributes, without the Summary RPC, whose output can be a blob.
func searchLiveRuns(ctx context.Context, live runs.LiveEngine, redaction redactor, input generated.ReadLiveAppRuns) ([]byte, error) {
	flowType := input.FlowType.Or("")
	definition, declared := live.Definitions.Definitions[flowType]
	if !declared {
		return nil, &callFailureError{code: toolfailure.InvalidArguments, message: "the deployed application declares the Flow types " + strings.Join(slices.Sorted(maps.Keys(live.Definitions.Definitions)), ", ")}
	}
	filters := make([]fdg.SearchFilter, 0, len(input.Filters))
	for _, item := range input.Filters {
		filter, err := searchFilter(item, definition)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}
	query, err := fdg.CompileQuery(flowType, nil, filters, definition)
	if err != nil {
		return nil, &callFailureError{code: toolfailure.InvalidArguments, message: err.Error()}
	}
	response, err := live.Flows.SearchFlows(ctx, &dexpb.SearchFlowsRequest{Query: fdg.EngineWorkflowSearchQuery(query), PageSize: int32(input.PageSize.Or(20))}) // #nosec G115 -- the schema bounds page_size to 50.
	if err != nil {
		return nil, fmt.Errorf("search the %s runs: %w", flowType, err)
	}
	entries := slices.Clone(response.GetFlowRuns())
	slices.SortStableFunc(entries, func(left, right *dexpb.SearchFlowsResponseEntry) int {
		return right.GetStartTime().AsTime().Compare(left.GetStartTime().AsTime())
	})
	page := liveSearchPage{FlowType: flowType, Runs: []liveRun{}, More: response.GetNextPageToken() != ""}
	for _, entry := range entries {
		if slices.ContainsFunc(page.Runs, func(run liveRun) bool { return run.FlowID == entry.GetFlowId() }) {
			continue
		}
		indexed := make(map[string]any, len(entry.GetIndexedAttributes()))
		for _, value := range entry.GetIndexedAttributes() {
			indexed[value.GetKey()] = dexValue(value.GetValue())
		}
		run := liveRun{FlowID: entry.GetFlowId(), Status: flowStatus(entry.GetFlowStatus()), StartTime: timestamp(entry.GetStartTime()), CloseTime: timestamp(entry.GetCloseTime()), IndexedAttributes: map[string]any{}}
		for _, attribute := range definition.IndexedAttributes {
			if value, found := indexed[attribute.IndexKey]; found {
				run.IndexedAttributes[attribute.AttributeKey] = boundedValue(redaction, value)
			}
		}
		page.Runs = append(page.Runs, run)
	}
	return redaction.fitted(len(page.Runs), func(count int) any {
		fitted := page
		fitted.Runs, fitted.More = page.Runs[:count], page.More || count < len(page.Runs)
		return fitted
	}), nil
}

// searchFilter types a filter's text values by the field they compare.
func searchFilter(item generated.ReadLiveAppRunsFiltersItem, definition fdg.V2Definition) (fdg.SearchFilter, error) {
	var isNumber, isBool bool
	for _, attribute := range definition.IndexedAttributes {
		if attribute.AttributeKey == item.Field {
			isNumber, isBool = attribute.ValueType.IsNumber(), attribute.ValueType.IsBool()
		}
	}
	values := make([]any, 0, len(item.Values))
	for _, text := range item.Values {
		switch {
		case isNumber:
			if _, err := strconv.ParseFloat(text, 64); err != nil {
				return fdg.SearchFilter{}, &callFailureError{code: toolfailure.InvalidArguments, message: "filter " + item.Field + " compares numbers"}
			}
			values = append(values, json.Number(text))
		case isBool:
			if text != "true" && text != "false" {
				return fdg.SearchFilter{}, &callFailureError{code: toolfailure.InvalidArguments, message: "filter " + item.Field + " compares true or false"}
			}
			values = append(values, text == "true")
		default:
			values = append(values, text)
		}
	}
	operator := item.Operator.Or(generated.ReadLiveAppRunsFiltersItemOperatorEq)
	return fdg.SearchFilter{Field: item.Field, Operator: string(operator), Values: comm.Must(json.Marshal(values))}, nil
}

// applicationFailure turns a Live application that changed while it was read
// into the failure the model corrects by reading again.
func applicationFailure(err error) error {
	if errors.Is(err, deployments.ErrApplicationChanged) {
		return &callFailureError{code: toolfailure.LiveNotReady, message: err.Error()}
	}
	return err
}

// liveRedaction replaces every value the Live application can hold: the
// deployment's frozen configuration and the environment its container takes
// from Secrets, the workload grant.
func (d *ProjectDiagnostics) liveRedaction(ctx context.Context, live runs.LiveEngine, application deployments.LiveApplication) (redactor, error) {
	deployment := live.Live.Deployment
	scope := infra.ScopeRef{ProjectID: deployment.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: live.Live.Storage}
	values, err := d.Configuration.ConfiguredValues(ctx, scope, deployment.Inputs.Configuration.Object)
	if err != nil {
		return redactor{}, fmt.Errorf("read the Live configured values: %w", err)
	}
	environment, err := application.Environment(ctx)
	if err != nil {
		return redactor{}, err
	}
	values = append(values, environment...)
	slices.SortStableFunc(values, func(left, right string) int { return len(right) - len(left) })
	redaction := newRedactor(deployment.ProjectID, d.Redactor)
	redaction.output.Values = values
	return redaction, nil
}
