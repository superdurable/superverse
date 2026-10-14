// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// summaryMarker starts the one line on stdout that holds summary.json.
const summaryMarker = "SUPERVERSE-APPTEST-SUMMARY "

// Bounds of the summary: it is one line the commit gate reads whole.
const (
	maximumSummaryBytes = 64 << 10
	// maximumStateReads bounds the executions whose state the summary reads.
	maximumStateReads = 25
	// maximumExcerpts and maximumTestFailures bound the listed problems.
	maximumExcerpts     = 5
	maximumTestFailures = 6
	maximumExcerptBytes = 1 << 10
	// unhealthyAttempts is the attempt count from which a failing Step is unhealthy.
	unhealthyAttempts = 3
)

// sdkUsageErrors are the Dex Go SDK's error types for state an invocation did
// not load, which no retry repairs.
var sdkUsageErrors = []string{"*dex.AttributeMapNotLoadedError", "*dex.ChannelMessagesNotLoadedError"}

// testRunSummary is summary.json and the summary line: the outcome of the
// command, the executions of the engine, the tests the harness recorded and
// the calls the connector mocks recorded. README.md specifies it.
type testRunSummary struct {
	RunID       string       `json:"runId"`
	Directory   string       `json:"directory"`
	Kept        bool         `json:"kept"`
	Phase       testRunPhase `json:"phase"`
	ExitCode    int          `json:"exitCode"`
	TimedOut    bool         `json:"timedOut,omitempty"`
	DurationMS  int64        `json:"durationMs"`
	EngineError string       `json:"engineError,omitempty"`
	// Executions counts the executions by Flow type and status.
	Executions map[string]map[string]int `json:"executions,omitempty"`
	// ExecutionsError is set when the executions could not be read.
	ExecutionsError string `json:"executionsError,omitempty"`
	// Unhealthy lists executions whose active Step failed at least three
	// attempts or failed with an SDK usage error; UnhealthyTotal counts all.
	Unhealthy      []executionExcerpt `json:"unhealthy,omitempty"`
	UnhealthyTotal int                `json:"unhealthyTotal"`
	// Unchecked counts running executions whose state was not read.
	Unchecked      int               `json:"unchecked,omitempty"`
	Tests          []recordedTest    `json:"tests"`
	TestFailures   []testFailure     `json:"testFailures,omitempty"`
	ConnectorCalls []json.RawMessage `json:"connectorCalls,omitempty"`
}

// executionExcerpt is one problem execution, bounded.
type executionExcerpt struct {
	FlowID     string `json:"flowId"`
	FlowType   string `json:"flowType"`
	Status     string `json:"status"`
	Step       string `json:"step,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	UsageError bool   `json:"usageError,omitempty"`
	Failure    string `json:"failure,omitempty"`
}

// recordedTest is one entry of the harness's tests.json.
type recordedTest struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
}

// testFailure is one failing test and the end of what go test printed for it.
type testFailure struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

// collectRunSummary reads the engine's executions, the reports and the
// failing tests of the output, before the engine stops.
func collectRunSummary(ctx context.Context, run testRun, summary *testRunSummary) {
	collectExecutions(ctx, summary)
	var tests struct {
		Tests []recordedTest `json:"tests"`
	}
	if err := supervision.ReadJSON(run.reportDirectory+"/tests.json", &tests); err == nil {
		summary.Tests = tests.Tests
	}
	var calls struct {
		Calls []json.RawMessage `json:"calls"`
	}
	if err := supervision.ReadJSON(run.reportDirectory+"/connector-calls.json", &calls); err == nil {
		summary.ConnectorCalls = calls.Calls
	}
	summary.TestFailures = failingTests(run.path("output.log"))
}

// collectExecutions counts the engine's executions and checks the health of
// the running ones through the real dexcli.
func collectExecutions(ctx context.Context, summary *testRunSummary) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	output, err := runEngineRead(ctx, "flow", "search", "--server", testDexAddress, "--all", "--no-hydrate", "--page-size", "100", "--timeout", "20s")
	var search struct {
		Flows []struct {
			FlowID     string `json:"flowId"`
			FlowType   string `json:"flowType"`
			FlowStatus string `json:"flowStatus"`
		} `json:"flows"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(output), &search)
	}
	if err != nil {
		summary.ExecutionsError = truncate(err.Error(), maximumExcerptBytes)
		return
	}
	summary.Executions = map[string]map[string]int{}
	reads := 0
	for _, flow := range search.Flows {
		status := strings.ToLower(strings.TrimPrefix(flow.FlowStatus, "FLOW_STATUS_"))
		if status == "" {
			status = "unspecified"
		}
		if summary.Executions[flow.FlowType] == nil {
			summary.Executions[flow.FlowType] = map[string]int{}
		}
		summary.Executions[flow.FlowType][status]++
		if status != "running" {
			continue
		}
		if reads == maximumStateReads {
			summary.Unchecked++
			continue
		}
		reads++
		if excerpt, unhealthy := readExecutionHealth(ctx, flow.FlowID, flow.FlowType); unhealthy {
			summary.UnhealthyTotal++
			if len(summary.Unhealthy) < maximumExcerpts {
				summary.Unhealthy = append(summary.Unhealthy, excerpt)
			}
		}
	}
}

// readExecutionHealth reads the active Steps of a running execution: one that
// failed at least three attempts, or whose last failure is an SDK usage error
// or a panic, makes it unhealthy. A failed read is an unhealthy excerpt too:
// the summary cannot vouch for the execution.
func readExecutionHealth(ctx context.Context, flowID, flowType string) (executionExcerpt, bool) {
	excerpt := executionExcerpt{FlowID: flowID, FlowType: flowType, Status: "running"}
	output, err := runEngineRead(ctx, "flow", "state", flowID, "--server", testDexAddress, "--no-hydrate", "--timeout", "10s")
	var state struct {
		ActiveStepExecutions []struct {
			StepType        string `json:"stepType"`
			LastFailureInfo *struct {
				BackendError string `json:"backendError"`
				Attempt      int    `json:"attempt"`
				Details      *struct {
					Detail     string `json:"detail"`
					ErrorType  string `json:"originalWorkerErrorType"`
					StackTrace string `json:"originalWorkerErrorStackTrace"`
				} `json:"details"`
			} `json:"lastFailureInfo"`
		} `json:"activeStepExecutions"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(output), &state)
	}
	if err != nil {
		excerpt.Failure = truncate("the state could not be read: "+err.Error(), maximumExcerptBytes)
		return excerpt, true
	}
	for _, step := range state.ActiveStepExecutions {
		failure := step.LastFailureInfo
		if failure == nil {
			continue
		}
		detail, usageError := failure.BackendError, false
		if failure.Details != nil {
			detail = failure.Details.Detail
			usageError = slices.Contains(sdkUsageErrors, failure.Details.ErrorType) || strings.HasPrefix(failure.Details.StackTrace, "panic: ")
		}
		if failure.Attempt >= unhealthyAttempts || usageError {
			excerpt.Step, excerpt.Attempt, excerpt.UsageError = step.StepType, failure.Attempt, usageError
			excerpt.Failure = truncate(detail, maximumExcerptBytes)
			return excerpt, true
		}
	}
	return excerpt, false
}

// runEngineRead runs one read of the real dexcli against the test engine and
// returns its stdout, the JSON answer.
func runEngineRead(ctx context.Context, arguments ...string) (string, error) {
	var stderr strings.Builder
	command := exec.CommandContext(ctx, realBinaries+"/dexcli", arguments...) //nolint:gosec // fixed read subcommands of the real dexcli
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("dexcli %s: %w: %s", strings.Join(arguments[:2], " "), err, truncate(strings.TrimSpace(stderr.String()), maximumExcerptBytes))
	}
	return string(output), nil
}

// failingTests reads the "--- FAIL: <name>" blocks that go test printed, each
// with its indented lines, bounded.
func failingTests(path string) []testFailure {
	file, err := os.Open(path) //nolint:gosec // the run's own log
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	var failures []testFailure
	var current *testFailure
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " ")
		if name, found := strings.CutPrefix(trimmed, "--- FAIL: "); found {
			name, _, _ = strings.Cut(name, " ")
			if len(failures) == maximumTestFailures {
				break
			}
			failures = append(failures, testFailure{Name: name})
			current = &failures[len(failures)-1]
			continue
		}
		if current != nil && strings.HasPrefix(line, "    ") && len(current.Output) < maximumExcerptBytes {
			current.Output = truncate(current.Output+strings.TrimSpace(line)+"\n", maximumExcerptBytes)
			continue
		}
		current = nil
	}
	for index := range failures {
		failures[index].Output = strings.TrimSpace(failures[index].Output)
	}
	return failures
}

// writeSummary writes summary.json and prints it as the summary line, both
// within 64 KiB: the listed problems shrink first, then the test names.
func writeSummary(run testRun, summary *testRunSummary) {
	data := mustEncode(json.Marshal(summary))
	for len(data) > maximumSummaryBytes {
		switch {
		case len(summary.ConnectorCalls) > 0:
			summary.ConnectorCalls = summary.ConnectorCalls[:len(summary.ConnectorCalls)/2]
		case len(summary.TestFailures) > 1:
			summary.TestFailures = summary.TestFailures[:len(summary.TestFailures)-1]
		case len(summary.Tests) > 0:
			summary.Tests = summary.Tests[:len(summary.Tests)/2]
		default:
			summary.Unhealthy, summary.TestFailures = nil, nil
		}
		data = mustEncode(json.Marshal(summary))
	}
	if err := supervision.WriteJSON(run.path("summary.json"), summary); err != nil {
		fmt.Fprintf(os.Stderr, "superverse-previewctl: write summary.json: %v\n", err)
	}
	fmt.Printf("\n%s%s\n", summaryMarker, data)
}

// mustEncode returns the encoding of a summary, whose connector calls are JSON the
// run decoded, so encoding fails only through a programming error.
func mustEncode(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit-3], "") + "..."
}
