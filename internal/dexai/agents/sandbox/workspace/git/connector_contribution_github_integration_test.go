// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package git

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// TestConnectorContributionGitHubIntegration contributes a connector to the
// GitHub test copy of the connectors library through the actual contribution
// helper in a container of the coding image and the upstream installation of
// the GitHub App, as a connector_develop subagent's finish and its
// contribution's pull request Step do. The worktree is created from the test
// repository's main; the finish commits the new module as the App's bot with
// the subject connector(<slug>): <summary> and pushes the branch without
// force; a replayed finish, as after a Worker restart or a lost answer,
// reuses the commit and pushes nothing new; and the draft pull request is
// opened twice, the second time as after a lost answer, which finds the first.
// GitHub then holds one branch at that commit, authored and committed by the
// bot with no other trailer, and one draft pull request against main with the
// library's title. No token appears in the clone's configuration or in any
// answer. The test closes the pull request and deletes the branch when it
// ends. It is a manual test, like TestPlatformIssueGitHubIntegration: run it
// only when the connector contribution code changes, never in a pull
// request's default lane run. Its inputs come from the environment, and a
// missing input or credential fails it.
func TestConnectorContributionGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	fullName := os.Getenv("SUPERVERSE_TEST_CONNECTORS_REPOSITORY")
	if fullName != "superdurable/dex-connectors-library-test" {
		t.Fatal("SUPERVERSE_TEST_CONNECTORS_REPOSITORY must name the test copy superdurable/dex-connectors-library-test, never the library")
	}
	app := connectorTestApp(t)
	upstream := &connectorlibrary.Upstream{App: app, FullName: fullName}
	library := connectorlibrary.New(upstream, nil, deploymentnames.HostedRepositoryPrefix(deploymentnames.EnvironmentTest))
	container := sandboxdtest.StartCodingContainerForTest(t)
	client := container.Client()
	shell(t, client, "set -e; cd /workspace; git init -q -b main .")
	operations := &GitOperations{Library: library}
	slug := "mockgh" + strings.ToLower(rand.Text()[:6])
	target := ContributionTarget{AgentID: "k3n8q5r1", ProjectID: "t" + strings.ToLower(rand.Text()[:5]), Slug: slug, Kind: conversation.ContributionNew}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Minute)
	defer cancel()

	worktree, err := operations.CreateContributionWorktree(ctx, client, target)
	if err != nil {
		t.Fatalf("create the contribution worktree: %v", err)
	}
	t.Cleanup(func() { removeContribution(t, app, fullName, worktree.Branch) })
	if worktree.Branch != library.Branch(target.ProjectID, slug) || worktree.Root != ContributionWorktreeRoot(target.AgentID) {
		t.Fatalf("the worktree is not on the connector's branch: %+v", worktree)
	}
	module := "connectors/" + slug
	fixture := fmt.Sprintf(`set -e; cd %s
cp -R connectors/hackernews %[2]s
rm -rf %[2]s/examples
sed -i 's|connectors/hackernews|%[2]s|' %[2]s/go.mod
sed -i -e 's|name: hacker-news-daily|name: %[3]s|' -e 's|displayName: Hacker News Daily|displayName: Mock GH|' -e 's|company: Hacker News|company: %[3]s|' %[2]s/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate %[2]s/connector.yaml
python3 -c 'import re; text = open("catalog.yaml").read(); head, _, rest = text.partition("directories:\n"); entries = sorted(set(re.findall(r"^  - (\S+)$", rest, re.M)) | {"%[2]s"}); open("catalog.yaml", "w").write(head + "directories:\n" + "".join("  - %%s\n" %% entry for entry in entries))'`, worktree.Root, module, slug)
	prepared, err := client.RunToExit(ctx, sandboxd.StartRequest{ExecutionID: sandboxd.PlatformExecutionID("connector-github-fixture"),
		Arguments: []string{"bash", "-c", fixture}, Environment: sandboxd.GitProcessEnvironment(), Timeout: 5 * time.Minute})
	if err != nil || prepared.ExitCode != 0 {
		t.Fatalf("prepare the complete GitHub connector fixture: %v, exit %d: %s", err, prepared.ExitCode, prepared.Stderr)
	}

	summary := "Build the " + slug + " test connector"
	finished, err := operations.FinishContribution(ctx, client, target, summary, true)
	if err != nil || finished.Rejected != "" || finished.Location != connectorlibrary.LocationUpstream || finished.Module != connectorlibrary.ModulePath+"/"+module {
		t.Fatalf("finish the contribution: %+v, %v", finished, err)
	}
	replayed, err := operations.FinishContribution(ctx, client, target, summary, true)
	if err != nil || replayed.Commit != finished.Commit || replayed.Module != finished.Module || replayed.Branch != finished.Branch ||
		replayed.Location != connectorlibrary.LocationUpstream || replayed.Unchanged || replayed.Missing || replayed.Rejected != "" {
		t.Fatalf("a replayed finish did not reuse the commit: %+v, %v", replayed, err)
	}
	identity, err := upstream.BotIdentityForTest(ctx)
	if err != nil {
		t.Fatalf("read the App's bot: %v", err)
	}
	requireBotCommit(ctx, t, app, fullName, worktree.Branch, finished.Commit, identity, ContributionSubject(slug, summary))

	draft := connectorlibrary.PullRequestDraft{Branch: worktree.Branch, Title: ContributionSubject(slug, summary), Body: "Opened by `TestConnectorContributionGitHubIntegration`, which closes it."}
	opened, err := library.OpenDraftPullRequest(ctx, draft, connectorlibrary.LocationUpstream)
	if err != nil || opened.Number <= 0 || !opened.Draft {
		t.Fatalf("open the draft pull request: %+v, %v", opened, err)
	}
	again, err := library.OpenDraftPullRequest(ctx, draft, connectorlibrary.LocationUpstream)
	if err != nil || again.Number != opened.Number {
		t.Fatalf("a repeated opening found another pull request: %+v, %v", again, err)
	}
	requireOnePullRequest(ctx, t, app, fullName, worktree.Branch, opened.Number, draft.Title)
	if configuration := shell(t, client, "git -C /workspace/.git/superverse/connectors/library config --list"); strings.Contains(strings.ToLower(configuration), "authorization") ||
		strings.Contains(configuration, "x-access-token") {
		t.Fatalf("the clone's configuration holds a credential: %q", configuration)
	}
	t.Logf("branch=%s commit=%s pull request #%d by %s", worktree.Branch, finished.Commit, opened.Number, identity.Name)
}

// connectorTestApp is the App's installation on the connectors test
// repository.
func connectorTestApp(t *testing.T) *githubapp.App {
	t.Helper()
	appID, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub App ID unavailable")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_CONNECTORS_INSTALLATION_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub connectors installation ID unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil {
		t.Fatal("GitHub key encoding invalid")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("GitHub key is unavailable")
	}
	return &githubapp.App{AppID: appID, InstallationID: installation, PrivateKey: key}
}

// testToken mints a token of the test repository with permissions, for the
// test's own reads and its cleanup.
func testToken(ctx context.Context, t *testing.T, app *githubapp.App, fullName string, permissions map[string]string) githubapp.Token {
	t.Helper()
	_, name, _ := strings.Cut(fullName, "/")
	token, err := app.OperationToken(ctx, []string{name}, permissions)
	if err != nil {
		t.Fatalf("mint a test token: %v", err)
	}
	return token
}

// requireBotCommit requires the branch's head to be commit, authored and
// committed by the bot with exactly the subject and no trailer.
func requireBotCommit(ctx context.Context, t *testing.T, app *githubapp.App, fullName, branch, commit string, bot connectorlibrary.Identity, subject string) {
	t.Helper()
	token := testToken(ctx, t, app, fullName, map[string]string{"contents": "read", "metadata": "read"})
	var head struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := app.CallWithToken(ctx, token, http.MethodGet, "/repos/"+fullName+"/branches/"+url.PathEscape(branch), nil, &head); err != nil || head.Commit.SHA != commit {
		t.Fatalf("the branch %s is not at %s: %+v, %v", branch, commit, head, err)
	}
	var read struct {
		Commit struct {
			Message   string `json:"message"`
			Author    struct{ Name, Email string }
			Committer struct{ Name, Email string }
		} `json:"commit"`
	}
	if err := app.CallWithToken(ctx, token, http.MethodGet, "/repos/"+fullName+"/commits/"+commit, nil, &read); err != nil {
		t.Fatalf("read the commit: %v", err)
	}
	if read.Commit.Message != subject || read.Commit.Author.Name != bot.Name || read.Commit.Author.Email != bot.Email ||
		read.Commit.Committer.Name != bot.Name || read.Commit.Committer.Email != bot.Email || strings.Contains(strings.ToLower(read.Commit.Message), "co-authored-by") {
		t.Fatalf("the commit is not the bot's, with the subject alone: %+v, want %+v", read.Commit, bot)
	}
}

// requireOnePullRequest requires exactly one pull request whose head is the
// branch: the number, a draft against main with the title.
func requireOnePullRequest(ctx context.Context, t *testing.T, app *githubapp.App, fullName, branch string, number int64, title string) {
	t.Helper()
	token := testToken(ctx, t, app, fullName, map[string]string{"pull_requests": "read", "metadata": "read"})
	owner, _, _ := strings.Cut(fullName, "/")
	var listed []struct {
		Number int64  `json:"number"`
		Title  string `json:"title"`
		Draft  bool   `json:"draft"`
		Base   struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	path := "/repos/" + fullName + "/pulls?state=all&head=" + url.QueryEscape(owner+":"+branch)
	if err := app.CallWithToken(ctx, token, http.MethodGet, path, nil, &listed); err != nil {
		t.Fatalf("list the pull requests of %s: %v", branch, err)
	}
	if len(listed) != 1 || listed[0].Number != number || listed[0].Title != title || !listed[0].Draft || listed[0].Base.Ref != connectorlibrary.DefaultBranchForTest {
		t.Fatalf("expected one draft pull request #%d %q against main, got %+v", number, title, listed)
	}
}

// removeContribution closes the branch's pull requests and deletes the
// branch, the test's own effects in the test repository.
func removeContribution(t *testing.T, app *githubapp.App, fullName, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	token := testToken(ctx, t, app, fullName, map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"})
	owner, _, _ := strings.Cut(fullName, "/")
	var listed []struct {
		Number int64 `json:"number"`
	}
	path := "/repos/" + fullName + "/pulls?state=open&head=" + url.QueryEscape(owner+":"+branch)
	if err := app.CallWithToken(ctx, token, http.MethodGet, path, nil, &listed); err != nil {
		t.Errorf("list the pull requests to close: %v", err)
	}
	for _, pull := range listed {
		if err := app.CallWithToken(ctx, token, http.MethodPatch, "/repos/"+fullName+"/pulls/"+strconv.FormatInt(pull.Number, 10), map[string]any{"state": "closed"}, nil); err != nil {
			t.Errorf("close pull request #%d: %v", pull.Number, err)
		}
	}
	if err := app.CallWithToken(ctx, token, http.MethodDelete, "/repos/"+fullName+"/git/refs/heads/"+branch, nil, nil); err != nil {
		t.Errorf("delete the branch %s: %v", branch, err)
	}
}
