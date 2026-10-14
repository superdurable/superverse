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
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd/sandboxdtest"
)

func TestSandboxStdinActualAPI(t *testing.T) {
	client := sandboxdtest.ClientForTest(t)
	for _, scenario := range []struct{ name, command, output string }{
		{"noninteractive-eof", "cat >/dev/null; printf stdin-eof-observed", "stdin-eof-observed"},
		{"fast-exit-race", "printf immediate-exit", "immediate-exit"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			id := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-stdin-check", CallID: fmt.Sprintf("%s-%d", scenario.name, time.Now().UnixNano())})
			completeActualProcess(t, client, sandboxd.StartRequest{
				ExecutionID: id, Arguments: []string{"bash", "-c", scenario.command}, Timeout: 5 * time.Second, MaximumOutputBytes: 4096,
			}, scenario.output)
		})
	}
}

// This calls the actual pinned sandboxd process API. A loopback transport does
// not replace the daemon, its process manager, or Linux process-group semantics.
func TestSandboxCancellationActualAPI(t *testing.T) {
	client := sandboxdtest.ClientForTest(t)
	for _, scenario := range []string{"deadline", "caller-cancel", "close"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 10 * time.Second
			if scenario == "deadline" {
				timeout = time.Second
			}
			id := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-cancellation-check", CallID: fmt.Sprintf("%s-%d", scenario, time.Now().UnixNano())})
			stream, err := client.StartForTest(ctx, sandboxd.StartRequest{ExecutionID: id, Arguments: []string{"bash", "-c", `sleep 60 & child=$!; printf '%s\n' "$child"; wait`}, Timeout: timeout, MaximumOutputBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := stream.Close(); err != nil {
					t.Error(err)
				}
			}()
			var output strings.Builder
			for !strings.Contains(output.String(), "\n") {
				event, err := stream.RecvForTest()
				if err != nil {
					t.Fatal(err)
				}
				if event.Kind == sandboxd.ProcessEventStdoutForTest {
					output.Write(event.Data)
				}
			}
			child, err := strconv.Atoi(strings.TrimSpace(output.String()))
			if err != nil || child <= 1 {
				t.Fatal("actual child identity was not received")
			}
			started := time.Now()
			if scenario == "caller-cancel" {
				cancel()
			}
			if scenario == "close" {
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for {
				event, err := stream.RecvForTest()
				if err != nil {
					if scenario == "deadline" && (!errors.Is(err, sandboxd.ErrProcessTimeoutForTest) || errors.Is(err, sandboxd.ErrProcessStopFailedForTest) || !stream.TimedOutForTest()) {
						t.Fatalf("deadline classification: %v", err)
					}
					if scenario == "close" && !errors.Is(err, io.EOF) {
						t.Fatalf("close classification: %v", err)
					}
					break
				}
				if event.Kind == sandboxd.ProcessEventExitForTest {
					break
				}
			}
			if time.Since(started) > 7*time.Second {
				t.Fatal("actual process cancellation exceeded its cleanup bound")
			}
			// A zombie is already stopped, but is not described as reaped. The
			// daemon's PID-1 reaping remains a separate observed limitation.
			probeID := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-cancellation-check", CallID: fmt.Sprintf("probe-%d", time.Now().UnixNano())})
			probeContext, probeCancel := context.WithTimeout(context.Background(), 7*time.Second)
			defer probeCancel()
			command := fmt.Sprintf(`deadline=$((SECONDS+2)); while test -r /proc/%d/stat; do state=$(awk '{print $3}' /proc/%d/stat); test "$state" = Z && exit 0; test "$SECONDS" -lt "$deadline" || exit 1; sleep 0.05; done`, child, child)
			probe, err := client.StartForTest(probeContext, sandboxd.StartRequest{ExecutionID: probeID, Arguments: []string{"bash", "-c", command}, Timeout: 5 * time.Second, MaximumOutputBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := probe.Close(); err != nil {
					t.Error(err)
				}
			}()
			for {
				event, err := probe.RecvForTest()
				if err != nil {
					t.Fatal(err)
				}
				if event.Kind == sandboxd.ProcessEventExitForTest {
					if event.ExitCode != 0 {
						t.Fatal("actual child process survived cancellation")
					}
					break
				}
			}
		})
	}
}

// Pause only the runner-owned real daemon. This exercises cancellation while
// connection/dispatch is pending rather than substituting a scripted server.
func TestSandboxStartBudgetActualAPI(t *testing.T) {
	container := os.Getenv("SUPERVERSE_TEST_SANDBOX_CONTAINER")
	if container == "" {
		t.Fatal("the runner-owned actual sandboxd container is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	identity, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Name}}", container).Output()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(identity)), "/sv2-process-cancel-") {
		t.Fatal("the actual fault-injection container is not owned by this runner")
	}
	client := sandboxdtest.ClientForTest(t)
	id := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-start-budget-check", CallID: fmt.Sprintf("budget-%d", time.Now().UnixNano())})
	request := sandboxd.StartRequest{ExecutionID: id, Arguments: []string{"bash", "-c", "printf bounded-start"}, Timeout: 250 * time.Millisecond, MaximumOutputBytes: 4096}
	t.Run("already-cancelled", func(t *testing.T) {
		cancelled, stop := context.WithCancel(context.Background())
		stop()
		stream, err := client.StartForTest(cancelled, request)
		if stream != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("an already cancelled start was not rejected")
		}
		// The same ID must still be available for the actual command below.
		completeActualProcess(t, client, request, "bounded-start")
	})
	t.Run("dispatch-budget", func(t *testing.T) {
		if err := exec.CommandContext(ctx, "docker", "pause", container).Run(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := exec.CommandContext(cleanup, "docker", "unpause", container).Run(); err != nil {
				t.Error(err)
			}
		})
		// A fresh lazy connection must negotiate with the paused daemon, so
		// this budget also covers connection establishment before Start returns.
		pendingClient := sandboxdtest.ClientForTest(t)
		started := time.Now()
		stream, err := pendingClient.StartForTest(context.Background(), request)
		if err == nil {
			_, err = stream.RecvForTest()
			// Missing Init is an uncertain effect; do not describe this as a
			// successfully stopped command or silently retry it.
			if closeErr := stream.Close(); closeErr == nil {
				t.Error("a missing Init must retain uncertain cleanup")
			}
		}
		if !errors.Is(err, sandboxd.ErrProcessTimeoutForTest) || !errors.Is(err, sandboxd.ErrProcessStopFailedForTest) || time.Since(started) > 2*time.Second {
			t.Fatalf("the real pending Start exceeded its timeout, which could not be confirmed as a stop: %v", err)
		}
	})
}

func TestSandboxExecutionReuseActualAPI(t *testing.T) {
	client := sandboxdtest.ClientForTest(t)
	id := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-execution-reuse-check", CallID: fmt.Sprintf("reuse-%d", time.Now().UnixNano())})
	request := sandboxd.StartRequest{ExecutionID: id, Arguments: []string{"bash", "-c", "printf first-run"}, Timeout: 5 * time.Second, MaximumOutputBytes: 4096}
	old := completeActualProcess(t, client, request, "first-run")
	request.Arguments = []string{"bash", "-c", "sleep 0.1; printf second-run"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err := client.StartForTest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := current.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	readActualProcess(t, current, "second-run")
}

// The pinned sandboxd reports a missing working directory as a missing
// command. Each refusal must prove that no process ran and release its
// execution ID, so the same call identity can start once the input is fixed.
func TestSandboxStartRefusalActualAPI(t *testing.T) {
	client := sandboxdtest.ClientForTest(t)
	for _, scenario := range []struct {
		name      string
		arguments []string
		cwd       sandboxd.WorkspacePath
	}{
		{"missing-working-directory", []string{"bash", "-c", "printf unreachable"}, ".,"},
		{"missing-command", []string{"superverse-missing-command"}, "."},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			id := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: "live-start-refusal-check", CallID: fmt.Sprintf("%s-%d", scenario.name, time.Now().UnixNano())})
			request := sandboxd.StartRequest{ExecutionID: id, Arguments: scenario.arguments, Timeout: 5 * time.Second, MaximumOutputBytes: 4096}.WithWorkingDirectoryForTest(scenario.cwd)
			stream, err := client.StartForTest(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = stream.RecvForTest(); !errors.Is(err, sandboxd.ErrProcessNotStarted) {
				t.Fatalf("refused start was not reported as not started: %v", err)
			}
			if err = stream.Close(); err != nil {
				t.Fatalf("refused start left an uncertain effect: %v", err)
			}
			request.Arguments = []string{"bash", "-c", "printf corrected"}
			request = request.WithWorkingDirectoryForTest(".")
			completeActualProcess(t, client, request, "corrected")
		})
	}
}

func completeActualProcess(t *testing.T, client *sandboxd.SandboxClient, request sandboxd.StartRequest, expected string) *sandboxd.ProcessStreamForTest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.StartForTest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stream.Close(); err != nil {
			t.Error(err)
		}
	})
	readActualProcess(t, stream, expected)
	return stream
}

func readActualProcess(t *testing.T, stream *sandboxd.ProcessStreamForTest, expected string) {
	t.Helper()
	var output strings.Builder
	for {
		event, err := stream.RecvForTest()
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == sandboxd.ProcessEventStdoutForTest {
			output.Write(event.Data)
		}
		if event.Kind == sandboxd.ProcessEventExitForTest {
			if event.ExitCode != 0 || output.String() != expected {
				t.Fatal("the actual process did not complete with its expected output")
			}
			return
		}
	}
}
