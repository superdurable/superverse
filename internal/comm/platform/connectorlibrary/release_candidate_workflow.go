// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
)

const releaseCandidateWorkflow = "release-candidate.yml"

// ReleaseCandidateRunState is the observed state of the exact workflow run
// correlated by the library's run-name contract.
type ReleaseCandidateRunState struct{ name string }

var (
	ReleaseCandidateRunMissing   = ReleaseCandidateRunState{"missing"}
	ReleaseCandidateRunPending   = ReleaseCandidateRunState{"pending"}
	ReleaseCandidateRunSucceeded = ReleaseCandidateRunState{"succeeded"}
	ReleaseCandidateRunFailed    = ReleaseCandidateRunState{"failed"}
)

func (state ReleaseCandidateRunState) String() string               { return state.name }
func (state ReleaseCandidateRunState) MarshalText() ([]byte, error) { return []byte(state.name), nil }
func (state *ReleaseCandidateRunState) UnmarshalText(value []byte) error {
	return comm.UnmarshalName(value, state, ReleaseCandidateRunState.String,
		ReleaseCandidateRunMissing, ReleaseCandidateRunPending, ReleaseCandidateRunSucceeded, ReleaseCandidateRunFailed)
}

// ReleaseCandidateRun is the bounded status and failure evidence of the
// workflow run for one exact candidate.
type ReleaseCandidateRun struct {
	ID        int64                    `json:"-"`
	Name      string                   `json:"name"`
	State     ReleaseCandidateRunState `json:"state"`
	Failure   ActionFailure            `json:"failure,omitzero"`
	UpdatedAt time.Time                `json:"updated_at,omitempty"`
}

// ReleaseCandidateBlockers are facts that make dispatch pointless. A released
// stable version is terminal; a known conflicting pull request is surfaced in
// the same failure so the agent knows its CI cannot run.
type ReleaseCandidateBlockers struct {
	StableTag           string `json:"stable_tag,omitempty"`
	PullRequestNumber   int64  `json:"pull_request_number,omitempty"`
	PullRequestConflict bool   `json:"pull_request_conflict,omitempty"`
}

// ReadReleaseCandidateRun finds the repository_dispatch run whose exact
// display title is the workflow's run-name contract. No run is a normal
// observation while GitHub schedules the dispatch.
func (library *Library) ReadReleaseCandidateRun(ctx context.Context, candidate ReleaseCandidate, location Location, createdAfter time.Time) (ReleaseCandidateRun, error) {
	name := releaseCandidateRunName(candidate)
	result := ReleaseCandidateRun{Name: name, State: ReleaseCandidateRunMissing}
	if location != LocationUpstream || library.upstream == nil {
		return result, ErrNotConfigured
	}
	token, err := library.upstream.checksToken(ctx)
	if err != nil {
		return result, err
	}
	var answer struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	query := "?event=repository_dispatch&per_page=100"
	if !createdAfter.IsZero() {
		query += "&created=" + url.QueryEscape(">="+createdAfter.UTC().Format(time.RFC3339))
	}
	path := "/repos/" + library.upstream.FullName + "/actions/workflows/" + releaseCandidateWorkflow + "/runs" + query
	if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &answer); err != nil {
		return result, fmt.Errorf("read release-candidate workflow runs: %w", classify(err))
	}
	for _, run := range answer.Runs {
		if run.DisplayTitle != name || run.Event != "repository_dispatch" {
			continue
		}
		return library.releaseCandidateRun(ctx, token, run)
	}
	return result, nil
}

// readReleaseCandidateRunByID reads an immutable historical run. Production
// correlation uses ReadReleaseCandidateRun; this boundary keeps live evidence
// for runs from before the run-name contract was merged.
func (library *Library) readReleaseCandidateRunByID(ctx context.Context, id int64) (ReleaseCandidateRun, error) {
	if library.upstream == nil || id <= 0 {
		return ReleaseCandidateRun{}, ErrNotConfigured
	}
	token, err := library.upstream.checksToken(ctx)
	if err != nil {
		return ReleaseCandidateRun{}, err
	}
	var run workflowRun
	path := "/repos/" + library.upstream.FullName + "/actions/runs/" + strconv.FormatInt(id, 10)
	if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &run); err != nil {
		return ReleaseCandidateRun{}, fmt.Errorf("read release-candidate workflow run %d: %w", id, classify(err))
	}
	return library.releaseCandidateRun(ctx, token, run)
}

type workflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (library *Library) releaseCandidateRun(ctx context.Context, token githubapp.Token, run workflowRun) (ReleaseCandidateRun, error) {
	result := ReleaseCandidateRun{ID: run.ID, Name: run.DisplayTitle, State: ReleaseCandidateRunPending, UpdatedAt: run.UpdatedAt}
	if run.Status != "completed" {
		return result, nil
	}
	if run.Conclusion == "success" {
		result.State = ReleaseCandidateRunSucceeded
		return result, nil
	}
	result.State = ReleaseCandidateRunFailed
	failure, err := library.readFailedWorkflowJobs(ctx, token, run.ID)
	if err != nil {
		failure.LogUnavailable = true
	}
	result.Failure = failure
	return result, nil
}

// ReadReleaseCandidateBlockers checks the stable release tag and the current
// pull request mergeability before a repository_dispatch mutation.
func (library *Library) ReadReleaseCandidateBlockers(ctx context.Context, candidate ReleaseCandidate, pullRequestNumber int64, location Location) (ReleaseCandidateBlockers, error) {
	var blockers ReleaseCandidateBlockers
	if location != LocationUpstream || library.upstream == nil {
		return blockers, nil
	}
	token, err := library.upstream.checksToken(ctx)
	if err != nil {
		return blockers, err
	}
	rcIndex := strings.LastIndex(candidate.Version, "-rc.")
	if rcIndex < 0 {
		return blockers, errors.New("a release candidate version must end in -rc.N")
	}
	if number, parseErr := strconv.Atoi(candidate.Version[rcIndex+4:]); parseErr != nil || number < 1 {
		return blockers, errors.New("a release candidate version must end in -rc.N")
	}
	stable := candidate.Version[:rcIndex]
	blockers.StableTag = candidate.Directory + "/" + stable
	var tag struct {
		Ref string `json:"ref"`
	}
	path := "/repos/" + library.upstream.FullName + "/git/ref/tags/" + escapeTag(blockers.StableTag)
	if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &tag); isNotFound(err) {
		blockers.StableTag = ""
	} else if err != nil {
		return blockers, fmt.Errorf("read the stable connector tag %s: %w", blockers.StableTag, classify(err))
	}
	if pullRequestNumber <= 0 {
		return blockers, nil
	}
	pullToken, err := library.upstream.pullRequestToken(ctx)
	if err != nil {
		return blockers, err
	}
	owner, name, _ := strings.Cut(library.upstream.FullName, "/")
	request := map[string]any{"query": `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { number mergeable } }
}`, "variables": map[string]any{"owner": owner, "name": name, "number": pullRequestNumber}}
	var answer struct {
		Data struct {
			Repository *struct {
				PullRequest *struct {
					Number    int64  `json:"number"`
					Mergeable string `json:"mergeable"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err = library.upstream.App.CallWithToken(ctx, pullToken, http.MethodPost, "/graphql", request, &answer); err != nil {
		return blockers, fmt.Errorf("read connector pull request %d: %w", pullRequestNumber, classify(err))
	}
	if len(answer.Errors) > 0 {
		return blockers, fmt.Errorf("read connector pull request %d: %s", pullRequestNumber, comm.TruncateUTF8(answer.Errors[0].Message, 256))
	}
	if answer.Data.Repository == nil || answer.Data.Repository.PullRequest == nil {
		return blockers, fmt.Errorf("connector pull request %d was not found", pullRequestNumber)
	}
	pull := answer.Data.Repository.PullRequest
	if pull.Mergeable == "UNKNOWN" {
		return blockers, fmt.Errorf("connector pull request %d mergeability is not ready", pullRequestNumber)
	}
	blockers.PullRequestNumber = pull.Number
	blockers.PullRequestConflict = pull.Mergeable == "CONFLICTING"
	return blockers, nil
}

func releaseCandidateRunName(candidate ReleaseCandidate) string {
	return "RC " + candidate.Directory + " " + candidate.Version + " " + candidate.Commit
}
