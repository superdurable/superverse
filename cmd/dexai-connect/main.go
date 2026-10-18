// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command dexai-connect connects a Codex conversation in Orca to a
// Superverse Project as its remote agent.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/superdurable/superversev2/cmd/dexai-connect/mcpserver"
	"github.com/superdurable/superversev2/cmd/dexai-connect/orca"
	"github.com/superdurable/superversev2/cmd/dexai-connect/platform"
	"github.com/superdurable/superversev2/cmd/dexai-connect/relay"
	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = bridgeapi.DevBridgeVersion

const localTimeout = 30 * time.Second

const usage = `dexai-connect connects a Codex conversation in Orca to a Superverse Project.

Commands:
  connect --endpoint <studio origin> [--session <orca session id> | --new --worktree <selector>]
                 choose an Orca Codex conversation or a new conversation in any
                 open worktree on the terminal, pair with a code from Studio (asked on the
                 terminal), bind it and run serve
  serve          run the relays of every binding (one per state directory)
  mcp            the MCP server Codex starts; serves the tools of $ORCA_AGENT_SESSION_ID
  status         print one line per binding
  trace          print per-call timing segments and percentiles from local trace
  disconnect [--connection <id>]
                 revoke a binding's connection and remove it
  version        print the version

Every command takes --state-dir (default $DEXAI_CONNECT_STATE_DIR, else
dexai-connect in the user's configuration directory).
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run runs one command and returns the process's exit code.
func run(arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command, args := arguments[0], arguments[1:]
	var err error
	switch command {
	case "serve":
		err = runServe(ctx, args, logger)
	case "mcp":
		err = runMCP(ctx, args, logger)
	case "connect":
		err = runConnect(ctx, args, logger)
	case "status":
		err = runStatus(ctx, args)
	case "trace":
		err = runTrace(args)
	case "disconnect":
		err = runDisconnect(ctx, args, logger)
	case "version":
		fmt.Printf("dexai-connect %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintln(os.Stderr, "dexai-connect:", err)
		return 1
	}
	return 0
}

func bridgeInfo() bridgeapi.BridgeInfo {
	return bridgeapi.BridgeInfo{Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// commandFlags parses args with the shared --state-dir flag.
type commandFlags struct {
	set      *flag.FlagSet
	stateDir *string
}

func newFlags(name string) commandFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	return commandFlags{set: set, stateDir: set.String("state-dir", "", "the Bridge's state directory")}
}

// open parses args and opens the state directory.
func (flags commandFlags) open(args []string) (*state.Dir, error) {
	if err := flags.set.Parse(args); err != nil {
		return nil, err
	}
	if flags.set.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments %q", flags.set.Args())
	}
	path := *flags.stateDir
	if path == "" {
		var err error
		if path, err = state.DefaultPath(); err != nil {
			return nil, err
		}
	}
	return state.Open(path)
}

func runServe(ctx context.Context, args []string, logger *slog.Logger) error {
	dir, err := newFlags("serve").open(args)
	if err != nil {
		return err
	}
	return serve(ctx, dir, logger)
}

// serve holds the state lock, listens on the local socket and runs every
// binding until ctx ends.
func serve(ctx context.Context, dir *state.Dir, logger *slog.Logger) error {
	if err := dir.Lock(); err != nil {
		return err
	}
	defer dir.Unlock()
	socketPath, err := dir.SocketPath()
	if err != nil {
		return err
	}
	client, err := orca.New(logger)
	if err != nil {
		return err
	}
	listener, err := mcpserver.Listen(ctx, socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(socketPath) }() //nolint:gosec // the socket in the Bridge's own state directory
	supervisor := relay.NewSupervisor(dir, client, bridgeInfo(), logger)
	logger.InfoContext(ctx, "dexai-connect serving", "version", version, "state_dir", dir.Path())
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return mcpserver.ServeLocal(ctx, listener, supervisor, logger) })
	group.Go(func() error { return supervisor.Run(ctx) })
	return group.Wait()
}

func runMCP(ctx context.Context, args []string, logger *slog.Logger) error {
	dir, err := newFlags("mcp").open(args)
	if err != nil {
		return err
	}
	socketPath, err := dir.SocketPath()
	if err != nil {
		return err
	}
	return mcpserver.Run(ctx, socketPath, os.Getenv(mcpserver.SessionEnvironment), version, os.Stdin, os.Stdout, logger)
}

func runConnect(ctx context.Context, args []string, logger *slog.Logger) error {
	flags := newFlags("connect")
	endpoint := flags.set.String("endpoint", "", "the Studio origin of the environment, such as https://studio.example.com")
	session := flags.set.String("session", "", "the Orca session to connect; chosen on the terminal when empty")
	newConversation := flags.set.Bool("new", false, "start a new conversation in --worktree instead of choosing one")
	worktree := flags.set.String("worktree", "", "the Orca worktree of a new conversation (id:<worktreeId> or path:<dir>); "+
		"narrows the new-conversation choice, and is needed with --new")
	codexConfig := flags.set.String("codex-config", "", "the Codex configuration file (default $CODEX_HOME/config.toml or ~/.codex/config.toml)")
	dir, err := flags.open(args)
	if err != nil {
		return err
	}
	if _, err = platform.ParseEndpoint(*endpoint); err != nil {
		return err
	}
	switch {
	case *session != "" && (*newConversation || *worktree != ""):
		return errors.New("--session connects an existing conversation; it takes neither --new nor --worktree")
	case *newConversation && *worktree == "":
		return errors.New("--new needs --worktree id:<worktreeId> or path:<dir>")
	}
	if *codexConfig == "" {
		if *codexConfig, err = relay.DefaultCodexConfig(); err != nil {
			return err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this program's path: %w", err)
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return fmt.Errorf("resolve this program's path: %w", err)
	}
	client, err := orca.New(logger)
	if err != nil {
		return err
	}
	sessionID := *session
	if sessionID == "" && !*newConversation {
		choice, chooseErr := chooseConversation(ctx, client, dir, *worktree)
		if chooseErr != nil {
			if errors.Is(chooseErr, errExitChoice) {
				return nil
			}
			return chooseErr
		}
		sessionID, *worktree = choice.sessionID, choice.worktree
	}
	stateDirArgument := ""
	if defaultPath, defaultErr := state.DefaultPath(); defaultErr != nil || filepath.Clean(defaultPath) != dir.Path() {
		stateDirArgument = dir.Path()
	}
	binding, err := relay.Bind(ctx, dir, client, relay.BindRequest{
		Endpoint: *endpoint, PairingCode: promptPairingCode, SessionID: sessionID, Worktree: *worktree, Bridge: bridgeInfo(),
		CodexConfig: *codexConfig, Executable: executable, StateDir: stateDirArgument,
	}, logger)
	if err != nil {
		return err
	}
	fmt.Printf("Paired with %s using Orca %s and Codex %s; connecting Orca session %s.\n",
		binding.Record.ProjectName, binding.Record.ProviderVersion, binding.Record.AgentVersion, binding.Record.SessionID)
	// Orca 1.4.224 and 1.4.225 publish the chat's tab to the runtime but may not open it
	// in a window that is already open.
	fmt.Printf("Codex restarts with the superverse tools on its next message. If the chat (Orca session %s) "+
		"is not shown in Orca's window, open it from Agent Session History in the right sidebar.\n", binding.Record.SessionID)
	running, err := dir.Running()
	if err != nil {
		return err
	}
	if running {
		socketPath, err := dir.SocketPath()
		if err != nil {
			return err
		}
		attachCtx, cancel := context.WithTimeout(ctx, localTimeout)
		defer cancel()
		if err := mcpserver.Attach(attachCtx, socketPath, binding.Record.ConnectionID); err != nil {
			return fmt.Errorf("hand the binding to the running serve: %w", err)
		}
		fmt.Println("The running dexai-connect serve now relays it.")
		return nil
	}
	return serve(ctx, dir, logger)
}

// promptPairingCode reads the pairing code on the terminal. It is never an
// argument and never logged.
func promptPairingCode(ctx context.Context) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("connect asks for the pairing code on a terminal: %w", err)
	}
	// A cancelled read may still hold the tty on macOS; process exit closes it.
	defer func() {
		if ctx.Err() == nil {
			_ = tty.Close()
		}
	}()
	if _, err = fmt.Fprint(tty, "Pairing code from Studio (Ctrl+C to cancel): "); err != nil {
		return "", fmt.Errorf("prompt for the pairing code: %w", err)
	}
	line, err := readTerminalLine(ctx, bufio.NewReader(tty))
	if err != nil {
		return "", fmt.Errorf("read the pairing code: %w", err)
	}
	code := strings.ToUpper(strings.Join(strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '-' || r == '\t' || r == '\r' || r == '\n'
	}), ""))
	if code == "" {
		return "", errors.New("no pairing code given")
	}
	return code, nil
}

func runStatus(ctx context.Context, args []string) error {
	dir, err := newFlags("status").open(args)
	if err != nil {
		return err
	}
	running, err := dir.Running()
	if err != nil {
		return err
	}
	if running {
		return printServeStatus(ctx, dir)
	}
	bindings, err := dir.Bindings()
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		fmt.Println("No Project is connected. Run dexai-connect connect, or connect from the Orca plugin.")
		return nil
	}
	for _, binding := range bindings {
		fmt.Printf("Bound to %s (Orca session %s) · not running: start dexai-connect serve\n",
			binding.Record.ProjectName, binding.Record.SessionID)
	}
	printOrcaVersionWarning(ctx)
	return nil
}

func printOrcaVersionWarning(ctx context.Context) {
	client, err := orca.New(slog.Default())
	if err != nil {
		return
	}
	runtime, err := client.Runtime(ctx)
	if err == nil && runtime.Warning != "" {
		fmt.Println(runtime.Warning)
	}
}

// printServeStatus prints the status lines of the running serve.
func printServeStatus(ctx context.Context, dir *state.Dir) error {
	socketPath, err := dir.SocketPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	lines, err := mcpserver.Status(ctx, socketPath)
	if err != nil {
		return err
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func runDisconnect(ctx context.Context, args []string, logger *slog.Logger) error {
	flags := newFlags("disconnect")
	connectionID := flags.set.String("connection", "", "the connection to disconnect; needed when several are bound")
	dir, err := flags.open(args)
	if err != nil {
		return err
	}
	bindings, err := dir.Bindings()
	if err != nil {
		return err
	}
	binding, err := chooseBinding(bindings, *connectionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	running, err := dir.Running()
	if err != nil {
		return err
	}
	if running {
		socketPath, err := dir.SocketPath()
		if err != nil {
			return err
		}
		if err := mcpserver.Disconnect(ctx, socketPath, binding.Record.ConnectionID); err != nil {
			return err
		}
	} else {
		if err := dir.Lock(); err != nil {
			return err
		}
		defer dir.Unlock()
		token, err := binding.Token()
		if err != nil {
			return err
		}
		client, err := platform.New(binding.Record.Endpoint, token, bridgeInfo(), logger)
		if err != nil {
			return err
		}
		if err := relay.Revoke(ctx, client); err != nil {
			return err
		}
		if err := binding.Remove(); err != nil {
			return err
		}
	}
	fmt.Printf("Disconnected from %s. The Session stays with the remote agent, shown offline, until a Builder switches it.\n",
		binding.Record.ProjectName)
	return nil
}

func chooseBinding(bindings []*state.Binding, connectionID string) (*state.Binding, error) {
	if connectionID == "" {
		switch len(bindings) {
		case 0:
			return nil, errors.New("no Project is connected")
		case 1:
			return bindings[0], nil
		default:
			names := make([]string, 0, len(bindings))
			for _, binding := range bindings {
				names = append(names, fmt.Sprintf("%s (%s)", binding.Record.ConnectionID, binding.Record.ProjectName))
			}
			return nil, fmt.Errorf("several Projects are connected; choose one with --connection: %s", strings.Join(names, ", "))
		}
	}
	for _, binding := range bindings {
		if binding.Record.ConnectionID == connectionID {
			return binding, nil
		}
	}
	return nil, fmt.Errorf("no binding has connection %s", connectionID)
}
