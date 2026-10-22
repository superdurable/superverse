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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	buildlocal "github.com/superdurable/superversev2/internal/dexai/delivery/releases/build/local"
)

// A real local builder cancellation fails the first source preparation. A
// repeated Prepare joins that acceptance; a new request admits attempt 2,
// which executes against the real Git host, Docker and Dex and reaches READY.
func TestReleaseSourceRetryDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*liveSourceTimeout+mockTurnTimeout)
	defer cancel()
	s := newMockSession(t, ctx, "")
	live := newLiveDelivery(s)
	project, commit, first := s.identity.ProjectID, s.identity.HeadCommit, uuid.NewString()
	prepare := func(request string) {
		t.Helper()
		if _, err := live.releases.Prepare(ctx, s.account, project, request, commit); err != nil {
			t.Fatalf("prepare Release: %s", dexflowtest.ErrorCategory(err))
		}
	}
	prepare(first)
	accepted := live.awaitRelease(ctx, commit, "initial source identity", func(r releases.Snapshot) bool { return len(r.PreparedSource.Requests) == 1 })
	profile, err := build.NewProfiles().Get(accepted.BuildProfileVersion)
	if err != nil {
		t.Fatal(err)
	}
	request := build.BuildRequest{ContractVersion: build.ContractVersion, BuildRevisionID: build.SourceRevisionID, SourceOnly: true,
		RequestID: accepted.Identity.RequestID, Attempt: 1, Environment: deploymentnames.EnvironmentTest,
		ProjectID: project, ReleaseID: commit, SourceCommit: commit, RepositoryFullName: accepted.Repository.FullName,
		ProfileVersion: profile.Version, Profile: profile.ID, ProfileDigest: profile.Digest}
	request.ImageTag = build.ImageTag(request)
	token, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	builder, err := buildlocal.New(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_URL"), strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	s.await(ctx, "the actual provider's first attempt", func() bool {
		result, inspectErr := builder.Inspect(ctx, request)
		if inspectErr != nil {
			t.Fatalf("inspect first source attempt: %s", dexflowtest.ErrorCategory(inspectErr))
		}
		if result.State == build.BuildReady || result.State == build.BuildFailed {
			t.Fatalf("source preparation settled before cancellation: %s", result.State)
		}
		return result.State == build.BuildRunning
	})
	if err = builder.Cancel(ctx, request); err != nil && !errors.Is(err, build.ErrPending) {
		t.Fatalf("cancel actual source preparation: %s", dexflowtest.ErrorCategory(err))
	}
	failed := live.awaitRelease(ctx, commit, "failed source preparation", func(r releases.Snapshot) bool { return r.PreparedSource.Status == releases.Failed })
	if failure := failed.PreparedSource.Failure; failure == nil || failure.Remedy != dexflow.RemedyPublishAgain {
		t.Fatalf("cancelled preparation has no Publish again remedy: %+v", failure)
	}
	log, err := builder.ReadLog(ctx, request, build.LogRead{Tail: true, MaximumBytes: 4096})
	if err != nil || !strings.Contains(log.Text, "BUILD_EXCEPTION class=") || strings.Contains(log.Text, strings.TrimSpace(string(token))) {
		t.Fatalf("source failure has no safe classified job log: %s", dexflowtest.ErrorCategory(err))
	}
	// Inject uncertainty into only this ended provider job's private record.
	// Its real child and containers already stopped; preserve that proof and
	// require the real service to query Docker before settling UNKNOWN again.
	path := filepath.Join(filepath.Dir(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE")), "release-builds", "test", project, commit, "source", "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read the owned provider fault-injection record")
	}
	var providerState map[string]json.RawMessage
	if err = json.Unmarshal(data, &providerState); err != nil || string(providerState["childSettled"]) != "true" {
		t.Fatal("the real cancelled job has no settled-child proof")
	}
	providerState["state"], providerState["failure"] = json.RawMessage(`"UNKNOWN"`), json.RawMessage(`"CANCEL_OUTCOME_UNKNOWN"`)
	data, err = json.Marshal(providerState)
	if err != nil {
		t.Fatal("encode the owned provider fault-injection record")
	}
	if err = os.WriteFile(path+".retry-test", data, 0o600); err != nil {
		t.Fatal("write the owned provider fault-injection record")
	}
	if err = os.Rename(path+".retry-test", path); err != nil {
		t.Fatal("admit the owned provider fault-injection record")
	}
	reconciliation, stopReconciliation := context.WithTimeout(ctx, time.Minute)
	defer stopReconciliation()
	s.await(reconciliation, "UNKNOWN source cleanup through real Docker", func() bool {
		reconciled, inspectErr := builder.Inspect(reconciliation, request)
		return inspectErr == nil && reconciled.State == build.BuildFailed
	})
	prepare(first)
	duplicate := live.awaitRelease(ctx, commit, "same-request acceptance", func(releases.Snapshot) bool { return true })
	if len(duplicate.PreparedSource.Requests) != 1 || duplicate.Revision != failed.Revision {
		t.Fatal("the original request admitted another preparation")
	}
	retry := uuid.NewString()
	prepare(retry)
	prepare(retry)
	ready := live.awaitRelease(ctx, commit, "retried source settled", func(r releases.Snapshot) bool {
		return r.PreparedSource.Status == releases.Ready || r.PreparedSource.Status == releases.Failed
	})
	if ready.PreparedSource.Status != releases.Ready {
		t.Fatalf("provider attempt 2 failed: %+v", ready.PreparedSource.Failure)
	}
	if len(ready.PreparedSource.Requests) != 2 || ready.PreparedSource.Failure != nil {
		t.Fatalf("retry did not retain exactly two admissions: %d", len(ready.PreparedSource.Requests))
	}
	request.Attempt = 2
	result, err := builder.Inspect(ctx, request)
	if err != nil || result.State != build.BuildReady || result.Attempt != 2 || result.OperationID == accepted.Identity.RequestID+"-1" {
		t.Fatalf("new provider attempt did not run: state=%s attempt=%d error=%s", result.State, result.Attempt, dexflowtest.ErrorCategory(err))
	}
	// A replay after READY also stays on the same provider attempt.
	prepare(retry)
	t.Logf("Release %s Project %s: provider attempt 1 cancelled; duplicate request unchanged; explicit retry reached READY on attempt 2", commit, project)
}
