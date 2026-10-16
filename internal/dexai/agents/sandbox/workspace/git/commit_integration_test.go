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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	githubhost "github.com/superdurable/superversev2/internal/comm/platform/repohost/github"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

func TestCommitRecoveryActualGitHubAndSandbox(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("explicit isolated GitHub effects are required")
	}
	client := sandboxdtest.ClientForTest(t)
	app, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub App ID is required")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_INSTALLATION_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub installation is required")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil {
		t.Fatal("GitHub key encoding invalid")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("GitHub key unavailable")
	}
	owner := os.Getenv("SUPERVERSE_TEST_GITHUB_OWNER")
	if owner == "" {
		t.Fatal("explicit GitHub owner required")
	}
	github := &githubhost.Client{App: &githubapp.App{AppID: app, InstallationID: installation, PrivateKey: key}, Owner: owner, RepositoryPrefix: deploymentnames.RepositoryPrefixLocalKind, Environment: deploymentnames.EnvironmentTest}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// Fixture commands stand in for model-authored effects, so they run in
	// sandboxd's own environment, where HOME is the workspace image's HOME.
	exec := func(environment []sandboxd.EnvironmentVariable, args ...string) string {
		t.Helper()
		result, err := workspaceGit{client: client}.run(ctx, environment, gitCommandTimeout, args...)
		if err != nil {
			t.Fatal(commandError("actual Git regression command", result, err))
		}
		return strings.TrimSpace(result.stdout)
	}
	// Never adopt or erase an existing user's checkout as a test fixture.
	if files := exec(nil, "find", ".", "-mindepth", "1", "-maxdepth", "1", "-print"); files != "" {
		t.Fatal("disposable sandbox workspace is not empty")
	}
	projectID := flowid.RandomID(4)
	setupID := flowid.RandomID(8)
	managed, err := github.ManagedRepository(ctx, repohost.SetupIdentity{ProjectID: projectID, ProjectName: "Commit", SetupID: setupID})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("isolated repository=%s project=%s setup=%s", managed.FullName, projectID, setupID)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if err := github.ArchiveManaged(cleanup, projectID, setupID, managed.FullName); err != nil {
			t.Error("archive test repository:", err)
		}
	})
	template := apptemplate.Current()
	initial, err := github.InitializeTemplate(ctx, managed, template.Revision, apptemplate.Current())
	if err != nil {
		t.Fatal(err)
	}
	repository := Repository{ProjectID: projectID, InstallationID: installation, RepositoryID: managed.ID, Owner: owner, Name: strings.TrimPrefix(managed.FullName, owner+"/"), CloneURL: managed.CloneURL, DefaultBranch: "main", ExpectedHeadSHA: initial}
	// The server's credentials mint actual repository-scoped installation tokens.
	credentials := &HostCredentials{Host: github}
	operations := &GitOperations{Credentials: credentials}
	checkout, err := operations.Checkout(ctx, client, repository)
	if err != nil || checkout.HeadCommitSHA != initial {
		t.Fatalf("actual checkout failed: %v", err)
	}
	credential, err := credentials.repositoryCredential(ctx, tokenRequest{installationID: installation, repositoryID: managed.ID})
	if err != nil {
		t.Fatal(err)
	}
	g := workspaceGit{client: client, token: credential.token}
	head := func() string {
		t.Helper()
		h, err := g.localHead(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	remote := func() string {
		t.Helper()
		h, found, err := g.remoteHead(ctx, "main")
		if err != nil || !found {
			t.Fatal("remote main is unavailable:", err)
		}
		return h
	}
	commit := func(path, call string) string {
		t.Helper()
		exec(nil, "bash", "-c", `printf '%s\n' "$2" > "$1"`, "git-regression", path, call)
		exec(nil, "git", "add", "--", path)
		exec(nil, append(append([]string{"git"}, committerFlags...), "commit", "-m", commitMessage("A"+strings.Repeat("摘要🙂", 12), call))...)
		return head()
	}
	publish := func(call string) (CommitResult, error) {
		// This scenario isolates Git recovery; the existing actual application
		// source-check test and full Studio journey cover the independent source gate.
		return operations.CommitAndPush(ctx, client, repository, CommitRequest{CallID: call, Summary: "Actual Git recovery regression", Blocked: true}, func(string) {})
	}
	assertPushed := func(want string, result CommitResult, err error) {
		t.Helper()
		if err != nil || result.CommitSHA != want || head() != want || remote() != want {
			t.Fatalf("expected exact clean published commit %s; got %s error=%v", want, result.CommitSHA, err)
		}
		if exec(nil, "git", "status", "--porcelain") != "" {
			t.Fatal("publish left a dirty workspace")
		}
	}
	// Reproduce the real failure: a previous call authored a commit, but the
	// model's post-validation call has a different identity.
	first := commit("git-recovery-first.txt", "previous-call")
	// Exercise the real gRPC process request and Git with a 72-byte cut that
	// lands inside a multibyte character. The complete turn marker must survive.
	message := exec(nil, "git", "log", "-1", "--format=%B")
	if !strings.HasPrefix(message, "A"+strings.Repeat("摘要🙂", 7)+"\n\n") || !hasTurnMarker(message, "previous-call") {
		t.Fatal("actual Git commit lost the bounded Unicode subject or recovery marker")
	}
	if remote() != initial {
		t.Fatal("local commit unexpectedly changed remote")
	}
	result, err := publish("fresh-call-after-validation")
	assertPushed(first, result, err)
	result, err = publish("fresh-call-after-validation")
	assertPushed(first, result, err)
	result, err = publish("another-clean-completion")
	assertPushed(first, result, err)
	// The model can write a global Git configuration under the image HOME and
	// repository hooks. Neither may reach the token-bearing commands: an
	// honored configuration sends fetch and push to an unreachable host, and
	// an honored pre-push hook records its run and rejects the push.
	exec(nil, "bash", "-c", `printf '[url "https://unreachable.invalid/"]\n\tinsteadOf = https://github.com/\n' > "$HOME/.gitconfig" && printf '.gitconfig\n' >> .git/info/exclude`)
	exec(nil, "bash", "-c", `printf '#!/bin/sh\ntouch .git/pre-push-ran\nexit 1\n' > .git/hooks/pre-push && chmod +x .git/hooks/pre-push`)
	isolated := commit("git-isolation.txt", "isolated-call")
	result, err = publish("isolated-call")
	assertPushed(isolated, result, err)
	if exec(nil, "bash", "-c", `if test -e .git/pre-push-ran; then echo ran; else echo skipped; fi`) != "skipped" {
		t.Fatal("a repository pre-push hook ran during a token-bearing push")
	}
	exec(nil, "bash", "-c", `rm -f .git/hooks/pre-push "$HOME/.gitconfig"`)
	repository.ExpectedHeadSHA = isolated
	// Preserve more than one already-authored local commit.
	commit("git-recovery-second.txt", "earlier-local-call")
	multiple := commit("git-recovery-third.txt", "later-local-call")
	result, err = publish("publish-multiple")
	assertPushed(multiple, result, err)
	repository.ExpectedHeadSHA = multiple
	// A real concurrent GitHub Contents write advances main while the Sandbox
	// has an independent local commit. The merge must be validated before push.
	concurrent := func(path string) string {
		t.Helper()
		payload, err := json.Marshal(map[string]string{"message": "Concurrent actual GitHub regression commit", "content": base64.StdEncoding.EncodeToString([]byte("concurrent remote change\n")), "branch": "main"})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, "https://api.github.com/repos/"+managed.FullName+"/contents/"+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+credential.token)
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: time.Minute}).Do(request)
		if err != nil {
			t.Fatal("concurrent GitHub request failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("concurrent GitHub write status=%d", response.StatusCode)
		}
		var value struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil || !flowid.IsCommitSHA(value.Commit.SHA) {
			t.Fatal("concurrent GitHub commit response invalid")
		}
		return value.Commit.SHA
	}
	commit("git-recovery-local-rebase.txt", "before-concurrent-change")
	concurrentHead := concurrent("git-recovery-remote-rebase.txt")
	_, err = publish("rebase-call")
	var known *sandboxd.ToolFailure
	if !errors.As(err, &known) || known.Code() != toolfailure.MainReverificationRequired || known.Effect() != toolfailure.EffectPartial {
		t.Fatalf("divergence must require revalidation: %v", err)
	}
	if remote() != concurrentHead {
		t.Fatal("rebase pushed before revalidation")
	}
	rebased := head()
	result, err = publish("new-call-after-revalidation")
	assertPushed(rebased, result, err)
	if exec(nil, "cat", "git-recovery-local-rebase.txt") == "" || exec(nil, "cat", "git-recovery-remote-rebase.txt") == "" {
		t.Fatal("rebase lost a side's source")
	}
	repository.ExpectedHeadSHA = rebased
	// Discard an actual successful operation's return and reconcile by replay.
	commit("git-recovery-response-loss.txt", "response-loss-call")
	wanted := head()
	_, _ = publish("response-loss-call")
	result, err = publish("response-loss-call")
	assertPushed(wanted, result, err)
	repository.ExpectedHeadSHA = wanted
	advanced := concurrent("git-recovery-fast-forward.txt")
	result, err = publish("remote-only-change")
	assertPushed(advanced, result, err)
	// The admitted token intentionally has no workflow-write permission. A
	// workflow addition receives a real GitHub rejection while main remains
	// unchanged; it must not be disguised as a rebase/revalidation request.
	exec(nil, "mkdir", "-p", ".github/workflows")
	exec(nil, "bash", "-c", `printf '%s\n' 'name: Manual recovery probe' 'on: workflow_dispatch' 'jobs:' '  probe:' '    runs-on: ubuntu-latest' '    steps:' '      - run: true' > .github/workflows/recovery-probe.yml`)
	_, err = publish("rejected-workflow-push")
	if !errors.As(err, &known) || known.Code() != toolfailure.GitPushRejected {
		t.Fatalf("unchanged main must preserve the actual push rejection: %v", err)
	}
	rejectedHead := head()
	_, err = publish("repeat-rejected-workflow-push")
	if !errors.As(err, &known) || known.Code() != toolfailure.GitPushRejected || remote() != advanced || head() != rejectedHead {
		t.Fatalf("rejected push replay must preserve both heads: %v", err)
	}
	t.Logf("actual GitHub rejected workflow write; local=%s remote=%s", rejectedHead, advanced)
	t.Logf("confirmed initial=%s recovered=%s multi=%s rebased=%s lost-response=%s fast-forward=%s", initial, first, multiple, rebased, wanted, advanced)
}
