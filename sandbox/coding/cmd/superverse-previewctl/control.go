// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

// The daemon's control socket lives in the Pod's ephemeral /tmp: it names the
// one daemon of this Pod and disappears with the Pod.
const (
	runtimeDirectory = "/tmp/superverse-preview"
	controlSocket    = runtimeDirectory + "/control.sock"
	// controlTimeout bounds one request; a command answers within the 10
	// seconds a platform Sandbox command may take.
	controlTimeout = 8 * time.Second
)

// Commands of the control protocol.
const (
	commandEnsure            = "environment-ensure"
	commandEnvironmentStatus = "environment-status"
	commandEnvironmentStop   = "environment-stop"
	commandBegin             = "begin"
	commandStatus            = "status"
	commandStop              = "stop"
)

// errNoSupervisor means no daemon answers on this Pod's control socket.
var errNoSupervisor = errors.New("no Preview supervisor is running")

// controlRequest is one request to the daemon. Begin carries the identity
// of the application start and the environment of the calling process,
// which the build and the application inherit.
type controlRequest struct {
	Command     string              `json:"command"`
	Application applicationIdentity `json:"application"`
	Environment []string            `json:"environment,omitempty"`
}

// ask sends one request to the daemon of this Pod and returns its answer.
func ask(request controlRequest) (answer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", controlSocket)
	if err != nil {
		return answer{}, fmt.Errorf("%w: %w", errNoSupervisor, err)
	}
	defer func() { _ = connection.Close() }()
	deadline, _ := ctx.Deadline()
	if err = connection.SetDeadline(deadline); err != nil {
		return answer{}, err
	}
	if err = json.NewEncoder(connection).Encode(request); err != nil {
		return answer{}, fmt.Errorf("send %s to the Preview supervisor: %w", request.Command, err)
	}
	var reply answer
	if err = json.NewDecoder(connection).Decode(&reply); err != nil {
		return answer{}, fmt.Errorf("read the Preview supervisor's answer to %s: %w", request.Command, err)
	}
	return reply, nil
}

// listenControl opens the control socket. Only the daemon that holds the
// state lock calls it, so a socket file left by a killed daemon is stale.
func listenControl() (net.Listener, error) {
	if err := os.MkdirAll(runtimeDirectory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(controlSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", controlSocket)
	if err != nil {
		return nil, fmt.Errorf("listen on the control socket: %w", err)
	}
	if err = os.Chmod(controlSocket, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

// serve answers control requests until the listener closes.
func (d *daemon) serve(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go d.handle(connection)
	}
}

func (d *daemon) handle(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(controlTimeout)); err != nil {
		return
	}
	var request controlRequest
	if err := json.NewDecoder(connection).Decode(&request); err != nil {
		return
	}
	if err := json.NewEncoder(connection).Encode(d.answer(request)); err != nil {
		log.Printf("answer %s: %v", request.Command, err)
	}
}

func (d *daemon) answer(request controlRequest) answer {
	switch request.Command {
	case commandEnsure:
		return d.environment.ensure()
	case commandEnvironmentStatus:
		return d.environment.status()
	case commandEnvironmentStop:
		d.requestStop()
		return answer{State: stateStopping}
	case commandBegin:
		if current := d.environment.status(); current.State != stateReady {
			return answer{State: stateError, Reason: "the Preview environment is " + current.State + ", not ready"}
		}
		return d.application.begin(request.Application, request.Environment)
	case commandStatus:
		return d.application.status()
	case commandStop:
		return d.application.stop()
	}
	return answer{State: stateError, Reason: fmt.Sprintf("unknown control command %q", request.Command)}
}
