// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package orca

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// The compiled allow-list: the only methods the Bridge ever calls.
const (
	methodStatus              = "status.get"
	methodTabs                = "session.tabs.listAll"
	methodCreate              = "agentSession.create"
	methodSend                = "agentSession.send"
	methodCancel              = "agentSession.cancel"
	methodHistory             = "agentSession.history"
	methodQueuedMessageDelete = "agentSession.queuedMessageDelete"
	methodClose               = "agentSession.close"
	methodReveal              = "agentSession.reveal"
)

var allowedMethods = map[string]bool{
	methodStatus: true, methodTabs: true, methodCreate: true, methodSend: true,
	methodCancel: true, methodHistory: true, methodQueuedMessageDelete: true,
	methodClose: true, methodReveal: true,
}

const (
	// callTimeout bounds one call; create and a first send may start Codex.
	callTimeout = 90 * time.Second
	// maxAnswerBytes is Orca 1.4.224 and 1.4.225's response frame bound, which
	// a full history page may reach.
	maxAnswerBytes = 4 << 20
	// keepalive is the frame Orca sends on long polls.
	keepalive = `{"_keepalive":true}`
	// RPC-level codes the Bridge tells apart.
	codeUnauthorized       = "unauthorized"
	codeInvalidArgument    = "invalid_argument"
	codeMethodNotFound     = "method_not_found"
	codeMethodNotSupported = "method_not_supported"
)

// errNoAnswer is a call whose outcome is uncertain: the socket closed or
// timed out without an answer, or Orca restarted in flight.
var errNoAnswer = errors.New("orca did not answer the call")

// errAnswerTooLarge is a complete answer longer than maxAnswerBytes: Orca
// answered, so the call is not uncertain, but the answer cannot be read.
// errRuntimeChanged is a fenced call refused before it was written: the
// runtime it would reach is not the one the caller named.
var errRuntimeChanged = errors.New("orca restarted; the call was not made")

var errAnswerTooLarge = errors.New("orca's answer exceeds 4 MiB")

// rpcError is a call Orca refused at the RPC level. Refusal is the code of
// error.data.refusal, when Orca recorded one.
type rpcError struct {
	Code    string
	Message string
	Refusal string
}

func (err *rpcError) Error() string {
	if err.Refusal != "" {
		return fmt.Sprintf("orca refused the call: %s (%s): %s", err.Code, err.Refusal, err.Message)
	}
	return fmt.Sprintf("orca refused the call: %s: %s", err.Code, err.Message)
}

// metadata is orca-runtime.json: the runtime's identity, socket and owner
// token, which change on every Orca restart.
type metadata struct {
	RuntimeID  string `json:"runtimeId"`
	PID        int    `json:"pid"`
	Transports []struct {
		Kind     string `json:"kind"`
		Endpoint string `json:"endpoint"`
	} `json:"transports"`
	AuthToken string `json:"authToken"`
}

func (meta metadata) socket() (string, error) {
	for _, transport := range meta.Transports {
		if transport.Kind == "unix" && transport.Endpoint != "" {
			return transport.Endpoint, nil
		}
	}
	return "", errors.New("orca-runtime.json lists no unix socket")
}

// transport calls the Orca runtime over its local unix socket, one request
// per connection, as Orca's own CLI does.
type transport struct {
	path string

	mu      sync.Mutex
	current *metadata
}

// userDataDir is $ORCA_USER_DATA_PATH, else Orca's default directory.
func userDataDir() (string, error) {
	if path := os.Getenv("ORCA_USER_DATA_PATH"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "orca"), nil
	}
	if config := os.Getenv("XDG_CONFIG_HOME"); config != "" {
		return filepath.Join(config, "orca"), nil
	}
	return filepath.Join(home, ".config", "orca"), nil
}

// metadata returns the runtime metadata, re-reading the file when asked.
func (t *transport) metadata(reload bool) (metadata, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && !reload {
		return *t.current, nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return metadata{}, fmt.Errorf("orca is not running (read orca-runtime.json): %w", err)
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, fmt.Errorf("decode orca-runtime.json: %w", err)
	}
	if meta.RuntimeID == "" || meta.AuthToken == "" || meta.PID <= 0 {
		return metadata{}, errors.New("orca-runtime.json lacks the runtime ID, token or process")
	}
	if err := syscall.Kill(meta.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return metadata{}, fmt.Errorf("orca is not running (process %d): %w", meta.PID, err)
	}
	t.current = &meta
	return meta, nil
}

// call sends one request and decodes its result into result. A failed dial
// or an unauthorized answer re-reads the metadata and tries once more; a
// changed runtime ID is errNoAnswer, after which the next call uses the new
// runtime.
func (t *transport) call(ctx context.Context, method string, params any, result any) (string, error) {
	return t.callAt(ctx, "", method, params, result)
}

// callAt is call fenced to runtimeID when it is not empty: a request is
// written only to that runtime, and errRuntimeChanged is returned when the
// metadata, read or re-read before any attempt, names another.
func (t *transport) callAt(ctx context.Context, runtimeID, method string, params any, result any) (string, error) {
	if !allowedMethods[method] {
		panic("orca method outside the allow-list: " + method)
	}
	var paramsJSON json.RawMessage
	if params != nil {
		paramsJSON = comm.Must(json.Marshal(params))
	}
	reload := false
	for attempt := 0; ; attempt++ {
		meta, err := t.metadata(reload)
		if err != nil {
			return "", err
		}
		if runtimeID != "" && meta.RuntimeID != runtimeID {
			return meta.RuntimeID, fmt.Errorf("%s: %w", method, errRuntimeChanged)
		}
		answeredBy, err := t.callOnce(ctx, meta, method, paramsJSON, result)
		var refused *rpcError
		retry := errors.As(err, &refused) && refused.Code == codeUnauthorized && refused.Refusal == ""
		var dialErr *net.OpError
		retry = retry || (errors.As(err, &dialErr) && dialErr.Op == "dial")
		if !retry || attempt > 0 {
			if errors.Is(err, errNoAnswer) || (err == nil && answeredBy != meta.RuntimeID) {
				_, _ = t.metadata(true)
			}
			if err == nil && answeredBy != meta.RuntimeID {
				return answeredBy, fmt.Errorf("%s: orca restarted during the call: %w", method, errNoAnswer)
			}
			return answeredBy, err
		}
		reload = true
	}
}

type answer struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Refusal *struct {
				Code string `json:"code"`
			} `json:"refusal"`
		} `json:"data"`
	} `json:"error"`
	Meta struct {
		RuntimeID string `json:"runtimeId"`
	} `json:"_meta"`
}

// callOnce makes one call on its own connection; it returns the runtime ID
// that answered. The request line holds the token, so it never reaches an
// error or a log.
func (t *transport) callOnce(ctx context.Context, meta metadata, method string, params json.RawMessage, result any) (string, error) {
	socket, err := meta.socket()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return "", fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	id := requestID()
	line := comm.Must(json.Marshal(struct { //nolint:gosec // the owner token goes only to the local Orca socket
		ID        string          `json:"id"`
		AuthToken string          `json:"authToken"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params,omitempty"`
	}{id, meta.AuthToken, method, params}))
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return "", fmt.Errorf("%s: %w", method, errNoAnswer)
	}
	reader := bufio.NewReaderSize(conn, 64<<10)
	for {
		frame, err := readFrame(reader)
		if errors.Is(err, errAnswerTooLarge) {
			return "", fmt.Errorf("%s: %w", method, err)
		}
		if err != nil {
			if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("%s: %w", method, errNoAnswer)
		}
		if string(frame) == keepalive {
			continue
		}
		var got answer
		if err := json.Unmarshal(frame, &got); err != nil {
			return "", fmt.Errorf("%s: decode the answer: %w", method, err)
		}
		if got.ID != id {
			return "", fmt.Errorf("%s: answer for another request", method)
		}
		if !got.OK {
			refused := &rpcError{Code: "unknown", Message: "no error detail"}
			if got.Error != nil {
				refused.Code, refused.Message = got.Error.Code, got.Error.Message
				if got.Error.Data != nil && got.Error.Data.Refusal != nil {
					refused.Refusal = got.Error.Data.Refusal.Code
				}
			}
			return got.Meta.RuntimeID, refused
		}
		if err := json.Unmarshal(got.Result, result); err != nil {
			return got.Meta.RuntimeID, fmt.Errorf("%s: decode the result: %w", method, err)
		}
		return got.Meta.RuntimeID, nil
	}
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		frame = append(frame, chunk...)
		if len(frame) > maxAnswerBytes {
			return nil, errAnswerTooLarge
		}
		if !isPrefix {
			return frame, nil
		}
	}
}

func requestID() string {
	random := make([]byte, 12)
	comm.Must(rand.Read(random))
	return "sv-" + hex.EncodeToString(random)
}
