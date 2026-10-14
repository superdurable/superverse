// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repobinding

import (
	"context"
	"errors"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// RepositoryBindingFlowClient fronts RepositoryBindingFlow, the owner of which
// Project a managed repository is bound to.
type RepositoryBindingFlowClient struct {
	Dex  *dex.Client
	Flow *RepositoryBindingFlow
}

// Find returns the repository's binding, or nil when it was never claimed or
// its released, closed execution passed the engine's retention. A retained
// closed binding stays readable and is never considered unclaimed.
func (c *RepositoryBindingFlowClient) Find(ctx context.Context, repositoryID int64) (*RepositoryBinding, error) {
	var binding RepositoryBinding
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowid.RepositoryBindingFlowID(repositoryID), c.Flow.GetRepositoryBinding, nil, &binding)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// Claim starts the binding execution of the repository if needed and claims it
// for one Project's Setup, returning the claim generation. A released binding,
// open or closed, refuses with FAILED_PRECONDITION: its repository is archived.
func (c *RepositoryBindingFlowClient) Claim(ctx context.Context, projectID, claimID string, repositoryID int64) (uint64, error) {
	flowID := flowid.RepositoryBindingFlowID(repositoryID)
	request := flowid.Fingerprint(repositoryID)
	_, err := c.Dex.StartFlow(ctx, c.Flow, flowID, nil, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &request, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		Attributes: c.Flow.InitialAttributes(repositoryID, dexflow.StartIdentity{RequestID: request, Fingerprint: request}),
	})
	if err != nil {
		return 0, err
	}
	var result repositoryResult
	err = c.Dex.InvokeRPC(ctx, flowID, c.Flow.ClaimRepository, repositoryClaim{RepositoryID: repositoryID, ProjectID: projectID, ClaimID: claimID}, &result)
	// StartFlow just joined the execution, so an inactive one has completed.
	if _, closed := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); closed {
		return 0, errBindingEnded()
	}
	if err != nil {
		return 0, err
	}
	if result.Rejection != nil {
		return 0, result.Rejection
	}
	return result.Binding.Generation, nil
}

// Release ends the binding of the archived repository for exactly the claimed
// generation, which completes its execution. A release that finds the execution
// closed confirms its own release in the retained binding before succeeding.
func (c *RepositoryBindingFlowClient) Release(ctx context.Context, projectID string, repositoryID int64, generation uint64) error {
	in := repositoryRelease{ProjectID: projectID, Generation: generation}
	var result repositoryResult
	err := c.Dex.InvokeRPC(ctx, flowid.RepositoryBindingFlowID(repositoryID), c.Flow.ReleaseRepository, in, &result)
	if _, closed := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); closed {
		binding, findErr := c.Find(ctx, repositoryID)
		if findErr != nil || binding == nil {
			return errors.Join(err, findErr)
		}
		if binding.Status != StatusReleased || !binding.releasableBy(in) {
			return errReleaseMismatch()
		}
		return nil
	}
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}
