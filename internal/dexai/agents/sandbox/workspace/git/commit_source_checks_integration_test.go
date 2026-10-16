// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package git_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// This scenario needs an owned, disposable sandboxd with a clean checkout of the
// real template, its restored dependencies and the platform's pinned dexcli. It
// compiles the actual application and renders its Flows with the platform's own
// generator; no scripted compiler, renderer, substituted transport or provider
// response is used. Every change it makes to the checkout is restored. The
// static and definitions stages run side by side, a failure of both reports
// static, and the commit stages only the tree the gate verified: a tracked file
// that the tests write fails the tree check. The log names each stage's
// duration_ms, for comparison across builds of the image.
func TestApplicationSourceGateActualSandbox(t *testing.T) {
	expected := os.Getenv("SUPERVERSE_TEST_SOURCE_CHECK_COMMIT")
	if !flowid.IsCommitSHA(expected) {
		t.Fatal("an exact template source commit is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	sandbox := liveCheckout{t: t, ctx: ctx, client: sandboxdtest.ClientForTest(t)}
	head := sandbox.git("rev-parse", "HEAD")
	if head != expected || sandbox.git("status", "--porcelain") != "" {
		t.Fatal("the source gate scenario requires a clean disposable checkout of the accepted template commit")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		restore := liveCheckout{t: t, ctx: cleanup, client: sandbox.client}
		if _, err := restore.run(sandboxd.GitProcessEnvironment(), "git", "reset", "--hard", head); err != nil {
			t.Error("restore the owned disposable checkout:", err)
		}
	})
	gate := &git.GitOperations{CheckDefinitions: workspace.CheckWorktreeDefinitionsForTest}
	// A different confirmed HEAD makes the gate verify the source.
	unaccepted := git.Repository{ExpectedHeadSHA: strings.Repeat("0", 40)}

	result, progress, err := sandbox.verify(gate, unaccepted)
	if err != nil || len(result.Stages) != 4 {
		t.Fatalf("the real template must pass every stage: %+v error=%v", result.Stages, err)
	}
	for index, stage := range []string{"manifest", "static", "definitions", "tests"} {
		if string(result.Stages[index].Stage) != stage || result.Stages[index].Status != git.SourceGatePassedForTest {
			t.Fatalf("stage %d is %+v, want %s passed", index, result.Stages[index], stage)
		}
	}
	// The template's nine reference tests run TextSummaryFlow through the llm
	// connector's mocks, so nothing is waived.
	if tests := result.Tests; tests == nil || tests.Passed != 9 || tests.Failed != 0 || tests.Flows != 1 || len(tests.Uncovered) != 0 {
		t.Fatalf("the test stage must report the template's reference tests with nothing uncovered: %+v", result.Tests)
	}
	if result.Stages[1].DurationMS <= 0 || result.Stages[2].DurationMS <= 0 || result.Stages[3].DurationMS <= 0 || len(progress) != 8 {
		t.Fatalf("stages must report durations and at most two progress lines each: %+v %q", result.Stages, progress)
	}
	if progress[2] != "Source gate: static started" || progress[3] != "Source gate: definitions started" || !strings.HasPrefix(progress[4], "Source gate: static passed") {
		t.Fatalf("the definitions stage must start beside the static stage and end after it: %q", progress)
	}
	for _, stage := range result.Stages {
		t.Logf("stage %s %s duration_ms=%d", stage.Stage, stage.Status, stage.DurationMS)
	}
	// The verified tree is the one the commit stages.
	if err = git.StageVerifiedTreeForTest(sandbox.ctx, sandbox.client, result); err != nil {
		t.Fatalf("the unchanged working tree must stage the verified tree: %v", err)
	}
	sandbox.git("reset", "--quiet")

	// The template's local checker historically omitted this deployment field.
	// Read the actual invalid file through sandboxd and reject it before builds
	// or Git mutation; the provider itself is neither called nor substituted.
	sandbox.shell(`python3 -c 'import json; from pathlib import Path; p=Path("dex-app.yaml"); d=json.loads(p.read_text()); d["connectors"]=[{"connectionName":"source-check","connectorId":"llm","version":"v0.2.0","authMethodId":"openai","operations":["generateText"]}]; p.write_text(json.dumps(d))'`)
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.SourceCheckFailed, "stage manifest failed", "connectors[0].modulePath")
	sandbox.requireUnchanged(head)

	// Reproduce an invalid HEAD accepted by an older host before this schema
	// check existed: a clean confirmed HEAD still meets the deployment schema.
	sandbox.git("add", "--", "dex-app.yaml")
	sandbox.git("-c", "user.name=Source Gate Verification", "-c", "user.email=source-gate@example.test", "commit", "-m", "Reproduce previously accepted invalid manifest")
	previouslyAccepted := sandbox.git("rev-parse", "HEAD")
	_, _, err = sandbox.verify(gate, git.Repository{ExpectedHeadSHA: previouslyAccepted})
	requireApplicationFailure(t, err, toolfailure.SourceCheckFailed, "connectors[0].modulePath")
	sandbox.git("reset", "--hard", head)

	// The next commit may not remove or change the checkStatic command that
	// HEAD declares.
	sandbox.shell(`python3 -c 'import json; from pathlib import Path; p=Path(".superverse/template.json"); d=json.loads(p.read_text()); d["commands"]["checkStatic"]="make generate"; p.write_text(json.dumps(d))'`)
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.SourceGateWeakened, `"make check-static"`)
	sandbox.git("checkout", "--", ".")
	sandbox.shell(`python3 -c 'import json; from pathlib import Path; p=Path(".superverse/template.json"); d=json.loads(p.read_text()); del d["commands"]["testApplication"]; p.write_text(json.dumps(d))'`)
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.SourceGateWeakened, `testApplication command "make test-app"`)
	sandbox.shell("rm .superverse/template.json")
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.SourceGateWeakened)
	sandbox.git("checkout", "--", ".")

	// More than 6 KiB of output on each stream precedes a real TypeScript error;
	// the report keeps the end of both, which names the failing file.
	sandbox.shell(`python3 - <<'EOF'
from pathlib import Path
makefile = Path("Makefile")
noise = "\t@python3 -c \"import sys; sys.stdout.write('noise ' * 2000); sys.stderr.write('noise ' * 2000)\"\n"
target = "check-static: generate check-fdg-v2\n"
makefile.write_text(makefile.read_text().replace(target, target + noise))
EOF`)
	sandbox.shell(`printf '\nconst sourceCheckTypeError: number = "invalid";\n' >> web/src/App.tsx`)
	_, progress, err = sandbox.verify(gate, unaccepted)
	report := requireApplicationFailure(t, err, toolfailure.SourceCheckFailed, "stage static failed", "make check-static exited with code", "App.tsx", "TS2322")
	if len(report) > 8<<10 || !strings.HasPrefix(progress[len(progress)-1], "Source gate: static failed") {
		t.Fatalf("the static report must stay within 8 KiB and its stage must report the failure: %d bytes, %q", len(report), progress)
	}
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")

	// A Step without a group fails both the template's own FDG check in the
	// static stage and the platform's: the static failure is the gate's
	// report, and the definitions failure is only shown as progress before it.
	sandbox.shell(`sed -i '/dex:group group-id:intake/d' internal/process/text_summary_flow.go`)
	_, progress, err = sandbox.verify(gate, unaccepted)
	report = requireApplicationFailure(t, err, toolfailure.SourceCheckFailed, "stage static failed", "make check-static exited with code")
	if strings.Contains(report, "stage definitions") || !slices.ContainsFunc(progress, func(line string) bool { return strings.HasPrefix(line, "Source gate: definitions failed") }) ||
		!strings.HasPrefix(progress[len(progress)-1], "Source gate: static failed") {
		t.Fatalf("a double failure must report static and show the definitions failure first: %q\n%s", progress, report)
	}
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")

	// With the template's own FDG check removed from its Makefile, the
	// platform's check still rejects a Step without a group while the static
	// stage passes, and it stages nothing: it renders the next commit through a
	// copy of the index.
	sandbox.shell(`python3 - <<'EOF'
from pathlib import Path
makefile = Path("Makefile")
makefile.write_text(makefile.read_text().replace("\tpython3 ./scripts/render-flow-definitions.py\n", "\ttrue\n"))
EOF`)
	sandbox.shell(`sed -i '/dex:group group-id:intake/d' internal/process/text_summary_flow.go`)
	_, progress, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.FlowDefinitionInvalid, "stage definitions failed", "v2_step_group", "internal/process/text_summary_flow.go")
	if !slices.ContainsFunc(progress, func(line string) bool { return strings.HasPrefix(line, "Source gate: static passed") }) {
		t.Fatalf("the static stage must pass beside the failing definitions stage: %q", progress)
	}
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")

	// A test that writes a tracked file passes every stage, but the tree the
	// commit would stage is not the one the gate verified: nothing is staged.
	sandbox.shell(`printf '//go:build apptest\n\npackage apptest\n\nimport "os"\n\nfunc init() {\n\tif file, err := os.OpenFile("../../LICENSE", os.O_APPEND|os.O_WRONLY, 0); err == nil {\n\t\t_, _ = file.WriteString("\\n")\n\t\t_ = file.Close()\n\t}\n}\n' > internal/apptest/tracked_write_test.go`)
	if result, _, err = sandbox.verify(gate, unaccepted); err != nil || result.Tests == nil {
		t.Fatalf("a test that writes a tracked file still passes the stages: %+v %v", result.Tests, err)
	}
	err = git.StageVerifiedTreeForTest(sandbox.ctx, sandbox.client, result)
	requireApplicationFailure(t, err, toolfailure.SourceCheckFailed, "changed after the source gate verified", "LICENSE")
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")
	sandbox.shell("rm internal/apptest/tracked_write_test.go")

	// A failing assertion fails the test stage with the test's own message and
	// the command that reruns it.
	sandbox.shell(`sed -i '0,/TextSummaryStatusDefect)/s//TextSummaryStatusCompleted)/' internal/apptest/text_summary_test.go`)
	_, _, err = sandbox.verify(gate, unaccepted)
	report = requireApplicationFailure(t, err, toolfailure.ApplicationTestsFailed, "stage tests failed", "make test-app exit", "- TestTextSummaryWithoutCredentialsReportsDefect:",
		"/tmp/superverse-apptest/runs/gate-", "next: make test-app RUN='^TestTextSummaryWithoutCredentialsReportsDefect$'")
	t.Logf("the report of a failing test:\n%s", report)
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")

	// Deleted tests leave the Flow untested.
	sandbox.shell("rm internal/apptest/text_summary_test.go")
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.ApplicationTestsIncomplete, "no test ran through apptest.Start", "Flow TextSummaryFlow never started")
	sandbox.git("checkout", "--", ".")

	// A production package that imports a connector mock fails before any test
	// runs. The mock contract ships in the template's own sdkgo module, so the
	// import needs no go.mod change.
	sandbox.shell(`sed -i 's|^import (|import (\n\t_ "github.com/superdurable/dex-connectors-library/sdkgo/connectormock"|' internal/process/handler_failures.go && gofmt -w internal/process/handler_failures.go`)
	_, _, err = sandbox.verify(gate, unaccepted)
	requireApplicationFailure(t, err, toolfailure.ConnectorMockInProduction, "stage definitions failed", "github.com/superdurable/dex-template-basic-process/internal/process")
	sandbox.requireUnchanged(head)
	sandbox.git("checkout", "--", ".")

	// A confirmed requirement without a passing test and without a reason
	// fails the trace; one traced to passing tests or a reason passes.
	confirmed := git.RequirementTrace{Confirmed: []string{"R1", "R2", "R3"}, Items: []git.TracedRequirement{
		{ID: "R1", Tests: []string{"TestTextSummaryRejectsBlankText"}},
		{ID: "R2", Tests: []string{"TestTextSummaryWithoutCredentialsReportsDefect", "TestTextSummaryNeverWritten"}},
	}}
	_, _, err = sandbox.verifyTraced(gate, unaccepted, confirmed)
	requireApplicationFailure(t, err, toolfailure.RequirementsUntested, "R2 names TestTextSummaryNeverWritten, which did not run", "R3 is not in the requirement trace")
	confirmed.Items[1].Tests = confirmed.Items[1].Tests[:1]
	confirmed.Items = append(confirmed.Items, git.TracedRequirement{ID: "R3", UntestedReason: "The llm connector has no mocks yet."})
	if result, _, err = sandbox.verifyTraced(gate, unaccepted, confirmed); err != nil || result.Tests == nil || result.Tests.Passed != 9 {
		t.Fatalf("a complete trace must pass: %+v %v", result.Tests, err)
	}

	// A taken port of the test engine keeps it from starting: the stage is the
	// platform's test_environment_unavailable, and nothing is committed.
	sandbox.shell(`setsid python3 -m http.server 8805 --bind 127.0.0.1 >/dev/null 2>&1 < /dev/null & for _ in $(seq 50); do awk 'FNR>1 && $2=="0100007F:2265"' /proc/net/tcp | grep -q . && exit 0; sleep 0.2; done; exit 1`)
	_, _, err = sandbox.verifyTraced(gate, unaccepted, confirmed)
	sandbox.shell(`pkill -f 'http.server 880[5]'; true`)
	var busy *sandboxd.ToolFailure
	if !errors.As(err, &busy) || busy.Code() != toolfailure.TestEnvironmentUnavailable || busy.Effect() != toolfailure.EffectNone ||
		!strings.Contains(busy.Error(), "127.0.0.1:8805 is in use") {
		t.Fatalf("want test_environment_unavailable for a taken engine port, got %v", err)
	}
	sandbox.requireUnchanged(head)

	// A clean HEAD the Session already accepted repeats no build and no test.
	result, _, err = sandbox.verify(gate, git.Repository{ExpectedHeadSHA: head})
	if err != nil || result.Tests != nil || len(result.Stages) != 4 || result.Stages[0].Status != git.SourceGatePassedForTest ||
		slices.ContainsFunc(result.Stages[1:], func(stage git.SourceGateStage) bool { return stage.Status != git.SourceGateSkippedForTest }) {
		t.Fatalf("a clean confirmed source should need no repeat gate: %+v %v", result, err)
	}
}

// liveCheckout runs commands in the owned disposable checkout.
type liveCheckout struct {
	t      *testing.T
	ctx    context.Context
	client *sandboxd.SandboxClient
}

func (c liveCheckout) verify(gate *git.GitOperations, repository git.Repository) (git.SourceGate, []string, error) {
	return c.verifyTraced(gate, repository, git.RequirementTrace{})
}

// verifyTraced runs the gate of a call with the requirement trace.
func (c liveCheckout) verifyTraced(gate *git.GitOperations, repository git.Repository, trace git.RequirementTrace) (git.SourceGate, []string, error) {
	var progress []string
	request := git.CommitRequest{CallID: "live-source-gate", Summary: "Live source gate", Trace: trace}
	result, err := gate.VerifyApplicationSource(c.ctx, c.client, repository, request, func(line string) { progress = append(progress, line) })
	return result, progress, err
}

func (c liveCheckout) run(environment []sandboxd.EnvironmentVariable, arguments ...string) (string, error) {
	output, err := c.client.RunToExit(c.ctx, sandboxd.StartRequest{
		ExecutionID: sandboxd.PlatformExecutionID("live-source-gate"), Arguments: arguments, Environment: environment, Timeout: time.Minute,
	})
	if err == nil && output.ExitCode != 0 {
		err = errors.New(strings.TrimSpace(output.Stderr))
	}
	return strings.TrimSpace(output.Stdout), err
}

// git runs a Git command in the trusted Git environment.
func (c liveCheckout) git(arguments ...string) string {
	c.t.Helper()
	output, err := c.run(sandboxd.GitProcessEnvironment(), append([]string{"git"}, arguments...)...)
	if err != nil {
		c.t.Fatalf("git %v: %v", arguments, err)
	}
	return output
}

func (c liveCheckout) shell(script string) {
	c.t.Helper()
	if _, err := c.run(nil, "bash", "-c", script); err != nil {
		c.t.Fatalf("%s: %v", script, err)
	}
}

// requireUnchanged asserts that a failed gate moved no HEAD and staged nothing.
func (c liveCheckout) requireUnchanged(head string) {
	c.t.Helper()
	if current := c.git("rev-parse", "HEAD"); current != head {
		c.t.Fatalf("a failed source gate moved HEAD to %s", current)
	}
	c.git("diff", "--cached", "--quiet")
}

func requireApplicationFailure(t *testing.T, err error, code toolfailure.Code, fragments ...string) string {
	t.Helper()
	var failure *sandboxd.ToolFailure
	if !errors.As(err, &failure) || failure.Code() != code || code.Owner() != toolfailure.OwnerApplication || failure.Effect() != toolfailure.EffectNone {
		t.Fatalf("want application failure %s without an effect, got %v", code, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(failure.Error(), fragment) {
			t.Fatalf("failure %s must mention %q: %s", code, fragment, failure.Error())
		}
	}
	return failure.Error()
}
