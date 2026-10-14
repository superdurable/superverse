// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package githubissues

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/githubapp"
)

// TestPlatformIssueGitHubIntegration files one incident of a fresh signature in
// the GitHub test repository through the App's installation there, as the
// server's github.issues configuration names them. The first report opens one
// issue; once GitHub's label listing returns it, a second report of the same
// signature from a new client, as a repeated Step on another Worker, opens
// none and finds the same issue, which is then the only issue with the label;
// and once the test closed the issue, a third report still finds it and opens
// none. The test closes the issue and deletes its label when it ends. It needs
// the App's Issues permission and its installation on the repository; missing
// inputs or credentials fail it.
func TestPlatformIssueGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	fullName := os.Getenv("SUPERVERSE_TEST_GITHUB_ISSUE_REPOSITORY")
	if strings.Count(fullName, "/") != 1 {
		t.Fatal("the GitHub issue test repository owner/name is required")
	}
	repository := &Repository{App: newTestApp(t), FullName: fullName}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	signature := sha256.Sum256([]byte(rand.Text()))
	label := IncidentLabel(deploymentnames.EnvironmentTest, hex.EncodeToString(signature[:]))
	issue := Issue{Label: label, Title: "Platform incident in test: githubissues integration test",
		Body: "Opened by `TestPlatformIssueGitHubIntegration`, which closes it and deletes its label."}
	t.Logf("repository=%s label=%s", fullName, label)

	first, err := repository.EnsureIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeTestIssue(t, repository, first.Number, label) })
	if !first.Opened || !first.Labeled || first.Number <= 0 || first.URL == "" {
		t.Fatalf("the first report did not open an issue: %+v", first)
	}
	// GitHub's issue listings are eventually consistent for a few seconds, so
	// two reports of one signature within that window may each open an issue.
	// That is accepted (README); the second report waits until the label
	// listing returns the first report's issue.
	awaitLabelListing(ctx, t, repository, label, first.Number)
	second, err := (&Repository{App: newTestApp(t), FullName: fullName}).EnsureIssue(ctx, issue)
	if err != nil || second.Opened || second.Number != first.Number {
		t.Fatalf("a second report of the signature opened an issue or found another: %+v (%v)", second, err)
	}
	if listed := labeledIssues(ctx, t, repository, label); len(listed) != 1 {
		t.Fatalf("issues %v carry %s, want only #%d", listed, label, first.Number)
	}
	if err = repository.call(ctx, http.MethodPatch, "/issues/"+strconv.FormatInt(first.Number, 10), map[string]any{"state": "closed"}, nil); err != nil {
		t.Fatal(err)
	}
	third, err := repository.EnsureIssue(ctx, issue)
	if err != nil || third.Opened || third.Number != first.Number {
		t.Fatalf("a report after the issue closed opened another or found another: %+v (%v)", third, err)
	}
	t.Logf("issue=%d opened once; two later reports found it, open and closed", first.Number)
}

// TestPlatformIssueLabelGitHubIntegration attaches the label of a fresh
// signature to an issue that lacks it, as an issue GitHub opened without the
// label it dropped: EnsureIssue reports such an issue unlabeled, and the
// PlatformIssueFlow commits its number before LabelIssue attaches the label.
// LabelIssue twice, as a retried attempt after a lost answer, leaves the issue
// the only one with the label, and a later report finds it and opens none. The
// inputs and the cleanup are those of TestPlatformIssueGitHubIntegration.
func TestPlatformIssueLabelGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	fullName := os.Getenv("SUPERVERSE_TEST_GITHUB_ISSUE_REPOSITORY")
	if strings.Count(fullName, "/") != 1 {
		t.Fatal("the GitHub issue test repository owner/name is required")
	}
	repository := &Repository{App: newTestApp(t), FullName: fullName}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	signature := sha256.Sum256([]byte(rand.Text()))
	label := IncidentLabel(deploymentnames.EnvironmentTest, hex.EncodeToString(signature[:]))
	issue := Issue{Label: label, Title: "Platform incident in test: githubissues label integration test",
		Body: "Opened without its label by `TestPlatformIssueLabelGitHubIntegration`, which closes it and deletes its label."}
	t.Logf("repository=%s label=%s", fullName, label)

	var unlabeled issueResponse
	if err := repository.call(ctx, http.MethodPost, "/issues", map[string]any{"title": issue.Title, "body": issue.Body}, &unlabeled); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeTestIssue(t, repository, unlabeled.Number, label) })
	if unlabeled.carries(label) {
		t.Fatalf("issue #%d carries %s before LabelIssue", unlabeled.Number, label)
	}
	for attempt := range 2 {
		if err := repository.LabelIssue(ctx, unlabeled.Number, label); err != nil {
			t.Fatalf("label attempt %d: %v", attempt+1, err)
		}
	}
	awaitLabelListing(ctx, t, repository, label, unlabeled.Number)
	found, err := repository.EnsureIssue(ctx, issue)
	if err != nil || found.Opened || !found.Labeled || found.Number != unlabeled.Number {
		t.Fatalf("a report after the label was attached opened an issue or found another: %+v (%v)", found, err)
	}
	if listed := labeledIssues(ctx, t, repository, label); len(listed) != 1 {
		t.Fatalf("issues %v carry %s, want only #%d", listed, label, unlabeled.Number)
	}
	t.Logf("issue=%d labeled twice; a later report found it", unlabeled.Number)
}

// The wait for the label listing: a read every labelListingPoll, for at most
// labelListingWait.
const (
	labelListingPoll = 2 * time.Second
	labelListingWait = time.Minute
)

// awaitLabelListing waits until GitHub's label listing returns issue number
// with label, and fails the test once labelListingWait passed without it.
func awaitLabelListing(ctx context.Context, t *testing.T, repository *Repository, label string, number int64) {
	t.Helper()
	deadline := time.Now().Add(labelListingWait)
	for !slices.Contains(labeledIssues(ctx, t, repository, label), number) {
		if !time.Now().Before(deadline) {
			t.Fatalf("GitHub's label listing did not return issue #%d with %s within %s", number, label, labelListingWait)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for GitHub's label listing of issue #%d: %v", number, ctx.Err())
		case <-time.After(labelListingPoll):
		}
	}
}

// labeledIssues lists the numbers of the issues, open or closed, that carry
// label.
func labeledIssues(ctx context.Context, t *testing.T, repository *Repository, label string) []int64 {
	t.Helper()
	var listed []issueResponse
	if err := repository.call(ctx, http.MethodGet, "/issues?state=all&per_page=100&labels="+url.QueryEscape(label), nil, &listed); err != nil {
		t.Fatal(err)
	}
	var numbers []int64
	for _, issue := range listed {
		if issue.PullRequest == nil && issue.carries(label) {
			numbers = append(numbers, issue.Number)
		}
	}
	return numbers
}

// removeTestIssue closes the test's issue and deletes its label, so the test
// repository keeps no open test issue and no test label.
func removeTestIssue(t *testing.T, repository *Repository, number int64, label string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	defer cancel()
	if err := repository.call(ctx, http.MethodPatch, "/issues/"+strconv.FormatInt(number, 10), map[string]any{"state": "closed"}, nil); err != nil {
		t.Errorf("close test issue #%d: %v", number, err)
	}
	if err := repository.call(ctx, http.MethodDelete, "/labels/"+url.PathEscape(label), nil, nil); err != nil {
		t.Errorf("delete test label %s: %v", label, err)
	}
}

// newTestApp acts as the App's installation on the issue test repository, as
// the server's github.appId and github.issues.installationId name it, with the
// server's token permissions. Missing or invalid credentials fail the test.
func newTestApp(t *testing.T) *githubapp.App {
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
	return &githubapp.App{AppID: appID, InstallationID: installation, PrivateKey: key, Permissions: TokenPermissions()}
}
