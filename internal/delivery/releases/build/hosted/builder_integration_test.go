// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build hosted

package hosted

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"
)

// TestBuilderRealAWS performs billable CodeBuild/ECR/S3 operations. It is
// deliberately outside Local Kind test discovery. The operator must first pass
// Native and full Kind E2E, provision isolated AWS test resources, and explicitly
// admit this test. Missing prerequisites fail; this test never substitutes mocks.
func TestBuilderRealAWS(t *testing.T) {
	required := func(name string) string {
		t.Helper()
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("missing required hosted integration input %s", name)
		}
		return value
	}
	if required("SUPERVERSE_HOSTED_BUILD_ACCEPTANCE") != "approved-after-kind" {
		t.Fatal("hosted acceptance was not explicitly admitted after Kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Minute)
	defer cancel()
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal("AWS SDK configuration failed")
	}
	profiles := build.NewProfiles()
	profiles.SelectHostedDefault()
	input := Config{Environment: deploymentnames.EnvironmentTest, ProjectName: required("SUPERVERSE_HOSTED_BUILD_PROJECT"), Region: config.Region, AccountID: required("AWS_ACCOUNT_ID"), RepositoryURI: required("SUPERVERSE_HOSTED_BUILD_REPOSITORY"), Bucket: required("SUPERVERSE_HOSTED_BUILD_BUCKET"), Prefix: required("SUPERVERSE_HOSTED_BUILD_PREFIX"), KMSKeyARN: required("SUPERVERSE_HOSTED_BUILD_KMS_KEY_ARN"), ToolImage: required("SUPERVERSE_HOSTED_BUILD_TOOL_IMAGE"), HostImage: required("SUPERVERSE_HOSTED_BUILD_HOST_IMAGE")}
	if input.Prefix != "sv2/test/builds" {
		t.Fatal("real builder integration is restricted to isolated test resources")
	}
	first, err := New(config, input, profiles)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(config, input, profiles)
	if err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	// Ten characters keep the test tag within Docker's 128, as a real Project ID does.
	project := "it" + hex.EncodeToString(random[:4])
	profile := profiles.Default()
	commit := required("SUPERVERSE_HOSTED_BUILD_TEST_COMMIT")
	// An image build binds one build revision to a frozen configuration digest;
	// the runner records both identities but never reads the configuration.
	request := build.BuildRequest{ContractVersion: build.ContractVersion, BuildRevisionID: "build-" + uuid.NewString(), ConfigurationDigest: "sha256:" + hex.EncodeToString(random[:]) + hex.EncodeToString(random[:]), ConfigurationRevision: 1, RequestID: hex.EncodeToString(random[:]) + hex.EncodeToString(random[:]), ProjectID: project, ReleaseID: commit, RepositoryFullName: required("SUPERVERSE_HOSTED_BUILD_TEST_REPOSITORY"), SourceCommit: commit, ProfileVersion: profile.Version, Profile: profile.ID, ProfileDigest: profile.Digest, Environment: deploymentnames.EnvironmentTest, Attempt: 1}
	request.ImageTag = build.ImageTag(request)
	if err = build.ValidateIdentity(request); err != nil {
		t.Fatal(err)
	}
	token := required("SUPERVERSE_HOSTED_BUILD_TEST_REPOSITORY_TOKEN")
	// Two real independent SDK clients share one durable admission object.
	failures := make(chan error, 2)
	for _, builder := range []*Builder{first, second} {
		go func(b *Builder) { _, e := b.Start(ctx, request, token); failures <- e }(builder)
	}
	for range 2 {
		if e := <-failures; e != nil && !errors.Is(e, errHostedConflict) {
			t.Fatal("hosted Start did not reach a reconcilable state")
		}
	}
	// A newly constructed client verifies restart recovery using only AWS objects.
	restarted, err := New(config, input, profiles)
	if err != nil {
		t.Fatal(err)
	}
	var ready build.BuildResult
	deadline := time.NewTimer(46 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for ready.State != build.BuildReady {
		result, e := restarted.Inspect(ctx, request)
		if e != nil {
			t.Fatal("hosted provider inspection failed")
		}
		if result.State == build.BuildFailed || result.State == build.BuildUnknown {
			t.Fatalf("actual build requires recovery: %s; scope=%s/%s", result.State, project, request.ReleaseID)
		}
		ready = result
		if ready.State == build.BuildReady {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("hosted integration deadline exceeded")
		case <-deadline.C:
			t.Fatal("build did not complete within its budget")
		case <-ticker.C:
		}
	}
	if ready.OperationID == "" || !flowid.IsSHA256Digest(ready.ImageDigest) || ready.ImageReference != input.RepositoryURI+"@"+ready.ImageDigest {
		t.Fatal("immutable image identity absent")
	}
	// QUEUE and HOST_PREPARATION come from CodeBuild, the rest from the
	// runner's v2 result.
	measured := map[string]bool{}
	for _, timing := range ready.Timings {
		if timing.StartedAt.IsZero() || timing.DurationMs <= 0 || measured[timing.Phase] {
			t.Fatalf("phase %q is unmeasured or repeated", timing.Phase)
		}
		measured[timing.Phase] = true
	}
	for _, phase := range []string{build.PhaseQueue, build.PhaseHostPreparation, build.PhaseDependencyPreparationForTest, build.PhaseAnalysisForTest, build.PhaseCompilationForTest, build.PhaseImagePublicationForTest} {
		if !measured[phase] {
			t.Fatalf("phase %s is missing", phase)
		}
	}
	h, _, err := restarted.readHead(ctx, request)
	if err != nil || h == nil || h.Phase != hostedReady || h.Result.Version == "" {
		t.Fatal("terminal exact result receipt absent")
	}
	if h.CodeBuild.Queue.Phase != build.PhaseQueue || h.CodeBuild.HostPreparation.Phase != build.PhaseHostPreparation {
		t.Fatal("the terminal receipt did not record CodeBuild's phases")
	}
	// The READY receipt answers alone, without CodeBuild, with the same phases.
	reread, err := first.Inspect(ctx, request)
	if err != nil || !slices.EqualFunc(reread.Timings, ready.Timings, func(a, b build.BuildPhaseTiming) bool {
		return a.Phase == b.Phase && a.StartedAt.Equal(b.StartedAt) && a.DurationMs == b.DurationMs
	}) {
		t.Fatal("the READY receipt did not keep the build's phases")
	}
	accepted, err := restarted.findExecution(ctx, h)
	if err != nil || accepted == nil {
		t.Fatal("accepted external execution was not uniquely reconciled")
	}
	joined, err := second.Start(ctx, request, token)
	if err != nil || joined.OperationID != ready.OperationID || joined.ImageDigest != ready.ImageDigest {
		t.Fatal("idempotent Start did not retain the accepted result")
	}
	if err = restarted.CleanupRelease(ctx, request); err != nil {
		t.Fatal("cleanup incomplete; retain scope for recovery")
	}
	after, err := first.Inspect(ctx, request)
	if err != nil || after.State != build.BuildMissing {
		t.Fatal("cleanup did not hide the removed build")
	}
	if err = second.CleanupRelease(ctx, request); err != nil {
		t.Fatal("idempotent cleanup failed")
	}
	if _, err = first.Start(ctx, request, token); err == nil {
		t.Fatal("late Start bypassed the cleanup fence")
	}
	request.Attempt++
	if _, err = second.Start(ctx, request, token); err == nil {
		t.Fatal("new attempt bypassed the permanent cleanup fence")
	}
	t.Logf("verified real CodeBuild execution %s and permanent cleanup fence for project %s", ready.OperationID, project)
}
