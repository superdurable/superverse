// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	processv1 "sigs.k8s.io/agent-sandbox/packages/sandboxd/spec/process/v1"

	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// processSignalTimeout bounds one kill request. A process that cannot be
// signalled within it stays an uncertain effect.
const processSignalTimeout = 5 * time.Second

// start reserves request.ExecutionID and starts the process. The returned
// stream owns the process until its exit event or Close. request.Timeout
// covers connection and dispatch as well as the process itself: when it
// passes, or ctx ends, the stream kills the process.
func (client *SandboxClient) start(ctx context.Context, request StartRequest) (*processStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	environment := environmentMap(request.Environment)
	limit := request.MaximumOutputBytes
	if limit <= 0 || limit > maximumProcessOutputBytes {
		limit = maximumProcessOutputBytes
	}
	stream := &processStream{
		client: client, executionID: request.ExecutionID, done: make(chan struct{}),
		budget: outputBudget{limit: limit, subject: "command", failure: func(cause error) *ToolFailure {
			// The command ran until the budget was exceeded, so it may have changed files.
			return NewToolFailure(toolfailure.CommandOutputLimitExceeded, cause).WithEffect(toolfailure.EffectPartial)
		}},
	}
	if err := client.reserve(stream); err != nil {
		return nil, err
	}
	// sandboxd cancels a process by killing only its parent. The stream owns
	// the RPC context so that its kill reaches the whole process group first.
	streamContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stream.context, stream.cancel = streamContext, cancel
	go stream.watchCancellation(ctx, time.NewTimer(request.Timeout))
	cwd := string(request.workingDirectory)
	if cwd == "" {
		cwd = "."
	}
	events, err := client.process.Start(streamContext, &processv1.StartRequest{
		Config: &processv1.ProcessConfig{Command: request.Arguments, EnvVars: environment, Cwd: proto.String(cwd)},
	})
	if err != nil {
		stream.finish()
		return nil, errors.Join(fmt.Errorf("start sandbox process: %w", err), ctx.Err(), stream.interruptionError())
	}
	stream.events = events
	return stream, nil
}

// processStream is the live event stream of one started process. A caller
// reads it to its exit event; Close abandons it and kills the process. Close
// may run concurrently with Recv and unblocks it. A closed stream cannot be
// reattached to its process.
type processStream struct {
	client      *SandboxClient
	executionID ExecutionID
	events      processv1.ProcessService_StartClient
	//nolint:containedctx // the stream owns its RPC beyond the caller's context
	context context.Context
	cancel  context.CancelFunc
	budget  outputBudget

	recvMu      sync.Mutex
	initialized bool         // guarded by recvMu
	processID   atomic.Int32 // set at init under the client lock, read by interrupt
	closed      atomic.Bool
	done        chan struct{} // closed once the execution is released
	finishOnce  sync.Once
	stopOnce    sync.Once
	failureMu   sync.Mutex
	stopCause   error
	stopError   error
}

func (stream *processStream) watchCancellation(ctx context.Context, timer *time.Timer) {
	defer timer.Stop()
	select {
	case <-stream.done:
	case <-ctx.Done():
		stream.interrupt(ctx.Err())
	case <-timer.C:
		stream.interrupt(errProcessTimeout)
	}
}

// recv returns the next event; after the exit event or Close it returns
// io.EOF. ErrProcessNotStarted proves that no process ran. Any other error
// has stopped the process, or reports that its outcome is uncertain.
func (stream *processStream) recv() (processEvent, error) {
	stream.recvMu.Lock()
	defer stream.recvMu.Unlock()
	if stream.closed.Load() {
		return processEvent{}, io.EOF
	}
	response, err := stream.events.Recv()
	if err != nil && !stream.initialized && startRefused(err) {
		// sandboxd answers these codes only before it starts the process, so
		// nothing ran and there is nothing to stop or reconcile.
		stream.closed.Store(true)
		stream.finish()
		return processEvent{}, fmt.Errorf("%w: %w", ErrProcessNotStarted, err)
	}
	if err != nil {
		stream.interrupt(nil)
		if errors.Is(err, io.EOF) {
			err = errStreamWithoutExit
		} else {
			err = fmt.Errorf("receive sandbox process event: %w", err)
		}
		return processEvent{}, errors.Join(err, stream.interruptionError())
	}
	event, err := stream.accept(response)
	if err != nil {
		stream.interrupt(nil)
		return processEvent{}, err
	}
	return event, nil
}

// accept applies one checked event to the stream: init records the process,
// output is charged to the budget and exit ends the stream.
func (stream *processStream) accept(response *processv1.StartResponse) (processEvent, error) {
	event, err := decodeProcessEvent(response, stream.initialized)
	if err != nil {
		return processEvent{}, err
	}
	switch event.Kind {
	case processEventInit:
		err = stream.initialize(event.processID)
	case processEventStdout, processEventStderr:
		err = stream.budget.charge(stream.context, stream.client.process, stream.processID.Load(), len(event.Data))
	case processEventExit:
		stream.closed.Store(true)
		stream.finish()
	}
	return event, err
}

// decodeProcessEvent checks one sandboxd Start event against the protocol
// order, given whether the init event has arrived: exactly one init with a
// process ID, then output, then one exit.
func decodeProcessEvent(response *processv1.StartResponse, initialized bool) (processEvent, error) {
	var event processEvent
	switch payload := response.GetEvent().(type) {
	case *processv1.StartResponse_Init:
		if initialized || payload.Init.GetProcessId() <= 0 {
			return processEvent{}, errors.New("sandbox process stream returned an invalid init event")
		}
		return processEvent{Kind: processEventInit, processID: payload.Init.GetProcessId()}, nil
	case *processv1.StartResponse_Stdout:
		event = processEvent{Kind: processEventStdout, Data: payload.Stdout}
	case *processv1.StartResponse_Stderr:
		event = processEvent{Kind: processEventStderr, Data: payload.Stderr}
	case *processv1.StartResponse_Exit:
		if payload.Exit == nil {
			return processEvent{}, errors.New("sandbox process stream returned an empty exit event")
		}
		event = processEvent{Kind: processEventExit, ExitCode: int(payload.Exit.ExitCode)}
	default:
		return processEvent{}, fmt.Errorf("unsupported sandbox process event %T", payload)
	}
	if !initialized {
		return processEvent{}, fmt.Errorf("sandbox process %s arrived before init", event.Kind)
	}
	return event, nil
}

// initialize records the process of this execution and gives it stdin EOF;
// sandboxd opens a stdin pipe even without a PTY. An Init that arrives after
// the execution was released is errProcessNotActive: the process started and
// was not stopped.
func (stream *processStream) initialize(processID int32) error {
	client := stream.client
	client.mu.Lock()
	owned := client.active[stream.executionID] == stream
	if owned {
		stream.processID.Store(processID)
	}
	client.mu.Unlock()
	if !owned {
		return fmt.Errorf("%w: %s", errProcessNotActive, stream.executionID)
	}
	stream.initialized = true
	return client.writeInput(stream.context, processID, nil)
}

// interrupt stops the stream once: it kills the process of this execution,
// bounded by processSignalTimeout, and releases the execution. Before Init,
// or when the kill fails, the outcome stays uncertain; Recv and Close report
// it, and the caller reconciles instead of running the command again.
func (stream *processStream) interrupt(cause error) {
	stream.stopOnce.Do(func() {
		select {
		case <-stream.done:
			return
		default:
		}
		stream.failureMu.Lock()
		stream.stopCause = cause
		stream.failureMu.Unlock()
		var stopErr error
		if processID := stream.processID.Load(); processID == 0 {
			stopErr = fmt.Errorf("%w: %w: %s", errProcessStopFailed, errProcessNotInitialized, stream.executionID)
		} else if err := killProcess(stream.context, stream.client.process, processID); err != nil {
			stopErr = fmt.Errorf("%w: stop sandbox process group: %w", errProcessStopFailed, err)
		}
		select {
		case <-stream.done:
		default:
			stream.failureMu.Lock()
			stream.stopError = stopErr
			stream.failureMu.Unlock()
		}
		stream.finish()
	})
}

func (stream *processStream) interruptionError() error {
	stream.failureMu.Lock()
	defer stream.failureMu.Unlock()
	return errors.Join(stream.stopCause, stream.stopError)
}

// TimedOutForTest reports whether the stream killed its process because the request's
// Timeout passed, and confirmed that the process group is gone. A process that
// exits as it is killed still yields its exit event, so the caller asks after
// either end of the stream.
func (stream *processStream) TimedOutForTest() bool {
	stream.failureMu.Lock()
	defer stream.failureMu.Unlock()
	return errors.Is(stream.stopCause, errProcessTimeout) && stream.stopError == nil
}

// Close abandons the stream and kills its process. It returns the uncertain
// outcome of that stop, if any.
func (stream *processStream) Close() error {
	stream.closed.Store(true)
	stream.interrupt(nil)
	stream.failureMu.Lock()
	defer stream.failureMu.Unlock()
	return stream.stopError
}

func (stream *processStream) finish() {
	stream.finishOnce.Do(func() {
		stream.cancel()
		stream.client.release(stream)
		close(stream.done)
	})
}

func (client *SandboxClient) reserve(stream *processStream) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if _, exists := client.active[stream.executionID]; exists {
		return fmt.Errorf("%w: %s", errExecutionActive, stream.executionID)
	}
	client.active[stream.executionID] = stream
	return nil
}

func (client *SandboxClient) release(stream *processStream) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.active[stream.executionID] == stream {
		delete(client.active, stream.executionID)
	}
}

// ProcessOutput is the complete output of a finished process.
type ProcessOutput struct {
	Stdout, Stderr string
	ExitCode       int
}

// RunToExit starts a process, reads it to its exit and closes it. A nonzero
// exit code is data, not an error. With an error it still returns the output
// read so far, for diagnostics.
func (client *SandboxClient) RunToExit(ctx context.Context, request StartRequest) (output ProcessOutput, err error) {
	stream, err := client.start(ctx, request)
	if err != nil {
		return output, err
	}
	var stdout, stderr strings.Builder
	defer func() {
		output.Stdout, output.Stderr = stdout.String(), stderr.String()
		err = errors.Join(err, stream.Close())
	}()
	for {
		event, recvErr := stream.recv()
		if recvErr != nil {
			return output, recvErr
		}
		switch event.Kind {
		case processEventStdout:
			stdout.Write(event.Data)
		case processEventStderr:
			stderr.Write(event.Data)
		case processEventExit:
			output.ExitCode = event.ExitCode
			return output, nil
		case processEventInit:
		}
	}
}

// runHelper runs one of the image's JSON helpers (the ledger and the file
// helper) directly on the process service, in environment, and returns its
// exit code. stdin receives input,
// then EOF. stdout goes to onStdout and stderr to stderr; once onStdout asks to
// stop, the helper is terminated and the returned stopped flag says its exit
// code means nothing. Helpers are not tool calls: they have no execution ID and
// end with ctx.
func (client *SandboxClient) runHelper(
	ctx context.Context,
	arguments []string,
	environment []EnvironmentVariable,
	input []byte,
	onStdout func([]byte) (stop bool, err error),
	stderr *diagnosticHead,
) (int, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := client.process.Start(ctx, &processv1.StartRequest{Config: &processv1.ProcessConfig{
		Command: arguments, Cwd: proto.String("."), EnvVars: environmentMap(environment),
	}})
	if err != nil {
		return 0, false, fmt.Errorf("start sandbox helper process: %w", err)
	}
	budget := outputBudget{limit: maximumProcessOutputBytes, subject: "helper process", failure: func(cause error) *ToolFailure {
		return NewToolFailure(toolfailure.ProcessOutputLimitExceeded, cause)
	}}
	var processID int32
	stopped := false
	for {
		response, err := events.Recv()
		if errors.Is(err, io.EOF) {
			return 0, stopped, errStreamWithoutExit
		}
		if err != nil && processID == 0 && startRefused(err) {
			// A helper's argv is fixed by the platform, so a refused start means
			// the Sandbox image lacks the program: nothing a call can correct.
			return 0, stopped, NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("start sandbox helper %q: %w", arguments[0], err))
		}
		if err != nil {
			return 0, stopped, fmt.Errorf("receive sandbox helper process event: %w", err)
		}
		event, err := decodeProcessEvent(response, processID != 0)
		if err != nil {
			return 0, stopped, err
		}
		switch event.Kind {
		case processEventInit:
			processID = event.processID
			err = client.writeInput(ctx, processID, input)
		case processEventStdout:
			stop := false
			if err = budget.charge(ctx, client.process, processID, len(event.Data)); err == nil {
				stop, err = onStdout(event.Data)
			}
			if err == nil && stop && !stopped {
				stopped = true
				err = terminateHelper(ctx, client.process, processID)
			}
		case processEventStderr:
			if err = budget.charge(ctx, client.process, processID, len(event.Data)); err == nil {
				stderr.write(event.Data)
			}
		case processEventExit:
			return event.ExitCode, stopped, nil
		}
		if err != nil {
			return 0, stopped, err
		}
	}
}

// terminateHelper asks a helper to stop early; its remaining events follow.
func terminateHelper(ctx context.Context, process processv1.ProcessServiceClient, processID int32) error {
	_, err := process.SendSignal(ctx, &processv1.SendSignalRequest{ProcessId: processID, Signal: processv1.Signal_SIGNAL_SIGTERM})
	if err != nil && !processAlreadyExited(err) {
		return fmt.Errorf("stop sandbox helper process: %w", err)
	}
	return nil
}

// RunHelperJSON runs one of the image's JSON helpers, superverse-exec or
// superverse-files, in the trusted helper environment and returns the JSON
// answer on its stdout. Exit code 0 is the helper's definite answer. Exit code
// 1 is a helper error, which says nothing about the work and may be repeated:
// it is transient. Exit code 2 with a JSON error is a helper error that a
// repetition repeats, such as a missing path: a platform tool_error that
// carries the helper's message. An answer over maxStdout bytes, any other exit
// and a helper the Sandbox cannot start are platform faults.
func (client *SandboxClient) RunHelperJSON(ctx context.Context, arguments []string, input []byte, maxStdout int) ([]byte, error) {
	return client.runHelperJSON(ctx, arguments, GitProcessEnvironment(), input, maxStdout)
}

// RunRemoteHelperJSON is RunHelperJSON for a helper whose Git commands talk
// to the repository at cloneURL, with token as their authorization when it is
// not empty: only the helper's environment carries it, scoped to the origin
// of cloneURL. An error whose diagnostics would show a form of the token is
// replaced by one that says so, keeping whether it is transient.
func (client *SandboxClient) RunRemoteHelperJSON(ctx context.Context, arguments []string, cloneURL, token string, input []byte, maxStdout int) ([]byte, error) {
	environment := GitProcessEnvironment()
	if token != "" {
		environment = GitRemoteEnvironment(CredentialScope(cloneURL), token)
	}
	answer, err := client.runHelperJSON(ctx, arguments, environment, input, maxStdout)
	for _, secret := range GitTokenForms(token) {
		if err != nil && strings.Contains(err.Error(), secret) {
			redacted := fmt.Errorf("%s failed with diagnostics that held the repository token", arguments[0])
			if IsTransient(err) {
				return nil, MarkTransient(redacted)
			}
			return nil, NewToolFailure(toolfailure.PlatformCommandUnavailable, redacted)
		}
	}
	return answer, err
}

func (client *SandboxClient) runHelperJSON(ctx context.Context, arguments []string, environment []EnvironmentVariable, input []byte, maxStdout int) ([]byte, error) {
	var stdout []byte
	stderr := &diagnosticHead{}
	overflow := false
	exitCode, stopped, err := client.runHelper(ctx, arguments, environment, input, func(data []byte) (bool, error) {
		if len(stdout)+len(data) > maxStdout {
			overflow = true
			return true, nil
		}
		stdout = append(stdout, data...)
		return false, nil
	}, stderr)
	switch {
	case err != nil:
		return nil, err
	case overflow || stopped:
		return nil, NewToolFailure(toolfailure.ProcessOutputLimitExceeded, fmt.Errorf("%s answered more than %d bytes", arguments[0], maxStdout))
	case exitCode == 0:
		return stdout, nil
	}
	var answer struct {
		Error string `json:"error"`
	}
	if (exitCode == 1 || exitCode == 2) && json.Unmarshal(stdout, &answer) == nil && answer.Error != "" {
		err := fmt.Errorf("%s: %s", strings.Join(arguments[:min(2, len(arguments))], " "), answer.Error)
		if exitCode == 2 {
			return nil, NewToolFailure(toolfailure.ToolError, err)
		}
		return nil, MarkTransient(err)
	}
	return nil, NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s exited with code %d: %s", arguments[0], exitCode, stderr.text()))
}

// maximumDiagnosticBytes bounds the stderr a failed helper's error carries.
const maximumDiagnosticBytes = 4 << 10

// diagnosticHead keeps the start of a helper's stderr for its error message.
type diagnosticHead struct{ data []byte }

func (head *diagnosticHead) write(data []byte) {
	head.data = append(head.data, data[:min(len(data), maximumDiagnosticBytes-len(head.data))]...)
}

// text is the kept stderr as valid UTF-8, or a note that there was none.
func (head *diagnosticHead) text() string {
	if text := strings.TrimSpace(strings.ToValidUTF8(string(head.data), "�")); text != "" {
		return text
	}
	return "no diagnostic output"
}

const processInputChunkBytes = 64 << 10

// writeInput is the only writer of a process's stdin: input in bounded
// chunks, then EOF. A process that already exited needs no EOF.
func (client *SandboxClient) writeInput(ctx context.Context, processID int32, input []byte) error {
	for start := 0; start < len(input); start += processInputChunkBytes {
		chunk := input[start:min(start+processInputChunkBytes, len(input))]
		if _, err := client.process.WriteStdin(ctx, &processv1.WriteStdinRequest{
			ProcessId: processID, Payload: &processv1.WriteStdinRequest_Input{Input: chunk},
		}); err != nil {
			return fmt.Errorf("write sandbox process input: %w", err)
		}
	}
	_, err := client.process.WriteStdin(ctx, &processv1.WriteStdinRequest{ProcessId: processID, Payload: &processv1.WriteStdinRequest_Eof{Eof: &emptypb.Empty{}}})
	if err != nil && !processAlreadyExited(err) {
		return fmt.Errorf("close sandbox process input: %w", err)
	}
	return nil
}

// outputBudget kills a process whose combined output exceeds its limit, so an
// over-producing process cannot keep consuming the shared Sandbox.
type outputBudget struct {
	limit, used int64
	subject     string
	// failure is the failure of a process that overran the budget.
	failure func(cause error) *ToolFailure
}

func (budget *outputBudget) charge(ctx context.Context, process processv1.ProcessServiceClient, processID int32, size int) error {
	if budget.used+int64(size) <= budget.limit {
		budget.used += int64(size)
		return nil
	}
	if err := killProcess(ctx, process, processID); err != nil {
		return fmt.Errorf("%w: kill sandbox %s after its output budget was exceeded: %w", errProcessStopFailed, budget.subject, err)
	}
	return budget.failure(fmt.Errorf("combined %s output exceeded %d bytes", budget.subject, budget.limit))
}

// killProcess sends SIGKILL to the process group of processID, bounded by
// processSignalTimeout even when ctx has already ended.
func killProcess(ctx context.Context, process processv1.ProcessServiceClient, processID int32) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processSignalTimeout)
	defer cancel()
	_, err := process.SendSignal(ctx, &processv1.SendSignalRequest{ProcessId: processID, Signal: processv1.Signal_SIGNAL_SIGKILL})
	if processAlreadyExited(err) {
		return nil
	}
	return err
}

// startRefused reports the pinned sandboxd Start statuses that precede the
// process: an empty command (InvalidArgument), a working directory outside the
// workspace (PermissionDenied) and a command or working directory that does
// not exist or cannot be executed (NotFound, PermissionDenied).
func startRefused(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied:
		return true
	default:
		return false
	}
}

func processAlreadyExited(err error) bool {
	code := status.Code(err)
	return code == codes.NotFound || code == codes.FailedPrecondition
}

// environmentMap converts an overlay into the process API's map. Every overlay
// is built by the platform, which names each variable once; a repeated name
// would silently override another entry.
func environmentMap(variables []EnvironmentVariable) map[string]string {
	result := make(map[string]string, len(variables))
	for _, variable := range variables {
		if _, duplicate := result[variable.Name]; duplicate {
			panic("duplicate environment variable " + variable.Name)
		}
		result[variable.Name] = variable.Value
	}
	return result
}
