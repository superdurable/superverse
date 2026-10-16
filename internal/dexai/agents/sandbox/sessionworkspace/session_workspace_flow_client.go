// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// flowID addresses the SessionWorkspaceFlow of a Project, for example
// session-workspace-a7k2.
func flowID(project string) string { return flowid.SessionWorkspaceFlowID(project) }

// startIdentity is the Start RequestID of the Flow of projectID: the
// fingerprint of its complete logical input, the Project's Session, so every
// start of the Project joins its one execution.
func startIdentity(projectID string) string {
	return flowid.Fingerprint(struct{ SessionFlowID string }{flowid.SessionFlowID(projectID)})
}

// ErrWorkspaceClosed reports a command for a Flow that ended or is closing
// for the Project's deletion: no instance is provisioned any more.
var ErrWorkspaceClosed = errors.New("the Session workspace is closed")

// The client's call bounds: a command RPC retries a lock conflict with
// another command within commandLockWait; the engine status read that joins
// the deletion's End is bounded by executionReadTimeout.
const (
	commandLockWait      = 10 * time.Second
	executionReadTimeout = 5 * time.Second
)

// SessionWorkspaceFlowClient is the typed boundary of SessionWorkspaceFlow:
// the Session's commands, the provisioning's report, the retained read and
// the end at the Project's deletion.
type SessionWorkspaceFlowClient struct {
	sdk       *dex.Client
	flow      *SessionWorkspaceFlow
	execution dexpb.FlowServiceClient
}

// newSessionWorkspaceFlowClient binds the client to the shared Dex client and
// the engine status reader.
func newSessionWorkspaceFlowClient(sdk *dex.Client, flow *SessionWorkspaceFlow, execution dexpb.FlowServiceClient) *SessionWorkspaceFlowClient {
	return &SessionWorkspaceFlowClient{sdk: sdk, flow: flow, execution: execution}
}

// Submit starts the Project's one execution, which its FlowID never reuses,
// and submits the command: true when admitted or a repeat, false for a
// command of an older request. The engine also joins a same-request start of
// an ended execution, so the command RPC confirms every submission; one that
// ended or is closing refuses it with ErrWorkspaceClosed.
func (c *SessionWorkspaceFlowClient) Submit(ctx context.Context, projectID string, command WorkspaceCommand) (bool, error) {
	request := startIdentity(projectID)
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, projectIDAttribute, projectID)
	dexflow.Add(attributes, InstanceAttribute, Instance{})
	_, err := c.sdk.StartFlow(ctx, c.flow, flowID(projectID), nil, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: attributes.Values(),
	})
	if err != nil {
		return false, fmt.Errorf("start the Session workspace of %s: %w", projectID, err)
	}
	answer, err := dexflow.InvokeLocked[commandAnswer](ctx, c.sdk, commandLockWait, flowID(projectID), c.flow.SubmitWorkspaceCommand, command)
	switch {
	case isMissingOrClosed(err):
		return false, fmt.Errorf("%w: %w", ErrWorkspaceClosed, err)
	case err == nil && answer.Closed:
		return false, ErrWorkspaceClosed
	}
	return answer.Accepted, err
}

// ReportProvisionOutcome tells the Flow that the provisioning of requestID
// decided its outcome: true when it waits for that instance or the report
// repeats, false for another instance or a Flow that ended or never started,
// which the provisioning only records.
func (c *SessionWorkspaceFlowClient) ReportProvisionOutcome(ctx context.Context, projectID, requestID string) (bool, error) {
	admitted, err := dexflow.InvokeLocked[bool](ctx, c.sdk, commandLockWait, flowID(projectID), c.flow.ReportProvisionOutcome, ProvisionReport{WorkspaceRequestID: requestID})
	if isMissingOrClosed(err) {
		return false, nil
	}
	return admitted, err
}

// Get reads the Flow, also a completed one; one that never started is
// *dex.FlowNotFoundError.
func (c *SessionWorkspaceFlowClient) Get(ctx context.Context, projectID string) (Snapshot, error) {
	var snapshot Snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.sdk, flowID(projectID), c.flow.GetSessionWorkspace, nil, &snapshot)
	return snapshot, err
}

// End ends the Flow of a deleting Project and reports whether its execution
// no longer runs: it never started, it completed or its engine execution
// stopped. A call that finds it running cancels its provisioning or
// reclamation, and a later call finds it ended. It returns the instance the
// Flow recorded, whose provisioning may still run.
func (c *SessionWorkspaceFlowClient) End(ctx context.Context, projectID string) (Snapshot, bool, error) {
	snapshot, err := c.Get(ctx, projectID)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return Snapshot{ProjectID: projectID}, true, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	readCtx, cancel := context.WithTimeout(ctx, executionReadTimeout)
	defer cancel()
	summary, err := c.execution.GetFlowSummary(readCtx, &dexpb.GetFlowSummaryRequest{FlowId: flowID(projectID)})
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("read the engine status of %s: %w", flowID(projectID), err)
	}
	if summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return snapshot, true, nil
	}
	_, err = dexflow.InvokeLocked[bool](ctx, c.sdk, commandLockWait, flowID(projectID), c.flow.CompleteSessionWorkspace, nil)
	if isMissingOrClosed(err) {
		// It ended between the reads.
		return snapshot, true, nil
	}
	return snapshot, false, err
}

// isMissingOrClosed classifies the typed outcomes of an active-required RPC
// whose target execution does not exist or has completed.
func isMissingOrClosed(err error) bool {
	var inactive *dex.FlowNotActiveOrNotFoundError
	var missing *dex.FlowNotFoundError
	return errors.As(err, &inactive) || errors.As(err, &missing)
}

// FlowIDForTest is flowID for the tests.
func FlowIDForTest(project string) string {
	return flowID(project)
}

// NewSessionWorkspaceFlowClientForTest is newSessionWorkspaceFlowClient for the tests.
func NewSessionWorkspaceFlowClientForTest(sdk *dex.Client, flow *SessionWorkspaceFlow, execution dexpb.FlowServiceClient) *SessionWorkspaceFlowClient {
	return newSessionWorkspaceFlowClient(sdk, flow, execution)
}
