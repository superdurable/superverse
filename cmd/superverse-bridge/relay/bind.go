// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/mcpserver"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/platform"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// BindRequest is what connect gathers to bind an Orca session to a Project.
type BindRequest struct {
	Endpoint string
	// PairingCode reads the one-time pairing code. Bind asks for it only after
	// the Orca session and the Codex configuration were checked, so a wrong
	// choice never spends the code.
	PairingCode func() (string, error)
	// SessionID is the Orca session to drive; empty creates a new conversation
	// in Worktree, an Orca worktree selector.
	SessionID string
	Worktree  string
	Bridge    bridgeapi.BridgeInfo
	// CodexConfig is the Codex configuration file that gets the MCP server entry.
	CodexConfig string
	// Executable is this Bridge's absolute path, which the entry runs.
	Executable string
	// StateDir is passed to mcp only when it is not the default directory.
	StateDir string
}

// Bind checks the Orca session (or the new conversation's worktree) and the
// Codex configuration, pairs with the code, installs the MCP server entry,
// binds the Orca session (resumed, so Codex loads the entry) and stores the
// binding. serve then activates it, imports the history and sends the brief.
// A failure after pairing revokes the new connection.
func Bind(ctx context.Context, dir *state.Dir, orca Orca, request BindRequest, logger *slog.Logger) (*state.Binding, error) {
	runtime, err := orca.Runtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach Orca: %w", err)
	}
	if err = checkBindable(dir, request.SessionID); err != nil {
		return nil, err
	}
	if request.SessionID == "" {
		if request.Worktree, err = orca.ResolveWorktree(ctx, request.Worktree); err != nil {
			return nil, err
		}
	} else if err = checkStructured(ctx, orca, request.SessionID); err != nil {
		return nil, err
	}
	if _, _, err = plannedMCPServer(request.CodexConfig, request.Executable, request.StateDir); err != nil {
		return nil, err
	}
	pairing, err := platform.New(request.Endpoint, "", request.Bridge, logger)
	if err != nil {
		return nil, err
	}
	code, err := request.PairingCode()
	if err != nil {
		return nil, err
	}
	paired, err := pairing.Pair(ctx, code, request.Bridge)
	if err != nil {
		return nil, err
	}
	connected, err := platform.New(request.Endpoint, paired.Token, request.Bridge, logger)
	if err != nil {
		return nil, err
	}
	binding, err := bindSession(ctx, dir, orca, request, runtime, paired)
	if err != nil {
		if revokeErr := Revoke(ctx, connected); revokeErr != nil {
			err = errors.Join(err, fmt.Errorf("revoke the unused connection: %w", revokeErr))
		}
		return nil, err
	}
	return binding, nil
}

func checkBindable(dir *state.Dir, sessionID string) error {
	bindings, err := dir.Bindings()
	if err != nil {
		return err
	}
	if len(bindings) >= state.MaxBindings {
		return fmt.Errorf("this Bridge already holds %d bindings; disconnect one first", state.MaxBindings)
	}
	for _, binding := range bindings {
		if sessionID != "" && binding.Record.OrcaSessionID == sessionID {
			return fmt.Errorf("the Orca session is already bound to Project %s", binding.Record.ProjectName)
		}
	}
	return nil
}

// checkStructured fails unless sessionID is a structured Codex conversation
// open in Orca whose journal can be read.
func checkStructured(ctx context.Context, orca Orca, sessionID string) error {
	conversations, err := orca.OpenConversations(ctx)
	if err != nil {
		return err
	}
	for _, conversation := range conversations {
		if conversation.Structured && conversation.SessionID == sessionID {
			return conversation.ReadError
		}
	}
	return fmt.Errorf("orca session %s is not an open structured Codex conversation; "+
		"a Codex conversation in a terminal cannot be connected, because two processes would write one thread", sessionID)
}

func bindSession(ctx context.Context, dir *state.Dir, orca Orca, request BindRequest, runtime Runtime,
	paired bridgeapi.PairResponse) (*state.Binding, error) {
	if err := InstallMCPServer(request.CodexConfig, request.Executable, request.StateDir); err != nil {
		return nil, err
	}
	var session Session
	var err error
	if request.SessionID == "" {
		session, err = orca.CreateSession(ctx, request.Worktree)
	} else {
		session, err = orca.ResumeSession(ctx, request.SessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("bind the Orca session: %w", err)
	}
	// Codex started its MCP servers before the binding existed, so the
	// superverse server would list no tools: the brief restarts it.
	if err = orca.Reload(ctx, session.ID, session.ProviderThreadID); err != nil {
		return nil, fmt.Errorf("reload the Orca session: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		host = "unknown host"
	}
	return dir.AddBinding(state.BindingRecord{
		Endpoint: request.Endpoint, ConnectionID: paired.ConnectionID, ProjectID: paired.ProjectID,
		ProjectName: paired.ProjectName, OrcaSessionID: session.ID, ProviderThreadID: session.ProviderThreadID,
		OrcaVersion: runtime.OrcaVersion, CodexVersion: runtime.CodexVersion, HostLabel: host, PairedAt: time.Now().UTC(),
	}, paired.Token)
}

// DefaultCodexConfig is $CODEX_HOME/config.toml, else ~/.codex/config.toml.
func DefaultCodexConfig() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

// InstallMCPServer writes the one [mcp_servers.superverse] entry into the
// Codex configuration, replacing an earlier one with its sub-tables, after
// backing the file up. Nothing else in the file changes: the result is parsed
// and compared before it replaces the file, and a file the Bridge cannot edit
// exactly is left untouched with an error. A lock file next to the
// configuration serializes concurrent connects.
func InstallMCPServer(path, executable, stateDir string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the Codex configuration directory: %w", err)
	}
	unlock, err := state.LockFile(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".superverse-lock"))
	if err != nil {
		return fmt.Errorf("lock the Codex configuration: %w", err)
	}
	defer unlock()
	existing, updated, err := plannedMCPServer(path, executable, stateDir)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if existing != nil {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat the Codex configuration: %w", err)
		}
		mode = info.Mode().Perm()
		backup := fmt.Sprintf("%s.superverse-backup-%s", path, time.Now().UTC().Format("20060102T150405Z"))
		if err := state.ReplaceFile(backup, existing, mode); err != nil {
			return fmt.Errorf("back up the Codex configuration: %w", err)
		}
	}
	if err := state.ReplaceFile(path, []byte(updated), mode); err != nil {
		return fmt.Errorf("write the Codex configuration: %w", err)
	}
	return nil
}

// plannedMCPServer reads the Codex configuration (nil when there is none) and
// returns it with the entry installed, writing nothing; Bind runs it before
// pairing so a configuration it cannot edit never spends the pairing code.
func plannedMCPServer(path, executable, stateDir string) (existing []byte, updated string, err error) {
	existing, err = os.ReadFile(path) //nolint:gosec // the Codex configuration the Builder chose
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("read the Codex configuration: %w", err)
	}
	if updated, err = withMCPServerEntry(string(existing), mcpServerEntry(executable, stateDir)); err != nil {
		return nil, "", fmt.Errorf("the Codex configuration %s was not changed: %w", path, err)
	}
	return existing, updated, nil
}

func mcpServerEntry(executable, stateDir string) string {
	args := []string{tomlString("mcp")}
	if stateDir != "" {
		args = append(args, tomlString("--state-dir"), tomlString(stateDir))
	}
	return fmt.Sprintf("[mcp_servers.%s]\ncommand = %s\nargs = [%s]\nenv_vars = [%s]\ntool_timeout_sec = %d\n",
		mcpserver.ServerName, tomlString(executable), strings.Join(args, ", "), tomlString(mcpserver.SessionEnvironment), mcpToolTimeoutSeconds)
}

// withMCPServerEntry drops an earlier superverse table and its sub-tables
// from config and appends entry. It refuses a config that is not TOML, and
// any edit whose result is not exactly config without its superverse server
// plus entry, such as a server defined with dotted keys or inline, or a
// multiline string holding a header-like line.
func withMCPServerEntry(config, entry string) (string, error) {
	var before, after, intended map[string]any
	if _, err := toml.Decode(config, &before); err != nil {
		return "", fmt.Errorf("it is not valid TOML: %w", err)
	}
	var kept []string
	inEntry := false
	for _, line := range strings.SplitAfter(config, "\n") {
		if header, ok := tableHeader(line); ok {
			inEntry = header
		}
		if !inEntry {
			kept = append(kept, line)
		}
	}
	result := strings.Join(kept, "")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if result != "" && !strings.HasSuffix(result, "\n\n") {
		result += "\n"
	}
	result += entry
	cannotReplace := fmt.Errorf("the Bridge cannot replace its %s server exactly (defined with dotted keys or inline, "+
		"or a multiline string holds a table header); remove that server by hand and connect again", mcpserver.ServerName)
	if _, err := toml.Decode(result, &after); err != nil {
		return "", cannotReplace
	}
	comm.Must(toml.Decode(entry, &intended))
	takeServer(before)
	if !reflect.DeepEqual(takeServer(after), takeServer(intended)) || !reflect.DeepEqual(before, after) {
		return "", cannotReplace
	}
	return result, nil
}

// tableHeader reports whether line is a table header, and whether that header
// names the superverse server or one of its sub-tables, however it is spelled.
func tableHeader(line string) (superverse, ok bool) {
	if !strings.HasPrefix(strings.TrimSpace(line), "[") {
		return false, false
	}
	var header map[string]any
	if _, err := toml.Decode(line, &header); err != nil {
		return false, false
	}
	servers, _ := header["mcp_servers"].(map[string]any)
	_, superverse = servers[mcpserver.ServerName]
	return superverse, true
}

// takeServer removes the superverse server from a decoded configuration, and
// mcp_servers when nothing else is left in it, and returns the server.
func takeServer(config map[string]any) any {
	servers, _ := config["mcp_servers"].(map[string]any)
	server := servers[mcpserver.ServerName]
	delete(servers, mcpserver.ServerName)
	if servers != nil && len(servers) == 0 {
		delete(config, "mcp_servers")
	}
	return server
}

// tomlString is a TOML basic string.
func tomlString(value string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"' || r == '\\':
			quoted.WriteByte('\\')
			quoted.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&quoted, "\\u%04X", r)
		default:
			quoted.WriteRune(r)
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}
