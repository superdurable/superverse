// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

import (
	"time"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// Mode names the Session's sole turn executor.
type Mode string

const (
	ModePlatform Mode = "platform"
	ModeRemote   Mode = "remote"
)

// Binding is public executor metadata, independent of the Bridge transport.
type Binding struct {
	OrcaVersion      string `json:"orcaVersion"`
	CodexVersion     string `json:"codexVersion"`
	OrcaSessionID    string `json:"orcaSessionId"`
	ProviderThreadID string `json:"providerThreadId"`
	HostLabel        string `json:"hostLabel"`
}

// Executor is the Session-owned authority and fence. RAPAIR only reads it;
// RAEXEC owns writes under executorLock, never the credential table.
type Executor struct {
	Mode       Mode             `json:"mode"`
	Epoch      uint64           `json:"epoch"`
	Generation uint64           `json:"generation"`
	Remote     *RemoteExecutor  `json:"remote,omitempty"`
	Handoff    *PlatformHandoff `json:"handoff,omitempty"`
}

// RemoteExecutor identifies the sole remote executor without credentials.
// Reported is the admission watermark; Consumed is the highest admitted
// report the Session consumed, advanced only in the consuming commit.
type RemoteExecutor struct {
	ConnectionID string             `json:"connectionId"`
	Owner        conversation.Actor `json:"owner"`
	Binding      Binding            `json:"binding"`
	Reported     uint64             `json:"reported"`
	Consumed     uint64             `json:"consumed"`
	ActivatedAt  time.Time          `json:"activatedAt"`
}

// Consume advances Consumed past a report of this executor's epoch and
// connection, of any generation; it reports whether Executor changed.
func (executor Executor) Consume(report RemoteReport) (Executor, bool) {
	if executor.Mode != ModeRemote || executor.Remote == nil || executor.Epoch != report.Epoch ||
		executor.Remote.ConnectionID != report.ConnectionID || report.Sequence <= executor.Remote.Consumed {
		return executor, false
	}
	remote := *executor.Remote
	remote.Consumed = report.Sequence
	executor.Remote = &remote
	return executor, true
}

// SettledThrough is the highest report sequence whose outcome the Session
// recorded: every consumed report, below the first of pending, the calls
// still without a tool_result. Consumption and the cursor past a call commit
// with its outcome, so it never decreases, and a new epoch starts at 0.
func (executor Executor) SettledThrough(pending []conversation.ToolCall) uint64 {
	if executor.Mode != ModeRemote || executor.Remote == nil {
		return 0
	}
	settled := executor.Remote.Consumed
	for _, call := range pending {
		if sequence, ok := callSequence(executor.Epoch, call.ID); ok {
			settled = min(settled, sequence-1)
		}
	}
	return settled
}

// HandoffReason is the closed reason set for returning to platform mode.
type HandoffReason string

const (
	HandoffSwitched          HandoffReason = "switched"
	HandoffConnectionRemoved HandoffReason = "connection_removed"
)

// PlatformHandoff is a bounded descriptor inside Executor, consumed only by
// the next platform turn after old work settles. Owners has at most four
// actors, oldest first; RAEXEC enforces that bound when writing it.
type PlatformHandoff struct {
	Epoch  uint64               `json:"epoch"`
	From   time.Time            `json:"from"`
	To     time.Time            `json:"to"`
	Owners []conversation.Actor `json:"owners"`
	Reason HandoffReason        `json:"reason"`
}
