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
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
)

// TestConnectorHeadChecksGitHubIntegration reads two immutable real library
// heads and their recorded Actions runs. It creates or reruns no workflow.
// A removed run or expired job log fails with a clear error, never a skip.
func TestConnectorHeadChecksGitHubIntegration(t *testing.T) {
	const repository = "superdurable/dex-connectors-library"
	const failedHead = "98748b33e42fdbc06dbec1e1a20e7a96fc86812f"
	const failedRun int64 = 38984480484
	const passedHead = "b799d9f4a35800eb09d170c3aa8731a14a00fd2f"
	const passedRun int64 = 38844989507
	appID, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil || appID <= 0 {
		t.Fatal("SUPERVERSE_TEST_GITHUB_APP_ID is required for the read-only connector CI test")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_CONNECTORS_INSTALLATION_ID"), 10, 64)
	if err != nil || installation <= 0 {
		t.Fatal("SUPERVERSE_TEST_CONNECTORS_INSTALLATION_ID is required for the read-only connector CI test")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil || len(raw) == 0 {
		t.Fatal("GITHUB_APP_PRIVATE_KEY_BASE64 is required for the read-only connector CI test")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("the configured GitHub App key is invalid")
	}
	app := &githubapp.App{AppID: appID, InstallationID: installation, PrivateKey: key}
	library := New(&Upstream{App: app, FullName: repository}, nil, deploymentnames.HostedRepositoryPrefix(deploymentnames.EnvironmentTest))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	readRun := func(id int64, head string) {
		t.Helper()
		token, err := library.upstream.checksToken(ctx)
		if err != nil {
			t.Fatalf("mint the read-only Actions token: %v", err)
		}
		var run struct {
			ID      int64  `json:"id"`
			HeadSHA string `json:"head_sha"`
			Status  string `json:"status"`
		}
		path := fmt.Sprintf("/repos/%s/actions/runs/%d", repository, id)
		if err := app.CallWithToken(ctx, token, http.MethodGet, path, nil, &run); err != nil {
			t.Fatalf("read retained library Actions run %d (head %s): %v", id, head, err)
		}
		if run.ID != id || run.HeadSHA != head || run.Status != "completed" {
			t.Fatalf("library Actions run %d no longer identifies completed head %s", id, head)
		}
	}
	readRun(passedRun, passedHead)
	passed, err := library.ReadHeadChecks(ctx, passedHead)
	if err != nil || passed.State != HeadChecksPassed {
		t.Fatalf("passing library head %s: %+v, %v", passedHead, passed, err)
	}
	readRun(failedRun, failedHead)
	failed, err := library.ReadHeadChecks(ctx, failedHead)
	if err != nil || failed.State != HeadChecksFailed {
		t.Fatalf("failing library head %s: %+v, %v", failedHead, failed, err)
	}
	failed, err = library.ReadFailedHeadJobs(ctx, failed)
	if err != nil {
		t.Fatalf("failed library run %d job log is unavailable or expired: %v", failedRun, err)
	}
	if len(failed.Jobs) == 0 || !strings.Contains(strings.Join(failed.Jobs, " "), "dex-compat-current") || failed.Step == "" || failed.LogTail == "" {
		t.Fatalf("failed run %d no longer serves the expected failed job, step and bounded log tail: jobs=%v step=%q tail_bytes=%d", failedRun, failed.Jobs, failed.Step, len(failed.LogTail))
	}
	if len(failed.LogTail) > maximumJobLogTail {
		t.Fatalf("failed library log tail exceeded %d bytes", maximumJobLogTail)
	}
}
