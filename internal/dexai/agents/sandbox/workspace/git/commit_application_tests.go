// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

const (
	// The test run starts beside the static stage and holds its engine until
	// the gate releases it: the hold, queue wait included, ends at
	// testHoldTimeout, after the static stage's limit and the mock guard. Its
	// tests get testRunTimeout from the release; the process limit adds 30
	// seconds for the teardown and summary.
	testHoldTimeout  = 12 * time.Minute
	testRunTimeout   = "6m"
	testRunLimit     = testHoldTimeout + 6*time.Minute + 30*time.Second
	testReleaseLimit = 30 * time.Second
	// testReleasePrefix names the release file of a held run by its run ID.
	testReleasePrefix = "/tmp/superverse-apptest-release-"
	// connectorMockPackage is the connectors library's mock contract, which
	// only the application's API tests may import.
	connectorMockPackage = "github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	// applicationTestSummaryMarker starts the summary line of
	// superverse-previewctl apptest run.
	applicationTestSummaryMarker = "SUPERVERSE-APPTEST-SUMMARY "
	// Bounds of the test stage's report, within the 8 KiB of a gate failure.
	maximumReportedTestFailures = 6
	maximumTestFailureBytes     = 900
	maximumTestOutputTailBytes  = 3 << 10
	maximumTestReportBytes      = 7 << 10
)

// testRunPhase is where a run of superverse-previewctl apptest ended, as its
// summary spells it (the previewctl README is the contract).
type testRunPhase string

const (
	// testRunPhaseQueued: the deadline passed while other runs held the engine.
	testRunPhaseQueued testRunPhase = "queued"
	// testRunPhaseEngine: the engine did not start twice.
	testRunPhaseEngine testRunPhase = "engine"
	// testRunPhaseHeld: the hold deadline passed before the release.
	testRunPhaseHeld testRunPhase = "held"
)

// testOutcome is the outcome of one test that the template harness records.
type testOutcome string

const (
	testOutcomePassed testOutcome = "passed"
	testOutcomeFailed testOutcome = "failed"
	// testOutcomeNotRun is a traced test that the run did not record.
	testOutcomeNotRun testOutcome = "not run"
)

// CommitRequest is one commit_and_push call: its identity, its summary, whether
// it hands off a blocked checkpoint without the gate, and the requirement
// trace the test stage checks.
type CommitRequest struct {
	CallID  string
	Summary string
	Blocked bool
	Trace   RequirementTrace
}

// RequirementTrace is the requirement trace of a call against the Session's
// confirmed requirements. Confirmed is empty when nothing is confirmed.
type RequirementTrace struct {
	Confirmed []string
	Items     []TracedRequirement
}

// TracedRequirement names the tests that prove one confirmed requirement, or
// why no test can.
type TracedRequirement struct {
	ID             string
	Tests          []string
	UntestedReason string
}

// ApplicationTestReport is the result of a passed test stage: the tests that
// ran, the rendered Flows that ran, and the connections whose connector has
// no mocks, which the coverage check waived.
type ApplicationTestReport struct {
	Passed    int      `json:"passed"`
	Failed    int      `json:"failed"`
	Flows     int      `json:"flows"`
	Uncovered []string `json:"uncovered"`
}

// applicationTestRun is the summary of superverse-previewctl apptest run.
type applicationTestRun struct {
	Directory       string                    `json:"directory"`
	Phase           testRunPhase              `json:"phase"`
	ExitCode        int                       `json:"exitCode"`
	TimedOut        bool                      `json:"timedOut"`
	DurationMS      int64                     `json:"durationMs"`
	EngineError     string                    `json:"engineError"`
	Executions      map[string]map[string]int `json:"executions"`
	ExecutionsError string                    `json:"executionsError"`
	Unhealthy       []struct {
		FlowID     string `json:"flowId"`
		FlowType   string `json:"flowType"`
		Status     string `json:"status"`
		Step       string `json:"step"`
		Attempt    int    `json:"attempt"`
		UsageError bool   `json:"usageError"`
		Failure    string `json:"failure"`
	} `json:"unhealthy"`
	UnhealthyTotal int `json:"unhealthyTotal"`
	Unchecked      int `json:"unchecked"`
	Tests          []struct {
		Name    string      `json:"name"`
		Outcome testOutcome `json:"outcome"`
	} `json:"tests"`
	TestFailures []struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	} `json:"testFailures"`
	ConnectorCalls []struct {
		Connection string `json:"connection"`
		Operation  string `json:"operation"`
		Branch     string `json:"branch"`
	} `json:"connectorCalls"`
}

// rejectProductionMocks fails a source whose production packages, built
// without the apptest tag, import the connector mock contract.
func (g workspaceGit) rejectProductionMocks(ctx context.Context) error {
	listed, err := g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), time.Minute,
		"go", "list", "-e", "-f", "{{.ImportPath}} {{join .Deps \" \"}}", "./...")
	if err != nil {
		if listed.exitCode == 0 {
			return commandError("list the production packages", listed, err)
		}
		return sandboxd.NewToolFailure(toolfailure.SourceCheckFailed, fmt.Errorf("go list ./... exited with code %d\n%s", listed.exitCode, commandTail(listed, maximumStaticReportBytes)))
	}
	var importers []string
	for line := range strings.SplitSeq(listed.stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && slices.ContainsFunc(fields[1:], isConnectorMock) {
			importers = append(importers, fields[0])
		}
	}
	if len(importers) == 0 {
		return nil
	}
	return sandboxd.NewToolFailure(toolfailure.ConnectorMockInProduction, fmt.Errorf("these production packages import %s: %s; connector mocks belong in internal/apptest only, behind the apptest build tag",
		connectorMockPackage, comm.TruncateUTF8(strings.Join(importers, ", "), 2<<10)))
}

func isConnectorMock(importPath string) bool {
	return importPath == connectorMockPackage || strings.HasPrefix(importPath, connectorMockPackage+"/")
}

// heldTestRun is the test stage's run of the declared testApplication target,
// started with the static stage: superverse-previewctl apptest run takes the
// engine lock, starts a fresh engine and waits for the release file before it
// runs the target. A run that is never released is killed with its process
// group, engine included.
type heldTestRun struct {
	release string
	cancel  context.CancelFunc
	done    chan struct{}
	result  processResult
	err     error
}

// startHeldTests starts the held run of command for the call of request.
func (g workspaceGit) startHeldTests(ctx context.Context, command string, request CommitRequest) *heldTestRun {
	id := testRunIdentity(request.CallID)
	ctx, cancel := context.WithCancel(ctx)
	run := &heldTestRun{release: testReleasePrefix + id, cancel: cancel, done: make(chan struct{})}
	arguments := append([]string{"superverse-previewctl", "apptest", "run", "--run-id", id, "--timeout", testRunTimeout,
		"--hold-until", run.release, "--hold-timeout", testHoldTimeout.String(), "--"}, strings.Fields(command)...)
	go func() {
		defer close(run.done)
		run.result, run.err = g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), testRunLimit, arguments...)
	}()
	return run
}

// releaseTests lets the held run start the tests.
func (run *heldTestRun) releaseTests(ctx context.Context, g workspaceGit) error {
	released, err := g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), testReleaseLimit, "touch", run.release)
	if err != nil {
		return commandError("release the test run", released, err)
	}
	return nil
}

// wait returns the held run's output once it ended.
func (run *heldTestRun) wait() (processResult, error) {
	<-run.done
	return run.result, run.err
}

// stop kills the held run unless it already ended, and waits for its end.
func (run *heldTestRun) stop() {
	run.cancel()
	<-run.done
}

// runApplicationTests releases the held run of the declared testApplication
// target and checks it: the tests passed, every rendered Flow ran, every
// wired branch of a mocked connector was returned, the engine stayed healthy
// and the confirmed requirements are traced to passing tests.
func (g workspaceGit) runApplicationTests(ctx context.Context, command string, request CommitRequest, flows []renderedFlow, held *heldTestRun) (*ApplicationTestReport, error) {
	if err := held.releaseTests(ctx, g); err != nil {
		return nil, err
	}
	mocked, err := g.mockedConnectors(ctx, flows)
	if err != nil {
		return nil, err
	}
	result, err := held.wait()
	if errors.Is(err, sandboxd.ErrProcessNotStarted) {
		return nil, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("start superverse-previewctl apptest: %w", err))
	}
	if err != nil && result.exitCode == 0 {
		// A missing exit is not a test result; the tool's classifier owns it.
		return nil, commandError("application tests "+command, result, err)
	}
	run, found := readTestRunSummary(result.stdout)
	if !found {
		return nil, sandboxd.NewToolFailure(toolfailure.TestEnvironmentUnavailable, fmt.Errorf("the test engine exited with code %d without its summary\n%s", result.exitCode, commandTail(result, maximumTestOutputTailBytes)))
	}
	switch {
	case run.Phase == testRunPhaseQueued || run.Phase == testRunPhaseEngine || run.Phase == testRunPhaseHeld:
		// A run that never reached its command left the release file behind.
		_, _ = g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), testReleaseLimit, "rm", "-f", held.release)
		return nil, sandboxd.NewToolFailure(toolfailure.TestEnvironmentUnavailable, errors.New(comm.TruncateUTF8(run.EngineError, 2<<10)))
	case run.ExecutionsError != "":
		return nil, sandboxd.NewToolFailure(toolfailure.TestEnvironmentUnavailable, fmt.Errorf("the test engine's executions could not be read: %s", comm.TruncateUTF8(run.ExecutionsError, 2<<10)))
	}
	report := ApplicationTestReport{Uncovered: []string{}}
	findings := testStageFindings{command: command, run: run}
	findings.checkOutcome(result, &report)
	findings.checkCoverage(flows, mocked, &report)
	findings.checkTrace(request)
	if failure := findings.failure(report); failure != nil {
		return nil, failure
	}
	return &report, nil
}

// testRunIdentity names the run's directory from the call and a nonce of the
// attempt, so the attempts of one call never share a directory.
func testRunIdentity(callID string) string {
	digest := sha256.Sum256([]byte(callID))
	nonce := make([]byte, 4)
	_, _ = rand.Read(nonce)
	return "gate-" + hex.EncodeToString(digest[:8]) + "-" + hex.EncodeToString(nonce)
}

// readTestRunSummary decodes the last summary line of the run's output.
func readTestRunSummary(stdout string) (applicationTestRun, bool) {
	index := strings.LastIndex(stdout, applicationTestSummaryMarker)
	if index < 0 {
		return applicationTestRun{}, false
	}
	line, _, _ := strings.Cut(stdout[index+len(applicationTestSummaryMarker):], "\n")
	var run applicationTestRun
	return run, json.Unmarshal([]byte(line), &run) == nil
}

// mockedConnectors reports, for each connector module the rendered Flows use,
// whether it ships its generated mock package, such as .../llm/llmmock.
func (g workspaceGit) mockedConnectors(ctx context.Context, flows []renderedFlow) (map[string]bool, error) {
	mocked := map[string]bool{}
	modules := map[string]string{}
	var packages []string
	for _, flow := range flows {
		for _, step := range flow.ConnectorSteps {
			if _, listed := mocked[step.ModulePath]; !listed && step.ModulePath != "" {
				mocked[step.ModulePath] = false
				mockPackage := step.ModulePath + "/" + path.Base(step.ModulePath) + "mock"
				modules[mockPackage] = step.ModulePath
				packages = append(packages, mockPackage)
			}
		}
	}
	if len(packages) == 0 {
		return mocked, nil
	}
	listed, err := g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), time.Minute,
		append([]string{"go", "list", "-e", "-f", "{{.ImportPath}} {{if .Error}}missing{{else}}found{{end}}"}, packages...)...)
	if err != nil {
		return nil, commandError("look for the connector mock packages", listed, err)
	}
	for line := range strings.SplitSeq(listed.stdout, "\n") {
		if importPath, state, ok := strings.Cut(strings.TrimSpace(line), " "); ok && state == "found" {
			mocked[modules[importPath]] = true
		}
	}
	return mocked, nil
}

// testStageFindings collects what the run's checks found; the most severe
// decides the failure code, and every finding goes into the report.
type testStageFindings struct {
	command                         string
	run                             applicationTestRun
	failed, incomplete, untested    bool
	failures, engine, coverage      []string
	requirements, rerun, outputTail []string
}

// checkOutcome reads the command's exit, the recorded tests and the engine's
// health.
func (f *testStageFindings) checkOutcome(result processResult, report *ApplicationTestReport) {
	for _, test := range f.run.Tests {
		switch test.Outcome {
		case testOutcomePassed:
			report.Passed++
		case testOutcomeFailed:
			report.Failed++
		case testOutcomeNotRun:
		}
	}
	if f.run.ExitCode != 0 || f.run.TimedOut || report.Failed > 0 {
		f.failed = true
		for index, failure := range f.run.TestFailures {
			if index == maximumReportedTestFailures {
				break
			}
			f.failures = append(f.failures, fmt.Sprintf("- %s: %s", failure.Name, comm.TruncateUTF8(strings.ReplaceAll(failure.Output, "\n", " "), maximumTestFailureBytes)))
			f.rerun = append(f.rerun, topLevelTest(failure.Name))
		}
		if len(f.run.TestFailures) == 0 {
			// A build failure or a panic names no failing test; its output does.
			f.outputTail = append(f.outputTail, "output (end):\n"+comm.TailUTF8(strings.TrimSpace(result.redact(beforeSummary(result.stdout))), maximumTestOutputTailBytes))
		}
	}
	for _, execution := range f.run.Unhealthy {
		f.failed = true
		cause := fmt.Sprintf("attempt %d", execution.Attempt)
		if execution.UsageError {
			cause = fmt.Sprintf("attempt %d, an SDK usage error", execution.Attempt)
		}
		f.engine = append(f.engine, fmt.Sprintf("engine: %s (%s) %s; Step %s %s: %s", execution.FlowID, execution.FlowType, execution.Status, execution.Step, cause, comm.TruncateUTF8(execution.Failure, maximumTestFailureBytes)))
	}
	if hidden := f.run.UnhealthyTotal - len(f.run.Unhealthy); hidden > 0 {
		f.engine = append(f.engine, fmt.Sprintf("engine: %d more unhealthy executions", hidden))
	}
	if f.run.Unchecked > 0 {
		f.engine = append(f.engine, fmt.Sprintf("engine: the health of %d running executions was not read", f.run.Unchecked))
	}
	if len(f.run.Tests) == 0 && !f.failed {
		f.incomplete = true
		f.coverage = append(f.coverage, "coverage: no test ran through apptest.Start in internal/apptest")
	}
}

// checkCoverage requires an execution of every rendered Flow and a returned
// call of every wired branch of a mocked connector; a connector without mocks
// is waived and listed.
func (f *testStageFindings) checkCoverage(flows []renderedFlow, mocked map[string]bool, report *ApplicationTestReport) {
	type wired struct{ connection, operation, branch string }
	returned := map[wired]bool{}
	for _, call := range f.run.ConnectorCalls {
		returned[wired{call.Connection, call.Operation, call.Branch}] = true
	}
	var neverStarted, neverReturned []string
	for _, flow := range flows {
		executions := 0
		for _, count := range f.run.Executions[flow.FlowType] {
			executions += count
		}
		if executions > 0 {
			report.Flows++
		} else {
			neverStarted = append(neverStarted, "Flow "+flow.FlowType+" never started")
		}
		for _, step := range flow.ConnectorSteps {
			if !mocked[step.ModulePath] {
				waiver := fmt.Sprintf("untested: %s %s has no mocks; upgrade it once a release with mocks is published", step.ConnectorID, step.ModuleVersion)
				if !slices.Contains(report.Uncovered, waiver) {
					report.Uncovered = append(report.Uncovered, waiver)
				}
				continue
			}
			for _, branch := range step.Branches {
				if !returned[wired{step.Connection, step.Operation, branch}] {
					neverReturned = append(neverReturned, fmt.Sprintf("branch %s.%s.%s never returned", step.Connection, step.Operation, branch))
				}
			}
		}
	}
	if gaps := slices.Concat(neverReturned, neverStarted); len(gaps) > 0 {
		f.incomplete = true
		f.coverage = append(f.coverage, "coverage: "+strings.Join(gaps, "; "))
	}
}

// checkTrace requires every confirmed requirement in the trace with tests
// that passed in this run, or with the reason no test can prove it.
func (f *testStageFindings) checkTrace(request CommitRequest) {
	if len(request.Trace.Confirmed) == 0 {
		return
	}
	traced := map[string]TracedRequirement{}
	for _, item := range request.Trace.Items {
		traced[item.ID] = item
	}
	for _, id := range request.Trace.Confirmed {
		item, isTraced := traced[id]
		switch {
		case !isTraced:
			f.requirements = append(f.requirements, id+" is not in the requirement trace")
		case len(item.Tests) == 0 && item.UntestedReason == "":
			f.requirements = append(f.requirements, id+" names no test and no untested_reason")
		}
		for _, test := range item.Tests {
			switch f.testOutcome(test) {
			case testOutcomePassed:
				continue
			case testOutcomeNotRun:
				f.requirements = append(f.requirements, fmt.Sprintf("%s names %s, which did not run", id, test))
			default:
				f.requirements = append(f.requirements, fmt.Sprintf("%s names %s, which did not pass", id, test))
			}
			f.rerun = append(f.rerun, topLevelTest(test))
		}
	}
	f.untested = len(f.requirements) > 0
}

// testOutcome is passed for a test that passed in this run, or whose recorded
// subtests all passed; otherwise it says what happened to it.
func (f *testStageFindings) testOutcome(name string) testOutcome {
	ran, failed := false, false
	for _, test := range f.run.Tests {
		if test.Name == name || strings.HasPrefix(test.Name, name+"/") {
			ran = true
			failed = failed || test.Outcome != testOutcomePassed
		}
	}
	switch {
	case !ran:
		return testOutcomeNotRun
	case failed:
		return testOutcomeFailed
	default:
		return testOutcomePassed
	}
}

// failure is the stage's failure, or nil when every check passed.
func (f *testStageFindings) failure(report ApplicationTestReport) error {
	var code toolfailure.Code
	switch {
	case f.failed:
		code = toolfailure.ApplicationTestsFailed
	case f.incomplete:
		code = toolfailure.ApplicationTestsIncomplete
	case f.untested:
		code = toolfailure.RequirementsUntested
	default:
		return nil
	}
	head := fmt.Sprintf("%s exit %d after %s (%d tests, %d failed)", f.command, f.run.ExitCode,
		(time.Duration(f.run.DurationMS) * time.Millisecond).Round(time.Second), len(f.run.Tests), report.Failed)
	if f.run.TimedOut {
		head += ", stopped at the " + testRunTimeout + " limit"
	}
	lines := append([]string{head}, f.failures...)
	lines = append(lines, f.outputTail...)
	lines = append(lines, f.engine...)
	lines = append(lines, f.coverage...)
	if len(f.requirements) > 0 {
		lines = append(lines, "requirements: "+strings.Join(f.requirements, "; "))
	}
	if f.run.Directory != "" {
		lines = append(lines, "report: "+f.run.Directory+" (output.log, summary.json, logs/); read it with bash")
	}
	next := "next: fix the source or the tests, run " + f.command
	if rerun := slices.Compact(slices.Sorted(slices.Values(f.rerun))); len(rerun) > 0 {
		next = fmt.Sprintf("next: %s RUN='%s'", f.command, strings.Join(rerun, "|"))
	}
	lines = append(lines, next+", then retry commit_and_push")
	return sandboxd.NewToolFailure(code, errors.New(comm.TruncateUTF8(strings.Join(lines, "\n"), maximumTestReportBytes)))
}

// topLevelTest is the test a go test -run pattern selects for a test name.
func topLevelTest(name string) string {
	top, _, _ := strings.Cut(name, "/")
	return "^" + top + "$"
}

// beforeSummary is the output before the summary line.
func beforeSummary(stdout string) string {
	if index := strings.LastIndex(stdout, applicationTestSummaryMarker); index >= 0 {
		return stdout[:index]
	}
	return stdout
}
