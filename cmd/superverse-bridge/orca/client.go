// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package orca is the Bridge's client of the Orca runtime's structured
// session protocol, over its local unix socket with the owner token.
package orca

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/relay"
	"github.com/superdurable/superversev2/internal/comm"
)

// Accept Orca and Codex patch releases within these minor lines. ORCACHECK
// verified their starting releases.
var (
	supportedOrcaVersions  = []string{"1.4."}
	supportedCodexVersions = []string{"0.162."}
)

func supportedVersion(version string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(version, prefix) })
}

func supportedVersionLines(prefixes []string) string {
	return strings.Join(prefixes, "x, ") + "x"
}

const (
	codexVersionTTL     = 5 * time.Minute
	codexVersionTimeout = 15 * time.Second
	// idlePoll is how often a wait for an idle session reads the history.
	idlePoll = time.Second
	// maxIdleWait bounds a wait for the latest turn to be positively over.
	maxIdleWait = 2 * time.Minute
	// uncertainRetries bounds the identical resends of a call without answer.
	uncertainRetries    = 5
	tabTypeAgentSession = "agent-session"
	tabTypeTerminal     = "terminal"
	agentCodex          = "codex"
	// Worktree selectors of a new conversation, and the separator of a
	// worktree ID (<repoId>::<path>).
	selectorID        = "id:"
	selectorPath      = "path:"
	worktreeSeparator = "::"
)

var codexVersionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// Client implements relay.Orca.
type Client struct {
	transport *transport
	logger    *slog.Logger

	mu      sync.Mutex
	fences  map[string]int64
	codex   string
	codexAt time.Time
}

var _ relay.Orca = (*Client)(nil)

// New returns the Orca client of this machine's Orca (orca-runtime.json in
// $ORCA_USER_DATA_PATH or Orca's default directory). It does not call Orca.
func New(logger *slog.Logger) (relay.Orca, error) {
	dir, err := userDataDir()
	if err != nil {
		return nil, err
	}
	return &Client{transport: &transport{path: filepath.Join(dir, "orca-runtime.json")}, logger: logger, fences: map[string]int64{}}, nil
}

// Runtime reads Orca's version and runtime ID (status.get) and Codex's
// version, and refuses versions outside the allow-lists.
func (client *Client) Runtime(ctx context.Context) (relay.Runtime, error) {
	var status struct {
		AppVersion string `json:"appVersion"`
	}
	// The runtime ID is the one that answered, which the transport proved
	// equal to orca-runtime.json's, the identity a fenced send compares.
	runtimeID, err := client.readFrom(ctx, methodStatus, nil, &status)
	if err != nil {
		return relay.Runtime{}, err
	}
	if !supportedVersion(status.AppVersion, supportedOrcaVersions) {
		return relay.Runtime{}, fmt.Errorf("orca %s is not supported; supported Orca versions: %s",
			status.AppVersion, supportedVersionLines(supportedOrcaVersions))
	}
	codex, err := client.codexVersion(ctx)
	if err != nil {
		return relay.Runtime{}, err
	}
	return relay.Runtime{OrcaVersion: status.AppVersion, CodexVersion: codex, RuntimeID: runtimeID}, nil
}

// codexVersion runs `codex --version` the way Orca resolves codex, through
// the login shell, and caches it briefly.
func (client *Client) codexVersion(ctx context.Context) (string, error) {
	client.mu.Lock()
	if client.codex != "" && time.Since(client.codexAt) < codexVersionTTL {
		defer client.mu.Unlock()
		return client.codex, nil
	}
	client.mu.Unlock()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, codexVersionTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, shell, "-lc", "codex --version").Output() //nolint:gosec // the login shell resolves codex as Orca does; the command is fixed
	if err != nil {
		return "", fmt.Errorf("run codex --version: %w", err)
	}
	version := codexVersionPattern.FindString(string(output))
	if !supportedVersion(version, supportedCodexVersions) {
		return "", fmt.Errorf("codex %q is not supported; supported Codex versions: %s",
			strings.TrimSpace(comm.TruncateUTF8(string(output), 200)), supportedVersionLines(supportedCodexVersions))
	}
	client.mu.Lock()
	client.codex, client.codexAt = version, time.Now()
	client.mu.Unlock()
	return version, nil
}

type tabSnapshot struct {
	Worktree string `json:"worktree"`
	Tabs     []tab  `json:"tabs"`
}

type tab struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Agent     string `json:"agent"`
	Title     string `json:"title"`
	// LaunchAgent and AgentStatus describe an agent running in a terminal.
	LaunchAgent string `json:"launchAgent"`
	AgentStatus *struct {
		AgentType string `json:"agentType"`
		UpdatedAt int64  `json:"updatedAt"`
	} `json:"agentStatus"`
}

// structured is a Codex chat with a session ID; it may still lack a provider thread.
func (tab tab) structured() bool {
	return tab.codexChat() && tab.SessionID != ""
}

func (tab tab) codexChat() bool {
	return tab.Type == tabTypeAgentSession && tab.Agent == agentCodex
}

// codexTerminal is Codex running in a terminal, which the Bridge refuses.
func (tab tab) codexTerminal() bool {
	return tab.Type == tabTypeTerminal && (tab.LaunchAgent == agentCodex || (tab.AgentStatus != nil && tab.AgentStatus.AgentType == agentCodex))
}

// worktreePath is the directory of a worktree ID (<repoId>::<path>).
func worktreePath(worktree string) string {
	if _, path, found := strings.Cut(worktree, worktreeSeparator); found {
		return path
	}
	return worktree
}

func (client *Client) tabs(ctx context.Context) ([]tabSnapshot, error) {
	var listed struct {
		Snapshots []tabSnapshot `json:"snapshots"`
	}
	err := client.read(ctx, methodTabs, nil, &listed)
	return listed.Snapshots, err
}

// OpenConversations lists the Codex conversations open in Orca's tabs,
// structured ones first and each group newest activity first. A structured
// conversation's activity is its newest journal row; one whose journal cannot
// be read is listed with that error. A terminal's activity is its agent
// status.
func (client *Client) OpenConversations(ctx context.Context) ([]relay.OpenConversation, error) {
	snapshots, err := client.tabs(ctx)
	if err != nil {
		return nil, err
	}
	var conversations []relay.OpenConversation
	for _, snapshot := range snapshots {
		for _, tab := range snapshot.Tabs {
			conversation := relay.OpenConversation{Worktree: snapshot.Worktree, Path: worktreePath(snapshot.Worktree), Title: tab.Title}
			switch {
			case tab.structured():
				answer, err := client.tail(ctx, tab.SessionID)
				if err != nil {
					conversation.ReadError = fmt.Errorf("read the journal of Orca session %s: %w", tab.SessionID, err)
				} else {
					conversation.ProviderThreadReady = answer.ProviderSession != nil && answer.ProviderSession.ID != ""
					if items := answer.Page.Items; len(items) > 0 && items[len(items)-1].ObservedAt > 0 {
						conversation.LastActivity = time.UnixMilli(items[len(items)-1].ObservedAt)
					}
				}
				conversation.SessionID, conversation.Structured = tab.SessionID, true
			case tab.codexChat():
				conversation.Structured = true
			case tab.codexTerminal():
				if tab.AgentStatus != nil && tab.AgentStatus.UpdatedAt > 0 {
					conversation.LastActivity = time.UnixMilli(tab.AgentStatus.UpdatedAt)
				}
			default:
				continue
			}
			conversations = append(conversations, conversation)
		}
	}
	slices.SortStableFunc(conversations, func(a, b relay.OpenConversation) int {
		if a.Structured != b.Structured {
			if a.Structured {
				return -1
			}
			return 1
		}
		return b.LastActivity.Compare(a.LastActivity)
	})
	return conversations, nil
}

// OpenWorktrees lists every distinct open Orca worktree from the tabs snapshot.
func (client *Client) OpenWorktrees(ctx context.Context) ([]relay.OpenWorktree, error) {
	snapshots, err := client.tabs(ctx)
	if err != nil {
		return nil, err
	}
	var worktrees []relay.OpenWorktree
	seen := map[string]bool{}
	for _, snapshot := range snapshots {
		if !strings.Contains(snapshot.Worktree, worktreeSeparator) || worktreePath(snapshot.Worktree) == "" || seen[snapshot.Worktree] {
			continue
		}
		seen[snapshot.Worktree] = true
		worktrees = append(worktrees, relay.OpenWorktree{Selector: selectorID + snapshot.Worktree, Path: worktreePath(snapshot.Worktree)})
	}
	return worktrees, nil
}

// ResolveWorktree checks a new conversation's worktree selector against the
// worktrees Orca lists and returns it as id:<worktreeId>. It accepts
// id:<worktreeId> and path:<dir>; Orca's other selectors are not resolved.
func (client *Client) ResolveWorktree(ctx context.Context, selector string) (string, error) {
	worktrees, err := client.OpenWorktrees(ctx)
	if err != nil {
		return "", err
	}
	for _, worktree := range worktrees {
		if selector == worktree.Selector || (strings.HasPrefix(selector, selectorPath) &&
			filepath.Clean(strings.TrimPrefix(selector, selectorPath)) == filepath.Clean(worktree.Path)) {
			return worktree.Selector, nil
		}
	}
	if !strings.HasPrefix(selector, selectorID) && !strings.HasPrefix(selector, selectorPath) {
		return "", fmt.Errorf("the worktree %q is not id:<worktreeId> or path:<dir>", selector)
	}
	paths := make([]string, 0, len(worktrees))
	for _, worktree := range worktrees {
		paths = append(paths, worktree.Path)
	}
	return "", fmt.Errorf("orca lists no worktree %q; open it in Orca first. Orca's worktrees: %s",
		selector, strings.Join(paths, ", "))
}

// CreateSession creates a structured Codex conversation in worktree, an Orca
// worktree selector.
func (client *Client) CreateSession(ctx context.Context, worktree string) (relay.Session, error) {
	sessionID := "sv-" + randomHex(12)
	fields := object{"worktree": worktree, "agent": agentCodex}
	params := object{
		paramEnvelope: mutationEnvelope(sessionID, relay.NewOperationID(time.Now()), nil, payloadFingerprint(methodCreate, sessionID, fields)),
		"worktree":    worktree, "agent": agentCodex,
	}
	var result mutationResult[struct {
		SessionID string `json:"sessionId"`
	}]
	if err := client.mutate(ctx, methodCreate, params, &result); err != nil {
		return relay.Session{}, err
	}
	if err := result.refused(); err != nil {
		return relay.Session{}, err
	}
	client.noteFence(sessionID, result.Fence)
	return client.confirmSession(ctx, sessionID)
}

// ResumeSession confirms that sessionID is a structured Codex conversation,
// waits until it is idle and returns its provider thread. It does not reload
// Codex: the methods for that are outside the allow-list (an owner run decides).
func (client *Client) ResumeSession(ctx context.Context, sessionID string) (relay.Session, error) {
	snapshots, err := client.tabs(ctx)
	if err != nil {
		return relay.Session{}, err
	}
	found := false
	for _, snapshot := range snapshots {
		for _, tab := range snapshot.Tabs {
			found = found || (tab.SessionID == sessionID && tab.structured())
		}
	}
	if !found {
		return relay.Session{}, fmt.Errorf("orca session %s is not an open structured Codex conversation; "+
			"a Codex conversation in a terminal cannot be connected, because two processes would write one thread", sessionID)
	}
	if _, err = client.waitIdle(ctx, sessionID, ""); err != nil {
		return relay.Session{}, err
	}
	return client.confirmSession(ctx, sessionID)
}

// Reload stops the idle session's Codex process with agentSession.close, so
// that the next send starts a new one that resumes the same thread and starts
// the MCP servers configured then, and shows the chat again with
// agentSession.reveal. It fails unless the session holds the bound thread
// before and after, and is idle.
func (client *Client) Reload(ctx context.Context, sessionID, providerThreadID string) error {
	answer, err := client.boundTail(ctx, sessionID, providerThreadID)
	if err != nil {
		return err
	}
	if !answer.Page.idle() {
		return fmt.Errorf("orca session %s started a Codex turn; connect again once it is over", sessionID)
	}
	var closed struct {
		OK bool `json:"ok"`
	}
	if err := client.read(ctx, methodClose, object{"sessionId": sessionID}, &closed); err != nil {
		return fmt.Errorf("stop Codex of Orca session %s: %w", sessionID, err)
	}
	if !closed.OK {
		return fmt.Errorf("orca did not stop Codex of session %s", sessionID)
	}
	var revealed struct {
		mutationResult[struct{}]
		Readable bool `json:"readable"`
	}
	if err := client.read(ctx, methodReveal, object{"sessionId": sessionID}, &revealed); err != nil {
		return fmt.Errorf("show Orca session %s: %w", sessionID, err)
	}
	if err := revealed.refused(); err != nil {
		return fmt.Errorf("show Orca session %s: %w", sessionID, err)
	}
	if !revealed.Readable {
		return fmt.Errorf("orca could not open the conversation of session %s", sessionID)
	}
	_, err = client.boundTail(ctx, sessionID, providerThreadID)
	return err
}

// confirmSession reads the session's provider thread, which identifies it.
func (client *Client) confirmSession(ctx context.Context, sessionID string) (relay.Session, error) {
	page, err := client.tail(ctx, sessionID)
	if err != nil {
		return relay.Session{}, err
	}
	if page.ProviderSession == nil || page.ProviderSession.ID == "" {
		return relay.Session{}, fmt.Errorf("orca session %s has no Codex thread to confirm its identity", sessionID)
	}
	return relay.Session{ID: sessionID, ProviderThreadID: page.ProviderSession.ID}, nil
}

// read makes a read-only call, resending it when it went unanswered.
func (client *Client) read(ctx context.Context, method string, params any, result any) error {
	_, err := client.readFrom(ctx, method, params, result)
	return err
}

// readFrom is read, also returning the runtime ID that answered.
func (client *Client) readFrom(ctx context.Context, method string, params any, result any) (string, error) {
	var runtimeID string
	var err error
	for range uncertainRetries {
		if runtimeID, err = client.transport.call(ctx, method, params, result); !errors.Is(err, errNoAnswer) {
			return runtimeID, err
		}
	}
	return runtimeID, err
}

// mutate makes a create, cancel or withdrawal, resending the identical
// params when a call went unanswered: Orca's ledger replays it.
func (client *Client) mutate(ctx context.Context, method string, params object, result any) error {
	return client.read(ctx, method, params, result)
}

func (client *Client) fence(sessionID string) int64 {
	client.mu.Lock()
	defer client.mu.Unlock()
	return max(client.fences[sessionID], 1)
}

func (client *Client) noteFence(sessionID string, fence int64) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.fences[sessionID] = max(client.fences[sessionID], fence)
}

func randomHex(bytes int) string {
	random := make([]byte, bytes)
	comm.Must(rand.Read(random))
	return hex.EncodeToString(random)
}
