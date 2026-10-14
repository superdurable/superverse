// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
)

// issueFlowID addresses the PlatformIssueFlow of signature in environment, for
// example platform-issue-test-3f9a1c2b7d4e5f60.
func issueFlowID(environment deploymentnames.Environment, signature string) string {
	return flowid.PlatformIssueFlowID(environment.String(), signature)
}

// PlatformIssueFlowClient is the typed boundary of PlatformIssueFlow in the
// server's environment.
type PlatformIssueFlowClient struct {
	dex         *dex.Client
	flow        *PlatformIssueFlow
	environment deploymentnames.Environment
}

// newPlatformIssueFlowClient binds the client to the shared Dex client and
// the server's environment, which every report names.
func newPlatformIssueFlowClient(sdk *dex.Client, flow *PlatformIssueFlow, environment deploymentnames.Environment) *PlatformIssueFlowClient {
	return &PlatformIssueFlowClient{dex: sdk, flow: flow, environment: environment}
}

// Report is how one report reached the Flow of its signature: its FlowID,
// and whether the Flow already ran for another report, which it joined.
type Report struct {
	FlowID string
	Joined bool
}

// Report starts the PlatformIssueFlow of the incident's signature in the
// client's environment. The FlowID is reused once no run is open: a retry of
// the same report joins a running execution, another report of the signature
// while one runs is joined to it, and a report after the run completed starts
// a new run, whose label query finds the issue the earlier one opened.
func (c *PlatformIssueFlowClient) Report(ctx context.Context, incident Incident) (Report, error) {
	incident.Environment = c.environment
	flowID, request := incident.flowID(), incident.requestIdentity()
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, IncidentAttribute, incident)
	_, err := c.dex.StartFlow(ctx, c.flow, flowID, nil, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseAllowIfNotRunning, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: attributes.Values(),
	})
	var running *dex.FlowAlreadyStartedError
	switch {
	case errors.As(err, &running):
		return Report{FlowID: flowID, Joined: true}, nil
	case err != nil:
		return Report{}, fmt.Errorf("start the report %s: %w", flowID, err)
	}
	return Report{FlowID: flowID}, nil
}

// GetForTest reads the latest run of the signature's Flow in the client's
// environment, also a completed one; a signature never reported is
// *dex.FlowNotFoundError.
func (c *PlatformIssueFlowClient) GetForTest(ctx context.Context, signature string) (snapshot, error) {
	var s snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.dex, issueFlowID(c.environment, signature), c.flow.GetPlatformIssue, nil, &s)
	return s, err
}

// FlowIDForTest is issueFlowID for the tests.
func FlowIDForTest(environment deploymentnames.Environment, signature string) string {
	return issueFlowID(environment, signature)
}

// NewPlatformIssueFlowClientForTest is newPlatformIssueFlowClient for the tests.
func NewPlatformIssueFlowClientForTest(sdk *dex.Client, flow *PlatformIssueFlow, environment deploymentnames.Environment) *PlatformIssueFlowClient {
	return newPlatformIssueFlowClient(sdk, flow, environment)
}
