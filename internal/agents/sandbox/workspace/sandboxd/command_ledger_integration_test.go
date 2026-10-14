// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package sandboxd_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd/sandboxdtest"
)

// TestCommandLedgerActualSandbox drives superverse-exec through the real
// sandboxd of a coding image container that the test owns: the processes,
// process groups, locks and PID 1 are the container's own.
func TestCommandLedgerActualSandbox(t *testing.T) {
	container := sandboxdtest.StartCodingContainerForTest(t)
	ledger := func() sandboxd.CommandLedger { return container.Client().CommandLedger() }
	ctx := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		t.Cleanup(cancel)
		return ctx
	}

	t.Run("one start per ID", func(t *testing.T) {
		id := ledgerID(t)
		request := commandRequest(id, fmt.Sprintf("echo started >> /tmp/starts-%s; sleep 2; echo done", id))
		var wait sync.WaitGroup
		answers := make([]sandboxd.CommandStatus, 20)
		failures := make([]error, 20)
		for index := range answers {
			wait.Go(func() { answers[index], failures[index] = ledger().Begin(ctx(t), request) })
		}
		wait.Wait()
		for index, answer := range answers {
			if failures[index] != nil || answer.Refusal != "" || (answer.State != sandboxd.CommandRunning && answer.State != sandboxd.CommandExited) {
				t.Fatalf("begin %d of one ID: %+v, %v", index, answer, failures[index])
			}
		}
		final := awaitExit(t, ledger(), id)
		if final.EndReason != sandboxd.CommandEndExit || final.ExitCode == nil || *final.ExitCode != 0 || final.Output.Projection.Head != "done\n" {
			t.Fatalf("the command did not complete once: %+v", final)
		}
		if starts := runShell(t, ledger(), "wc -l < /tmp/starts-"+string(id)); starts != "1\n" {
			t.Fatalf("20 concurrent begins of one ID started %q commands", starts)
		}
		repeated, err := ledger().Begin(ctx(t), request)
		if err != nil || repeated.State != sandboxd.CommandExited || repeated.Refusal != "" {
			t.Fatalf("a repeated begin did not answer the finished run: %+v, %v", repeated, err)
		}
		request.Command = []string{"bash", "-c", "echo another command"}
		conflict, err := ledger().Begin(ctx(t), request)
		if err != nil || conflict.Refusal != sandboxd.CommandRefusalExecutionConflict || conflict.State != sandboxd.CommandExited {
			t.Fatalf("another command under the same ID was not a conflict: %+v, %v", conflict, err)
		}
	})

	t.Run("output windows by offset", func(t *testing.T) {
		id := ledgerID(t)
		begun, err := ledger().Begin(ctx(t), withWait(commandRequest(id, `printf 'line-1\nline-2\nline-3\n'; echo error >&2`), 20*time.Second))
		if err != nil || begun.State != sandboxd.CommandExited {
			t.Fatalf("begin: %+v, %v", begun, err)
		}
		first, err := ledger().Status(ctx(t), id, 0, sandboxd.CommandOutputView{From: 0, Max: 7})
		if err != nil || first.Output.Window == nil || first.Output.Window.Data != "line-1\n" || first.Output.Window.Next != 7 {
			t.Fatalf("first window: %+v, %v", first.Output.Window, err)
		}
		rest, err := ledger().Status(ctx(t), id, 0, sandboxd.CommandOutputView{From: first.Output.Window.Next, Max: 1 << 10})
		if err != nil || rest.Output.Window.Data != "line-2\nline-3\nerror\n" || rest.Output.Window.Next != rest.Output.Bytes {
			t.Fatalf("second window: %+v, %v", rest.Output.Window, err)
		}
		if !strings.HasPrefix(rest.Output.Path, "/tmp/superverse-exec/runs/"+string(id)+"/") {
			t.Fatalf("output path %q is not the run's file", rest.Output.Path)
		}
	})

	t.Run("lifetime kills the process group", func(t *testing.T) {
		id := ledgerID(t)
		request := commandRequest(id, "(sleep 31.25; echo late) & echo started; sleep 32.25")
		request.Lifetime = 2 * time.Second
		if _, err := ledger().Begin(ctx(t), request); err != nil {
			t.Fatal(err)
		}
		final := awaitExit(t, ledger(), id)
		if final.EndReason != sandboxd.CommandEndLifetimeLimit || final.Output.Projection.Head != "started\n" {
			t.Fatalf("the lifetime did not end the command: %+v", final)
		}
		if left := runShell(t, ledger(), `pgrep -f "sleep 3[12].25" || echo none`); left != "none\n" {
			t.Fatalf("processes of the group survived the lifetime: %q", left)
		}
	})

	t.Run("output limit", func(t *testing.T) {
		id := ledgerID(t)
		request := withWait(commandRequest(id, "yes 0123456789abcdef"), 20*time.Second)
		begun, err := ledger().Begin(ctx(t), request)
		if err == nil && begun.State == sandboxd.CommandRunning {
			begun = awaitExit(t, ledger(), id)
		}
		if err != nil || begun.EndReason != sandboxd.CommandEndOutputLimit || begun.Output.Bytes != 16<<20 {
			t.Fatalf("the output limit did not end the command at 16 MiB: %+v, %v", begun, err)
		}
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		id := ledgerID(t)
		if _, err := ledger().Begin(ctx(t), commandRequest(id, "echo up; sleep 60")); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			stopped, err := ledger().Stop(ctx(t), id, 2*time.Second, sandboxd.CommandOutputView{Head: 100, Tail: 100})
			if err != nil || stopped.State != sandboxd.CommandExited || stopped.EndReason != sandboxd.CommandEndStopped || stopped.Output.Projection.Head != "up\n" {
				t.Fatalf("stop: %+v, %v", stopped, err)
			}
		}
		unknown, err := ledger().Stop(ctx(t), ledgerID(t), time.Second, sandboxd.CommandOutputView{})
		if err != nil || unknown.State != sandboxd.CommandNotFound {
			t.Fatalf("stop of an unknown ID: %+v, %v", unknown, err)
		}
	})

	t.Run("refusals start nothing", func(t *testing.T) {
		request := commandRequest(ledgerID(t), "touch /tmp/refused")
		request.WorkingDirectory = "/workspace/missing"
		refused, err := ledger().Begin(ctx(t), request)
		if err != nil || refused.Refusal != sandboxd.CommandRefusalNoWorkingDir || refused.State != sandboxd.CommandNotFound {
			t.Fatalf("a missing working directory was not refused: %+v, %v", refused, err)
		}
		request = withWait(commandRequest(ledgerID(t), "echo same instance"), 20*time.Second)
		now := time.Now()
		request.NotBefore = &now
		started, err := ledger().Begin(ctx(t), request)
		if err != nil || started.Refusal != "" || started.State != sandboxd.CommandExited {
			t.Fatalf("a retry on the instance that was already running did not start: %+v, %v", started, err)
		}
		if marker := runShell(t, ledger(), "test -e /tmp/refused && echo present || echo absent"); marker != "absent\n" {
			t.Fatal("a refused command ran")
		}
	})

	t.Run("background limit", func(t *testing.T) {
		var ids []sandboxd.ExecutionID
		t.Cleanup(func() {
			for _, id := range ids {
				_, _ = ledger().Stop(context.Background(), id, time.Second, sandboxd.CommandOutputView{})
			}
		})
		for index := range 5 {
			id := ledgerID(t)
			request := commandRequest(id, "sleep 120")
			request.Background = true
			answer, err := ledger().Begin(ctx(t), request)
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
			if index < 4 && (answer.State != sandboxd.CommandRunning || answer.Refusal != "") {
				t.Fatalf("background command %d was not started: %+v", index+1, answer)
			}
			if index == 4 && (answer.Refusal != sandboxd.CommandRefusalBackgroundLimit || answer.State != sandboxd.CommandNotFound) {
				t.Fatalf("a fifth background command was not refused: %+v", answer)
			}
		}
		foreground := ledgerID(t)
		ids = append(ids, foreground)
		if _, err := ledger().Begin(ctx(t), commandRequest(foreground, "sleep 120")); err != nil {
			t.Fatal(err)
		}
		converted, err := ledger().Background(ctx(t), foreground, time.Minute)
		if err != nil || converted.Refusal != sandboxd.CommandRefusalBackgroundLimit || converted.Background {
			t.Fatalf("a conversion beyond the limit was not refused: %+v, %v", converted, err)
		}
		if _, err = ledger().Stop(ctx(t), ids[0], time.Second, sandboxd.CommandOutputView{}); err != nil {
			t.Fatal(err)
		}
		converted, err = ledger().Background(ctx(t), foreground, time.Minute)
		if err != nil || converted.Refusal != "" || !converted.Background || converted.Deadline == nil || time.Until(*converted.Deadline) < 50*time.Second {
			t.Fatalf("a conversion within the limit did not move the deadline: %+v, %v", converted, err)
		}
	})

	t.Run("garbage collection", func(t *testing.T) {
		old := "ledger-gc-old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		runShell(t, ledger(), fmt.Sprintf(`set -e; dir=/tmp/superverse-exec/runs/%s; mkdir -p $dir; touch $dir/lock $dir/output.log
echo '{"id":"%s","digest":"x","command":["true"],"workingDirectory":"/workspace","background":false,"admittedAt":"2020-01-01T00:00:00Z","deadline":"2020-01-01T00:01:00Z"}' > $dir/admission.json
echo '{"startedAt":"2020-01-01T00:00:00Z","endedAt":"2020-01-01T00:00:01Z","endReason":"exit","outputBytes":0}' > $dir/exit.json`, old, old))
		for range 17 {
			runShell(t, ledger(), "head -c 16000000 /dev/zero")
		}
		// A begin collects garbage after it started its command, so a command
		// of the ledger can measure the ledger while its own begin removes runs.
		// A command that ends first settles the collection (a begin has returned
		// when it ends), and the measurement then runs outside the ledger, as a
		// plain sandboxd process that no collection races.
		runShell(t, ledger(), "true")
		measured, err := container.Client().RunToExit(ctx(t), sandboxd.StartRequest{ExecutionID: sandboxd.PlatformExecutionID("ledger-gc-measure"), Timeout: 30 * time.Second,
			Arguments: []string{"sh", "-c", fmt.Sprintf("du -sb /tmp/superverse-exec/runs | cut -f1; test -e /tmp/superverse-exec/runs/%s && echo kept || echo removed", old)}})
		if err != nil || measured.ExitCode != 0 {
			t.Fatalf("the ledger could not be measured: %+v, %v", measured, err)
		}
		usage := measured.Stdout
		lines := strings.Fields(usage)
		total, err := strconv.ParseInt(lines[0], 10, 64)
		if err != nil || total > 257<<20 || lines[1] != "removed" {
			t.Fatalf("garbage collection left %q", usage)
		}
	})

	t.Run("a container restart loses the run", func(t *testing.T) {
		id := ledgerID(t)
		if _, err := ledger().Begin(ctx(t), commandRequest(id, "sleep 300")); err != nil {
			t.Fatal(err)
		}
		container.Restart()
		lost, err := ledger().Status(ctx(t), id, 0, sandboxd.CommandOutputView{})
		if err != nil || lost.State != sandboxd.CommandLost {
			t.Fatalf("a run whose supervisor ended with the container is not lost: %+v, %v", lost, err)
		}
	})

	t.Run("a new instance is told apart from a missed attempt", func(t *testing.T) {
		id := ledgerID(t)
		firstAttempt := time.Now()
		if _, err := ledger().Begin(ctx(t), commandRequest(id, "sleep 300")); err != nil {
			t.Fatal(err)
		}
		container.Replace()
		missing, err := ledger().Status(ctx(t), id, 0, sandboxd.CommandOutputView{})
		if err != nil || missing.State != sandboxd.CommandNotFound {
			t.Fatalf("a new instance knows the run: %+v, %v", missing, err)
		}
		request := commandRequest(id, "sleep 300")
		notBefore := firstAttempt.Add(-time.Minute)
		request.NotBefore = &notBefore
		replaced, err := ledger().Begin(ctx(t), request)
		if err != nil || replaced.Refusal != sandboxd.CommandRefusalInstanceReplaced || replaced.State != sandboxd.CommandNotFound {
			t.Fatalf("a retry on a replaced instance was not refused: %+v, %v", replaced, err)
		}
		if after, err := ledger().Status(ctx(t), id, 0, sandboxd.CommandOutputView{}); err != nil || after.State != sandboxd.CommandNotFound {
			t.Fatalf("the refused retry started the command: %+v, %v", after, err)
		}
	})
}

func ledgerID(t *testing.T) sandboxd.ExecutionID {
	return sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: t.Name(), CallID: strconv.FormatInt(time.Now().UnixNano(), 10)})
}

func commandRequest(id sandboxd.ExecutionID, script string) sandboxd.CommandRequest {
	return sandboxd.CommandRequest{ID: id, WorkingDirectory: sandboxd.WorkspaceRoot, Command: []string{"bash", "-c", script},
		Lifetime: 5 * time.Minute, Output: sandboxd.CommandOutputView{Head: 8000, Tail: 22000}}
}

func withWait(request sandboxd.CommandRequest, wait time.Duration) sandboxd.CommandRequest {
	request.Wait = min(wait, 25*time.Second)
	return request
}

// awaitExit polls the run until it exited, for at most two minutes.
func awaitExit(t *testing.T, ledger sandboxd.CommandLedger, id sandboxd.ExecutionID) sandboxd.CommandStatus {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		status, err := ledger.Status(ctx, id, 20*time.Second, sandboxd.CommandOutputView{Head: 8000, Tail: 22000})
		cancel()
		if err != nil && !sandboxd.IsTransient(err) {
			t.Fatal(err)
		}
		if err == nil && status.State != sandboxd.CommandRunning {
			if status.State != sandboxd.CommandExited {
				t.Fatalf("the run ended as %s", status.State)
			}
			return status
		}
	}
	t.Fatal(errors.New("the run did not exit within two minutes"))
	return sandboxd.CommandStatus{}
}

// runShell runs a short command through the ledger and returns its output.
func runShell(t *testing.T, ledger sandboxd.CommandLedger, script string) string {
	t.Helper()
	id := ledgerID(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := ledger.Begin(ctx, commandRequest(id, script)); err != nil {
		t.Fatal(err)
	}
	status := awaitExit(t, ledger, id)
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("%q failed: %+v", script, status)
	}
	return status.Output.Projection.Head + status.Output.Projection.Tail
}
