// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// FlowID addresses the PreviewFlow of a Project.
func FlowID(project string) string { return flowid.PreviewFlowID(project) }

// executionReadTimeout bounds the engine status read that joins a snapshot.
const executionReadTimeout = 5 * time.Second

// lockWait bounds the retries of a lock conflict of a locked RPC; the polling
// rounds that send commands outlast it.
const lockWait = 10 * time.Second

// PreviewFlowClient is the typed boundary of PreviewFlow.
type PreviewFlowClient struct {
	Dex  *dex.Client
	Flow *PreviewFlow
	// Execution reads the engine status of one execution by its stable
	// FlowID: diagnostic engine state, never a substitute for the Preview
	// snapshot, and never a RunID for callers.
	Execution dexpb.FlowServiceClient
}

// Start idempotently starts the Preview of the Project id, on its first
// command. Its FlowID is never reused: a retry joins the execution, also a
// completed one.
func (c *PreviewFlowClient) Start(ctx context.Context, id sandbox.Identity) error {
	attributes, err := initialAttributes(id, c.Flow.Names)
	if err != nil {
		return err
	}
	request := flowid.Fingerprint(struct{ ProjectID string }{id.ProjectID})
	_, err = c.Dex.StartFlow(ctx, c.Flow, FlowID(id.ProjectID), nil, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: attributes,
	})
	return err
}

// Get reads the Preview, also a completed one; a Project that never ran a
// Preview command has none, which is *dex.FlowNotFoundError. While a command is active, Get joins a
// bounded engine status read of exactly this execution, and one that failed,
// was canceled or was terminated reads with the failure
// PREVIEW_EXECUTION_STOPPED. An unavailable engine read fails; it never
// manufactures a running or a stopped Preview.
func (c *PreviewFlowClient) Get(ctx context.Context, project string) (Snapshot, error) {
	var s Snapshot
	flowID := FlowID(project)
	if err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowID, c.Flow.GetPreview, nil, &s); err != nil || s.ActiveCommand.ID == "" {
		return s, err
	}
	stopped, err := c.isExecutionStopped(ctx, flowID)
	if err != nil {
		return Snapshot{}, err
	}
	if stopped {
		s.Failure, s.FailureOwner = newFailure(failureExecutionStopped, s.ActiveCommand.Kind, ""), conversation.DeliveryOwnerPlatform
	}
	return s, nil
}

// isExecutionStopped reports whether the engine execution of flowID ended by
// failure, cancellation or termination. A completed execution is not stopped:
// its Project ended it.
func (c *PreviewFlowClient) isExecutionStopped(ctx context.Context, flowID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, executionReadTimeout)
	defer cancel()
	summary, err := c.Execution.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		return false, fmt.Errorf("read the engine status of %s: %w", flowID, err)
	}
	if summary.GetFlowExecutionId().GetFlowId() != flowID || summary.GetFlowType() != dex.GetFinalFlowType(c.Flow) {
		return false, errors.New("preview execution scope mismatch")
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

// Command admits command on the Preview.
func (c *PreviewFlowClient) Command(ctx context.Context, project string, command Command) (Snapshot, error) {
	return commandOutcome(dexflow.InvokeLocked[commandResult](ctx, c.Dex, lockWait, FlowID(project), c.Flow.RequestPreviewCommand, command))
}

// RequestDeployment asks the Preview to deploy the pushed commit of request
// and returns its answer. A Preview that ended with its Project, or never
// started, is ErrClosed.
func (c *PreviewFlowClient) RequestDeployment(ctx context.Context, project string, request DeploymentRequest) (DeploymentAnswer, error) {
	answer, err := dexflow.InvokeLocked[DeploymentAnswer](ctx, c.Dex, lockWait, FlowID(project), c.Flow.RequestPreviewDeployment, request)
	if isMissingOrClosed(err) {
		return DeploymentAnswer{}, fmt.Errorf("%w: %w", ErrClosed, err)
	}
	return answer, err
}

// ReadDefinitions reads the Preview and the definitions of its READY preparation.
func (c *PreviewFlowClient) ReadDefinitions(ctx context.Context, project string) (Snapshot, []appmanifest.FlowDefinition, error) {
	state, err := c.Get(ctx, project)
	if err != nil {
		return state, nil, err
	}
	definitions, err := c.Flow.Preview.readPreviewDefinitions(ctx, state)
	return state, definitions, err
}

// ReadPreparedSource reads the AppManifest the Project's Preview prepared for
// commit from storage: it makes no Flow call and needs no Preview execution.
// A commit the Preview never prepared is ErrSourceNotPrepared.
func (c *PreviewFlowClient) ReadPreparedSource(ctx context.Context, project, commit string) (*pb.AppManifestSnapshot, error) {
	return c.Flow.Preview.readPreparedSource(ctx, project, commit)
}

// NotifyWorkspaceSettled announces to the Preview that the Session's
// workspace request it may wait for settled. A Project without a Preview, or
// whose Preview ended, has nothing to announce to.
func (c *PreviewFlowClient) NotifyWorkspaceSettled(ctx context.Context, project string, settled WorkspaceSettlement) error {
	_, err := dexflow.InvokeLocked[bool](ctx, c.Dex, lockWait, FlowID(project), c.Flow.NotifyWorkspaceSettled, settled)
	if isMissingOrClosed(err) {
		return nil
	}
	return err
}

// End ends the Preview of a deleting Project and reports whether its execution
// no longer runs: it never started, it completed, or its engine execution
// stopped. A call that finds it running cancels its branches, and a later
// call finds it ended.
func (c *PreviewFlowClient) End(ctx context.Context, project string) (bool, error) {
	flowID := FlowID(project)
	var s Snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowID, c.Flow.GetPreview, nil, &s)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	readCtx, cancel := context.WithTimeout(ctx, executionReadTimeout)
	defer cancel()
	summary, err := c.Execution.GetFlowSummary(readCtx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		return false, fmt.Errorf("read the engine status of %s: %w", flowID, err)
	}
	if summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return true, nil
	}
	_, err = dexflow.InvokeLocked[bool](ctx, c.Dex, lockWait, flowID, c.Flow.EndPreview, nil)
	if isMissingOrClosed(err) {
		// It ended between the reads.
		return true, nil
	}
	return false, err
}

// isMissingOrClosed classifies the typed outcomes of an active-required RPC
// whose target execution does not exist or has completed.
func isMissingOrClosed(err error) bool {
	var inactive *dex.FlowNotActiveOrNotFoundError
	var missing *dex.FlowNotFoundError
	return errors.As(err, &inactive) || errors.As(err, &missing)
}

// commandOutcome turns the answer of a locked command RPC into the matching
// error.
func commandOutcome(out commandResult, err error) (Snapshot, error) {
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &inactive) {
		// A locked RPC needs a running execution: the Preview has completed,
		// or none exists. Both typed classifications stay matchable.
		return Snapshot{}, fmt.Errorf("%w: %w", ErrClosed, err)
	}
	if err != nil {
		return Snapshot{}, err
	}
	switch out.Rejection {
	case "":
		return out.Snapshot, nil
	case rejectionBusy:
		return out.Snapshot, ErrBusy
	case rejectionNotHeld:
		return out.Snapshot, ErrNotHeld
	default:
		return out.Snapshot, ErrConflict
	}
}
