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
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// workerHealthCheckTimeout bounds the Worker check, which includes resource
// ownership verification and a Native forwarding lease.
const workerHealthCheckTimeout = 15 * time.Second

// StartRequest starts one Flow on the serving target's Worker.
type StartRequest struct {
	FlowType, FlowID string
	// RequestID is the caller's operation UUID; every retry of one Start reuses it.
	RequestID string
	// TargetRevision is the serving target the caller observed in the catalog.
	TargetRevision string
	// Input is the Start Step's JSON input.
	Input json.RawMessage
}

// Start admits a Flow on the serving target's Worker and returns its FlowID
// without reading the Flow back or exposing a RunID. A retry of the same
// operation is the same Dex request; RecoverStart resolves a lost response.
func (f *FlowAPI) Start(ctx context.Context, request StartRequest) Document {
	admission, rejection, admitted := f.admitStart(request)
	if !admitted {
		return rejection
	}
	checkContext, cancelCheck := context.WithTimeout(ctx, workerHealthCheckTimeout)
	defer cancelCheck()
	if err := f.checkWorker(checkContext); err != nil {
		return CodedErrorDocument(http.StatusPreconditionFailed, "WORKER_UNHEALTHY", "The selected Worker is unavailable ("+err.Error()+"); check the deployment status")
	}
	if _, err := f.client.StartFlow(ctx, &dexpb.StartFlowRequest{
		FlowId: request.FlowID, FlowType: request.FlowType, StartStepType: admission.start.StepType,
		StepInput: admission.stepInput, RequestId: admission.requestID,
		StepOptions: &dexpb.StepOptions{SkipWaitFor: admission.start.SkipWaitFor},
		FlowStartOptions: &dexpb.FlowStartOptions{
			IdReusePolicy:             dexpb.IdReusePolicy_ID_REUSE_POLICY_DISALLOW_REUSE,
			FlowAlreadyStartedOptions: &dexpb.FlowAlreadyStartedOptions{IgnoreAlreadyStartedError: true},
			FlowConfigOverride:        &dexpb.FlowConfig{WorkerTarget: &dexpb.WorkerTarget{Address: f.authorization.WorkerTarget, IsHeadlessAddress: f.authorization.WorkerHeadless}},
		},
	}); err != nil {
		return grpcErrorDocument(err, "StartFlow")
	}
	return JSONDocument(http.StatusOK, v2StartResponse{FlowID: request.FlowID})
}

// RecoverStart reports whether an earlier Start of the same operation admitted
// its Flow, by comparing the Flow's Dex RequestID with the operation's.
func (f *FlowAPI) RecoverStart(ctx context.Context, request StartRequest) Document {
	admission, rejection, admitted := f.admitStart(request)
	if !admitted {
		return rejection
	}
	summary, err := f.client.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: request.FlowID})
	if status.Code(err) == codes.NotFound {
		return CodedErrorDocument(http.StatusNotFound, "START_NOT_FOUND", "This start has not been observed; retry the same operation")
	}
	if err != nil {
		return grpcErrorDocument(err, "GetFlowSummary")
	}
	if summary.GetRequestId() != admission.requestID || summary.GetFlowType() != request.FlowType {
		return CodedErrorDocument(http.StatusConflict, "START_IDENTITY_CONFLICT", "The Flow ID belongs to another operation")
	}
	return JSONDocument(http.StatusOK, v2StartResponse{FlowID: request.FlowID})
}

type v2StartResponse struct {
	FlowID string `json:"flowId"`
}

type startAdmission struct {
	start     *fdg.V2StartDefinition
	stepInput *dexpb.Value
	requestID string
}

// admitStart validates a Start or recovery request and derives its Dex
// RequestID; a rejected request returns its error document.
func (f *FlowAPI) admitStart(request StartRequest) (startAdmission, Document, bool) {
	if request.TargetRevision != f.authorization.TargetRevision {
		return startAdmission{}, CodedErrorDocument(http.StatusConflict, "START_TARGET_CHANGED", "The selected start target has changed; reload Runs"), false
	}
	if _, err := uuid.Parse(request.RequestID); err != nil {
		return startAdmission{}, errorDocument(http.StatusBadRequest, "requestId must be a UUID"), false
	}
	if err := validateFlowID(request.FlowID); err != nil {
		return startAdmission{}, errorDocument(http.StatusBadRequest, err.Error()), false
	}
	definition, declared := f.bundle.Definitions[request.FlowType]
	if !declared || definition.Start == nil {
		return startAdmission{}, errorDocument(http.StatusBadRequest, "Flow type has no supported Start definition"), false
	}
	stepInput, input, err := fdg.StartStepInput(definition.Start.Input, request.Input)
	if err != nil {
		return startAdmission{}, errorDocument(http.StatusBadRequest, err.Error()), false
	}
	return startAdmission{start: definition.Start, stepInput: stepInput, requestID: f.startRequestID(request, input)}, Document{}, true
}

// startRequestID is the Dex RequestID of a Start: the logical fingerprint of
// the whole operation, bound to the definition revision. Dex persists it in
// every started execution's request memo, so the fingerprint label and the
// order of its fields never change.
func (f *FlowAPI) startRequestID(request StartRequest, input any) string {
	authorization := f.authorization
	return boundStartRequestID(f.bundle.Revision, flowid.Fingerprint([]any{
		"dex-web-hosted-start-v1", authorization.ActorID, authorization.TargetRevision, authorization.WorkerTarget,
		f.bundle.Revision, request.FlowType, request.FlowID, request.RequestID, input,
	}))
}

func validateFlowID(flowID string) error {
	if strings.TrimSpace(flowID) == "" {
		return errors.New("flow ID is required")
	}
	if strings.ContainsAny(flowID, "/$:") {
		return errors.New("flow ID contains a reserved character")
	}
	return nil
}
