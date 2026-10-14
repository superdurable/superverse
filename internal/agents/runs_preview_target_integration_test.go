// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/studio/flowapi"
	"github.com/superdurable/superversev2/internal/studio/runs"
)

// TestRunsPreviewTargetFollowsReplacedInstanceDexIntegration drives the Studio Runs of a Preview
// target on a Session whose instance a lost node replaces. The Preview serves on the first
// instance: a request that omits the instance or names it is admitted, one that names another is
// refused. After the replacement the Preview still serves on the lost instance, so every request
// is refused, the new instance's included, until the Preview starts on it. Then the new instance
// is admitted, and a read or a write that names the old one is refused. The Runs read the Session's
// summary, instance and Preview freshly for each request. Engine reads on one instance share one
// pooled Dex connection; the new instance's identity misses the pool and dials its own.
func TestRunsPreviewTargetFollowsReplacedInstanceDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), agents.PreviewReplacementTimeoutForTest)
	defer cancel()
	scenario := agents.NewPreviewReplacementForTest(t, ctx)
	studio := &runs.Runs{Projects: scenario.Projects, Sessions: scenario.Service, Objects: scenario.Objects, Connections: scenario.Connections,
		Names: deploymentnames.MustFor(deploymentnames.EnvironmentTest), Logger: slog.New(slog.DiscardHandler), DexConnections: 4}
	t.Cleanup(func() { _ = studio.Close() })
	old := scenario.Instance(ctx)
	requireAdmitted(t, ctx, studio, scenario, "", "omitting the instance")
	requireAdmitted(t, ctx, studio, scenario, old, "the serving instance "+old)
	requireRefused(t, ctx, studio, scenario, "ws-not-the-instance", "another instance")
	requireEngineReads(ctx, t, studio, scenario.Account, previewTarget(scenario, old), 1)

	scenario.ReplaceInstance(ctx)
	replaced := scenario.Instance(ctx)
	if replaced == old {
		t.Fatalf("the instance %s was not replaced", old)
	}
	requireRefused(t, ctx, studio, scenario, old, "the lost instance "+old+" before the Preview moved")
	requireRefused(t, ctx, studio, scenario, replaced, "the new instance "+replaced+" before the Preview moved")

	scenario.StartPreview(ctx)
	requireAdmitted(t, ctx, studio, scenario, "", "omitting the instance after the Preview moved")
	requireAdmitted(t, ctx, studio, scenario, replaced, "the new instance "+replaced)
	requireEngineReads(ctx, t, studio, scenario.Account, previewTarget(scenario, replaced), 2)
	requireRefused(t, ctx, studio, scenario, old, "the lost instance "+old+" after the Preview moved")
	t.Logf("project %s: the Runs target admitted %s, refused the lost %s", scenario.ProjectID, replaced, old)
}

// previewTarget is the Flow API context of a Preview request that names instance, or omits it.
func previewTarget(scenario *agents.PreviewReplacementForTest, instance string) *pb.FlowApiContext {
	return &pb.FlowApiContext{Target: &pb.FlowApiTarget{ProjectId: scenario.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW,
		WorkspaceRequestId: instance, Generation: sandbox.InstanceGeneration}}
}

// requireAdmitted requires the Preview definitions read to be served for a request that names
// instance.
func requireAdmitted(t *testing.T, ctx context.Context, studio *runs.Runs, scenario *agents.PreviewReplacementForTest, instance, what string) {
	t.Helper()
	document, err := studio.Definitions(ctx, scenario.Account, previewTarget(scenario, instance))
	if err != nil || document.StatusCode != http.StatusOK {
		t.Fatalf("a Runs read %s was not admitted: status %d (%v)", what, document.StatusCode, err)
	}
}

// requireRefused requires a read and a write that name instance to be refused as a changed
// instance, and the write before any engine is dialed.
func requireRefused(t *testing.T, ctx context.Context, studio *runs.Runs, scenario *agents.PreviewReplacementForTest, instance, what string) {
	t.Helper()
	_, err := studio.Definitions(ctx, scenario.Account, previewTarget(scenario, instance))
	requireInstanceChanged(t, "read", what, err)
	_, err = studio.Invoke(ctx, scenario.Account, previewTarget(scenario, instance), runs.Write, func(context.Context, *flowapi.FlowAPI) flowapi.Document {
		t.Fatalf("a Runs write named %s and reached the engine", what)
		return flowapi.Document{}
	})
	requireInstanceChanged(t, "write", what, err)
}

func requireInstanceChanged(t *testing.T, kind, what string, err error) {
	t.Helper()
	var rejection *dexflow.Rejection
	if !errors.As(err, &rejection) || !rejection.HasCode(dexflow.RejectionFailedPrecondition) || rejection.Message != "Preview Workspace instance changed" {
		t.Fatalf("a Runs %s naming %s was not refused as a changed instance (%v)", kind, what, err)
	}
}
