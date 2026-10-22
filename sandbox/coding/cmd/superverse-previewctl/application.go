// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The Preview application's fixed local contract (coding profile v4): the
// app serves the Preview route on 3000 and its Worker listens on 18811 for
// the Sandbox's own Dex.
const (
	applicationPort      = 3000
	applicationHealthURL = "http://127.0.0.1:3000/api/health"
	previewWorkerAddress = "127.0.0.1:18811"
	// applicationStartupBudget is how long a started application may take to
	// pass its health probe before the start fails as the application's own.
	applicationStartupBudget = 5 * time.Minute
	// applicationBinary is where the build writes the server inside its tree.
	applicationBinary = ".preview/server"
	maximumTailBytes  = 4 << 10
	// minimumRedactedBytes is the shortest configured value redacted from an
	// answer; shorter values cannot be credentials.
	minimumRedactedBytes = 8
)

// applicationIdentity is every accepted input of one application start: a
// running application with the same identity is not rebuilt.
type applicationIdentity struct {
	Commit              string `json:"commit,omitempty"`
	ConfigurationDigest string `json:"configurationDigest,omitempty"`
	GrantGeneration     uint64 `json:"grantGeneration,omitempty"`
}

// application owns the build and the process group of the Preview
// application. Starts and stops run one at a time, each after the previous.
type application struct {
	state stateDirectory
	owner *owner

	mu sync.Mutex
	// attempt is the latest start, nil after a stop.
	attempt *applicationAttempt
	running *runningApplication
	// stopping is set while a stop runs; last is closed when the latest
	// start or stop finished.
	stopping bool
	last     chan struct{}
}

type applicationAttempt struct {
	identity applicationIdentity
	answer   answer
	// reported is set once begin answered this attempt's failure; the next
	// begin of the same identity builds again.
	reported bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func (a *applicationAttempt) settled() bool {
	switch a.answer.State {
	case stateFailed, stateSourceMissing, stateError:
		return true
	}
	return false
}

func (a *applicationAttempt) reply() answer {
	reply := a.answer
	reply.applicationIdentity = a.identity
	return reply
}

type runningApplication struct {
	identity  applicationIdentity
	process   *child
	worker    *child
	tree      sourceTree
	logOffset int64
	redact    redactor
}

func newApplication(state stateDirectory, records *owner) *application {
	finished := make(chan struct{})
	close(finished)
	return &application{state: state, owner: records, last: finished}
}

// begin admits a start of identity, or answers the start already admitted for
// it. A different identity supersedes the start in progress.
func (a *application) begin(identity applicationIdentity, environment []string) answer {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.observeExit()
	if current := a.attempt; current != nil && current.identity == identity && (!current.settled() || !current.reported) {
		current.reported = current.settled()
		return current.reply()
	}
	if a.attempt != nil {
		a.attempt.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempt := &applicationAttempt{identity: identity, answer: answer{State: stateBuilding}, cancel: cancel, done: make(chan struct{})}
	previous := a.last
	a.attempt, a.last = attempt, attempt.done
	go a.start(ctx, attempt, previous, environment)
	return attempt.reply()
}

// status answers the start in progress, else the application that serves
// with its own identity, which a failed later build leaves running, else the
// outcome of the latest start.
func (a *application) status() answer {
	a.mu.Lock()
	a.observeExit()
	var reply answer
	switch {
	case a.stopping:
		reply = answer{State: stateStopping}
	case a.attempt != nil && (a.attempt.answer.State == stateBuilding || a.attempt.answer.State == stateStarting):
		reply = a.attempt.reply()
	case a.running != nil:
		reply = answer{State: stateRunning, applicationIdentity: a.running.identity}
	case a.attempt != nil:
		reply = a.attempt.reply()
	default:
		reply = answer{State: stateStopped}
	}
	a.mu.Unlock()
	if reply.State == stateRunning {
		reply.ApplicationPort = applicationPort
		reply.WorkerListening = listening(previewWorkerAddress)
	}
	return reply
}

// stop requests that the start in progress and the running application end,
// and answers stopping until they did.
func (a *application) stop() answer {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.observeExit()
	switch {
	case a.stopping:
		return answer{State: stateStopping}
	case a.attempt == nil && a.running == nil:
		return answer{State: stateStopped}
	}
	if a.attempt != nil {
		a.attempt.cancel()
		a.attempt = nil
	}
	previous, done := a.last, make(chan struct{})
	a.stopping, a.last = true, done
	go func() {
		defer close(done)
		<-previous
		a.mu.Lock()
		running := a.running
		a.running = nil
		a.mu.Unlock()
		if running != nil {
			running.stop()
			running.tree.release()
			a.owner.set("application", nil)
			a.owner.set("application-worker", nil)
		}
		a.mu.Lock()
		a.stopping = false
		a.mu.Unlock()
	}()
	return answer{State: stateStopping}
}

// shutdown stops everything and waits for it, for the daemon's stop.
func (a *application) shutdown() {
	a.stop()
	a.mu.Lock()
	last := a.last
	a.mu.Unlock()
	<-last
}

// observeExit forgets a running application that exited on its own, and
// turns it into a failure of its start when that start is the latest. A start
// still waiting for its application's health probe observes it itself. a.mu
// is held.
func (a *application) observeExit() {
	running := a.running
	if running == nil || (running.process.running() && running.worker.running()) || (a.attempt != nil && a.attempt.answer.State == stateStarting) {
		return
	}
	a.running = nil
	running.stop()
	running.tree.release()
	a.owner.set("application", nil)
	a.owner.set("application-worker", nil)
	if a.attempt != nil && a.attempt.identity == running.identity && a.attempt.answer.State == stateRunning {
		a.attempt.answer = answer{State: stateFailed, Reason: fmt.Sprintf("the application exited (%s): %s",
			running.process.exitStatus(), running.redact.apply(tail(a.state.log("app.log"), running.logOffset)))}
	}
}

func (a *application) settle(attempt *applicationAttempt, settled answer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt.answer = settled
}

// start builds the commit's tree and swaps the application to it. A failed
// build leaves the running application alone; a started application that
// exits or misses its health probe within the startup budget is stopped, and
// then no application runs.
func (a *application) start(ctx context.Context, attempt *applicationAttempt, previous <-chan struct{}, environment []string) {
	defer close(attempt.done)
	<-previous
	if ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	serving := a.running != nil && a.running.identity == attempt.identity && a.running.process.running() && a.running.worker.running()
	a.mu.Unlock()
	if serving {
		a.settle(attempt, answer{State: stateRunning})
		return
	}
	tree, err := ensureSource(ctx, a.state, attempt.identity.Commit, true)
	switch {
	case errors.Is(err, errSourceMissing):
		a.settle(attempt, answer{State: stateSourceMissing, Reason: err.Error()})
		return
	case err != nil:
		if ctx.Err() == nil {
			a.settle(attempt, answer{State: stateError, Reason: err.Error()})
		}
		return
	}
	redact := newRedactor(environment)
	if err = a.build(ctx, tree.path, environment, redact); err != nil {
		tree.release()
		if ctx.Err() == nil {
			a.settle(attempt, answer{State: stateFailed, Reason: err.Error()})
		}
		return
	}
	a.swap(ctx, attempt, tree, environment, redact)
}

// build runs the application's fixed build steps in its own tree. No step
// runs repository code from the workspace, and the result is a binary, so
// the swap starts no compiler.
func (a *application) build(ctx context.Context, tree string, environment []string, redact redactor) error {
	output, err := a.state.openLog("build.log")
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	offset := logSize(output)
	install := "install"
	if _, err = os.Stat(filepath.Join(tree, "web", "package-lock.json")); err == nil {
		install = "ci"
	}
	// The template keeps its generated API clients out of Git, so a clean
	// tree generates them before the web build and the server build.
	for _, step := range [][]string{
		{"npm", "--prefix", "web", install, "--ignore-scripts"},
		{"make", "generate"},
		{"npm", "--prefix", "web", "run", "build"},
		{"go", "build", "-o", applicationBinary, "./cmd/server"},
	} {
		_, _ = fmt.Fprintf(output, "$ %s\n", strings.Join(step, " "))
		process, err := startChild(tree, step, environment, output)
		if err != nil {
			return err
		}
		a.owner.set("build", process)
		select {
		case <-process.exited:
		case <-ctx.Done():
			process.stop()
		}
		a.owner.set("build", nil)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if process.result != nil {
			return fmt.Errorf("the application build step %q failed (%s): %s", strings.Join(step, " "), process.exitStatus(), redact.apply(tail(a.state.log("build.log"), offset)))
		}
	}
	return nil
}

// swap stops the running application, starts the built one and waits for
// its health probe. Only a healthy start deletes the previous tree.
func (a *application) swap(ctx context.Context, attempt *applicationAttempt, tree sourceTree, environment []string, redact redactor) {
	a.mu.Lock()
	previous := a.running
	a.running = nil
	a.mu.Unlock()
	if previous != nil {
		previous.stop()
		a.owner.set("application", nil)
		a.owner.set("application-worker", nil)
	}
	output, err := a.state.openLog("app.log")
	if err != nil {
		tree.release()
		a.settle(attempt, answer{State: stateError, Reason: err.Error()})
		return
	}
	offset := logSize(output)
	_, _ = fmt.Fprintf(output, "=== Preview application %s ===\n", attempt.identity.Commit)
	// Both processes read one platform-owned YAML document from the exact source tree.
	runtime := []byte(fmt.Sprintf("worker:\n  mode: push\n  bindAddress: %s\n  target: %s\n  readinessAddress: 127.0.0.1:8081\n  maxConcurrentTasks: 4\n  idleStreamsPerQueue: 1\n  stopDrainTimeout: 10s\n", previewWorkerAddress, previewWorkerAddress))
	if err := writeApplicationRuntime(runtime); err != nil {
		_ = output.Close()
		tree.release()
		a.settle(attempt, answer{State: stateError, Reason: fmt.Sprintf("write Preview runtime: %v", err)})
		return
	}
	process, err := startChild(tree.path, []string{filepath.Join(tree.path, applicationBinary), "web"}, applicationEnvironment(environment), output)
	var worker *child
	if err == nil {
		a.owner.set("application", process)
		worker, err = startChild(tree.path, []string{filepath.Join(tree.path, applicationBinary), "worker"}, applicationEnvironment(environment), output)
		if err != nil {
			process.stop()
			a.owner.set("application", nil)
		} else {
			a.owner.set("application-worker", worker)
		}
	}
	_ = output.Close()
	if err != nil {
		tree.release()
		a.settle(attempt, answer{State: stateError, Reason: err.Error()})
		return
	}
	a.owner.set("application", process)
	a.mu.Lock()
	a.running = &runningApplication{identity: attempt.identity, process: process, worker: worker, tree: tree, logOffset: offset, redact: redact}
	attempt.answer = answer{State: stateStarting}
	a.mu.Unlock()

	err = waitHealthy(ctx, process, worker)
	if err == nil {
		a.settle(attempt, answer{State: stateRunning})
		if previous != nil {
			previous.tree.release()
			if previous.tree.commit != tree.commit {
				removeSourceTree(a.state, previous.tree.commit)
			}
		}
		return
	}
	worker.stop()
	process.stop()
	a.owner.set("application", nil)
	a.owner.set("application-worker", nil)
	a.mu.Lock()
	if a.running != nil && a.running.process == process {
		a.running = nil
	}
	a.mu.Unlock()
	tree.release()
	if previous != nil {
		previous.tree.release()
	}
	if ctx.Err() == nil {
		a.settle(attempt, answer{State: stateFailed, Reason: err.Error() + ": " + redact.apply(tail(a.state.log("app.log"), offset))})
	}
}

// applicationEnvironment is the caller's environment with the fixed local
// addresses of profile v4, which no caller value overrides.
func applicationEnvironment(environment []string) []string {
	return append(append([]string(nil), environment...),
		fmt.Sprintf("PORT=%d", applicationPort),
		"HOST=0.0.0.0",
		"DEX_FLOW_SERVICE_ADDRESS="+dexAddress,
		"DEX_BLOB_CACHE_DIR="+filepath.Join(runtimeDirectory, "app-blobs"),
	)
}

// waitHealthy waits until the application passes its health probe, exits,
// or the startup budget ends.
func waitHealthy(ctx context.Context, process, worker *child) error {
	ctx, cancel := context.WithTimeout(ctx, applicationStartupBudget)
	defer cancel()
	for {
		if healthy(ctx) && workerHealthy(ctx) {
			return nil
		}
		select {
		case <-process.exited:
			return fmt.Errorf("the web process exited during startup (%s)", process.exitStatus())
		case <-worker.exited:
			return fmt.Errorf("the Worker exited during startup (%s)", worker.exitStatus())
		case <-ctx.Done():
			return fmt.Errorf("the application did not pass GET /api/health within %s", applicationStartupBudget)
		case <-time.After(time.Second):
		}
	}
}

func healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, applicationHealthURL, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}

func listening(address string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func logSize(file *os.File) int64 {
	info, err := file.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}

// tail is the end of a log written since offset, at most maximumTailBytes.
func tail(path string, offset int64) string {
	file, err := os.Open(path) //nolint:gosec // a log of the state directory
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	size := logSize(file)
	if offset < size-maximumTailBytes {
		offset = size - maximumTailBytes
	}
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(file, maximumTailBytes))
	return strings.TrimSpace(strings.ToValidUTF8(string(data), ""))
}

// redactor removes the storage credentials and project values the platform
// passed from text an answer repeats: the application may print them.
type redactor []string

func newRedactor(environment []string) redactor {
	var values redactor
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "AWS_") || strings.HasPrefix(name, "DEX_PROJECT_") {
			values = append(values, value)
		}
		if name == "AWS_SHARED_CREDENTIALS_FILE" {
			profile, err := os.ReadFile(value) //nolint:gosec // the credential profile the platform delivered
			if err != nil {
				continue
			}
			for line := range strings.SplitSeq(string(profile), "\n") {
				if _, secret, found := strings.Cut(line, "="); found {
					values = append(values, strings.TrimSpace(secret))
				}
			}
		}
	}
	values = slices.DeleteFunc(values, func(value string) bool { return len(value) < minimumRedactedBytes })
	// Longer values first, so a value that contains another is removed whole.
	slices.SortFunc(values, func(first, second string) int { return len(second) - len(first) })
	return values
}

func (r redactor) apply(text string) string {
	for _, value := range r {
		text = strings.ReplaceAll(text, value, "[REDACTED]")
	}
	return text
}

// stop ends both owned process groups before their source tree is released.
func (r *runningApplication) stop() {
	r.worker.stop()
	r.process.stop()
}

func workerHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8081/readyz", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

// writeApplicationRuntime replaces only the supervisor-owned runtime file.
func writeApplicationRuntime(contents []byte) error {
	directory := "/tmp/superverse-app"
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "runtime.yaml"), contents, 0o600)
}
