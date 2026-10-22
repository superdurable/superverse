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
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// repositoryResponse is the part of a GitHub repository that Superverse reads.
type repositoryResponse struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
}

func (r repositoryResponse) repository() repohost.Repository {
	return repohost.Repository{
		ID: r.ID, FullName: r.FullName, CloneURL: r.CloneURL, DefaultBranch: r.DefaultBranch,
		Description: r.Description, Private: r.Private, Archived: r.Archived,
	}
}

// commitResponse is one entry of a branch's history.
type commitResponse struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message   string `json:"message"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// ReadRepository returns the repository named owner/name.
func (c *Client) ReadRepository(ctx context.Context, fullName string) (repohost.Repository, error) {
	var repository repositoryResponse
	if err := c.repositoryRequest(ctx, http.MethodGet, fullName, "", nil, &repository); err != nil {
		return repohost.Repository{}, err
	}
	return repository.repository(), nil
}

// head returns the commit a branch points to.
func (c *Client) head(ctx context.Context, fullName, branch string) (string, error) {
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := c.repositoryRequest(ctx, http.MethodGet, fullName, "/git/ref/heads/"+url.PathEscape(branch), nil, &result)
	return result.Object.SHA, err
}

// ReadFile reads one repository-relative file at an exact commit.
func (c *Client) ReadFile(ctx context.Context, fullName, commit, path string) ([]byte, error) {
	var file struct{ Encoding, Content string }
	if err := c.repositoryRequest(ctx, http.MethodGet, fullName, "/contents/"+path+"?ref="+url.QueryEscape(commit), nil, &file); err != nil {
		return nil, err
	}
	if file.Encoding != "base64" {
		return nil, errors.New("unsupported repository content encoding")
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
}

// ListCommits discovers branch candidates without filtering the author.
func (c *Client) ListCommits(ctx context.Context, fullName, branch string) ([]repohost.Commit, error) {
	var listed []commitResponse
	if err := c.repositoryRequest(ctx, http.MethodGet, fullName, "/commits?per_page=100&sha="+url.QueryEscape(branch), nil, &listed); err != nil {
		return nil, err
	}
	commits := make([]repohost.Commit, 0, len(listed))
	for _, entry := range listed {
		commits = append(commits, repohost.Commit{SHA: entry.SHA, Message: entry.Commit.Message, CommittedAt: entry.Commit.Committer.Date})
	}
	return commits, nil
}

// ValidateReleaseCommit accepts any commit reachable from branch, regardless of
// its author.
func (c *Client) ValidateReleaseCommit(ctx context.Context, fullName, branch, commit string) error {
	var result struct {
		Status    string `json:"status"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := c.repositoryRequest(ctx, http.MethodGet, fullName, "/compare/"+commit+"..."+url.PathEscape(branch), nil, &result); err != nil {
		return err
	}
	if result.MergeBase.SHA != commit || (result.Status != "identical" && result.Status != "ahead") {
		return repohost.ErrCommitUnreachable
	}
	return nil
}

// IsAncestor compares ancestor as the base with descendant as the head:
// GitHub answers identical for the same commit and ahead when the head has
// the base in its history, with the base as their merge base, and behind or
// diverged otherwise. A commit GitHub does not find is ErrNotFound; so are
// two commits without a common history, for which GitHub has no comparison.
func (c *Client) IsAncestor(ctx context.Context, fullName, ancestor, descendant string) (bool, error) {
	if !flowid.IsCommitSHA(ancestor) || !flowid.IsCommitSHA(descendant) {
		return false, fmt.Errorf("%w: ancestry compares exact commit SHAs", repohost.ErrRejected)
	}
	var result struct {
		Status    string `json:"status"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := c.repositoryRequest(ctx, http.MethodGet, fullName, "/compare/"+ancestor+"..."+descendant+"?per_page=1", nil, &result); err != nil {
		return false, fmt.Errorf("compare %s with %s in %s: %w", ancestor, descendant, fullName, err)
	}
	return result.MergeBase.SHA == ancestor && (result.Status == "identical" || result.Status == "ahead"), nil
}

// describeCommitQuery reads a commit's message and the size of its history,
// which counts every ancestor and the commit itself, in one GraphQL request.
const describeCommitQuery = `query($owner: String!, $name: String!, $oid: GitObjectID!) {
  repository(owner: $owner, name: $name) { object(oid: $oid) { ... on Commit { message history { totalCount } } } }
}`

// DescribeCommit returns the version of a commit: the number of its
// ancestors, itself included, minus one, and its subject.
func (c *Client) DescribeCommit(ctx context.Context, fullName, commit string) (repohost.CommitVersion, error) {
	if _, err := repositoryPath(fullName); err != nil {
		return repohost.CommitVersion{}, err
	}
	owner, name, _ := strings.Cut(fullName, "/")
	request := map[string]any{"query": describeCommitQuery, "variables": map[string]string{"owner": owner, "name": name, "oid": commit}}
	var result struct {
		Data struct {
			Repository *struct {
				Object *struct {
					Message string `json:"message"`
					History struct {
						TotalCount uint64 `json:"totalCount"`
					} `json:"history"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.installationRequest(ctx, http.MethodPost, "/graphql", request, &result); err != nil {
		return repohost.CommitVersion{}, err
	}
	repository := result.Data.Repository
	switch {
	case len(result.Errors) > 0 && result.Errors[0].Type != "NOT_FOUND":
		return repohost.CommitVersion{}, fmt.Errorf("describe commit %s of %s: %s", commit, fullName, result.Errors[0].Message)
	case repository == nil || repository.Object == nil:
		return repohost.CommitVersion{}, fmt.Errorf("commit %s of %s: %w", commit, fullName, repohost.ErrNotFound)
	}
	return repohost.NewCommitVersion(repository.Object.History.TotalCount, repository.Object.Message), nil
}
