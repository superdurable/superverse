// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
)

const maxConcurrentPairings = 16

// Endpoint serves only the Bridge protocol, independently of the Studio BFF.
// RAEXEC extends its exact route switch with activate, sync and report.
type Endpoint struct {
	service                              *Connections
	downloads                            *downloads
	logger                               *slog.Logger
	pairSlots                            chan struct{}
	controlSlots, reportSlots, syncSlots chan struct{}
	reads                                *readAdmission
	connectionReports                    connectionReports
	lifetime                             context.Context
	cancel                               context.CancelFunc
}

// NewEndpoint binds singleton services and the fixed environment artifact root.
func NewEndpoint(service *Connections, downloadRoot string, logger *slog.Logger, maxLongPolls, maxReads, maxReports int) *Endpoint {
	if maxReports < 1 || maxReports > 256 || maxLongPolls < 1 || maxLongPolls > 10000 || maxReads < 1 || maxReads > 64 {
		panic("invalid remote endpoint admission bounds")
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Endpoint{service: service, downloads: &downloads{root: downloadRoot, minimumVersion: service.MinimumBridgeVersion}, logger: logger,
		pairSlots: make(chan struct{}, maxConcurrentPairings), controlSlots: make(chan struct{}, 16), reportSlots: make(chan struct{}, maxReports), syncSlots: make(chan struct{}, maxLongPolls),
		reads: newReadAdmission(maxReads, maxLongPolls), connectionReports: connectionReports{active: make(map[string]int)}, lifetime: lifetime, cancel: cancel}
}

// ServeHTTP accepts exact paths and methods; no filesystem path comes from a request.
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
		e.writeError(w, r, refusal(bridgeapi.ErrorMalformed))
		return
	}
	switch r.URL.Path {
	case bridgeapi.PathPair:
		if !method(w, r, http.MethodPost) {
			return
		}
		// Pairing has a separate per-replica admission bound, with no waiters.
		// Acquire before reading a body or authenticating; RAEXEC control/report
		// admission must not consume these slots.
		release, admitted := e.admit(w, r, e.pairSlots)
		if !admitted {
			return
		}
		defer release()

		var request bridgeapi.PairRequest
		if err := decodeBody(w, r, bridgeapi.MaxPairRequestBytes, &request); err != nil {
			e.writeError(w, r, err)
			return
		}
		ctx, cancel := e.requestContext(r, 10*time.Second)
		defer cancel()
		response, err := e.service.pair(ctx, request)
		if err != nil {
			e.writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	case bridgeapi.PathActivate, bridgeapi.PathSync, bridgeapi.PathReport:
		if !method(w, r, http.MethodPost) {
			return
		}
		switch r.URL.Path {
		case bridgeapi.PathActivate:
			e.serveActivate(w, r)
		case bridgeapi.PathSync:
			e.serveSync(w, r)
		case bridgeapi.PathReport:
			e.serveReport(w, r)
		}
	case bridgeapi.PathDisconnect:
		if !method(w, r, http.MethodPost) {
			return
		}
		release, admitted := e.admit(w, r, e.controlSlots)
		if !admitted {
			return
		}
		defer release()
		var request bridgeapi.DisconnectRequest
		if err := decodeBody(w, r, bridgeapi.MaxDisconnectRequestBytes, &request); err != nil {
			e.writeError(w, r, err)
			return
		}
		token, err := bearer(r)
		if err != nil {
			e.writeError(w, r, err)
			return
		}
		ctx, cancel := e.requestContext(r, 10*time.Second)
		defer cancel()
		access, err := e.service.authenticate(ctx, token)
		if err == nil {
			err = e.service.disconnect(ctx, access)
		}
		if err != nil {
			e.writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, bridgeapi.DisconnectResponse{})
	default:
		if strings.HasPrefix(r.URL.Path, "/remote-agent/v1/downloads/") {
			if method(w, r, http.MethodGet) {
				e.serveDownload(w, r)
			}
			return
		}
		e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
	}
}

func method(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, bridgeapi.ErrorBody{Code: bridgeapi.ErrorMalformed, Message: "method not allowed"})
	return false
}

func bearer(r *http.Request) (string, error) {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") || len(headers[0]) != len("Bearer ")+43 {
		return "", refusal(bridgeapi.ErrorUnauthorized)
	}
	return strings.TrimPrefix(headers[0], "Bearer "), nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return refusal(bridgeapi.ErrorTooLarge)
	}
	if err != nil || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return refusal(bridgeapi.ErrorMalformed)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return refusal(bridgeapi.ErrorMalformed)
	}
	return nil
}

func (e *Endpoint) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, projects.ErrForbidden) {
		e.writeError(w, r, refusal(bridgeapi.ErrorForbidden))
		return
	}
	var report *remoteexec.ReportRefusal
	if errors.As(err, &report) {
		response := refusalResponse(report)
		if response.Code.Status() == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "2")
		}
		writeJSON(w, response.Code.Status(), response)
		return
	}
	var rejected *requestError
	if errors.As(err, &rejected) {
		if rejected.code.Status() == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "2")
		}
		writeJSON(w, rejected.code.Status(), bridgeapi.ErrorBody{Code: rejected.code, Message: rejected.Error()})
		return
	}
	if errors.Is(err, postgres.ErrNotFound) {
		e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
		return
	}
	// SQL errors may contain credentials minted inside Pair, which this layer
	// never receives. Log only typed metadata, never an error's text/values.
	sqlState, constraint := postgres.ErrorMetadata(err)
	failure, level := "dependency", slog.LevelError
	if errors.Is(err, context.Canceled) {
		failure, level = "canceled", slog.LevelInfo
	} else if errors.Is(err, context.DeadlineExceeded) {
		failure = "deadline_exceeded"
	}
	e.logger.Log(r.Context(), level, "Remote agent HTTP request failed", "method", r.Method, "path", r.URL.Path,
		"failure", failure, "error_type", fmt.Sprintf("%T", err), "sqlstate", sqlState, "constraint", constraint)
	writeJSON(w, http.StatusServiceUnavailable, bridgeapi.ErrorBody{Code: bridgeapi.ErrorUnavailable, Message: "remote agent service unavailable"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Server returns bounded HTTP process resources; bootstrap owns start and drain.
func (e *Endpoint) Server(listen string) *http.Server {
	return &http.Server{Addr: listen, Handler: e, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
}
