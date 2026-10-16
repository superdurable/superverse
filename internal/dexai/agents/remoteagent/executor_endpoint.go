// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// Replica-local disposable admission; global report admission bounds this map.
type connectionReports struct {
	mu     sync.Mutex
	active map[string]int
}

func (gate *connectionReports) admit(connection string) (func(), bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.active[connection] >= 4 {
		return nil, false
	}
	gate.active[connection]++
	return func() {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		gate.active[connection]--
		if gate.active[connection] == 0 {
			delete(gate.active, connection)
		}
	}, true
}

func (e *Endpoint) admit(w http.ResponseWriter, r *http.Request, slots chan struct{}) (func(), bool) {
	if e.lifetime.Err() != nil {
		e.writeError(w, r, refusal(bridgeapi.ErrorUnavailable))
		return nil, false
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		e.writeError(w, r, refusal(bridgeapi.ErrorRateLimited))
		return nil, false
	}
}

func (e *Endpoint) requestContext(r *http.Request, budget time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	stop := context.AfterFunc(e.lifetime, cancel)
	return ctx, func() { stop(); cancel() }
}

func (e *Endpoint) serveActivate(w http.ResponseWriter, r *http.Request) {
	release, admitted := e.admit(w, r, e.controlSlots)
	if !admitted {
		return
	}
	defer release()
	ctx, cancel := e.requestContext(r, 15*time.Second)
	defer cancel()
	token, err := bearer(r)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	access, err := e.service.authenticate(ctx, token)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	var input bridgeapi.ActivateRequest
	if err = decodeBody(w, r, bridgeapi.MaxActivateRequestBytes, &input); err != nil {
		e.writeError(w, r, err)
		return
	}
	response, err := e.service.activate(ctx, access, input)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (e *Endpoint) serveReport(w http.ResponseWriter, r *http.Request) {
	// No queue, before body/auth: even unauthenticated requests consume only
	// bounded transport state and immediately get rate_limited when saturated.
	release, admitted := e.admit(w, r, e.reportSlots)
	if !admitted {
		return
	}
	defer release()
	ctx, cancel := e.requestContext(r, 15*time.Second)
	defer cancel()
	token, err := bearer(r)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	access, err := e.service.authenticate(ctx, token)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	releaseConnection, admitted := e.connectionReports.admit(access.ConnectionID)
	if !admitted {
		e.writeError(w, r, refusal(bridgeapi.ErrorRateLimited))
		return
	}
	defer releaseConnection()
	var input bridgeapi.Report
	if err = decodeBody(w, r, bridgeapi.MaxReportRequestBytes, &input); err != nil {
		e.writeError(w, r, err)
		return
	}
	response, err := e.service.report(ctx, access, input)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Close rejects admission, wakes queued reads and cancels all active long polls.
// bootstrap calls it before HTTP drain; each request still releases its own slot.
func (e *Endpoint) Close() {
	e.cancel()
	e.reads.close()
}

func readAdmissionError(err error) error {
	if errors.Is(err, errReadAdmissionClosed) {
		return refusal(bridgeapi.ErrorUnavailable)
	}
	return refusal(bridgeapi.ErrorRateLimited)
}
