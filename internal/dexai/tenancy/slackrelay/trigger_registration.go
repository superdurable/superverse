// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package slackrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// registrationStatus is the closed set of answers to a registration.
type registrationStatus string

const (
	registrationRegistered            registrationStatus = "REGISTERED"
	registrationConnectionNotSelected registrationStatus = "CONNECTION_NOT_SELECTED"
	registrationNotRelayed            registrationStatus = "NOT_RELAYED"
)

// maxConnectionNameRunes bounds a connection name, the trigger inbox key's
// rule.
const maxConnectionNameRunes = 256

type registrationRequest struct {
	SharedConnectionID string `json:"sharedConnectionId"`
	ConnectionName     string `json:"connectionName"`
}

// ServeRegistration answers an application's POST of RegistrationPath on the
// credential read listener: the scope its token authenticates registers the
// connection name whose Slack trigger receives the relayed events of the
// connection it selects. The request names no address; the platform derives
// the receiver from the scope's public route.
func (r *Relay) ServeRegistration(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != RegistrationPath {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, hasToken := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	if !hasToken || token == "" {
		http.Error(response, "a scope token is required", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxRegistrationBody))
	if maxBytes := (*http.MaxBytesError)(nil); errors.As(err, &maxBytes) {
		http.Error(response, "the request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	var registration registrationRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err != nil || decoder.Decode(&registration) != nil || registration.SharedConnectionID == "" || !validConnectionName(registration.ConnectionName) {
		http.Error(response, "the request is malformed", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), databaseDeadline*5)
	defer cancel()
	err = r.dao.RegisterTrigger(ctx, connectiondao.TriggerRegistration{
		TokenHash: connections.ScopeTokenHash(token), ConnectionID: registration.SharedConnectionID, ConnectionName: registration.ConnectionName,
		ConnectorID: connections.SlackConnectorID, Limit: registrationLimit,
	})
	status := registrationRegistered
	switch {
	case errors.Is(err, connectiondao.ErrNotFound):
		http.Error(response, "the scope token is unknown", http.StatusUnauthorized)
		return
	case errors.Is(err, connectiondao.ErrNotSelected):
		status = registrationConnectionNotSelected
	case errors.Is(err, connectiondao.ErrNotRelayed):
		status = registrationNotRelayed
	case errors.Is(err, connectiondao.ErrRegistrationLimit):
		http.Error(response, "the connection already has its limit of registered connection names in this scope", http.StatusConflict)
		return
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	case err != nil:
		r.logger.ErrorContext(request.Context(), "slack trigger registration failed", "error", err)
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(map[string]registrationStatus{"status": status})
}

// validConnectionName admits 1 to 256 characters without leading or
// trailing space, NUL, CR or LF.
func validConnectionName(name string) bool {
	count := utf8.RuneCountInString(name)
	return count >= 1 && count <= maxConnectionNameRunes && utf8.ValidString(name) && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\x00\r\n")
}
