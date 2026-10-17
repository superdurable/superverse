// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package connectorlibrary

import (
	"context"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
)

// TestReleaseCandidateWorkflowGitHubIntegration reads immutable real workflow
// runs and the omv3 stable-tag/PR blockers. It dispatches or reruns nothing;
// removed runs, expired logs or changed incident facts fail clearly.
func TestReleaseCandidateWorkflowGitHubIntegration(t *testing.T) {
	const failedRun int64 = 38979345261
	const passedRun int64 = 38985509210
	library := liveReleaseCandidateLibrary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	failed, err := library.readReleaseCandidateRunByID(ctx, failedRun)
	if err != nil {
		t.Fatalf("failed release-candidate run %d is unavailable: %v", failedRun, err)
	}
	if failed.State != ReleaseCandidateRunFailed || len(failed.Failure.Jobs) == 0 || failed.Failure.Step == "" || failed.Failure.LogTail == "" {
		t.Fatalf("failed run %d no longer serves failed jobs, step and bounded log tail: %+v", failedRun, failed)
	}
	if !strings.Contains(strings.Join(failed.Failure.Jobs, " "), "verify") ||
		failed.Failure.Step != "Verify the Connector with the latest stable Dex CLI and Web" || len(failed.Failure.LogTail) > maximumJobLogTail {
		t.Fatalf("failed run %d no longer identifies the expected verify job and step within a %d-byte tail", failedRun, maximumJobLogTail)
	}
	passed, err := library.readReleaseCandidateRunByID(ctx, passedRun)
	if err != nil || passed.State != ReleaseCandidateRunSucceeded {
		t.Fatalf("successful release-candidate run %d: %+v, %v", passedRun, passed, err)
	}

	candidate := ReleaseCandidate{Directory: "connectors/stripe", Version: "v0.22.0-rc.2", Tag: "connectors/stripe/v0.22.0-rc.2", Commit: "5d461f7f00000000000000000000000000000000"}
	blockers, err := library.ReadReleaseCandidateBlockers(ctx, candidate, 247, LocationUpstream)
	if err != nil {
		t.Fatalf("read omv3 release-candidate blockers: %v", err)
	}
	if blockers.StableTag != "connectors/stripe/v0.22.0" {
		t.Fatalf("released stable tag was not found: %+v", blockers)
	}
	if !blockers.PullRequestConflict {
		t.Fatalf("conflicting pull request 247 is no longer reported as conflicting: %+v", blockers)
	}
}

func liveReleaseCandidateLibrary(t *testing.T) *Library {
	t.Helper()
	appID, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil || appID <= 0 {
		t.Fatal("SUPERVERSE_TEST_GITHUB_APP_ID is required for the read-only release-candidate test")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_CONNECTORS_INSTALLATION_ID"), 10, 64)
	if err != nil || installation <= 0 {
		t.Fatal("SUPERVERSE_TEST_CONNECTORS_INSTALLATION_ID is required for the read-only release-candidate test")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil || len(raw) == 0 {
		t.Fatal("GITHUB_APP_PRIVATE_KEY_BASE64 is required for the read-only release-candidate test")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("the configured GitHub App key is invalid")
	}
	app := &githubapp.App{AppID: appID, InstallationID: installation, PrivateKey: key}
	return New(&Upstream{App: app, FullName: "superdurable/dex-connectors-library"}, nil,
		deploymentnames.HostedRepositoryPrefix(deploymentnames.EnvironmentTest))
}
