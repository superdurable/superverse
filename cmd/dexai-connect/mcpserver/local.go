// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/superdurable/superversev2/cmd/dexai-connect/platform"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// The local protocol between mcp and serve: over the 0600 Unix socket in the
// state directory, one JSON request line per connection, answered by one JSON
// line (watch: a line per tool set change until either side closes).

// localOp is what a local request asks serve for.
type localOp string

const (
	opTools      localOp = "tools"
	opCall       localOp = "call"
	opWatch      localOp = "watch"
	opStatus     localOp = "status"
	opDisconnect localOp = "disconnect"
	opAttach     localOp = "attach"
)

type localRequest struct {
	Op           localOp         `json:"op"`
	Session      string          `json:"session,omitempty"`
	Name         string          `json:"name,omitempty"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	ConnectionID string          `json:"connectionId,omitempty"`
	LocalOnly    bool            `json:"localOnly,omitempty"`
}

type localResponse struct {
	Tools  []bridgeapi.Tool `json:"tools,omitempty"`
	Digest string           `json:"digest,omitempty"`
	// Instructions is the platform's agent context of the session.
	Instructions string      `json:"instructions,omitempty"`
	Result       *CallResult `json:"result,omitempty"`
	Lines        []string    `json:"lines,omitempty"`
	Error        string      `json:"error,omitempty"`
	EndpointGone bool        `json:"endpointGone,omitempty"`
}

// CallResult is the outcome of one tool call as Codex sees it. Delivered,
// set by serve for a result it must account for, is told once whether mcp
// acknowledged writing the response to Codex.
type CallResult struct {
	Text      string     `json:"text"`
	IsError   bool       `json:"isError,omitempty"`
	Timing    CallTiming `json:"timing,omitempty"`
	Delivered func(bool) `json:"-"`
}

// CallTiming contains only the remote call identity and timestamps. It travels
// over the private local socket, never in an MCP tool result.
type CallTiming struct {
	CallID         string `json:"callId,omitempty"`
	ReportedAt     int64  `json:"reportedAt,omitempty"`
	ReportAnswerAt int64  `json:"reportAnswerAt,omitempty"`
	SyncResultAt   int64  `json:"syncResultAt,omitempty"`
}

// ackWait bounds how long serve waits for mcp to acknowledge a call's
// response after answering it; past it the result counts as undelivered.
const ackWait = 30 * time.Second

// Local line bounds: a request carries at most a report's arguments; an
// answer at most one sync entry's tool result, with room for JSON escaping.
const (
	maxLocalLineBytes   = 4 * bridgeapi.MaxReportRequestBytes
	maxLocalAnswerBytes = 2*bridgeapi.MaxSyncResponseBytes + 64<<10
)

// maxLocalConnections bounds the connections serve handles at once.
const maxLocalConnections = 64

// Backend is what serve offers mcp: the tool set of an Orca session (empty
// when it is not bound), its calls, its changes, the status and disconnect.
type Backend interface {
	Tools(session string) (tools []bridgeapi.Tool, digest string)
	// Instructions is the platform's agent context of a bound session, which
	// mcp passes on as its initialize instructions; empty when unknown.
	Instructions(session string) string
	Call(ctx context.Context, session, name string, arguments json.RawMessage) (CallResult, error)
	// WatchTools calls changed with the current digest, then with each new
	// one, until ctx ends.
	WatchTools(ctx context.Context, session string, changed func(digest string) error) error
	Status() []string
	Disconnect(ctx context.Context, connectionID string, localOnly bool) error
	// Attach starts the relay of a binding another process stored.
	Attach(ctx context.Context, connectionID string) error
}

// Listen opens the local socket at path with mode 0600. The caller holds the
// state directory's lock, so a socket file left at path is stale.
func Listen(ctx context.Context, path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("remove the stale local socket: %w", err)
	}
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on the local socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("restrict the local socket: %w", err)
	}
	return listener, nil
}

// ServeLocal answers local requests until ctx ends; it closes listener.
func ServeLocal(ctx context.Context, listener net.Listener, backend Backend, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	var wait sync.WaitGroup
	defer wait.Wait()
	defer func() { _ = listener.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	// Every return first ends the open requests, including watches, and
	// closes the listener before the wait, so an Accept failure returns at once.
	defer cancel()
	slots := make(chan struct{}, maxLocalConnections)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept a local connection: %w", err)
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			logger.WarnContext(ctx, "too many local connections; refused one")
			continue
		}
		wait.Go(func() {
			defer func() { <-slots }()
			defer func() { _ = conn.Close() }()
			connCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			stopOnClose := context.AfterFunc(connCtx, func() { _ = conn.Close() })
			defer stopOnClose()
			if err := serveLocalConn(connCtx, cancel, conn, backend); err != nil && connCtx.Err() == nil {
				logger.WarnContext(ctx, "local request failed", "error", err)
			}
		})
	}
}

func serveLocalConn(ctx context.Context, cancel context.CancelFunc, conn net.Conn, backend Backend) error {
	reader := bufio.NewReader(conn)
	line, err := readLine(reader, maxLocalLineBytes)
	if err != nil {
		return err
	}
	var request localRequest
	if err := comm.DecodeStrictJSON(line, &request); err != nil {
		return writeLine(conn, localResponse{Error: "malformed local request"})
	}
	// The peer sends nothing more except a call's acknowledgement: its close
	// cancels the request, such as a watch or a call whose MCP client gave up.
	peer := make(chan error, 1)
	go func() {
		_, err := reader.ReadByte()
		peer <- err
		cancel()
	}()
	switch request.Op {
	case opWatch:
		return backend.WatchTools(ctx, request.Session, func(digest string) error {
			return writeLine(conn, localResponse{Digest: digest})
		})
	case opCall:
		result, err := backend.Call(ctx, request.Session, request.Name, request.Arguments)
		response := localResponse{Result: &result}
		if err != nil {
			response = localResponse{Error: err.Error()}
		}
		err = writeLine(conn, response)
		if result.Delivered != nil {
			result.Delivered(err == nil && acknowledged(peer))
		}
		return err
	}
	return writeLine(conn, answerLocal(ctx, request, backend))
}

// acknowledged waits for mcp's acknowledgement, the one byte it writes after
// its response reached its standard output; a close or ackWait is none.
func acknowledged(peer <-chan error) bool {
	timer := time.NewTimer(ackWait)
	defer timer.Stop()
	select {
	case err := <-peer:
		return err == nil
	case <-timer.C:
		return false
	}
}

func answerLocal(ctx context.Context, request localRequest, backend Backend) localResponse {
	switch request.Op {
	case opTools:
		tools, digest := backend.Tools(request.Session)
		return localResponse{Tools: tools, Digest: digest, Instructions: backend.Instructions(request.Session)}
	case opStatus:
		return localResponse{Lines: backend.Status()}
	case opDisconnect:
		if err := backend.Disconnect(ctx, request.ConnectionID, request.LocalOnly); err != nil {
			return localResponse{Error: err.Error(), EndpointGone: errors.Is(err, platform.ErrEndpointGone)}
		}
		return localResponse{}
	case opAttach:
		if err := backend.Attach(ctx, request.ConnectionID); err != nil {
			return localResponse{Error: err.Error()}
		}
		return localResponse{}
	case opWatch, opCall:
		panic("a watch or call is answered by serveLocalConn")
	default:
		return localResponse{Error: fmt.Sprintf("unknown local operation %q", request.Op)}
	}
}

// Status asks a running serve for its status lines.
func Status(ctx context.Context, socketPath string) ([]string, error) {
	response, err := askLocal(ctx, socketPath, localRequest{Op: opStatus})
	return response.Lines, err
}

// Disconnect asks a running serve to disconnect one binding.
func Disconnect(ctx context.Context, socketPath, connectionID string) error {
	_, err := askLocal(ctx, socketPath, localRequest{Op: opDisconnect, ConnectionID: connectionID})
	return err
}

// Forget stops and removes a local binding without revoking its platform access.
func Forget(ctx context.Context, socketPath, connectionID string) error {
	_, err := askLocal(ctx, socketPath, localRequest{Op: opDisconnect, ConnectionID: connectionID, LocalOnly: true})
	return err
}

// Attach asks a running serve to start the relay of a new binding.
func Attach(ctx context.Context, socketPath, connectionID string) error {
	_, err := askLocal(ctx, socketPath, localRequest{Op: opAttach, ConnectionID: connectionID})
	return err
}

func askLocal(ctx context.Context, socketPath string, request localRequest) (localResponse, error) {
	response, done, err := callLocal(ctx, socketPath, request)
	done(false)
	return response, err
}

// callLocal asks serve and keeps the connection open: done(true)
// acknowledges that the answer reached Codex, done(false) does not, and
// either closes the connection.
func callLocal(ctx context.Context, socketPath string, request localRequest) (localResponse, func(bool), error) {
	conn, err := dialLocal(ctx, socketPath)
	if err != nil {
		return localResponse{}, func(bool) {}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	done := func(delivered bool) {
		if delivered {
			_, _ = conn.Write([]byte{'\n'})
		}
		stop()
		_ = conn.Close()
	}
	if err = writeLine(conn, request); err != nil {
		done(false)
		return localResponse{}, func(bool) {}, err
	}
	line, err := readLine(bufio.NewReader(conn), maxLocalAnswerBytes)
	if err != nil {
		done(false)
		return localResponse{}, func(bool) {}, err
	}
	var response localResponse
	if err := comm.DecodeStrictJSON(line, &response); err != nil {
		done(false)
		return localResponse{}, func(bool) {}, fmt.Errorf("decode the local answer: %w", err)
	}
	if response.Error != "" {
		done(false)
		if response.EndpointGone {
			return response, func(bool) {}, fmt.Errorf("%w: %s", platform.ErrEndpointGone, response.Error)
		}
		return response, func(bool) {}, errors.New(response.Error)
	}
	return response, done, nil
}

func dialLocal(ctx context.Context, socketPath string) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("reach dexai-connect serve: %w", err)
	}
	return conn, nil
}

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("read a local line: %w", err)
		}
		line = append(line, chunk...)
		if len(line) > limit {
			return nil, fmt.Errorf("a local line exceeds %d bytes", limit)
		}
		if !isPrefix {
			return line, nil
		}
	}
}

func writeLine(conn net.Conn, value any) error {
	data := append(comm.Must(json.Marshal(value)), '\n')
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write a local line: %w", err)
	}
	return nil
}
