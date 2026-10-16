// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package sandboxtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// A command's output is bounded to its head and tail with the path of the full
// output, valid UTF-8 and the exit code. Uses the owned sandboxd of a coding
// image: its real process APIs, no application file is changed and no scripted
// service replaces sandboxd.
func TestToolOutputActualSandbox(t *testing.T) {
	execution, _ := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := invoke(ctx, t, execution, BeginCommand, map[string]any{"description": "Print a lot",
		"command": `python3 -c "import sys; print('你好' * 5000); print('tail-marker'); print('error-' * 5000, file=sys.stderr); sys.exit(7)"`})
	if !result.KnownFailure || *result.ExitCode != 7 || !strings.HasSuffix(result.Content, "\nexit code: 7") || *result.OmittedBytes == 0 {
		t.Fatalf("bounded output lost the command's failure or its cut: %+v", result)
	}
	if !strings.Contains(result.Content, " bytes omitted · full output: /tmp/superverse-exec/runs/cmd-") || len(result.Content) > 30_300 || !utf8.ValidString(result.Content) {
		t.Fatal("the command output exceeded its budget, broke UTF-8 or did not name its full output")
	}
}

// A command whose agent root does not exist never starts in the actual
// Sandbox. The model receives a failure of its call that names the directory.
func TestBashStartRefusalActualSandbox(t *testing.T) {
	execution, _ := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	call := bashCall(t, map[string]any{"command": "printf unreachable", "description": "Print", "timeout_seconds": 10})
	call.Root = "/workspace/missing-root"
	result, err := classifyToolError(invokeCall(ctx, execution, BeginCommand, call))
	failure := result.Failure
	if err != nil || failure == nil || failure.Code != toolfailure.CommandNotStarted || failure.Effect != toolfailure.EffectNone ||
		!strings.Contains(failure.Message, "/workspace/missing-root") {
		t.Fatalf("refused start did not return a correctable failure of the call: %+v, %v", result, err)
	}
}

// A command that outlives its timeout is pending until the Session settles it:
// the ledger moves it to the background, where it continues as a task, and the
// model reads its output so far, the start and the end, and its task ID. The
// task's output is paged with read_task_output and stop_task stops it, after
// which a child that would touch a marker later never does. The command is the
// one of the command-timeout scenario, plus the marker.
func TestBashTimeoutActualSandbox(t *testing.T) {
	execution, client := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	late := "late-marker-" + time.Now().UTC().Format("20060102T150405.000000000")
	call := bashCall(t, map[string]any{"command": "(sleep 6; touch " + late + ") & for i in $(seq 1 4000); do printf 'line-%04d\\n' $i; done; sleep 60; echo never-printed",
		"description": "Print numbered lines, then wait", "timeout_seconds": 2})
	begun, err := classifyToolError(invokeCall(ctx, execution, BeginCommand, call))
	if err != nil || !begun.Pending {
		t.Fatalf("a command running past its begin wait is not pending: %+v, %v", begun, err)
	}
	result, err := classifyToolError(invokeCall(ctx, execution, MoveTimedOutCommand, call))
	taskID := string(sandboxd.CommandTaskID(call.Identity))
	if err != nil || result.Failure != nil || result.KnownFailure || result.Background == nil || result.Background.TaskID != taskID || result.Background.Deadline.IsZero() ||
		!strings.HasPrefix(result.Content, "line-0001\n") || !strings.Contains(result.Content, "\nline-4000\n") ||
		!strings.Contains(result.Content, "continues in the background as task "+taskID) || strings.Contains(result.Content, "never-printed") {
		t.Fatalf("the command past its timeout did not move to the background with its output so far: %+v, %v", result, err)
	}
	moved, err := classifyToolError(invokeCall(ctx, execution, MoveTimedOutCommand, call))
	if err != nil || moved.Background == nil || moved.Background.TaskID != taskID {
		t.Fatalf("a repeated move did not find the command in the background: %+v, %v", moved, err)
	}
	page := invoke(ctx, t, execution, ReadTaskOutput, map[string]any{"task_id": taskID, "offset": 10})
	if !strings.HasPrefix(page.Content, "Task "+taskID+": running since ") || !strings.Contains(page.Content, "\nOutput bytes 10 to 30010 of 40000; read on with offset 30010.\nline-0002\nline-0003\n") {
		t.Fatalf("read_task_output did not page the running task's output from its offset: %.400s", page.Content)
	}
	stopped := invoke(ctx, t, execution, StopTask, map[string]any{"task_id": taskID})
	if stopped.Content != "Task "+taskID+": stopped." {
		t.Fatalf("stop_task did not stop the task: %q", stopped.Content)
	}
	if again := invoke(ctx, t, execution, StopTask, map[string]any{"task_id": taskID}); again.Content != stopped.Content {
		t.Fatalf("a repeated stop_task changed the answer: %q", again.Content)
	}
	time.Sleep(8 * time.Second)
	if _, err := client.ReadFile(ctx, sandboxd.ReadFileRequest{Path: sandboxd.WorkspacePath(late), Limit: 1}); err == nil {
		t.Fatal("the process group survived the stop")
	}
	state := invoke(ctx, t, execution, CommandTaskStatus, map[string]any{"task_id": taskID}).Task
	if state == nil || state.Running || state.Lost || state.EndReason != sandboxd.CommandEndStopped || state.OutputBytes == 0 || state.EndedAt.IsZero() {
		t.Fatalf("the watcher's status of the stopped task is wrong: %+v", state)
	}
	settled, err := classifyToolError(invokeCall(ctx, execution, SettleCommand, call))
	if err != nil || settled.Failure == nil || settled.Failure.Code != toolfailure.CommandInterrupted || settled.Failure.Effect != toolfailure.EffectPartial {
		t.Fatalf("settling the stopped command did not report the interruption: %+v, %v", settled, err)
	}
}

// A command started with run_in_background answers its task ID at once and
// runs on: the watcher's status follows it to its exit, and read_task_output
// reads its whole output with its exit code. An ID that names no task is the
// call's own failure.
func TestBackgroundCommandActualSandbox(t *testing.T) {
	execution, _ := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	call := bashCall(t, map[string]any{"command": "sleep 3; echo background-done; exit 3", "description": "Wait, then print", "run_in_background": true})
	started, err := classifyToolError(invokeCall(ctx, execution, BeginCommand, call))
	taskID := string(sandboxd.CommandTaskID(call.Identity))
	if err != nil || started.Pending || started.Failure != nil || started.Background == nil || started.Background.TaskID != taskID ||
		started.Content != "The command runs in the background as task "+taskID+". You are notified when it ends; read its output with read_task_output and stop it with stop_task." {
		t.Fatalf("a background command did not answer its task at once: %+v, %v", started, err)
	}
	var state *TaskState
	for state == nil || state.Running {
		state = invoke(ctx, t, execution, CommandTaskStatus, map[string]any{"task_id": taskID}).Task
	}
	if state.Lost || state.EndReason != sandboxd.CommandEndExit || state.ExitCode == nil || *state.ExitCode != 3 || state.OutputBytes != int64(len("background-done\n")) {
		t.Fatalf("the watcher's status of the ended task is wrong: %+v", state)
	}
	output := invoke(ctx, t, execution, ReadTaskOutput, map[string]any{"task_id": taskID})
	if output.Content != "Task "+taskID+": exited with code 3.\nOutput bytes 0 to 16, the end of the output.\nbackground-done\n" {
		t.Fatalf("read_task_output did not read the ended task: %q", output.Content)
	}
	for _, unknown := range []string{"cmd-aaaaaaaaaaaa", "agent-k3n8q5r1", "../etc"} {
		failed := invoke(ctx, t, execution, ReadTaskOutput, map[string]any{"task_id": unknown})
		if failed.Failure == nil || failed.Failure.Code != toolfailure.TaskNotFound {
			t.Fatalf("read_task_output of %q did not fail with task_not_found: %+v", unknown, failed)
		}
	}
}

// The connector catalog is the public published document, fetched from the
// actual Sandbox: a match names its exact Go module and the command that adds
// it to go.mod. A missing prerequisite fails instead of supplying a fixture.
func TestConnectorCatalogActualSandbox(t *testing.T) {
	execution, _ := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result := invoke(ctx, t, execution, ConnectorCatalog, map[string]any{"query": "github", "limit": 1})
	var catalog struct {
		SHA256  string                          `json:"sha256"`
		Matches []connectorlibrary.CatalogMatch `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result.Content), &catalog); err != nil || result.Failure != nil || len(catalog.SHA256) != 64 || len(catalog.Matches) != 1 || catalog.Matches[0].ID != "github" || len(result.Content) > 16<<10 {
		t.Fatalf("actual published catalog discovery failed: %s", result.Content)
	}
	match := catalog.Matches[0]
	module := "github.com/superdurable/dex-connectors-library/" + match.Directory
	if match.Module != module || match.InstallCommand != "go get "+module+"@"+match.Version || strings.Contains(result.Content, "module:") {
		t.Fatalf("the catalog match does not name its exact Go module: %s", result.Content)
	}
	t.Logf("catalog_sha256=%s connector=%s version=%s module=%s result_bytes=%d", catalog.SHA256, match.ID, match.Version, match.Module, len(result.Content))
}

// newActualSandboxExecution connects to the owned sandboxd of the test
// environment; missing endpoints fail the test.
func newActualSandboxExecution(t *testing.T) (*Execution, *sandboxd.SandboxClient) {
	t.Helper()
	client := sandboxdtest.ClientForTest(t)
	return &Execution{executor: client, catalog: &connectorCatalogCache{}}, client
}

// invoke runs one tool with JSON arguments under a fresh call identity,
// classifies its error as the Runner does and fails the test on a fault that
// Dex would repeat.
func invoke(ctx context.Context, t *testing.T, execution *Execution, tool Tool, arguments map[string]any) Result {
	t.Helper()
	call := bashCall(t, arguments)
	result, err := classifyToolError(invokeCall(ctx, execution, tool, call))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// bashCall is a trusted call with JSON arguments in the workspace root under a
// fresh identity.
func bashCall(t *testing.T, arguments map[string]any) Call {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return Call{Arguments: string(encoded), Mutating: true, Root: sandboxd.WorkspaceRoot,
		Identity: sandboxd.InvocationIdentity{FlowID: t.Name(), CallID: time.Now().UTC().Format("20060102T150405.000000000")}}
}

func invokeCall(ctx context.Context, execution *Execution, tool Tool, call Call) (Call, Result, error) {
	result, err := tool(execution, ctx, call)
	return call, result, err
}
