// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
)

// HeadCheckState is the aggregate GitHub Actions result of one exact head.
// Pending includes a head whose first suite has not yet appeared.
type HeadCheckState struct{ name string }

var (
	HeadChecksPending = HeadCheckState{"pending"}
	HeadChecksPassed  = HeadCheckState{"passed"}
	HeadChecksFailed  = HeadCheckState{"failed"}
)

func (state HeadCheckState) String() string               { return state.name }
func (state HeadCheckState) MarshalText() ([]byte, error) { return []byte(state.name), nil }
func (state *HeadCheckState) UnmarshalText(value []byte) error {
	return comm.UnmarshalName(value, state, HeadCheckState.String, HeadChecksPending, HeadChecksPassed, HeadChecksFailed)
}

// HeadChecks is the bounded result of one head's GitHub Actions suites. A
// failed head gets its job names and output in a separate terminal-only read.
type HeadChecks struct {
	Head           string         `json:"head"`
	State          HeadCheckState `json:"state"`
	SuiteCount     int            `json:"suite_count"`
	Jobs           []string       `json:"jobs,omitempty"`
	Step           string         `json:"step,omitempty"`
	LogTail        string         `json:"log_tail,omitempty"`
	LogUnavailable bool           `json:"log_unavailable,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ActionFailure is bounded evidence from one failed GitHub Actions run. The
// caller applies its Project-scoped redactor before retaining or returning it.
type ActionFailure struct {
	Jobs           []string `json:"jobs,omitempty"`
	Step           string   `json:"step,omitempty"`
	LogTail        string   `json:"log_tail,omitempty"`
	LogUnavailable bool     `json:"log_unavailable,omitempty"`
}

const (
	maximumCheckSuites = 100
	maximumCheckRuns   = 400
	maximumFailedJobs  = 12
	maximumJobLogRead  = 512 << 10
	maximumJobLogTail  = 3 << 10
)

// ReadHeadChecks performs one aggregate check-suites read for an exact head.
// Codecov and review bots can remain queued after Actions has finished, so
// only the library's GitHub Actions suites decide the library CI result.
func (library *Library) ReadHeadChecks(ctx context.Context, head string) (HeadChecks, error) {
	result := HeadChecks{Head: head, State: HeadChecksPending, UpdatedAt: time.Now().UTC()}
	if library.upstream == nil {
		return result, ErrNotConfigured
	}
	if err := validateHead(head); err != nil {
		return result, err
	}
	token, err := library.upstream.checksToken(ctx)
	if err != nil {
		return result, err
	}
	var answer struct {
		TotalCount  int `json:"total_count"`
		CheckSuites []struct {
			HeadSHA    string `json:"head_sha"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			App        struct {
				Slug string `json:"slug"`
			} `json:"app"`
		} `json:"check_suites"`
	}
	path := "/repos/" + library.upstream.FullName + "/commits/" + head + "/check-suites?per_page=100"
	if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &answer); err != nil {
		return result, fmt.Errorf("read the connector head's check suites: %w", classify(err))
	}
	if answer.TotalCount > maximumCheckSuites {
		return result, fmt.Errorf("connector head has more than %d check suites", maximumCheckSuites)
	}
	allPassed := true
	for _, suite := range answer.CheckSuites {
		if suite.App.Slug != "github-actions" || suite.HeadSHA != head {
			continue
		}
		result.SuiteCount++
		if suite.Status != "completed" {
			allPassed = false
			continue
		}
		switch suite.Conclusion {
		case "success", "neutral", "skipped":
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
			result.State = HeadChecksFailed
		case "":
			allPassed = false
		default:
			return result, fmt.Errorf("unknown GitHub Actions conclusion %q", suite.Conclusion)
		}
	}
	if result.State != HeadChecksFailed && result.SuiteCount > 0 && allPassed {
		result.State = HeadChecksPassed
	}
	return result, nil
}

// ReadFailedHeadJobs is called once after a terminal failure. It lists failed
// check runs in bounded pages and reads only the first failed job's output.
func (library *Library) ReadFailedHeadJobs(ctx context.Context, checks HeadChecks) (HeadChecks, error) {
	if library.upstream == nil || checks.State != HeadChecksFailed {
		return checks, errors.New("failed GitHub Actions checks required")
	}
	if err := validateHead(checks.Head); err != nil {
		return checks, err
	}
	token, err := library.upstream.checksToken(ctx)
	if err != nil {
		return checks, err
	}
	type checkRun struct {
		ID         int64     `json:"id"`
		Name       string    `json:"name"`
		Conclusion string    `json:"conclusion"`
		StartedAt  time.Time `json:"started_at"`
		App        struct {
			Slug string `json:"slug"`
		} `json:"app"`
	}
	var failed []checkRun
	for page := 1; page <= maximumCheckRuns/100; page++ {
		var answer struct {
			TotalCount int        `json:"total_count"`
			CheckRuns  []checkRun `json:"check_runs"`
		}
		path := "/repos/" + library.upstream.FullName + "/commits/" + checks.Head + "/check-runs?per_page=100&page=" + strconv.Itoa(page)
		if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &answer); err != nil {
			return checks, fmt.Errorf("read failed connector jobs: %w", classify(err))
		}
		if answer.TotalCount > maximumCheckRuns {
			return checks, fmt.Errorf("connector head has more than %d check runs", maximumCheckRuns)
		}
		for _, run := range answer.CheckRuns {
			if run.App.Slug == "github-actions" && isFailedConclusion(run.Conclusion) {
				failed = append(failed, run)
			}
		}
		if page*100 >= answer.TotalCount {
			break
		}
	}
	sort.Slice(failed, func(i, j int) bool { return failed[i].StartedAt.Before(failed[j].StartedAt) })
	for _, run := range failed {
		if len(checks.Jobs) >= maximumFailedJobs {
			break
		}
		checks.Jobs = append(checks.Jobs, comm.TruncateUTF8(run.Name, 160))
	}
	if len(failed) == 0 {
		return checks, nil
	}
	jobID := failed[0].ID // GitHub Actions check-run ID is the workflow-job ID.
	var job struct {
		Steps []struct {
			Name        string    `json:"name"`
			Conclusion  string    `json:"conclusion"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
		} `json:"steps"`
	}
	path := "/repos/" + library.upstream.FullName + "/actions/jobs/" + strconv.FormatInt(jobID, 10)
	if err = library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &job); err != nil {
		checks.LogUnavailable = true
		return checks, nil
	}
	var firstStarted, firstCompleted time.Time
	for _, step := range job.Steps {
		if isFailedConclusion(step.Conclusion) {
			checks.Step = comm.TruncateUTF8(step.Name, 160)
			firstStarted, firstCompleted = step.StartedAt, step.CompletedAt
			break
		}
	}
	data, err := library.upstream.App.ReadRedirectedTextWithToken(ctx, token, path+"/logs", maximumJobLogRead)
	if err != nil {
		checks.LogUnavailable = true
		return checks, nil
	}
	checks.LogTail = firstFailedStepTail(data, firstStarted, firstCompleted)
	if checks.LogTail == "" {
		checks.LogUnavailable = true
	}
	return checks, nil
}

// readFailedWorkflowJobs reads the failed jobs of one exact workflow run and
// the bounded log tail of its first failed job. It shares the CI reader's
// limits and log parsing instead of creating a second output boundary.
func (library *Library) readFailedWorkflowJobs(ctx context.Context, token githubapp.Token, runID int64) (ActionFailure, error) {
	var failure ActionFailure
	type workflowJob struct {
		ID         int64     `json:"id"`
		Name       string    `json:"name"`
		Conclusion string    `json:"conclusion"`
		StartedAt  time.Time `json:"started_at"`
		Steps      []struct {
			Name        string    `json:"name"`
			Conclusion  string    `json:"conclusion"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
		} `json:"steps"`
	}
	var failed []workflowJob
	for page := 1; page <= maximumCheckRuns/100; page++ {
		var answer struct {
			TotalCount int           `json:"total_count"`
			Jobs       []workflowJob `json:"jobs"`
		}
		path := "/repos/" + library.upstream.FullName + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/jobs?per_page=100&page=" + strconv.Itoa(page)
		if err := library.upstream.App.CallWithToken(ctx, token, http.MethodGet, path, nil, &answer); err != nil {
			return failure, fmt.Errorf("read failed release-candidate jobs: %w", classify(err))
		}
		if answer.TotalCount > maximumCheckRuns {
			return failure, fmt.Errorf("release-candidate run has more than %d jobs", maximumCheckRuns)
		}
		for _, job := range answer.Jobs {
			if isFailedConclusion(job.Conclusion) {
				failed = append(failed, job)
			}
		}
		if page*100 >= answer.TotalCount {
			break
		}
	}
	sort.Slice(failed, func(i, j int) bool { return failed[i].StartedAt.Before(failed[j].StartedAt) })
	for _, job := range failed {
		if len(failure.Jobs) >= maximumFailedJobs {
			break
		}
		failure.Jobs = append(failure.Jobs, comm.TruncateUTF8(job.Name, 160))
	}
	if len(failed) == 0 {
		return failure, nil
	}
	var firstStarted, firstCompleted time.Time
	for _, step := range failed[0].Steps {
		if isFailedConclusion(step.Conclusion) {
			failure.Step = comm.TruncateUTF8(step.Name, 160)
			firstStarted, firstCompleted = step.StartedAt, step.CompletedAt
			break
		}
	}
	path := "/repos/" + library.upstream.FullName + "/actions/jobs/" + strconv.FormatInt(failed[0].ID, 10) + "/logs"
	data, err := library.upstream.App.ReadRedirectedTextWithToken(ctx, token, path, maximumJobLogRead)
	if err != nil {
		failure.LogUnavailable = true
		return failure, nil
	}
	failure.LogTail = firstFailedStepTail(data, firstStarted, firstCompleted)
	if failure.LogTail == "" {
		failure.LogUnavailable = true
	}
	return failure, nil
}

// firstFailedStepTail keeps only timestamped lines within the first failed
// step. If GitHub's ranged response omitted the step, no other step's output
// is mislabeled as its tail.
func firstFailedStepTail(data []byte, started, completed time.Time) string {
	if started.IsZero() || completed.IsZero() {
		return ""
	}
	var selected []byte
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		stamp, _, ok := bytes.Cut(line, []byte{' '})
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, string(stamp))
		if err == nil && !at.Before(started) && !at.After(completed.Add(2*time.Second)) {
			selected = append(selected, line...)
			selected = append(selected, '\n')
		}
	}
	if len(selected) > maximumJobLogTail {
		selected = selected[len(selected)-maximumJobLogTail:]
	}
	return strings.ToValidUTF8(string(selected), "")
}

func isFailedConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return true
	default:
		return false
	}
}

func validateHead(head string) error {
	if len(head) != 40 || strings.Trim(head, "0123456789abcdef") != "" {
		return errors.New("a connector head must be a full lowercase SHA-1")
	}
	return nil
}

// checksToken reuses one narrowly scoped read token across polling rounds.
// A refresh happens only when its expiry approaches, never per round.
func (upstream *Upstream) checksToken(ctx context.Context) (githubapp.Token, error) {
	upstream.checksMu.Lock()
	defer upstream.checksMu.Unlock()
	if upstream.checksReadToken.Value != "" && upstream.checksReadToken.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return upstream.checksReadToken, nil
	}
	_, name, _ := strings.Cut(upstream.FullName, "/")
	// The official connectors library is public. GitHub serves its check
	// suites, workflow jobs and job logs to an authenticated installation with
	// the installation's existing metadata permission; no new App permission
	// or workflow run is requested here.
	token, err := upstream.App.OperationToken(ctx, []string{name}, map[string]string{"metadata": "read"})
	if err != nil {
		return githubapp.Token{}, fmt.Errorf("mint a connector check-read token: %w", classify(err))
	}
	upstream.checksReadToken = token
	return token, nil
}
