// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// ConnectorContributionFlowClient is the typed boundary of
// ConnectorContributionFlow: the submission of a completed develop
// subagent's contribution, the close of a deleting Project's contributions,
// the retained read and the search of a Project's contributions.
type ConnectorContributionFlowClient struct {
	sdk  *dex.Client
	flow *ConnectorContributionFlow
}

// newConnectorContributionFlowClient binds the client to the shared Dex
// client.
func newConnectorContributionFlowClient(sdk *dex.Client, flow *ConnectorContributionFlow) *ConnectorContributionFlowClient {
	return &ConnectorContributionFlowClient{sdk: sdk, flow: flow}
}

// commandLockWait bounds the lock-conflict retries of a command, whose RPC
// waits for a Step that holds the status lock.
const commandLockWait = 20 * time.Second

// ErrContributionClosed reports a submission for a contribution that ended,
// such as a deleting Project's.
var ErrContributionClosed = errors.New("the connector contribution has ended")

// Submit starts the contribution of submission's connector under its own
// FlowID, which is never reused; the same submission repeated joins the
// execution. A later submission of the same connector, a new head of its
// branch, finds the execution started by another request and updates it
// instead, so a Project contributes each connector once. A contribution that
// ended refuses every submission with ErrContributionClosed. The engine also
// joins a same-request start of an ended execution, which the start cannot
// tell from a new one, so the update RPC confirms every submission: it
// changes nothing for the submission that started the execution and is
// refused once the execution ended.
func (c *ConnectorContributionFlowClient) Submit(ctx context.Context, submission Submission) error {
	requestID := submission.requestID()
	_, err := c.sdk.StartFlow(ctx, c.flow, submission.Identity.flowID(), nil, dex.StartFlowOptions{
		RequestID: &requestID, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: startAttributes(submission),
	})
	var started *dex.FlowAlreadyStartedError
	if err != nil && !errors.As(err, &started) {
		return err
	}
	accepted, err := dexflow.InvokeLocked[bool](ctx, c.sdk, commandLockWait, submission.Identity.flowID(), c.flow.UpdateConnectorContribution, submission)
	var inactive *dex.FlowNotActiveOrNotFoundError
	switch {
	case errors.As(err, &inactive):
		return ErrContributionClosed
	case err != nil:
		return err
	case !accepted:
		return ErrContributionClosed
	}
	return nil
}

// Close asks the contribution to end and answers whether it has ended; one
// whose execution ended has. A repeated close is harmless.
func (c *ConnectorContributionFlowClient) Close(ctx context.Context, id Identity) (bool, error) {
	ended, err := dexflow.InvokeLocked[bool](ctx, c.sdk, commandLockWait, id.flowID(), c.flow.CloseConnectorContribution, nil)
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &inactive) {
		return true, nil
	}
	return ended, err
}

// Snapshot reads the contribution, also an ended one; one that never started
// is *dex.FlowNotFoundError.
func (c *ConnectorContributionFlowClient) Snapshot(ctx context.Context, id Identity) (Snapshot, error) {
	var snapshot Snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.sdk, id.flowID(), c.flow.GetConnectorContributionSnapshot, nil, &snapshot)
	return snapshot, err
}

// maximumSearchPages bounds a search of a Project's contributions: a Session
// contributes to at most agenttasks.MaximumConnectorContributions connectors.
const maximumSearchPages = 5

// ProjectContributions finds the contributions of projectID by their Flow
// type and the indexed ProjectID, those that have not ended when open is
// set, because a contribution is not a Project child. The search is
// eventually consistent: a contribution started a moment ago may be missing.
func (c *ConnectorContributionFlowClient) ProjectContributions(ctx context.Context, projectID string, open bool) ([]Identity, error) {
	query := "FlowType = '" + dex.GetFinalFlowType(c.flow) + "' AND keyword2 = '" + projectID + "'"
	if open {
		query += " AND keyword3 != '" + StatusClosed.String() + "'"
	}
	var found []Identity
	token := ""
	for range maximumSearchPages {
		page, err := c.sdk.SearchFlows(ctx, query, 100, token)
		if err != nil {
			return nil, err
		}
		for _, flow := range page.Flows {
			if slug, isContribution := flowid.ParseConnectorContributionFlowID(projectID, flow.FlowID); isContribution {
				found = append(found, Identity{ProjectID: projectID, Slug: slug})
			}
		}
		if token = page.NextPageToken; token == "" {
			break
		}
	}
	return found, nil
}

// NewConnectorContributionFlowClientForTest is newConnectorContributionFlowClient for the tests.
func NewConnectorContributionFlowClientForTest(sdk *dex.Client, flow *ConnectorContributionFlow) *ConnectorContributionFlowClient {
	return newConnectorContributionFlowClient(sdk, flow)
}
