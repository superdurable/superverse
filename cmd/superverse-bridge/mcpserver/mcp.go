// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package mcpserver is the stdio MCP server that Codex starts, and the local
// socket protocol through which it reaches serve.
package mcpserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/trace"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/comm"
)

// SessionEnvironment names the Orca session whose tools mcp serves; Codex
// passes it because the MCP server entry lists it in env_vars.
const SessionEnvironment = "ORCA_AGENT_SESSION_ID"

// ServerName is the MCP server's name, the key of its Codex configuration.
const ServerName = "superverse"

// supportedProtocolVersions are the MCP revisions this server speaks, newest
// first; an initialize asking for another gets the newest.
var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// watchRetry is how long mcp waits before it reaches serve again.
const watchRetry = 5 * time.Second

const jsonRPCVersion = "2.0"

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// MCP methods this server answers or receives.
const (
	methodInitialize       = "initialize"
	methodInitialized      = "notifications/initialized"
	methodPing             = "ping"
	methodToolsList        = "tools/list"
	methodToolsCall        = "tools/call"
	methodToolsListChanged = "notifications/tools/list_changed"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
	timing  CallTiming
}

// maxConcurrentCalls bounds the tool calls one MCP session runs at once; a
// call beyond it is answered with a tool error at once.
const maxConcurrentCalls = 8

// Run serves MCP on in and out for one Orca session until in ends or ctx
// does. An empty session, or one serve has not bound, has zero tools.
func Run(ctx context.Context, socketPath, session, version string, in io.Reader, out io.Writer, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &server{socketPath: socketPath, session: session, version: version, logger: logger,
		calls: make(chan struct{}, maxConcurrentCalls), writes: make(chan outputWrite)}
	var wait sync.WaitGroup
	defer wait.Wait()
	// The writer owns out. A write Codex stops draining may block it for
	// good; every writer waits with ctx, so Run still returns and the process
	// exits after it.
	go server.writeLoop(ctx, out)
	// A read blocked on a terminal or pipe may not end when in closes, so the
	// reader runs on its own and Run returns once ctx ends; the process exits
	// after Run.
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64<<10), maxLocalLineBytes)
		for scanner.Scan() {
			select {
			case lines <- slices.Clone(scanner.Bytes()):
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
		close(lines)
	}()
	if closer, ok := in.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	for {
		var line []byte
		select {
		case <-ctx.Done():
			return nil
		case read, open := <-lines:
			if !open {
				cancel()
				if err := <-readErr; err != nil {
					return fmt.Errorf("read MCP input: %w", err)
				}
				return nil
			}
			line = read
		}
		if len(line) == 0 {
			continue
		}
		server.handle(ctx, &wait, line)
	}
}

// handle answers one MCP message; tool calls run concurrently, up to
// maxConcurrentCalls.
func (server *server) handle(ctx context.Context, wait *sync.WaitGroup, line []byte) {
	var message rpcMessage
	if err := json.Unmarshal(line, &message); err != nil || message.JSONRPC != jsonRPCVersion {
		server.reply(ctx, json.RawMessage("null"), nil, &rpcError{Code: rpcParseError, Message: "not a JSON-RPC 2.0 message"})
		return
	}
	switch {
	case message.Method == "":
		// A response to a request this server never sends.
	case len(message.ID) == 0:
		// Notifications; a cancelled call stays reported to the platform. A
		// repeated initialized notification starts no second watch.
		if message.Method == methodInitialized && server.session != "" && !server.watching {
			server.watching = true
			wait.Go(func() { server.watch(ctx) })
		}
	case message.Method == methodToolsCall:
		mcpAt := time.Now().UnixMilli()
		select {
		case server.calls <- struct{}{}:
			wait.Go(func() {
				defer func() { <-server.calls }()
				server.answerCall(ctx, message, mcpAt)
			})
		default:
			server.reply(ctx, message.ID, toolError(fmt.Sprintf(
				"%d Superverse calls are already running in this session; wait for one to finish", maxConcurrentCalls)), nil)
		}
	default:
		server.answer(ctx, message)
	}
}

// answerCall records a complete timing row after the MCP writer returns.
func (server *server) answerCall(ctx context.Context, message rpcMessage, mcpAt int64) {
	var request struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(message.Params, &request)
	if len(request.Name) == 0 || len(request.Name) > 128 || strings.IndexFunc(request.Name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) >= 0 {
		request.Name = "invalid"
	}
	var random [8]byte
	_, _ = rand.Read(random[:])
	result, rpcErr, done := server.call(ctx, message.Params)
	written := server.reply(ctx, message.ID, result, rpcErr)
	codexAt := time.Now().UnixMilli()
	done(written)
	outcome := "ok"
	if !written {
		outcome = "write_failed"
	} else if rpcErr != nil {
		outcome = "rpc_error"
	} else if value, ok := result.(callToolResult); ok && value.IsError {
		outcome = "tool_error"
	}
	call := trace.Call{LocalID: hex.EncodeToString(random[:]), Tool: request.Name, MCPAt: mcpAt, CodexAt: codexAt, Outcome: outcome}
	if value, ok := result.(callToolResult); ok {
		call.CallID, call.ReportedAt, call.ReportAnswerAt, call.SyncResultAt = value.timing.CallID, value.timing.ReportedAt, value.timing.ReportAnswerAt, value.timing.SyncResultAt
	}
	if err := trace.Append(filepath.Dir(server.socketPath), call); err != nil {
		server.logger.WarnContext(ctx, "write call timing trace", "error", err)
	}
}

type server struct {
	socketPath string
	session    string
	version    string
	logger     *slog.Logger
	// calls holds a token per running tool call.
	calls chan struct{}
	// watching is set once the watch runs; only Run's dispatcher reads it.
	watching bool
	// writes carries each output line to writeLoop, in order.
	writes chan outputWrite
}

// outputWrite is one line for standard output and where its result goes.
type outputWrite struct {
	data []byte
	done chan error
}

func (server *server) answer(ctx context.Context, message rpcMessage) {
	switch message.Method {
	case methodInitialize:
		server.reply(ctx, message.ID, server.initialize(ctx, message.Params), nil)
	case methodPing:
		server.reply(ctx, message.ID, struct{}{}, nil)
	case methodToolsList:
		server.reply(ctx, message.ID, map[string]any{"tools": server.tools(ctx)}, nil)
	case methodToolsCall:
		result, rpcErr, done := server.call(ctx, message.Params)
		done(server.reply(ctx, message.ID, result, rpcErr))
	default:
		server.reply(ctx, message.ID, nil, &rpcError{Code: rpcMethodNotFound, Message: "method not found: " + message.Method})
	}
}

// initialize answers with the platform's agent context as instructions, when
// serve has it; the Bridge has no instructions of its own.
func (server *server) initialize(ctx context.Context, params json.RawMessage) any {
	var request struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &request)
	version := supportedProtocolVersions[0]
	if slices.Contains(supportedProtocolVersions, request.ProtocolVersion) {
		version = request.ProtocolVersion
	}
	answer := map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
		"serverInfo":      map[string]any{"name": ServerName, "version": server.version},
	}
	if server.session != "" {
		response, err := askLocal(ctx, server.socketPath, localRequest{Op: opTools, Session: server.session})
		if err != nil {
			server.logger.WarnContext(ctx, "read the Superverse instructions", "error", err)
		} else if response.Instructions != "" {
			answer["instructions"] = response.Instructions
		}
	}
	return answer
}

// tools lists the session's tools; with serve unreachable it lists none, and
// the watch announces them once serve answers.
func (server *server) tools(ctx context.Context) []mcpTool {
	listed := []mcpTool{}
	if server.session == "" {
		return listed
	}
	response, err := askLocal(ctx, server.socketPath, localRequest{Op: opTools, Session: server.session})
	if err != nil {
		server.logger.WarnContext(ctx, "list the Superverse tools", "error", err)
		return listed
	}
	for _, tool := range response.Tools {
		listed = append(listed, mcpTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return listed
}

// call runs one tool call through serve; done acknowledges to serve whether
// the response reached Codex.
func (server *server) call(ctx context.Context, params json.RawMessage) (any, *rpcError, func(bool)) {
	none := func(bool) {}
	var request struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &request); err != nil || request.Name == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "tools/call needs a tool name"}, none
	}
	if len(request.Arguments) == 0 || string(request.Arguments) == "null" {
		request.Arguments = json.RawMessage("{}")
	}
	if len(request.Arguments) > bridgeapi.MaxToolArgumentsBytes {
		return toolError(fmt.Sprintf("the arguments exceed %d bytes", bridgeapi.MaxToolArgumentsBytes)), nil, none
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(request.Arguments, &object); err != nil || object == nil {
		return toolError("the arguments must be a JSON object"), nil, none
	}
	if server.session == "" {
		return toolError("this Codex session is not connected to Superverse"), nil, none
	}
	response, done, err := callLocal(ctx, server.socketPath, localRequest{
		Op: opCall, Session: server.session, Name: request.Name, Arguments: request.Arguments,
	})
	if err != nil {
		return toolError("Superverse could not run the tool: " + err.Error()), nil, none
	}
	if response.Result == nil {
		done(false)
		return nil, &rpcError{Code: rpcInvalidRequest, Message: "serve answered no result"}, none
	}
	return callToolResult{Content: []textContent{{Type: "text", Text: response.Result.Text}}, IsError: response.Result.IsError, timing: response.Result.Timing}, nil, done
}

func toolError(text string) callToolResult {
	return callToolResult{Content: []textContent{{Type: "text", Text: text}}, IsError: true}
}

// watch tells Codex when the session's tool set changes, including when
// serve first answers after it was unreachable.
func (server *server) watch(ctx context.Context) {
	seen := ""
	for ctx.Err() == nil {
		err := server.watchOnce(ctx, &seen)
		if ctx.Err() != nil {
			return
		}
		server.logger.DebugContext(ctx, "the tool watch ended; retrying", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(watchRetry):
		}
	}
}

func (server *server) watchOnce(ctx context.Context, seen *string) error {
	conn, err := dialLocal(ctx, server.socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := writeLine(conn, localRequest{Op: opWatch, Session: server.session}); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	for {
		line, err := readLine(reader, maxLocalAnswerBytes)
		if err != nil {
			return err
		}
		var response localResponse
		if err := comm.DecodeStrictJSON(line, &response); err != nil {
			return fmt.Errorf("decode the tool watch: %w", err)
		}
		if response.Error != "" {
			return errors.New(response.Error)
		}
		if response.Digest != *seen {
			*seen = response.Digest
			server.notify(ctx, methodToolsListChanged)
		}
	}
}

// reply writes a response and reports whether the write returned.
func (server *server) reply(ctx context.Context, id json.RawMessage, result any, rpcErr *rpcError) bool {
	if rpcErr == nil && result == nil {
		result = struct{}{}
	}
	return server.write(ctx, rpcMessage{JSONRPC: jsonRPCVersion, ID: id, Result: result, Error: rpcErr})
}

func (server *server) notify(ctx context.Context, method string) {
	server.write(ctx, rpcMessage{JSONRPC: jsonRPCVersion, Method: method})
}

// write hands one message to writeLoop and reports whether its write
// returned without error; cancellation ends the wait, never the write.
func (server *server) write(ctx context.Context, message rpcMessage) bool {
	request := outputWrite{data: append(comm.Must(json.Marshal(message)), '\n'), done: make(chan error, 1)}
	select {
	case server.writes <- request:
	case <-ctx.Done():
		return false
	}
	select {
	case err := <-request.done:
		if err != nil {
			server.logger.WarnContext(ctx, "write MCP output", "error", err)
		}
		return err == nil
	case <-ctx.Done():
		return false
	}
}

// writeLoop writes the output lines one at a time until ctx ends.
func (server *server) writeLoop(ctx context.Context, out io.Writer) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-server.writes:
			_, err := out.Write(request.data)
			request.done <- err
		}
	}
}
