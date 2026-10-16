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
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
)

// procfsAddress is a local address of /proc/net/tcp or tcp6.
var procfsAddress = regexp.MustCompile(`^[0-9A-F]{8}([0-9A-F]{24})?:[0-9A-F]{4}$`)

// apptestSummary is the part of the summary line of superverse-previewctl
// apptest run that these scenarios read.
type apptestSummary struct {
	Directory  string                    `json:"directory"`
	Kept       bool                      `json:"kept"`
	Phase      string                    `json:"phase"`
	ExitCode   int                       `json:"exitCode"`
	TimedOut   bool                      `json:"timedOut"`
	Executions map[string]map[string]int `json:"executions"`
}

// TestApplicationTestEngineActualSandbox drives superverse-previewctl apptest
// run through the real sandboxd of a coding image container that the test
// owns, with the image's real Temporal and dexcli: the engine listens on
// loopback only, a second run queues and never kills the first, a queue wait
// past the deadline and a command past its timeout exit 124 after teardown,
// killing the caller's process group leaves no engine process or listener,
// the orphans of a command end with it, and at most three failed run directories stay.
func TestApplicationTestEngineActualSandbox(t *testing.T) {
	container := sandboxdtest.StartCodingContainerForTest(t)
	client := container.Client()

	t.Run("a run binds loopback only and removes its directory", func(t *testing.T) {
		// The command lists the listeners while the engine runs; sandboxd's own
		// ports 8080 (1F90) and 9090 (2382) are the only ones beyond loopback.
		output, summary := runApptest(t, client, "2m", "bash", "-c",
			`echo "dex=$DEX_FLOW_SERVICE_ADDRESS worker=$DEX_WORKER_TARGET"; awk 'FNR>1 && $4=="0A" {print $2}' /proc/net/tcp /proc/net/tcp6`)
		if summary.ExitCode != 0 || summary.Kept || summary.Phase != "command" {
			t.Fatalf("a passing run: %+v\n%s", summary, output)
		}
		if !strings.Contains(output, "dex=127.0.0.1:8805 worker=127.0.0.1:18813") {
			t.Fatalf("the command did not get the test engine's addresses:\n%s", output)
		}
		for _, port := range []string{":1C43", ":2265", ":2266"} {
			if !strings.Contains(output, "0100007F"+port) {
				t.Fatalf("the engine does not listen on 127.0.0.1%s:\n%s", port, output)
			}
		}
		for line := range strings.SplitSeq(output, "\n") {
			address := strings.TrimSpace(line)
			loopback := strings.HasPrefix(address, "0100007F:") || strings.HasPrefix(address, "00000000000000000000000001000000:")
			sandboxd := strings.HasSuffix(address, ":1F90") || strings.HasSuffix(address, ":2382")
			if procfsAddress.MatchString(address) && !loopback && !sandboxd {
				t.Fatalf("a listener of the run is not on loopback: %s", address)
			}
		}
		if exists := shell(t, client, "test -e "+summary.Directory+" && echo kept || echo removed"); exists != "removed" {
			t.Fatalf("a passing run kept its directory %s", summary.Directory)
		}
	})

	t.Run("a second run queues and the first is never killed", func(t *testing.T) {
		var wait sync.WaitGroup
		var first apptestSummary
		var firstOutput string
		wait.Go(func() { firstOutput, first = runApptest(t, client, "2m", "bash", "-c", "sleep 15; echo first-done") })
		awaitEngine(t, client)
		started := time.Now()
		output, second := runApptest(t, client, "2m", "echo", "second-done")
		wait.Wait()
		if first.ExitCode != 0 || !strings.Contains(firstOutput, "first-done") {
			t.Fatalf("the first run was disturbed: %+v\n%s", first, firstOutput)
		}
		if second.ExitCode != 0 || !strings.Contains(output, "another application test run holds the engine; waiting") || time.Since(started) < 5*time.Second {
			t.Fatalf("the second run did not wait for the first: %+v\n%s", second, output)
		}
	})

	t.Run("a queue wait past the deadline exits 124 before any engine starts", func(t *testing.T) {
		var wait sync.WaitGroup
		var first apptestSummary
		wait.Go(func() { _, first = runApptest(t, client, "2m", "sleep", "25") })
		awaitEngine(t, client)
		output, queued := runApptest(t, client, "5s", "echo", "never")
		wait.Wait()
		if queued.ExitCode != 124 || queued.Phase != "queued" || strings.Contains(output, "never\n") {
			t.Fatalf("the queued run did not give up at its deadline: %+v\n%s", queued, output)
		}
		if first.ExitCode != 0 {
			t.Fatalf("the holder was disturbed by the queued run: %+v", first)
		}
	})

	t.Run("a command past its timeout exits 124 after teardown", func(t *testing.T) {
		output, summary := runApptest(t, client, "20s", "bash", "-c", "(sleep 300 &) ; sleep 300")
		if summary.ExitCode != 124 || !summary.TimedOut || !summary.Kept {
			t.Fatalf("the timed-out run: %+v\n%s", summary, output)
		}
		requireNoEngine(t, client)
		// The orphan the command left in the background is gone too.
		if left := shell(t, client, `pgrep -f 'sleep 30[0]' | wc -l`); left != "0" {
			t.Fatalf("%s processes of the timed-out command survived", left)
		}
	})

	t.Run("killing the caller's process group leaves no process or listener", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = client.RunToExit(ctx, apptestRequest("2m", "sleep", "300"))
		}()
		awaitEngine(t, client)
		// The client kills the whole process group of a cancelled command.
		cancel()
		<-done
		requireNoEngine(t, client)
		// The next run finds no leftover engine and starts its own.
		if _, summary := runApptest(t, client, "2m", "true"); summary.ExitCode != 0 {
			t.Fatalf("the run after a killed one: %+v", summary)
		}
	})

	t.Run("failed runs keep at most three directories", func(t *testing.T) {
		for range 4 {
			if _, summary := runApptest(t, client, "2m", "false"); summary.ExitCode != 1 || !summary.Kept {
				t.Fatalf("a failing run: %+v", summary)
			}
		}
		kept := shell(t, client, "ls -d /tmp/superverse-apptest/runs/*/summary.json | wc -l")
		if kept != "3" {
			t.Fatalf("%s failed run directories stayed, want 3", kept)
		}
	})
}

func apptestRequest(timeout string, command ...string) sandboxd.StartRequest {
	return sandboxd.StartRequest{
		ExecutionID: sandboxd.PlatformExecutionID("apptest-engine"),
		Arguments:   append([]string{"superverse-previewctl", "apptest", "run", "--timeout", timeout, "--"}, command...),
		Environment: sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot),
		Timeout:     5 * time.Minute,
	}
}

// runApptest runs one apptest run to its exit and returns its output and its
// summary line.
func runApptest(t *testing.T, client *sandboxd.SandboxClient, timeout string, command ...string) (string, apptestSummary) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := client.RunToExit(ctx, apptestRequest(timeout, command...))
	if err != nil {
		t.Errorf("apptest run %v: %v", command, err)
		return "", apptestSummary{}
	}
	combined := output.Stdout + output.Stderr
	index := strings.LastIndex(output.Stdout, "SUPERVERSE-APPTEST-SUMMARY ")
	var summary apptestSummary
	if index < 0 {
		t.Errorf("apptest run %v printed no summary:\n%s", command, combined)
		return combined, summary
	}
	line, _, _ := strings.Cut(output.Stdout[index+len("SUPERVERSE-APPTEST-SUMMARY "):], "\n")
	if err = json.Unmarshal([]byte(line), &summary); err != nil {
		t.Errorf("the summary line does not decode: %v: %s", err, line)
	}
	if summary.ExitCode != output.ExitCode {
		t.Errorf("the run exited %d, but its summary says %d", output.ExitCode, summary.ExitCode)
	}
	return combined, summary
}

// shell runs a short script and returns its trimmed stdout.
func shell(t *testing.T, client *sandboxd.SandboxClient, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := client.RunToExit(ctx, sandboxd.StartRequest{ExecutionID: sandboxd.PlatformExecutionID("apptest-probe"), Arguments: []string{"bash", "-c", script}, Timeout: time.Minute})
	if err != nil || output.ExitCode != 0 {
		t.Fatalf("%s: %v %+v", script, err, output)
	}
	return strings.TrimSpace(output.Stdout)
}

// awaitEngine waits until the test engine's Dex listens.
func awaitEngine(t *testing.T, client *sandboxd.SandboxClient) {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if shell(t, client, `awk 'FNR>1 && $4=="0A" && $2=="0100007F:2265"' /proc/net/tcp | wc -l`) == "1" {
			return
		}
	}
	t.Fatal("the test engine did not start within a minute")
}

// requireNoEngine requires that no engine process and no engine listener
// remain.
func requireNoEngine(t *testing.T, client *sandboxd.SandboxClient) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(250 * time.Millisecond) {
		processes := shell(t, client, `pgrep -f '^/usr/local/libexec/superverse/' | wc -l`)
		listeners := shell(t, client, `awk 'FNR>1 && $4=="0A" && ($2 ~ /:(1C43|1C4D|1C4E|2265|2266|497D)$/)' /proc/net/tcp | wc -l`)
		if processes == "0" && listeners == "0" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine left %s processes and %s listeners", processes, listeners)
		}
	}
}
