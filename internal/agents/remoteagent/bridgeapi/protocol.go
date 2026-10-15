// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bridgeapi

import (
	"encoding/json"
	"fmt"
	"time"
)

// Paths of the remote agent endpoint. Every path but the downloads is a POST
// with a JSON body; only pair carries no bearer token.
const (
	PathPair              = "/remote-agent/v1/pair"
	PathActivate          = "/remote-agent/v1/activate"
	PathSync              = "/remote-agent/v1/sync"
	PathReport            = "/remote-agent/v1/report"
	PathDisconnect        = "/remote-agent/v1/disconnect"
	PathDownloadsManifest = "/remote-agent/v1/downloads/manifest"
)

// Request body limits, in bytes, that the endpoint enforces with 413.
const (
	MaxPairRequestBytes       = 4 << 10
	MaxActivateRequestBytes   = 8 << 10
	MaxSyncRequestBytes       = 4 << 10
	MaxReportRequestBytes     = 512 << 10
	MaxDisconnectRequestBytes = 1 << 10
)

// SyncWait is the longest a sync waits for something new before it answers.
const SyncWait = 25 * time.Second

// PairRequest redeems a one-time pairing code.
type PairRequest struct {
	Code   string     `json:"code"`
	Bridge BridgeInfo `json:"bridge"`
}

// BridgeInfo identifies the Bridge build that pairs.
type BridgeInfo struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// PairResponse carries the connection token, returned once.
type PairResponse struct {
	ConnectionID string `json:"connectionId"`
	Token        string `json:"token"`
	ProjectID    string `json:"projectId"`
	ProjectName  string `json:"projectName"`
}

// ActivateRequest makes the connection the Session's executor. A repeat by the
// current executor is idempotent and returns the current state and tool set.
type ActivateRequest struct {
	Binding Binding `json:"binding"`
}

// Binding is the Orca conversation a connection drives, for display only.
type Binding struct {
	OrcaVersion      string `json:"orcaVersion"`
	CodexVersion     string `json:"codexVersion"`
	OrcaSessionID    string `json:"orcaSessionId"`
	ProviderThreadID string `json:"providerThreadId"`
	HostLabel        string `json:"hostLabel"`
}

// ActivateResponse is the executor state after activation. LastSequence is
// the conversation's last entry, Reported the highest admitted report.
type ActivateResponse struct {
	Epoch         uint64 `json:"epoch"`
	Generation    uint64 `json:"generation"`
	LastSequence  uint64 `json:"lastSequence"`
	Reported      uint64 `json:"reported"`
	Tools         []Tool `json:"tools"`
	ToolSetDigest string `json:"toolSetDigest"`
}

// Tool is one tool of the remote tool set, served to Codex as an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// SyncRequest reads the Session after the Bridge's own delivery cursor.
// After is the previous answer's ExaminedThrough; StateDigest is the last
// state digest the Bridge received (empty when it has none). Context asks for
// AgentContext; an answer that carries it is never held as a long poll.
type SyncRequest struct {
	Epoch       uint64 `json:"epoch"`
	After       uint64 `json:"after"`
	StateDigest string `json:"stateDigest"`
	Context     bool   `json:"context,omitempty"`
}

// AgentContextState describes the repository context requested for the brief.
type AgentContextState string

const (
	AgentContextReady       AgentContextState = "ready"
	AgentContextPending     AgentContextState = "pending"
	AgentContextUnavailable AgentContextState = "unavailable"
)

// SyncResponse is the Session's ReadRemote answer: at most MaxSyncEntries
// entries after the cursor, and the state the Bridge's brief and loops need.
// ExaminedThrough is the last sequence the Session examined, which the next
// request's After must be, never the last entry's or LastSequence; it never
// passes an entry left out for the budget, so the next answer starts with it.
// Dropped is a range lost to retention while the Bridge was behind.
// When StateDigest differs from the request's, PendingQuestion, Requirements
// and Todos replace the cached values and an absent one clears its value (no
// pending question any more); when it is unchanged, absent means keep the
// cached value. State values stay typed JSON: their text may be shortened
// with a "[truncated: open Studio for the full <field>]" marker, passed on as
// is, but the JSON is never cut.
type SyncResponse struct {
	Epoch           uint64           `json:"epoch"`
	Generation      uint64           `json:"generation"`
	LastSequence    uint64           `json:"lastSequence"`
	ExaminedThrough uint64           `json:"examinedThrough"`
	Entries         []Entry          `json:"entries"`
	Dropped         *DroppedRange    `json:"dropped,omitempty"`
	Interrupt       *Interrupt       `json:"interrupt,omitempty"`
	StateDigest     string           `json:"stateDigest"`
	PendingQuestion *PendingQuestion `json:"pendingQuestion,omitempty"`
	Requirements    string           `json:"requirements,omitempty"`
	Todos           []Todo           `json:"todos,omitempty"`
	Head            *Head            `json:"head,omitempty"`
	ToolSetDigest   string           `json:"toolSetDigest"`
	// SettledThrough is the highest report sequence of this connection and
	// epoch whose outcome the Session durably recorded: the report consumed
	// and, for a tool call, its tool_result appended. It never decreases
	// within an epoch and is part of the state digest; absent reads as 0.
	SettledThrough uint64 `json:"settledThrough"`
	// AgentContext, only when requested, contains the platform's remote
	// instructions, shared Session rules and repository context, at most 64 KiB.
	// The Bridge adds no instructions.
	AgentContext      string            `json:"agentContext,omitempty"`
	AgentContextState AgentContextState `json:"agentContextState"`
}

// DroppedRange is the conversation sequences, inclusive, that retention
// removed before the Bridge read them.
type DroppedRange struct {
	From    uint64 `json:"from"`
	Through uint64 `json:"through"`
}

// Sync answer bounds.
const (
	// MaxSyncEntries bounds the entries of one answer.
	MaxSyncEntries = 64
	// SyncSoftBudgetBytes is the entry text one answer aims for. The first
	// entry after the cursor is always included, even when it alone exceeds
	// the budget, so a single large entry is never an error.
	SyncSoftBudgetBytes = 1 << 20
	// MaxSyncResponseBytes is the most a Bridge reads of one sync answer; the
	// server may ask to raise it.
	MaxSyncResponseBytes = 16 << 20
)

// Entry is one conversation entry of the projection sync returns: only the
// user-role input entries that this connection did not report itself, and the
// tool_result entries of this connection's remote calls (CallID
// remote-<epoch>-<sequence>); never assistant entries, thinking, system
// markers or platform tool arguments. Text is the entry's text blocks joined
// by a blank line, exactly what the model reads: an input entry's canonical
// sender envelope, which carries the actor. Via names the connection that
// reported an entry.
type Entry struct {
	Sequence            uint64       `json:"sequence"`
	Role                EntryRole    `json:"role"`
	Kind                EntryKind    `json:"kind"`
	Text                string       `json:"text,omitempty"`
	Via                 string       `json:"via,omitempty"`
	AnsweredInputCallID string       `json:"answeredInputCallId,omitempty"`
	ToolResults         []ToolResult `json:"toolResults,omitempty"`
}

// ToolResult is the outcome of one tool call, matched to the call by CallID.
// Status, when present, classifies a result that is not the call's final
// outcome.
type ToolResult struct {
	CallID  string           `json:"callId"`
	Content string           `json:"content"`
	IsError bool             `json:"isError,omitempty"`
	Status  ToolResultStatus `json:"status,omitempty"`
}

// ToolResultStatus is the closed set of result classifications.
type ToolResultStatus string

const (
	// ToolResultStatusWaitingForUser is request_user_input's placeholder: the
	// question is open and its answer arrives as an entry whose
	// AnsweredInputCallID is the call, which is the call's actual result.
	// confirm_requirements' placeholder carries no status: it ends the turn.
	ToolResultStatusWaitingForUser ToolResultStatus = "waiting_for_user"
)

// EntryRole is the Claude role of an entry.
type EntryRole string

const (
	EntryRoleUser      EntryRole = "user"
	EntryRoleAssistant EntryRole = "assistant"
	EntryRoleSystem    EntryRole = "system"
)

// EntryKind is what an entry is; the spellings are the Session's.
type EntryKind string

const (
	EntryKindUserMessage  EntryKind = "user_message"
	EntryKindModelReply   EntryKind = "model_reply"
	EntryKindToolResult   EntryKind = "tool_result"
	EntryKindNotice       EntryKind = "notice"
	EntryKindSegmentStart EntryKind = "segment_start"
	EntryKindNotification EntryKind = "notification"
)

// Delivered reports whether the Bridge sends the entry to Codex as input: a
// user-role message, notice or notification that no Bridge of this connection
// reported itself.
func (entry Entry) Delivered(connectionID string) bool {
	if entry.Role != EntryRoleUser || entry.Via == connectionID {
		return false
	}
	switch entry.Kind {
	case EntryKindUserMessage, EntryKindNotice, EntryKindNotification:
		return true
	case EntryKindModelReply, EntryKindToolResult, EntryKindSegmentStart:
		return false
	default:
		return false
	}
}

// Interrupt is a Stop the Session recorded and no turn end applied yet.
type Interrupt struct {
	Turn        int64     `json:"turn"`
	RequestedAt time.Time `json:"requestedAt"`
	By          string    `json:"by,omitempty"`
}

// PendingQuestion is the question batch or card the Session waits on. CallID is
// present only when the question is one of this connection's own remote calls
// (remote-<epoch>-<sequence>), the only questions the Bridge can answer; Text is
// the question as the model asked it, sent for context either way.
type PendingQuestion struct {
	CallID string `json:"callId,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Text   string `json:"text,omitempty"`
}

// Todo is one item of the agent's progress list.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Head is the Sandbox workspace's current commit.
type Head struct {
	Commit string `json:"commit"`
	Branch string `json:"branch,omitempty"`
}

// DisconnectRequest revokes the calling connection; it has no fields.
type DisconnectRequest struct{}

// DisconnectResponse acknowledges a revocation; it has no fields.
type DisconnectResponse struct{}

// DevBridgeVersion is what a Bridge built without a release version reports. A
// server accepts exactly this version only when remoteAgent.acceptDevBridge is
// set (local profiles); any other version, prereleases included, is compared by
// semantic-version precedence against remoteAgent.minimumBridgeVersion.
const DevBridgeVersion = "0.0.0-dev"

// DownloadsManifest lists the Bridge and plugin builds this environment serves.
// It is the one schema on disk and on the wire: make build-bridge writes the
// artifacts into downloadsDir/manifest.json and the server fills in
// MinimumBridgeVersion from its configuration when it serves the file.
type DownloadsManifest struct {
	MinimumBridgeVersion string     `json:"minimumBridgeVersion"`
	Artifacts            []Artifact `json:"artifacts"`
}

// Artifact is one downloadable build. Path is its name under the downloads path,
// /remote-agent/v1/downloads/<path>; only listed paths are served.
type Artifact struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OS      string `json:"os,omitempty"`
	Arch    string `json:"arch,omitempty"`
	SHA256  string `json:"sha256"`
	Path    string `json:"path"`
}

// Validate refuses an artifact without the fields a download verifies.
func (artifact Artifact) Validate() error {
	if artifact.Name == "" || artifact.Version == "" || artifact.Path == "" || len(artifact.SHA256) != 64 {
		return fmt.Errorf("artifact %q needs a name, version, path and SHA-256 digest", artifact.Name)
	}
	return nil
}
