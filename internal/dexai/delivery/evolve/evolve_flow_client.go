// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// lockWait bounds the retries of a lock conflict of a locked RPC; the review
// Steps that hold the lock make short provider calls.
const lockWait = 10 * time.Second

// ErrNotStarted reports a Project whose EvolveFlow does not run: it never had
// a Live deployment, or it is being deleted.
var ErrNotStarted = errors.New("the Project has no goal measurement")

// EvolveFlowClient is the typed boundary of EvolveFlow.
type EvolveFlowClient struct {
	dex      *dex.Client
	flow     *EvolveFlow
	projects *projects.ProjectFlowClient
}

// Bind connects the client to the server's one Dex client.
func (c *EvolveFlowClient) Bind(sdk *dex.Client) { c.dex = sdk }

// LiveChanged tells the Project's EvolveFlow that a Live deployment
// committed. It first admits the Flow as a child of the Project, which a
// deleting Project refuses, then starts it once and reports the change; a
// repeated or older change changes nothing.
func (c *EvolveFlowClient) LiveChanged(ctx context.Context, projectID string, change LiveRelease) error {
	flowID := flowid.EvolveFlowID(projectID)
	err := c.projects.AdmitChild(ctx, projectID, projects.ChildEvolve, flowID)
	if errors.Is(err, projects.ErrChildAdmissionClosed) {
		return nil
	}
	if err != nil {
		return err
	}
	request := flowid.Fingerprint(struct{ ProjectID string }{projectID})
	if _, err = c.dex.StartFlow(ctx, c.flow, flowID, goalsReading{Revision: change.Revision, Commit: change.BuildCommit}, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		Attributes: initialAttributes(projectID, change),
	}); err != nil {
		return fmt.Errorf("start %s: %w", flowID, err)
	}
	_, err = dexflow.InvokeLocked[bool](ctx, c.dex, lockWait, flowID, c.flow.LiveChanged, change)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		// The Flow completed with its deleting Project.
		return nil
	}
	return err
}

// Get reads the Project's goal measurement, also a completed one; a Project
// that never had a Live deployment has none: ErrNotStarted.
func (c *EvolveFlowClient) Get(ctx context.Context, projectID string) (Snapshot, error) {
	var snapshot Snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.dex, flowid.EvolveFlowID(projectID), c.flow.GetEvolve, nil, &snapshot)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return snapshot, err
}

// StartReview asks for a review now; the admission names the review it
// started, or why it started none. A Project without a running EvolveFlow is
// ErrNotStarted.
func (c *EvolveFlowClient) StartReview(ctx context.Context, projectID string, request ReviewRequest) (ReviewAdmission, error) {
	admission, err := dexflow.InvokeLocked[ReviewAdmission](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.StartReview, request)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return ReviewAdmission{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return admission, err
}

// CleanupProjectEvolve ends the EvolveFlow of a deleting Project and reports
// whether it has ended: the first pass cancels its Steps and a later pass
// finds it completed. A Project whose Flow never started has nothing to end.
func (c *EvolveFlowClient) CleanupProjectEvolve(ctx context.Context, projectID string) (bool, error) {
	_, err := dexflow.InvokeLocked[bool](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.CompleteProjectEvolve, nil)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return true, nil
	}
	return false, err
}
