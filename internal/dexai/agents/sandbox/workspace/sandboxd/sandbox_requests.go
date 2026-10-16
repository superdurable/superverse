// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var (
	errExecutionActive = errors.New("execution is already active")
	// errProcessNotActive means Init arrived after the execution was released:
	// the process started but could not be stopped, an uncertain effect.
	errProcessNotActive = errors.New("process is not active")
	// errProcessNotInitialized means a deadline or cancellation stopped the
	// stream before Init: whether the process started is unknown.
	errProcessNotInitialized = errors.New("process has not emitted its init event")
	errStreamWithoutExit     = errors.New("process stream ended without an exit event")
	// errProcessTimeout is the stop cause of a stream whose StartRequest.Timeout
	// passed. With no errProcessStopFailed the process group was killed.
	errProcessTimeout = errors.New("process exceeded its timeout")
	// errProcessStopFailed means the stream could not confirm that it killed the
	// process: the kill request failed or the process had no ID yet. The process
	// may still run.
	errProcessStopFailed = errors.New("process stop was not confirmed")
	// ErrProcessNotStarted means sandboxd refused the start before its init
	// event, such as a missing command or working directory: no process ran.
	ErrProcessNotStarted = errors.New("sandbox process was not started")
)

type EnvironmentVariable struct {
	Name  string
	Value string
}

// StartRequest starts one process in the workspace. The process gets stdin
// EOF right after it started; no caller writes to its stdin.
type StartRequest struct {
	ExecutionID      ExecutionID
	Arguments        []string
	workingDirectory WorkspacePath // "" is the workspace root
	Environment      []EnvironmentVariable
	Timeout          time.Duration
	// MaximumOutputBytes is a hard combined stdout/stderr budget, at most and
	// by default maximumProcessOutputBytes. Exceeding it kills the process: a
	// projection limit would let it keep consuming the shared Sandbox.
	MaximumOutputBytes int64
}

type processEventKind string

const (
	processEventInit   processEventKind = "init"
	processEventStdout processEventKind = "stdout"
	processEventStderr processEventKind = "stderr"
	processEventExit   processEventKind = "exit"
)

// processEvent is one event of a process stream, in order: exactly one init,
// then output, then one exit. processID is set only on init, Data only on
// output and ExitCode only on exit.
type processEvent struct {
	Kind      processEventKind
	processID int32
	Data      []byte
	ExitCode  int
}

type ReadFileRequest struct {
	Path   WorkspacePath
	offset int64
	Limit  int // 1 to maximumReadBytes
}

type ReadFileResult struct {
	Data       []byte
	nextOffset int64
	EOF        bool
}

// ExecutionID names one process start in its Sandbox. The client admits one
// live process per ID, so a retried tool call cannot run its command twice
// concurrently.
type ExecutionID string

// InvocationIdentity is the durable identity of one tool call.
type InvocationIdentity struct {
	FlowID string
	CallID string
}

// DeriveExecutionID hashes the call identity into a stable execution ID, so
// every retry of one call uses the same one.
func DeriveExecutionID(identity InvocationIdentity) ExecutionID {
	digest := sha256.New()
	for _, field := range []string{"superverse-agent-execution-v1", identity.FlowID, identity.CallID} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		digest.Write(length[:])
		digest.Write([]byte(field))
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest.Sum(nil))
	return ExecutionID(executionIDPrefix + strings.ToLower(encoded))
}

const executionIDPrefix = "exec_v1_"

// CommandTaskID is the command ledger ID of a bash call and the task ID the
// model reads: "cmd-" and the first twelve characters of the call's execution
// digest, such as cmd-qjo5x7m2k4ab. A task ID is therefore its own ledger ID,
// so read_task_output and stop_task resolve it in the Sandbox without a Flow
// read. Sixty bits keep the calls of one Sandbox's ledger apart.
func CommandTaskID(identity InvocationIdentity) ExecutionID {
	return ExecutionID("cmd-" + strings.TrimPrefix(string(DeriveExecutionID(identity)), executionIDPrefix)[:12])
}

// commandTaskIDPattern is the form of every CommandTaskID.
var commandTaskIDPattern = regexp.MustCompile(`^cmd-[a-z2-7]{12}$`)

// IsCommandTaskID reports whether a task ID names a command.
func IsCommandTaskID(taskID string) bool {
	return commandTaskIDPattern.MatchString(taskID)
}

var platformExecutionSequence atomic.Uint64

// PlatformExecutionID gives one run of a platform command, which is not a tool
// call, a fresh identity, so every run starts its own process. purpose names
// the kind of command.
func PlatformExecutionID(purpose string) ExecutionID {
	return DeriveExecutionID(InvocationIdentity{
		FlowID: purpose, CallID: fmt.Sprintf("%d-%d", time.Now().UnixNano(), platformExecutionSequence.Add(1)),
	})
}
