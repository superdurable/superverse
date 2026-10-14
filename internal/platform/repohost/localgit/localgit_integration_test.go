// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package localgit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/superdurable/superversev2/internal/app/apptemplate"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/repohost"
)

// These tests run the local Git host as the server does, over a real listener
// and an empty temporary directory, and drive it with the real git command.
// Nothing about the host or git is replaced.

var testSigningKey = []byte("0123456789abcdef0123456789abcdef")

// testProjectName is the name of every test Project; its slug is my-app, so the
// repository of Project ab12 is localkind-my-app-ab12.
const testProjectName = "My App"

// setupIdentity is a setup of a Project named testProjectName.
func setupIdentity(projectID, setupID string) repohost.SetupIdentity {
	return repohost.SetupIdentity{ProjectID: projectID, ProjectName: testProjectName, SetupID: setupID}
}

// startHost serves a host over a loopback listener, as bootstrap does.
func startHost(t *testing.T, dir string) (*Host, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	host, err := New(Config{Dir: dir, CloneBaseURL: base, SigningKey: testSigningKey, RepositoryPrefix: deploymentnames.RepositoryPrefixLocalKind, Environment: deploymentnames.EnvironmentTest}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: host, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return host, base
}

// gitClient runs git against the host with a repository credential, in the form
// the Sandbox sends it: an extra Authorization header scoped to the host.
type gitClient struct {
	t     *testing.T
	home  string
	scope string
	token string
	extra []string
}

func newGitClient(t *testing.T, base, token string) gitClient {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("the local Git host tests need the git command on PATH")
	}
	return gitClient{t: t, home: t.TempDir(), scope: base + "/", token: token}
}

func (g gitClient) run(dir string, arguments ...string) (string, error) {
	g.t.Helper()
	var prefix []string
	if g.token != "" {
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+g.token))
		prefix = []string{"-c", "http." + g.scope + ".extraheader=" + header}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append(append(prefix, g.extra...), arguments...)...)
	command.Dir = dir
	command.Env = append(os.Environ(), "HOME="+g.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (g gitClient) must(dir string, arguments ...string) string {
	g.t.Helper()
	output, err := g.run(dir, arguments...)
	if err != nil {
		g.t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return output
}

// commit writes content to path in the clone and commits it.
func (g gitClient) commit(dir, path, content, message string) string {
	g.t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
	g.must(dir, "add", "--all")
	g.must(dir, "commit", "-m", message)
	return g.must(dir, "rev-parse", "HEAD")
}

// managedRepository creates a Project's repository and commits the template.
func managedRepository(t *testing.T, host *Host, projectID, setupID string) (repohost.Repository, string) {
	t.Helper()
	ctx := context.Background()
	repository, err := host.ManagedRepository(ctx, setupIdentity(projectID, setupID))
	if err != nil {
		t.Fatal(err)
	}
	commit, err := host.InitializeTemplate(ctx, repository, apptemplate.Current().Revision)
	if err != nil {
		t.Fatal(err)
	}
	return repository, commit
}

func credential(t *testing.T, host *Host, repository repohost.Repository) string {
	t.Helper()
	issued, err := host.RepositoryCredential(context.Background(), repository.ID)
	if err != nil {
		t.Fatal(err)
	}
	return issued.Token
}

// TestLocalGitHostTemplateAndRepositories covers the repository identities and
// lifecycle: the managed repository, its IDs, the deterministic template commit,
// reads and archival.
func TestLocalGitHostTemplateAndRepositories(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	host, _ := startHost(t, dir)

	absent, err := host.FindManagedRepository(ctx, setupIdentity("ab12", "setup-1"))
	if err != nil || absent != nil {
		t.Fatalf("a repository that was never created must not be found: %v %v", absent, err)
	}
	if err = host.ArchiveManaged(ctx, "ab12", "setup-1", repositoryOwner+"/localkind-my-app-ab12"); err != nil {
		t.Fatal("archiving an absent repository must succeed:", err)
	}

	first, firstCommit := managedRepository(t, host, "ab12", "setup-1")
	if first.FullName != repositoryOwner+"/localkind-my-app-ab12" || !first.Private || first.Archived || first.DefaultBranch != "main" ||
		first.Description != "superverse-managed:v2:test:ab12:setup-1" || first.ID != 1 {
		t.Fatalf("unexpected managed repository %+v", first)
	}
	again, err := host.ManagedRepository(ctx, setupIdentity("ab12", "setup-1"))
	if err != nil || again != first {
		t.Fatalf("repeating the creation must return the repository: %v %+v", err, again)
	}
	if _, err = host.ManagedRepository(ctx, setupIdentity("ab12", "setup-2")); err == nil {
		t.Fatal("another setup must not take over a managed repository")
	}
	if host.InstallationID() != 1 {
		t.Fatal("the local installation ID is 1")
	}
	// This host reads a repository of another host as absent, so a binding
	// recorded under another installation is refused with the host to run.
	if host.Provider() != repohost.ProviderLocalGit || repohost.CheckInstallation(host, installationID) != nil {
		t.Fatal("the local host is the localgit provider and passes its own installation")
	}
	if err = repohost.CheckInstallation(host, 51234567); !errors.Is(err, repohost.ErrOtherHost) ||
		!strings.Contains(err.Error(), "run the server with the "+repohost.ProviderGitHub.String()+" repository host") {
		t.Fatalf("a binding of another installation must be ErrOtherHost and name GitHub: %v", err)
	}

	// A new host over the same directory is a restart: the same repository, the same
	// commit, and the next repository ID continues the sequence.
	restarted, _ := startHost(t, dir)
	read, err := restarted.ReadRepository(ctx, first.FullName)
	if err != nil || read.ID != first.ID || read.Description != first.Description {
		t.Fatalf("a restarted host must read the repository: %v %+v", err, read)
	}
	replay, err := restarted.InitializeTemplate(ctx, read, apptemplate.Current().Revision)
	if err != nil || replay != firstCommit {
		t.Fatalf("replaying the initialization must return the same commit: %v %s %s", err, replay, firstCommit)
	}
	if _, err = restarted.InitializeTemplate(ctx, read, apptemplate.RevisionPrefixForTest+strings.Repeat("0", 40)); err == nil {
		t.Fatal("a template revision this server does not serve must fail")
	}
	second, secondCommit := managedRepository(t, restarted, "cd34", "setup-3")
	if second.ID != 2 {
		t.Fatalf("the second repository must have ID 2, got %d", second.ID)
	}
	// The template commit has no parent and a fixed author and time, so the same
	// template is the same commit in every repository and every host.
	if secondCommit != firstCommit {
		t.Fatalf("the template commit must be deterministic: %s != %s", secondCommit, firstCommit)
	}
	other, _ := startHost(t, t.TempDir())
	_, otherCommit := managedRepository(t, other, "ef56", "setup-4")
	if otherCommit != firstCommit {
		t.Fatalf("an independent host must create the same commit: %s != %s", otherCommit, firstCommit)
	}

	// An ID is never reused: it follows the sequence file, and without the file the
	// highest repository on disk.
	if err = os.Remove(filepath.Join(dir, sequenceFile)); err != nil {
		t.Fatal(err)
	}
	third, err := restarted.ManagedRepository(ctx, setupIdentity("gh78", "setup-5"))
	if err != nil || third.ID != 3 {
		t.Fatalf("the sequence must be rebuilt from the repositories: %v %+v", err, third)
	}

	// Reads at the template commit.
	contents, err := host.ReadFile(ctx, first.FullName, firstCommit, "dex-app.yaml")
	if err != nil || len(contents) == 0 {
		t.Fatalf("read the template manifest: %v", err)
	}
	if _, err = host.ReadFile(ctx, first.FullName, firstCommit, "missing.txt"); !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("a missing file must be ErrNotFound: %v", err)
	}
	if _, err = host.ReadFile(ctx, first.FullName, strings.Repeat("a", 40), "dex-app.yaml"); !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("a missing commit must be ErrNotFound: %v", err)
	}
	if _, err = host.ReadRepository(ctx, repositoryOwner+"/missing"); !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("a missing repository must be ErrNotFound: %v", err)
	}
	if _, err = host.ReadRepository(ctx, "elsewhere/"+"localkind-my-app-ab12"); !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("another owner has no repository: %v", err)
	}
	commits, err := host.ListCommits(ctx, first.FullName, "main")
	if err != nil || len(commits) != 1 || commits[0].SHA != firstCommit || commits[0].CommittedAt.Year() != 2026 {
		t.Fatalf("list the template commit: %v %+v", err, commits)
	}
	if err = host.ValidateReleaseCommit(ctx, first.FullName, "main", firstCommit); err != nil {
		t.Fatal(err)
	}
	if err = host.ValidateReleaseCommit(ctx, first.FullName, "main", strings.Repeat("b", 40)); !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("an unknown commit must be ErrNotFound: %v", err)
	}

	// The name carries the slug of the Project's name, so another Project name
	// finds nothing under the same Project ID.
	if other, findErr := host.FindManagedRepository(ctx, repohost.SetupIdentity{ProjectID: "ab12", ProjectName: "Newsletter Builder", SetupID: "setup-1"}); findErr != nil || other != nil {
		t.Fatalf("a repository is found by its Project's name and ID: %v %v", other, findErr)
	}
	// A repository that a Project created before its name carried the slug keeps
	// the name its binding recorded: it is read and archived by that name.
	host.store.creating.Lock()
	legacy, err := host.store.create("sv2-test-xy99", repohost.ManagedMarker(deploymentnames.EnvironmentTest, "xy99", "setup-7"), true)
	host.store.creating.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if read, readErr := host.ReadRepository(ctx, legacy.FullName); readErr != nil || read.FullName != repositoryOwner+"/sv2-test-xy99" || read.Archived {
		t.Fatalf("the recorded name reads the repository: %v %+v", readErr, read)
	}
	if err = host.ArchiveManaged(ctx, "xy99", "setup-7", legacy.FullName); err != nil {
		t.Fatal(err)
	}
	if read, readErr := host.ReadRepository(ctx, legacy.FullName); readErr != nil || !read.Archived {
		t.Fatalf("the recorded name archives the repository: %v %+v", readErr, read)
	}

	// Archival keeps the repository readable, is owned by the setup's marker, and
	// replays.
	if err = host.ArchiveManaged(ctx, "ab12", "setup-other", first.FullName); err == nil {
		t.Fatal("another setup must not archive a repository it does not own")
	}
	if err = host.ArchiveManaged(ctx, "ab12", "setup-1", first.FullName); err != nil {
		t.Fatal(err)
	}
	if err = restarted.ArchiveManaged(ctx, "ab12", "setup-1", first.FullName); err != nil {
		t.Fatal("replaying a confirmed archive must succeed:", err)
	}
	archived, err := host.ReadRepository(ctx, first.FullName)
	if err != nil || !archived.Archived {
		t.Fatalf("the repository must read as archived: %v %+v", err, archived)
	}
	if _, err = host.ManagedRepository(ctx, setupIdentity("ab12", "setup-1")); err == nil {
		t.Fatal("an archived repository must not be handed out again")
	}
	if _, err = host.ReadFile(ctx, first.FullName, firstCommit, "dex-app.yaml"); err != nil {
		t.Fatal("an archived repository must stay readable:", err)
	}
}

// TestLocalGitHostSmartHTTPIntegration drives the standard smart HTTP protocol
// with the real git command: clone, fetch, fetch by commit, ls-remote, push of
// thin packs, refusals and credentials.
func TestLocalGitHostSmartHTTPIntegration(t *testing.T) {
	ctx := context.Background()
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	url := repository.CloneURL
	if url != base+"/superverse-local/localkind-my-app-ab12.git" {
		t.Fatalf("unexpected clone URL %s", url)
	}
	alice := newGitClient(t, base, credential(t, host, repository))
	bob := newGitClient(t, base, credential(t, host, repository))

	// ls-remote lists HEAD and the branch at the template commit.
	listing := alice.must("", "ls-remote", url)
	if !strings.Contains(listing, template+"\tHEAD") || !strings.Contains(listing, template+"\trefs/heads/main") {
		t.Fatalf("unexpected ls-remote output:\n%s", listing)
	}
	if out := alice.must("", "ls-remote", "--exit-code", url, "refs/heads/main"); !strings.HasPrefix(out, template) {
		t.Fatalf("unexpected ls-remote of the branch: %s", out)
	}
	if _, err := alice.run("", "ls-remote", "--exit-code", url, "refs/heads/missing"); err == nil {
		t.Fatal("ls-remote --exit-code of a missing branch must exit non-zero")
	}

	// Clone checks out the default branch at the template commit, for each protocol
	// version a client may ask for; the host answers every one with the first.
	aliceDir := filepath.Join(t.TempDir(), "alice")
	for _, version := range []string{"0", "2"} {
		cloneDir := filepath.Join(t.TempDir(), "version-"+version)
		alice.must("", "-c", "protocol.version="+version, "clone", url, cloneDir)
		if head := alice.must(cloneDir, "rev-parse", "HEAD"); head != template {
			t.Fatalf("protocol %s: clone is at %s, not %s", version, head, template)
		}
		if branch := alice.must(cloneDir, "branch", "--show-current"); branch != "main" {
			t.Fatalf("protocol %s: clone is on %q", version, branch)
		}
	}
	alice.must("", "clone", url, aliceDir)
	bobDir := filepath.Join(t.TempDir(), "bob")
	bob.must("", "clone", url, bobDir)

	// Push a change that Git sends as a thin pack: a small edit of a larger file.
	var lines strings.Builder
	for line := range 2000 {
		fmt.Fprintf(&lines, "line %d of a file that is large enough to be sent as a delta\n", line)
	}
	alice.commit(aliceDir, "notes/large.txt", lines.String(), "Add a large file")
	alice.must(aliceDir, "push", "origin", "main")
	content := strings.Replace(lines.String(), "line 1000 ", "LINE 1000 ", 1)
	edited := alice.commit(aliceDir, "notes/large.txt", content, "Edit one line of the large file")
	alice.must(aliceDir, "push", "origin", "main")
	if head, err := host.branchHead(repository.FullName, "main"); err != nil || head != edited {
		t.Fatalf("the host must hold the pushed commit: %v %s", err, head)
	}
	// Two new versions of the file in one push: Git deltas one against the other inside the pack.
	content = strings.Replace(content, "line 500 ", "LINE 500 ", 1)
	alice.commit(aliceDir, "notes/large.txt", content, "Edit the large file again")
	content = strings.Replace(content, "line 1500 ", "LINE 1500 ", 1)
	edited = alice.commit(aliceDir, "notes/large.txt", content, "Edit the large file once more")
	alice.must(aliceDir, "push", "origin", "main")
	verifyDir := filepath.Join(t.TempDir(), "verify")
	alice.must("", "clone", url, verifyDir)
	if head := alice.must(verifyDir, "rev-parse", "HEAD"); head != edited {
		t.Fatalf("a fresh clone must be at the pushed commit %s, got %s", edited, head)
	}
	if output, fsckErr := alice.run(verifyDir, "fsck", "--strict", "--full"); fsckErr != nil {
		t.Fatalf("the pushed repository must be intact: %v\n%s", fsckErr, output)
	}
	if content, err := host.ReadFile(ctx, repository.FullName, edited, "notes/large.txt"); err != nil || !strings.Contains(string(content), "LINE 1000 ") {
		t.Fatalf("the host must serve the pushed file: %v", err)
	}

	// Fetch brings the other clone forward, which negotiates with what it has.
	bob.must(bobDir, "fetch", "origin")
	bob.must(bobDir, "merge", "--ff-only", "origin/main")
	if head := bob.must(bobDir, "rev-parse", "HEAD"); head != edited {
		t.Fatalf("fetch must bring the clone to %s, got %s", edited, head)
	}
	if out := bob.must(bobDir, "fsck", "--strict"); strings.Contains(out, "missing") {
		t.Fatalf("the fetched repository is incomplete:\n%s", out)
	}

	// A fetch from a clone with many local commits sends a request that Git
	// compresses and negotiates over several rounds.
	for i := range 80 {
		bob.commit(bobDir, fmt.Sprintf("local/%d.txt", i), fmt.Sprint(i), fmt.Sprintf("Local commit %d", i))
	}
	next := alice.commit(aliceDir, "notes/next.txt", "next\n", "Another commit")
	alice.must(aliceDir, "push", "origin", "main")
	bob.must(bobDir, "fetch", "origin")
	if remote := bob.must(bobDir, "rev-parse", "origin/main"); remote != next {
		t.Fatalf("fetch with many local commits must reach %s, got %s", next, remote)
	}

	// An exact commit can be fetched into an empty repository.
	exactDir := filepath.Join(t.TempDir(), "exact")
	alice.must("", "init", exactDir)
	alice.must(exactDir, "fetch", url, edited)
	if fetched := alice.must(exactDir, "rev-parse", "FETCH_HEAD"); fetched != edited {
		t.Fatalf("fetching by commit returned %s", fetched)
	}
	alice.must(exactDir, "checkout", "--detach", edited)
	if _, err := os.Stat(filepath.Join(exactDir, "notes/large.txt")); err != nil {
		t.Fatal("the fetched commit must be checked out:", err)
	}
	// Shallow requests are not advertised: a client that asks for depth 1 is refused.
	if output, err := alice.run("", "clone", "--depth=1", url, filepath.Join(t.TempDir(), "shallow")); err == nil || !strings.Contains(output, "shallow") {
		t.Fatalf("a shallow clone must be refused by the client, as the host does not advertise it: %v\n%s", err, output)
	}

	// A stale clone cannot push: Git refuses locally, and a forced push is refused
	// by the host itself, which only moves a branch forward.
	staleDir := filepath.Join(t.TempDir(), "stale")
	alice.must("", "clone", url, staleDir)
	alice.must(staleDir, "reset", "--hard", template)
	alice.commit(staleDir, "diverged.txt", "diverged\n", "Diverge from main")
	if output, err := alice.run(staleDir, "push", "origin", "main"); err == nil || !strings.Contains(output, "rejected") {
		t.Fatalf("a non-fast-forward push must be rejected: %v\n%s", err, output)
	}
	output, err := alice.run(staleDir, "push", "--force", "origin", "main")
	if err == nil || !strings.Contains(output, "non-fast-forward") {
		t.Fatalf("the host must refuse a forced non-fast-forward push: %v\n%s", err, output)
	}
	if head, headErr := host.branchHead(repository.FullName, "main"); headErr != nil || head != next {
		t.Fatalf("a refused push must not move the branch: %v %s", headErr, head)
	}
	if output, err = alice.run(staleDir, "push", "origin", "--delete", "main"); err == nil {
		t.Fatalf("the host must refuse to delete a branch:\n%s", output)
	}
	// Other branches and tags are created, and a tag never moves.
	alice.must(staleDir, "push", "origin", "HEAD:refs/heads/side")
	alice.must(staleDir, "tag", "v1")
	alice.must(staleDir, "push", "origin", "v1")
	if listing = alice.must("", "ls-remote", url); !strings.Contains(listing, "refs/heads/side") || !strings.Contains(listing, "refs/tags/v1") {
		t.Fatalf("the new branch and tag must be listed:\n%s", listing)
	}
	alice.must(staleDir, "tag", "-f", "v1", template)
	if _, err = alice.run(staleDir, "push", "--force", "origin", "v1"); err == nil {
		t.Fatal("a tag must not move")
	}
	// A release commit must lie on the branch: the side branch commit is not on main.
	sideCommit := alice.must(staleDir, "rev-parse", "HEAD")
	if err = host.ValidateReleaseCommit(ctx, repository.FullName, "main", sideCommit); !errors.Is(err, repohost.ErrCommitUnreachable) {
		t.Fatalf("a commit off the branch must be ErrCommitUnreachable: %v", err)
	}
	if err = host.ValidateReleaseCommit(ctx, repository.FullName, "main", edited); err != nil {
		t.Fatal(err)
	}
	listed, err := host.ListCommits(ctx, repository.FullName, "main")
	if err != nil || len(listed) != 6 || listed[0].SHA != next || listed[5].SHA != template {
		t.Fatalf("list commits newest first: %v %+v", err, listed)
	}
	// A commit's version counts its ancestors as git rev-list --count does,
	// the template's root commit V0, with the first line of its message cut at
	// 72 code points.
	for _, commit := range []string{template, edited, next} {
		version, describeErr := host.DescribeCommit(ctx, repository.FullName, commit)
		count := alice.must(staleDir, "rev-list", "--count", commit)
		subject := repohost.NewCommitVersion(1, alice.must(staleDir, "log", "-1", "--format=%s", commit)).Subject
		if describeErr != nil || strconv.FormatUint(version.Number+1, 10) != count || version.Subject != subject {
			t.Fatalf("the version of %s must be V%s - 1 with %q: %v %+v", commit, count, subject, describeErr, version)
		}
	}
	if version, describeErr := host.DescribeCommit(ctx, repository.FullName, template); describeErr != nil || version.Number != 0 {
		t.Fatalf("the template's root commit must be V0: %v %+v", describeErr, version)
	}
	if version, describeErr := host.DescribeCommit(ctx, repository.FullName, next); describeErr != nil || version.Number != 5 {
		t.Fatalf("the sixth commit on main must be V5: %v %+v", describeErr, version)
	}
	if _, describeErr := host.DescribeCommit(ctx, repository.FullName, strings.Repeat("c", 40)); !errors.Is(describeErr, repohost.ErrNotFound) {
		t.Fatalf("an unknown commit must be ErrNotFound: %v", describeErr)
	}
}

// TestLocalGitHostLockWaitsIntegration checks that a slow push does not hold its
// repository's lock while the client sends it, and that archival and template
// initialization stop waiting for the lock when their context ends.
func TestLocalGitHostLockWaitsIntegration(t *testing.T) {
	dir := t.TempDir()
	host, _ := startHost(t, dir)
	repository, template := managedRepository(t, host, "ab12", "setup-1")

	// A push holds the lock: archival and initialization give up with their context.
	release, err := host.store.lock(context.Background(), repository.ID)
	if err != nil {
		t.Fatal(err)
	}
	waited := time.Now()
	short, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err = host.ArchiveManaged(short, "ab12", "setup-1", repository.FullName); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("archival must stop waiting with its context: %v", err)
	}
	if _, err = host.InitializeTemplate(short, repository, apptemplate.Current().Revision); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("template initialization must stop waiting with its context: %v", err)
	}
	if elapsed := time.Since(waited); elapsed > 5*time.Second {
		t.Fatalf("the waits must end with their context, they took %s", elapsed)
	}
	release()
	if commit, initErr := host.InitializeTemplate(context.Background(), repository, apptemplate.Current().Revision); initErr != nil || commit != template {
		t.Fatalf("a released lock must be taken again: %v %s", initErr, commit)
	}

	// A client that sends the pack slowly does not hold the lock: archival goes
	// ahead, and the push then finds the repository archived.
	token := credential(t, host, repository)
	body, writer := io.Pipe()
	answered := make(chan int, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodPost, repository.CloneURL+"/git-receive-pack", body)
		if requestErr != nil {
			answered <- 0
			return
		}
		request.SetBasicAuth(gitUsername, token)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			answered <- 0
			return
		}
		_ = response.Body.Close()
		answered <- response.StatusCode
	}()
	var commands bytes.Buffer
	encoder := pktline.NewEncoder(&commands)
	if err = encoder.EncodeString(strings.Repeat("0", 40) + " " + template + " refs/heads/slow\x00report-status\n"); err != nil {
		t.Fatal(err)
	}
	if err = encoder.Flush(); err != nil {
		t.Fatal(err)
	}
	empty := pack()
	if _, err = writer.Write(append(commands.Bytes(), empty[:12]...)); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		receiving, globErr := filepath.Glob(filepath.Join(dir, receivingPrefix+"*"))
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(receiving) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never started to receive the pack")
		}
	}
	archiving, cancelArchive := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelArchive()
	if err = host.ArchiveManaged(archiving, "ab12", "setup-1", repository.FullName); err != nil {
		t.Fatal("archival must not wait for a push that is still being sent:", err)
	}
	if _, err = writer.Write(empty[12:]); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if status := <-answered; status != http.StatusForbidden {
		t.Fatalf("the push must find the repository archived, got status %d", status)
	}
	if receiving, _ := filepath.Glob(filepath.Join(dir, receivingPrefix+"*")); len(receiving) != 0 {
		t.Fatalf("the received pack must be removed: %v", receiving)
	}
}

// TestLocalGitHostConcurrentPushesIntegration races pushes of different commits
// onto one branch: the host keeps exactly one.
func TestLocalGitHostConcurrentPushesIntegration(t *testing.T) {
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)
	const pushers = 6
	clients := make([]gitClient, pushers)
	directories := make([]string, pushers)
	commits := make([]string, pushers)
	for i := range pushers {
		clients[i] = newGitClient(t, base, token)
		directories[i] = filepath.Join(t.TempDir(), fmt.Sprint(i))
		clients[i].must("", "clone", repository.CloneURL, directories[i])
		commits[i] = clients[i].commit(directories[i], "racer.txt", fmt.Sprint(i), fmt.Sprintf("Racer %d", i))
	}
	var group sync.WaitGroup
	results := make([]error, pushers)
	for i := range pushers {
		group.Add(1)
		go func() {
			defer group.Done()
			_, results[i] = clients[i].run(directories[i], "push", "--force", "origin", "main")
		}()
	}
	group.Wait()
	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		}
	}
	head, err := host.branchHead(repository.FullName, "main")
	if err != nil || winners != 1 {
		t.Fatalf("exactly one concurrent push must win, %d did (head %s, template %s): %v", winners, head, template, err)
	}
	won := false
	for i, err := range results {
		if err == nil && commits[i] == head {
			won = true
		}
	}
	if !won {
		t.Fatalf("the branch must point to the winner's commit, not %s", head)
	}
}

// TestLocalGitHostCredentialsIntegration checks who may do what: scoped,
// expiring credentials, the archived repository and the administrator's JSON
// interface.
func TestLocalGitHostCredentialsIntegration(t *testing.T) {
	ctx := context.Background()
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	elsewhere, _ := managedRepository(t, host, "cd34", "setup-2")
	url := repository.CloneURL
	valid := credential(t, host, repository)

	// refused asks for the ref discovery of the repository with the credential: the
	// host answers the status, and git, which answers a 401 by asking for a user
	// name with prompts off, fails.
	refused := func(name, token string, status int) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/info/refs?service=git-upload-pack", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			request.SetBasicAuth("x-access-token", token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s must be answered %d, got %d", name, status, response.StatusCode)
		}
		if output, gitErr := newGitClient(t, base, token).run("", "ls-remote", url); gitErr == nil {
			t.Fatalf("git must fail with %s:\n%s", name, output)
		}
	}
	refused("a request without a credential", "", http.StatusUnauthorized)
	refused("a tampered credential", valid[:len(valid)-2]+"xx", http.StatusUnauthorized)
	refused("an expired credential", host.signer.mint(tokenClaims{Scope: scopeRepository, RepositoryID: repository.ID, ExpiresAt: time.Now().Add(-time.Minute).Unix()}), http.StatusUnauthorized)
	refused("a credential signed with another key", tokenSigner{key: []byte("another key another key another!")}.mint(tokenClaims{Scope: scopeRepository, RepositoryID: repository.ID, ExpiresAt: time.Now().Add(time.Hour).Unix()}), http.StatusUnauthorized)
	refused("a credential of another repository", credential(t, host, elsewhere), http.StatusForbidden)
	refused("an administrator token for Git", host.signer.mint(tokenClaims{Scope: scopeAdministrator, ExpiresAt: time.Now().Add(time.Hour).Unix()}), http.StatusForbidden)

	// A client without a header gets a challenge and answers it through a credential
	// helper, as the host builder does.
	helper := newGitClient(t, base, "")
	helper.extra = []string{"-c", "credential.helper=", "-c", `credential.helper=!f() { echo username=x-access-token; echo password=` + valid + `; }; f`}
	if out := helper.must("", "ls-remote", url); !strings.Contains(out, template) {
		t.Fatalf("a credential helper must authenticate:\n%s", out)
	}
	// The host builder fetches the exact commit of a Release this way, into a new repository.
	buildDir := filepath.Join(t.TempDir(), "build")
	helper.must("", "init", buildDir)
	helper.must(buildDir, "remote", "add", "origin", url)
	helper.must(buildDir, "fetch", "origin", template)
	helper.must(buildDir, "checkout", "--detach", template)
	if head := helper.must(buildDir, "rev-parse", "HEAD"); head != template {
		t.Fatalf("the builder's checkout is at %s, not %s", head, template)
	}

	// The administrator's interface.
	administrator := host.signer.mint(tokenClaims{Scope: scopeAdministrator, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	get := func(token, path string) (int, []byte) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+apiPrefix+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, body
	}
	if status, _ := get("", "repositories/"+repository.FullName); status != http.StatusUnauthorized {
		t.Fatalf("the JSON interface must require a token, got %d", status)
	}
	if status, _ := get(valid, "repositories/"+repository.FullName); status != http.StatusForbidden {
		t.Fatalf("a repository token must not open the JSON interface, got %d", status)
	}
	status, body := get(administrator, "repositories/"+repository.FullName+"/branch?name=main")
	var branch struct{ Name, SHA string }
	if err := json.Unmarshal(body, &branch); status != http.StatusOK || err != nil || branch.SHA != template || branch.Name != "main" {
		t.Fatalf("branch head: %d %s", status, body)
	}
	status, body = get(administrator, "repositories/"+repository.FullName+"/file?ref="+template+"&path=dex-app.yaml")
	var file struct{ Path, Content string }
	if err := json.Unmarshal(body, &file); status != http.StatusOK || err != nil || file.Path != "dex-app.yaml" {
		t.Fatalf("file: %d %s", status, body)
	}
	if decoded, err := base64.StdEncoding.DecodeString(file.Content); err != nil || len(decoded) == 0 || !flowid.IsCommitSHA(template) {
		t.Fatalf("file content: %v", err)
	}
	if status, _ = get(administrator, "repositories/"+repository.FullName+"/file?ref="+template+"&path=missing"); status != http.StatusNotFound {
		t.Fatalf("a missing file must be 404, got %d", status)
	}
	// A branch that does not exist is not found, and a name Git does not allow is a
	// bad request, both answered without disturbing the host.
	for path, want := range map[string]int{
		"/branch?name=missing": http.StatusNotFound, "/branch?name=a..b": http.StatusBadRequest, "/branch?name=": http.StatusBadRequest,
		"/file?ref=missing&path=dex-app.yaml": http.StatusNotFound, "/file?ref=a..b&path=dex-app.yaml": http.StatusBadRequest,
	} {
		if status, body = get(administrator, "repositories/"+repository.FullName+path); status != want {
			t.Fatalf("%s must be answered %d, got %d: %s", path, want, status, body)
		}
	}
	if _, err := host.branchHead(repository.FullName, "missing"); !errors.Is(err, repohost.ErrNotFound) || errors.Is(err, errInvalidBranch) {
		t.Fatalf("a missing branch must be ErrNotFound: %v", err)
	}
	if _, err := host.branchHead(repository.FullName, "a..b"); !errors.Is(err, errInvalidBranch) || !errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("an invalid branch name must be errInvalidBranch and ErrNotFound: %v", err)
	}
	status, body = get(administrator, "repositories/"+repository.FullName)
	var described struct {
		ID       int64
		Archived bool
		CloneURL string
	}
	if err := json.Unmarshal(body, &described); status != http.StatusOK || err != nil || described.ID != repository.ID || described.CloneURL != url {
		t.Fatalf("repository: %d %s", status, body)
	}

	// An archived repository refuses pushes and still serves reads.
	client := newGitClient(t, base, valid)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", url, cloneDir)
	client.commit(cloneDir, "after.txt", "x\n", "Commit before archival")
	client.must(cloneDir, "push", "origin", "main")
	if err := host.ArchiveManaged(ctx, "ab12", "setup-1", repository.FullName); err != nil {
		t.Fatal(err)
	}
	client.commit(cloneDir, "later.txt", "y\n", "Commit after archival")
	if output, err := client.run(cloneDir, "push", "origin", "main"); err == nil || !strings.Contains(output, "403") {
		t.Fatalf("an archived repository must refuse pushes: %v\n%s", err, output)
	}
	client.must(cloneDir, "fetch", "origin")
	client.must("", "ls-remote", url)
	client.must("", "clone", url, filepath.Join(t.TempDir(), "after"))
}
