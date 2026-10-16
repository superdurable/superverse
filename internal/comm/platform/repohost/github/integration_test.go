// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package github

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// This opt-in test creates only a tagged V2 test repository and archives it on completion.
func TestApplicationTemplateGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	owner := os.Getenv("SUPERVERSE_TEST_GITHUB_OWNER")
	if owner == "" {
		t.Fatal("explicit GitHub owner is required")
	}
	client := newTestClient(t, owner, deploymentnames.EnvironmentTest)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	projectID := flowid.RandomID(4)
	setupID := flowid.RandomID(8)
	setup := repohost.SetupIdentity{ProjectID: projectID, ProjectName: "Template", SetupID: setupID}
	name := repohost.ManagedName(deploymentnames.RepositoryPrefixLocalKind, setup.ProjectName, projectID)
	t.Logf("project=%s setup=%s repository=%s/%s", projectID, setupID, owner, name)
	// Project deletion archives a repository that may be gone already.
	if err := client.ArchiveManaged(ctx, projectID, setupID, owner+"/"+name); err != nil {
		t.Fatal("archiving an absent managed repository failed", err)
	}
	repository, err := client.ManagedRepository(ctx, setup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if archiveErr := client.ArchiveManaged(cleanup, projectID, setupID, repository.FullName); archiveErr != nil {
			t.Errorf("V2 repository archive incomplete: %v", archiveErr)
		}
	}()
	// A retried Step finds the repository its first attempt created.
	again, err := client.ManagedRepository(ctx, setup)
	if err != nil || again.ID != repository.ID {
		t.Fatal("repeated managed repository creation changed its identity", err)
	}
	template := apptemplate.Current()
	revision := template.Revision
	if _, err = client.InitializeTemplate(ctx, repository, apptemplate.RevisionPrefixForTest+strings.Repeat("0", 40), apptemplate.Current()); err == nil {
		t.Fatal("a revision this server does not serve must fail before any upload")
	}
	commit, err := client.InitializeTemplate(ctx, repository, revision, apptemplate.Current())
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTestClient(t, owner, deploymentnames.EnvironmentTest)
	repeated, err := restarted.InitializeTemplate(ctx, repository, revision, apptemplate.Current())
	if err != nil || commit != repeated {
		t.Fatal("initialization replay changed accepted commit", err)
	}
	verifyApplicationTemplateGitHubTree(t, ctx, restarted, repository, commit, template)
	if err = client.ValidateReleaseCommit(ctx, repository.FullName, "main", commit); err != nil {
		t.Fatal(err)
	}
	// The template's root commit is V0, with its subject, from one GraphQL query.
	if version, describeErr := client.DescribeCommit(ctx, repository.FullName, commit); describeErr != nil || version.Number != 0 || version.Subject == "" {
		t.Fatalf("the template's root commit must be V0 with a subject: %v %+v", describeErr, version)
	}
	if _, describeErr := client.DescribeCommit(ctx, repository.FullName, strings.Repeat("c", 40)); !errors.Is(describeErr, repohost.ErrNotFound) {
		t.Fatalf("an unknown commit must be ErrNotFound: %v", describeErr)
	}
	contents, err := client.ReadFile(ctx, repository.FullName, commit, "dex-app.yaml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := appmanifest.ExtractAppManifest(commit, contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Connectors) != 1 || manifest.Environment.AsMap()["port"] != float64(8080) {
		t.Fatal("unexpected application template manifest")
	}
	token, err := client.RepositoryCredential(ctx, repository.ID)
	if err != nil || token.Token == "" {
		t.Fatal("scoped repository credential failed", err)
	}
	t.Logf("repositoryID=%d commit=%s manifest=%s template=%s replay=same-commit", repository.ID, commit, manifest.ManifestDigest, revision)
	if err = client.ArchiveManaged(ctx, projectID, setupID, repository.FullName); err != nil {
		t.Fatal("managed repository archive failed", err)
	}
	archived, err := restarted.ReadRepository(ctx, repository.FullName)
	if err != nil || !archived.Archived {
		t.Fatal("GitHub did not confirm the archived repository", err)
	}
	if err = restarted.ArchiveManaged(ctx, projectID, setupID, repository.FullName); err != nil {
		t.Fatal("fresh-client replay of a confirmed archive failed", err)
	}
	t.Log("archive=confirmed fresh-client replay=already-archived")
}

// Compare the actual remote tree with every embedded template blob. No GitHub
// responses are replaced by fixtures.
func verifyApplicationTemplateGitHubTree(t *testing.T, ctx context.Context, client *Client, repository repohost.Repository, commit string, template *repohost.Template) {
	t.Helper()
	var result struct {
		Tree      []repohost.TreeEntry
		Truncated bool
	}
	if err := client.repositoryRequest(ctx, http.MethodGet, repository.FullName, "/git/trees/"+commit+"?recursive=1", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Truncated {
		t.Fatal("GitHub tree was truncated")
	}
	expected := map[string]repohost.TreeEntry{}
	for _, file := range template.Files {
		expected[file.Path] = file.TreeEntry()
	}
	actual := map[string]repohost.TreeEntry{}
	for _, entry := range result.Tree {
		if entry.Type != apptemplate.ObjectTypeTreeForTest {
			actual[entry.Path] = entry
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("managed Git tree differs from the embedded application template")
	}
	t.Logf("template=%s exact remote blobs=%d fresh-client replay=same-commit", template.Revision, len(actual))
}

// newTestClient acts as the GitHub App installation the test environment
// configures. Missing or invalid credentials fail the test.
func newTestClient(t *testing.T, owner string, environment deploymentnames.Environment) *Client {
	t.Helper()
	app, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub App ID unavailable")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_INSTALLATION_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub installation ID unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil {
		t.Fatal("GitHub key encoding invalid")
	}
	key, err := githubapp.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("GitHub key is unavailable")
	}
	return &Client{App: &githubapp.App{AppID: app, InstallationID: installation, PrivateKey: key}, Owner: owner, RepositoryPrefix: deploymentnames.RepositoryPrefixLocalKind, Environment: environment}
}
