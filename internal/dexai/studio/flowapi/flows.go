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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	v2DefaultPageSize = 50
	// v2RPCConcurrency bounds the Summary RPCs one search runs at a time.
	v2RPCConcurrency = 8
	v2RPCTimeout     = 5 * time.Second
)

// Catalog is the serving V2 catalog: the contract of every Flow type, the
// definition revision and the target's Start capability.
func (f *FlowAPI) Catalog() Document {
	flowTypes := make([]string, 0, len(f.bundle.Definitions))
	for flowType := range f.bundle.Definitions {
		flowTypes = append(flowTypes, flowType)
	}
	sort.Strings(flowTypes)
	entries := make([]v2CatalogEntry, 0, len(flowTypes))
	for _, flowType := range flowTypes {
		entries = append(entries, v2CatalogEntry{FlowType: flowType, Definition: f.bundle.Definitions[flowType]})
	}
	// Studio Runs resolved the actor, the target and its Worker before it built
	// this adapter, so every served target can start Flows.
	return JSONDocument(http.StatusOK, v2Catalog{
		DefinitionRevision: f.bundle.Revision, Enabled: len(entries) > 0, Flows: entries,
		StartFlow: v2StartCapability{Enabled: true, TargetRevision: f.authorization.TargetRevision},
	})
}

// SearchRequest searches the Flows of one Flow type through its indexed Attributes.
type SearchRequest struct {
	FlowType string
	// WorkQueueOnly limits the result to the Inbox: Flows whose current state
	// awaits one of the declared Action permissions. It filters a view; it
	// does not authorize anything.
	WorkQueueOnly bool
	Filters       []fdg.SearchFilter
	PageSize      int32
	NextPageToken string
}

// Search lists one page of Flows, newest first, each with its Summary when its
// execution is bound to the current definitions.
func (f *FlowAPI) Search(ctx context.Context, request SearchRequest) Document {
	definition, declared := f.bundle.Definitions[request.FlowType]
	if !declared {
		return errorDocument(http.StatusBadRequest, "flowType has no valid Flow Definition Graph 2.0 contract")
	}
	if request.PageSize < 0 {
		return errorDocument(http.StatusBadRequest, "pageSize must be non-negative")
	}
	if request.PageSize == 0 || request.PageSize > v2DefaultPageSize {
		request.PageSize = v2DefaultPageSize
	}
	var workQueuePermissions []string
	if request.WorkQueueOnly {
		workQueuePermissions = fdg.InboxPermissions(f.bundle)
	}
	query, err := fdg.CompileQuery(request.FlowType, workQueuePermissions, request.Filters, definition)
	if err != nil {
		return errorDocument(http.StatusBadRequest, err.Error())
	}
	result, err := f.client.SearchFlows(ctx, &dexpb.SearchFlowsRequest{
		Query: fdg.EngineWorkflowSearchQuery(query), PageSize: request.PageSize, NextPageToken: request.NextPageToken,
	})
	if err != nil {
		return grpcErrorDocument(err, "SearchFlows")
	}
	flowRuns := slices.Clone(result.GetFlowRuns())
	sort.SliceStable(flowRuns, func(left int, right int) bool {
		return flowRuns[left].GetStartTime().AsTime().After(flowRuns[right].GetStartTime().AsTime())
	})
	flows := make([]v2Flow, 0, len(flowRuns))
	seenFlowIDs := make(map[string]bool, len(flowRuns))
	for _, entry := range flowRuns {
		if seenFlowIDs[entry.GetFlowId()] {
			continue
		}
		seenFlowIDs[entry.GetFlowId()] = true
		physicalValues := keyValueMap(entry.GetIndexedAttributes())
		indexedValues := make(map[string]any, len(definition.IndexedAttributes))
		for _, attribute := range definition.IndexedAttributes {
			indexedValues[attribute.AttributeKey] = fdg.ResponseValue(physicalValues[attribute.IndexKey], attribute.ValueType)
		}
		flows = append(flows, v2Flow{
			FlowID: entry.GetFlowId(), FlowType: entry.GetFlowType(),
			FlowStatus: flowStatusLabel(entry.GetFlowStatus()), FlowStatusCode: int32(entry.GetFlowStatus()),
			StartTime: timestamp(entry.GetStartTime()), CloseTime: timestamp(entry.GetCloseTime()),
			IndexedAttributes: indexedValues,
		})
	}
	f.loadSummaries(ctx, definition.Summary, flows)
	return JSONDocument(http.StatusOK, v2SearchResponse{Flows: flows, NextPageToken: result.GetNextPageToken()})
}

// loadSummaries invokes the Summary RPC of each Flow whose execution is bound to
// the current definitions. A Flow that cannot be read keeps a SummaryError;
// these goroutines run outside any recovering handler, so they never panic.
func (f *FlowAPI) loadSummaries(ctx context.Context, view fdg.V2RPCView, flows []v2Flow) {
	semaphore := make(chan struct{}, v2RPCConcurrency)
	var waitGroup sync.WaitGroup
	for index := range flows {
		waitGroup.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			callContext, cancelCall := context.WithTimeout(ctx, v2RPCTimeout)
			defer cancelCall()
			flow := &flows[index]
			flow.DefinitionBinding = definitionUnavailable
			summary, err := f.client.GetFlowSummary(callContext, &dexpb.GetFlowSummaryRequest{FlowId: flow.FlowID})
			if err == nil && summary.GetFlowType() != flow.FlowType {
				err = errors.New("the execution has another Flow type")
			}
			if err != nil {
				flow.IndexedAttributes = map[string]any{}
				flow.SummaryError = "Execution definition could not be verified (" + err.Error() + "); use engine diagnostics"
				return
			}
			flow.DefinitionBinding, flow.DefinitionRevision = startRequestBinding(summary.GetRequestId(), f.bundle.Revision)
			if flow.DefinitionBinding != definitionCurrent {
				flow.IndexedAttributes = map[string]any{}
				flow.SummaryError = executionDefinitionError{}.Error()
				return
			}
			values, err := f.invokeView(callContext, flow.FlowID, view)
			if err != nil {
				flow.SummaryError = err.Error()
				return
			}
			flow.Summary = values
		})
	}
	waitGroup.Wait()
}

// FlowRef names one Flow of a declared Flow type.
type FlowRef struct{ FlowType, FlowID string }

// Display reads a Flow's Display RPC, the Attributes its Actions read, and the
// Actions whose condition the current state meets.
func (f *FlowAPI) Display(ctx context.Context, flow FlowRef) Document {
	definition, declared := f.bundle.Definitions[flow.FlowType]
	if !declared || flow.FlowID == "" {
		return errorDocument(http.StatusBadRequest, "flowType and flowId are required")
	}
	summary, err := f.readCurrentSummary(ctx, flow)
	if err != nil {
		return grpcErrorDocument(err, "GetFlowSummary")
	}
	displayValues, err := f.invokeView(ctx, flow.FlowID, definition.Display)
	if err != nil {
		return grpcErrorDocument(err, definition.Display.RPCName)
	}
	attributeSnapshot := map[string]any{}
	if keys := v2SnapshotKeys(definition); len(keys) > 0 {
		attributes, attributeErr := f.client.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: flow.FlowID, Keys: keys})
		if attributeErr != nil {
			return grpcErrorDocument(attributeErr, "GetAttributes")
		}
		attributeSnapshot = keyValueMap(attributes.GetAttributes())
	}
	eligibleActions := make([]string, 0, len(definition.Actions))
	for _, action := range definition.Actions {
		if actionConditionMatches(action.Condition, attributeSnapshot[action.Condition.AttributeKey]) {
			eligibleActions = append(eligibleActions, action.RPCName)
		}
	}
	return JSONDocument(http.StatusOK, v2DisplayResponse{
		FlowID: flow.FlowID, FlowType: flow.FlowType,
		FlowStatus: flowStatusLabel(summary.GetFlowStatus()), FlowStatusCode: int32(summary.GetFlowStatus()),
		IsActive: summary.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_RUNNING,
		Display:  displayValues, AttributeSnapshot: fdg.ResponseSnapshot(attributeSnapshot),
		EligibleActions: eligibleActions,
	})
}

// EditRequest writes one editable Display field of a running Flow. Value is JSON.
type EditRequest struct {
	FlowRef
	AttributeKey string
	Value        json.RawMessage
}

// EditDisplay writes an editable Display field, with its index and the Action
// permission mappings that read it.
func (f *FlowAPI) EditDisplay(ctx context.Context, request EditRequest) Document {
	var value any
	if err := comm.DecodeStrictJSONNumbers(request.Value, &value); err != nil {
		return errorDocument(http.StatusBadRequest, "value must be one JSON value")
	}
	definition, declared := f.bundle.Definitions[request.FlowType]
	if !declared || request.FlowID == "" || request.AttributeKey == "" {
		return errorDocument(http.StatusBadRequest, "flowType, flowId, and attributeKey are required")
	}
	field, editable := editableDisplayField(definition.Display.Fields, request.AttributeKey)
	if !editable {
		return errorDocument(http.StatusBadRequest, "attribute is not editable")
	}
	if err := f.requireActiveFlow(ctx, request.FlowRef); err != nil {
		return grpcErrorDocument(err, "GetFlowSummary")
	}
	encoded, err := fdg.EncodeValue(value, field.ValueType)
	if err != nil {
		return errorDocument(http.StatusBadRequest, err.Error())
	}
	write := &dexpb.AttributeWrite{Key: request.AttributeKey, Value: encoded}
	if attribute, indexed := findIndexedAttribute(definition, request.AttributeKey); indexed {
		write.IndexConfig = &dexpb.IndexConfig{Enable: true, Type: fdg.IndexType(attribute.IndexType), IndexKey: attribute.IndexKey}
	}
	if _, err = f.client.SetAttributes(ctx, &dexpb.SetAttributesRequest{
		FlowId: request.FlowID, Attributes: []*dexpb.AttributeWrite{write}, RequestId: uuid.NewString(),
		ActionPermissionMappings: actionPermissionMappingsForAttributeWrite(definition, request.AttributeKey),
	}); err != nil {
		return grpcErrorDocument(err, "SetAttributes")
	}
	return JSONDocument(http.StatusOK, map[string]any{"attributeKey": request.AttributeKey, "value": value})
}

// readCurrentSummary reads a Flow of flow's type whose execution is bound to
// the current definitions.
func (f *FlowAPI) readCurrentSummary(ctx context.Context, flow FlowRef) (*dexpb.GetFlowSummaryResponse, error) {
	summary, err := f.client.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flow.FlowID})
	if err != nil {
		return nil, err
	}
	if summary.GetFlowType() != flow.FlowType {
		return nil, status.Error(codes.NotFound, "Flow does not match the requested flowType")
	}
	if err = requireCurrentDefinition(summary.GetRequestId(), f.bundle.Revision); err != nil {
		return nil, err
	}
	return summary, nil
}

// requireActiveFlow admits a write to a running Flow bound to the current definitions.
func (f *FlowAPI) requireActiveFlow(ctx context.Context, flow FlowRef) error {
	summary, err := f.readCurrentSummary(ctx, flow)
	if err != nil {
		return err
	}
	if summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return status.Error(codes.FailedPrecondition, "Flow is not active")
	}
	return nil
}

// invokeView invokes a Summary or Display RPC and checks its output against the view.
func (f *FlowAPI) invokeView(ctx context.Context, flowID string, view fdg.V2RPCView) (map[string]any, error) {
	result, err := f.client.InvokeRPC(ctx, &dexpb.InvokeRPCRequest{
		FlowId: flowID, RpcName: view.RPCName, Input: fdg.NullDexValue(),
		TimeoutSeconds: int32(v2RPCTimeout.Seconds()), RequestId: uuid.NewString(),
	})
	if err != nil {
		return nil, err
	}
	output, err := f.hydrateBlob(ctx, flowID, result.GetOutput())
	if err != nil {
		return nil, err
	}
	values, isObject := dexValue(output).(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("%s returned a non-object value", view.RPCName)
	}
	if err = fdg.ValidateViewOutput(view, values); err != nil {
		return nil, err
	}
	for _, field := range view.Fields {
		values[field.AttributeKey] = fdg.ResponseValue(values[field.AttributeKey], field.ValueType)
	}
	return values, nil
}

// hydrateBlob loads a value that InvokeRPC left behind a blob ID.
func (f *FlowAPI) hydrateBlob(ctx context.Context, flowID string, value *dexpb.Value) (*dexpb.Value, error) {
	var blobID string
	switch kind := value.GetKind().(type) {
	case *dexpb.Value_InternalBlobIdForStringValue:
		blobID = kind.InternalBlobIdForStringValue
	case *dexpb.Value_InternalBlobIdForObjValue:
		blobID = kind.InternalBlobIdForObjValue
	default:
		return value, nil
	}
	result, err := f.client.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{
		Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: flowID, BlobValue: value}},
	})
	if err != nil {
		return nil, err
	}
	hydrated, loaded := result.GetValues()[blobID]
	if !loaded {
		return nil, errors.New("value blob unavailable")
	}
	return hydrated, nil
}

// v2SnapshotKeys are the Attributes the Display Actions read: each condition
// Attribute and each Attribute-sourced input.
func v2SnapshotKeys(definition fdg.V2Definition) []string {
	var keys []string
	add := func(key string) {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, action := range definition.Actions {
		add(action.Condition.AttributeKey)
		for _, field := range action.Input.Fields {
			if field.FromAttribute() {
				add(field.AttributeKey)
			}
		}
	}
	return keys
}

func keyValueMap(values []*dexpb.KV) map[string]any {
	mapped := make(map[string]any, len(values))
	for _, value := range values {
		mapped[value.GetKey()] = dexValue(value.GetValue())
	}
	return mapped
}

func editableDisplayField(fields []fdg.V2ViewField, attributeKey string) (fdg.V2ViewField, bool) {
	for _, field := range fields {
		if field.AttributeKey == attributeKey && field.Editable {
			return field, true
		}
	}
	return fdg.V2ViewField{}, false
}

func findIndexedAttribute(definition fdg.V2Definition, attributeKey string) (fdg.V2IndexedAttribute, bool) {
	for _, attribute := range definition.IndexedAttributes {
		if attribute.AttributeKey == attributeKey {
			return attribute, true
		}
	}
	return fdg.V2IndexedAttribute{}, false
}

type v2Catalog struct {
	DefinitionRevision string            `json:"definitionRevision"`
	Enabled            bool              `json:"enabled"`
	Flows              []v2CatalogEntry  `json:"flows"`
	StartFlow          v2StartCapability `json:"startFlow"`
}

type v2CatalogEntry struct {
	FlowType   string           `json:"flowType"`
	Definition fdg.V2Definition `json:"definition"`
}

type v2StartCapability struct {
	Enabled        bool   `json:"enabled"`
	TargetRevision string `json:"targetRevision,omitempty"`
}

type v2SearchResponse struct {
	Flows         []v2Flow `json:"flows"`
	NextPageToken string   `json:"nextPageToken"`
}

type v2Flow struct {
	DefinitionBinding  string         `json:"definitionBinding"`
	DefinitionRevision string         `json:"definitionRevision,omitempty"`
	FlowID             string         `json:"flowId"`
	FlowType           string         `json:"flowType"`
	FlowStatus         string         `json:"flowStatus"`
	FlowStatusCode     int32          `json:"flowStatusCode"`
	StartTime          *string        `json:"startTime"`
	CloseTime          *string        `json:"closeTime"`
	IndexedAttributes  map[string]any `json:"indexedAttributes"`
	Summary            map[string]any `json:"summary,omitempty"`
	SummaryError       string         `json:"summaryError,omitempty"`
}

type v2DisplayResponse struct {
	FlowID            string         `json:"flowId"`
	FlowType          string         `json:"flowType"`
	FlowStatus        string         `json:"flowStatus"`
	FlowStatusCode    int32          `json:"flowStatusCode"`
	IsActive          bool           `json:"isActive"`
	Display           map[string]any `json:"display"`
	AttributeSnapshot map[string]any `json:"attributeSnapshot"`
	EligibleActions   []string       `json:"eligibleActions"`
}
