// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// SubagentFlowClient is the typed boundary of SubagentFlow: the start with the
// full fingerprint RequestID, the stop, the retained read, the search of a
// Project's subagents for its cleanup and the best-effort activity of one
// subagent.
type SubagentFlowClient struct {
	sdk  *dex.Client
	flow *SubagentFlow
}

// newSubagentFlowClient binds the client to the shared Dex client.
func newSubagentFlowClient(sdk *dex.Client, flow *SubagentFlow) *SubagentFlowClient {
	return &SubagentFlowClient{sdk: sdk, flow: flow}
}

// Start idempotently starts the subagent of start under its own FlowID, which
// is never reused: a repeated start with the same request joins the
// execution, also a completed one, so a subagent never runs twice. A retry
// that carries a fresher lease joins too; the execution keeps its first
// input. The subagent has no Flow timeout: its duration limit is its own
// business timer.
func (c *SubagentFlowClient) Start(ctx context.Context, start agenttasks.SubagentStart) error {
	requestID := start.RequestID()
	_, err := c.sdk.StartFlow(ctx, c.flow, start.Identity.FlowID(), nil, dex.StartFlowOptions{
		RequestID: &requestID, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: startAttributes(start),
	})
	return err
}

// stopLockWait bounds the lock-conflict retries of a stop, whose RPC waits
// for a lifecycle Step that holds the status lock.
const stopLockWait = 10 * time.Second

// Stop asks the subagent to stop and answers whether it has ended. A
// subagent that never started, and one whose execution ended, has ended; a
// repeated stop is harmless.
func (c *SubagentFlowClient) Stop(ctx context.Context, id agenttasks.SubagentIdentity) (bool, error) {
	answer, err := dexflow.InvokeLocked[stopAnswer](ctx, c.sdk, stopLockWait, id.FlowID(), c.flow.StopSubagent, nil)
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &inactive) {
		return true, nil
	}
	return answer.Ended, err
}

// SnapshotForTest reads the subagent, also a completed one; one that never started
// is *dex.FlowNotFoundError.
func (c *SubagentFlowClient) SnapshotForTest(ctx context.Context, id agenttasks.SubagentIdentity) (subagentSnapshot, error) {
	var snapshot subagentSnapshot
	err := dexflow.InvokeRetainedRead(ctx, c.sdk, id.FlowID(), c.flow.GetSubagentSnapshot, nil, &snapshot)
	return snapshot, err
}

// maximumSearchPages bounds the search of a Project's subagents: a Session
// runs at most three at once, and a deletion stops the ones it found in its
// next pass too.
const maximumSearchPages = 20

// SessionSubagents finds the subagents of the Session of projectID that have
// not ended, by their Flow type, the indexed ProjectID and the indexed
// status, because a subagent is not a Project child; a deleting Project stops
// each of them. Discovery is eventually consistent, so the deletion asks again
// in its next pass, and a subagent it misses is denied by the closed Session
// at its admission. The query's values are a validated ID and declared
// statuses.
func (c *SubagentFlowClient) SessionSubagents(ctx context.Context, projectID string) ([]agenttasks.SubagentIdentity, error) {
	statuses := make([]string, 0, len(flowStatuses))
	for _, status := range flowStatuses {
		if !status.ended() {
			statuses = append(statuses, "'"+status.String()+"'")
		}
	}
	query := "FlowType = '" + dex.GetFinalFlowType(c.flow) + "' AND keyword2 = '" + projectID + "' AND keyword3 IN (" + strings.Join(statuses, ", ") + ")"
	var subagents []agenttasks.SubagentIdentity
	token := ""
	for range maximumSearchPages {
		page, err := c.sdk.SearchFlows(ctx, query, 100, token)
		if err != nil {
			return nil, err
		}
		for _, found := range page.Flows {
			if agentID, isSubagent := flowid.ParseSubagentFlowID(projectID, found.FlowID); isSubagent {
				subagents = append(subagents, agenttasks.SubagentIdentity{ProjectID: projectID, AgentID: agentID})
			}
		}
		if token = page.NextPageToken; token == "" {
			break
		}
	}
	return subagents, nil
}

// Activity is one best-effort activity event of a subagent, with the token
// that resumes its Stream after it.
type Activity struct {
	Event       conversation.AgentEvent
	ResumeToken string
	CreatedAt   time.Time
	Source      string
}

// ReadActivity long-polls the subagent's AgentActivity Stream after the
// token; its Session's Watch relays it as the subagent's live status line.
func (c *SubagentFlowClient) ReadActivity(ctx context.Context, id agenttasks.SubagentIdentity, resumeToken string) (Activity, error) {
	var activity Activity
	message, err := c.sdk.ReadStream(ctx, id.FlowID(), agentActivityStream, resumeToken, &activity.Event)
	if err != nil {
		return Activity{}, err
	}
	activity.ResumeToken, activity.CreatedAt, activity.Source = message.ResumeToken, message.CreatedTime, message.Source
	return activity, nil
}

// LatestActivityToken is the token of the newest retained activity event of
// the subagent, or "" when its Stream retains none.
func (c *SubagentFlowClient) LatestActivityToken(ctx context.Context, id agenttasks.SubagentIdentity) (string, error) {
	var page dex.StreamMessagesPage[conversation.AgentEvent]
	if err := c.sdk.ListStreamMessages(ctx, id.FlowID(), agentActivityStream, 1, "", &page); err != nil || len(page.Messages) == 0 {
		return "", err
	}
	return page.Messages[0].ResumeToken, nil
}

// NewSubagentFlowClientForTest is newSubagentFlowClient for the tests.
func NewSubagentFlowClientForTest(sdk *dex.Client, flow *SubagentFlow) *SubagentFlowClient {
	return newSubagentFlowClient(sdk, flow)
}
