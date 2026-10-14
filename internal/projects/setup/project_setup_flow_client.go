// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup

import (
	"context"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
)

// ProjectSetupFlowClient fronts ProjectSetupFlow, one setup attempt of
// a Project. It implements projects.SetupLifecycle for the owning ProjectFlow.
type ProjectSetupFlowClient struct {
	Dex  *dex.Client
	Flow *ProjectSetupFlow
}

// StartForProject starts the Project's accepted Setup. No Project lock spans
// the call: Setup registers its cleanup membership under Project admission
// before any effect, so a concurrent deletion rejects it, and a response-lost
// retry carries the Project's start identity as RequestID, returns the same
// execution and cannot run Setup twice.
func (c *ProjectSetupFlowClient) StartForProject(ctx context.Context, project projects.Project) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	in := setupStart{
		Identity: project.Identity, ProjectID: project.ProjectID, SetupID: project.SetupID(),
		TemplateRevision: project.TemplateRevision,
	}
	_, err := c.Dex.StartFlow(ctx, c.Flow, project.SetupFlowID, nil, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &in.Identity.RequestID,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: c.Flow.InitialAttributes(in),
		ConfigOverride: &dex.FlowConfig{AttributeStoreNames: postgres.ProjectSetupStore.Names()},
	})
	return err
}

// Read returns one Setup execution, also after it completed.
func (c *ProjectSetupFlowClient) Read(ctx context.Context, flowID string) (Setup, error) {
	var setup Setup
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowID, c.Flow.GetProjectSetup, nil, &setup)
	return setup, err
}

// RemoveForProjectCleanup asks a Setup to stop for Project deletion and
// reports whether its cleanup completed: a cancelled or completed Setup has
// nothing left to clean up.
func (c *ProjectSetupFlowClient) RemoveForProjectCleanup(ctx context.Context, flowID string) (bool, error) {
	setup, err := c.Read(ctx, flowID)
	if err != nil || setup.Status == StatusCancelled || setup.Status == StatusComplete {
		return err == nil, err
	}
	// The Setup's cleanup would archive its repository through this server's
	// host, which reads a repository of another host as absent: the Project's
	// deletion fails with the host to run the server with instead.
	if err = setup.checkRepositoryHost(c.Flow.Host); err != nil {
		return false, err
	}
	var result setupCleanupResult
	if err = c.Dex.InvokeRPC(ctx, flowID, c.Flow.RequestProjectSetupCleanup, nil, &result); err != nil {
		return false, err
	}
	return false, result.Rejection.Err()
}
