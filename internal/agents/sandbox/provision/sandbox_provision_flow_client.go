// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
)

// provisionFlowID addresses the provisioning of the instance requestID of a Project's
// Session.
func provisionFlowID(project, requestID string) string {
	return flowid.SandboxProvisionFlowID(project, requestID)
}

// executionReadTimeout bounds the engine status read that joins a snapshot.
const executionReadTimeout = 5 * time.Second

// SandboxProvisionFlowClient is the typed boundary of SandboxProvisionFlow.
type SandboxProvisionFlowClient struct {
	dex  *dex.Client
	flow *SandboxProvisionFlow
	// execution reads the engine status of one execution by its stable
	// FlowID: diagnostic engine state, never a substitute for the
	// provisioning snapshot, and never a RunID for callers.
	execution dexpb.FlowServiceClient
	names     deploymentnames.Names
}

// newSandboxProvisionFlowClient binds the client to the shared Dex client and
// the engine status reader.
func newSandboxProvisionFlowClient(sdk *dex.Client, flow *SandboxProvisionFlow, execution dexpb.FlowServiceClient, names deploymentnames.Names) *SandboxProvisionFlowClient {
	return &SandboxProvisionFlowClient{dex: sdk, flow: flow, execution: execution, names: names}
}

// Start idempotently starts the provisioning of in. Every instance has its own
// FlowID, which is never reused: a retry of the same request joins its
// execution, also a completed one, and never provisions again.
func (c *SandboxProvisionFlowClient) Start(ctx context.Context, in Request) error {
	attributes, err := initialAttributes(in, c.names)
	if err != nil {
		return err
	}
	request := in.requestIdentity()
	_, err = c.dex.StartFlow(ctx, c.flow, provisionFlowID(in.Identity.ProjectID, in.WorkspaceRequestID), nil, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: attributes,
	})
	return err
}

// Get reads the provisioning of requestID, also a completed one; one that
// never started is *dex.FlowNotFoundError. A provisioning without an outcome
// joins a bounded engine status read of exactly its execution, and one that
// failed, was canceled or was terminated reads as failed
// WORKSPACE_EXECUTION_STOPPED with the resources it recorded. An unavailable
// engine read fails; it never manufactures an outcome.
func (c *SandboxProvisionFlowClient) Get(ctx context.Context, project, requestID string) (Snapshot, error) {
	var s Snapshot
	flowID := provisionFlowID(project, requestID)
	if err := dexflow.InvokeRetainedRead(ctx, c.dex, flowID, c.flow.GetProvision, nil, &s); err != nil || s.Outcome != nil {
		return s, err
	}
	stopped, err := c.isExecutionStopped(ctx, flowID)
	if err != nil {
		return Snapshot{}, err
	}
	if stopped {
		outcome := s.failed(failureExecutionStopped.failure(""))
		s.Outcome = &outcome
	}
	return s, nil
}

// isExecutionStopped reports whether the engine execution of flowID ended by
// failure, cancellation or termination.
func (c *SandboxProvisionFlowClient) isExecutionStopped(ctx context.Context, flowID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, executionReadTimeout)
	defer cancel()
	summary, err := c.execution.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		return false, fmt.Errorf("read the engine status of %s: %w", flowID, err)
	}
	if summary.GetFlowExecutionId().GetFlowId() != flowID || summary.GetFlowType() != dex.GetFinalFlowType(c.flow) {
		return false, errors.New("sandbox provisioning execution scope mismatch")
	}
	switch summary.GetFlowStatus() {
	case dexpb.FlowStatus_FLOW_STATUS_RUNNING, dexpb.FlowStatus_FLOW_STATUS_COMPLETED:
		return false, nil
	case dexpb.FlowStatus_FLOW_STATUS_FAILED, dexpb.FlowStatus_FLOW_STATUS_CANCELED, dexpb.FlowStatus_FLOW_STATUS_TERMINATED, dexpb.FlowStatus_FLOW_STATUS_SERVER_SIDE_TIMEOUT_INTERNAL_ONLY:
		return true, nil
	default:
		return false, fmt.Errorf("the engine status of %s is unavailable", flowID)
	}
}

// FlowIDForTest is provisionFlowID for the tests.
func FlowIDForTest(project string, requestID string) string {
	return provisionFlowID(project, requestID)
}

// NewSandboxProvisionFlowClientForTest is newSandboxProvisionFlowClient for the tests.
func NewSandboxProvisionFlowClientForTest(sdk *dex.Client, flow *SandboxProvisionFlow, execution dexpb.FlowServiceClient, names deploymentnames.Names) *SandboxProvisionFlowClient {
	return newSandboxProvisionFlowClient(sdk, flow, execution, names)
}
