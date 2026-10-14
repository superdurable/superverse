// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"testing"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/connect"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/kubetest"
)

// LiveProjectForTest is a throwaway READY Project of the lane, created by
// its account, whose HEAD's Release serves Live from one READY deployment,
// with the clients that read and change it, the platform's artifact bucket
// and the lane's connections to Live engines, for the tests of other packages
// that read Live. The Project's deletion at the end of the test removes its
// Live.
type LiveProjectForTest struct {
	ProjectID, Account, ReleaseID, BuildRevisionID, DeploymentID string
	Generation                                                   uint64
	Projects                                                     *projects.ProjectFlowClient
	Releases                                                     *releases.ProjectReleaseFlowClient
	Deployments                                                  *deployments.ProjectDeployFlowClient
	Objects                                                      *objectstore.Store
	Connections                                                  infra.Connections
}

// NewLiveProjectForTest creates a LiveProjectForTest as a person does: a
// Project whose Session starts on the mock model, the Release of its HEAD,
// the Release's configuration (configureConnections), a build of it and its
// deployment. It needs the inputs of the Live delivery feedback tests.
func NewLiveProjectForTest(t *testing.T, ctx context.Context) LiveProjectForTest {
	t.Helper()
	session := newMockSession(t, ctx, "")
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the Release of %s is %s", head, release.PreparedSource.Status)
	}
	session.configureConnections(ctx, live.configuration, live.target(head))
	built, _ := live.build(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), PreviewSkipReason: skipPreview})
	if built.Status != releases.Ready {
		t.Fatalf("the build of %s is %s: %+v", head, built.Status, built.Failure)
	}
	deployment := live.deploy(ctx, head, built)
	if deployment.Status != deployments.Ready {
		t.Fatalf("the deployment of %s is %s: %+v", head, deployment.Status, deployment.Failure)
	}
	return LiveProjectForTest{
		ProjectID: session.identity.ProjectID, Account: session.account, ReleaseID: head, BuildRevisionID: built.BuildRevisionID,
		DeploymentID: deployment.DeploymentID, Generation: deployment.Lease.Generation,
		Projects: live.projects, Releases: live.releases, Deployments: live.deployments, Objects: artifactStore(ctx, t),
		Connections: laneConnections(t),
	}
}

// laneConnections reaches the Live engines of the lane's Projects as the
// lane's server does: through the Kind cluster, or the Native Docker Engine,
// which connects as well as it stores.
func laneConnections(t *testing.T) infra.Connections {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		return &connect.Connections{Infrastructure: kubetest.InfrastructureForTest(t)}
	}
	return laneStorage(t).(infra.Connections)
}
