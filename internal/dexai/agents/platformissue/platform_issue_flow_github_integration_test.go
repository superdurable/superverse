// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/comm/platform/githubissues"
)

// TestPlatformIssueFlowGitHubIntegration is TestPlatformIssueJoinDexIntegration
// with the real issue repository, as the server's github.issues configuration
// names it: three reports of a fresh signature, two of them joined to the
// first, run one PlatformIssueFlow run, which opens exactly one issue. Two
// reports that each called githubissues.EnsureIssue at the same moment could
// each open one; the joined run does not. Once GitHub's label listing returns
// the issue, it is the only issue with the signature's label. The test closes
// the issue and deletes its label when it ends. It is a manual test with the
// inputs of TestPlatformIssueGitHubIntegration (internal/comm/platform/githubissues)
// and those of TestPlatformIssueJoinDexIntegration; a missing input or
// credential fails it.
func TestPlatformIssueFlowGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	fullName := os.Getenv("SUPERVERSE_TEST_GITHUB_ISSUE_REPOSITORY")
	if strings.Count(fullName, "/") != 1 {
		t.Fatal("the GitHub issue test repository owner/name is required")
	}
	app := issueApp(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	incident, state := joinReports(ctx, t, &githubissues.Repository{App: app, FullName: fullName})
	label := githubissues.IncidentLabel(incident.Environment, incident.signature())
	if state.Outcome.IssueNumber > 0 {
		t.Cleanup(func() { removeIssue(t, app, fullName, state.Outcome.IssueNumber, label) })
	}
	if state.Outcome.Status != outcomeOpened || state.Outcome.IssueNumber <= 0 || state.Outcome.URL == "" {
		t.Fatalf("the run of %s did not open an issue: %+v", incident.flowID(), *state.Outcome)
	}
	// GitHub's issue listings are eventually consistent for a few seconds.
	for deadline := time.Now().Add(time.Minute); ; {
		numbers := labeledIssues(ctx, t, app, fullName, label)
		if len(numbers) == 1 && numbers[0] == state.Outcome.IssueNumber {
			break
		}
		if len(numbers) > 1 || !time.Now().Before(deadline) {
			t.Fatalf("issues %v carry %s, want only #%d", numbers, label, state.Outcome.IssueNumber)
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("issue=%d opened once for three reports of %s", state.Outcome.IssueNumber, label)
}

// labeledIssues lists the numbers of the issues, open or closed, that carry
// label.
func labeledIssues(ctx context.Context, t *testing.T, app *githubapp.App, fullName, label string) []int64 {
	t.Helper()
	var listed []struct {
		Number      int64     `json:"number"`
		PullRequest *struct{} `json:"pull_request"`
	}
	if err := app.Call(ctx, http.MethodGet, "/repos/"+fullName+"/issues?state=all&per_page=100&labels="+url.QueryEscape(label), nil, &listed); err != nil {
		t.Fatal(err)
	}
	var numbers []int64
	for _, issue := range listed {
		if issue.PullRequest == nil {
			numbers = append(numbers, issue.Number)
		}
	}
	return numbers
}

// removeIssue closes the test's issue and deletes its label, so the test
// repository keeps no open test issue and no test label.
func removeIssue(t *testing.T, app *githubapp.App, fullName string, number int64, label string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	defer cancel()
	if err := app.Call(ctx, http.MethodPatch, "/repos/"+fullName+"/issues/"+strconv.FormatInt(number, 10), map[string]any{"state": "closed"}, nil); err != nil {
		t.Errorf("close test issue #%d: %v", number, err)
	}
	if err := app.Call(ctx, http.MethodDelete, "/repos/"+fullName+"/labels/"+url.PathEscape(label), nil, nil); err != nil {
		t.Errorf("delete test label %s: %v", label, err)
	}
}

// issueApp acts as the App's installation on the issue test repository, as
// the server's github.appId and github.issues.installationId name it, with the
// server's token permissions. Missing or invalid credentials fail the test.
func issueApp(t *testing.T) *githubapp.App {
	t.Helper()
	appID, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub App ID unavailable")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_ISSUE_INSTALLATION_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub issue installation ID unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil {
		t.Fatal("GitHub key encoding invalid")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("GitHub key is unavailable")
	}
	return &githubapp.App{AppID: appID, InstallationID: installation, PrivateKey: key, Permissions: githubissues.TokenPermissions()}
}
