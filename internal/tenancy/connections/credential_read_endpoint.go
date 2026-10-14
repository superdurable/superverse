// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// Applications read the credentials of the shared connections their scope
// uses through one endpoint, outside the business protocol and the Studio
// BFF, on a listener of its own (docs/api/credential-read.md). A scope token,
// stored only as its SHA-256, authenticates the scope; its usages authorize
// the connection. Requests and responses are never logged.

// CredentialReadPath is the endpoint's one route.
const CredentialReadPath = "/internal/connection-credentials/read"

const (
	// maxReadRequestBytes bounds a request body.
	maxReadRequestBytes = 4 << 10
	// maxCredentialBytes bounds the credential of a response.
	maxCredentialBytes = 64 << 10
	// maxConcurrentReads bounds the reads in flight; a read that waits for
	// another's renewal holds a slot, and the database pool is small.
	maxConcurrentReads = 64
	// maxScopeReads bounds the reads in flight of one Project scope, so a
	// Preview's code cannot hold every slot that Live reads need.
	maxScopeReads = 8
	// maxReadURLBytes bounds the endpoint URL a configuration carries.
	maxReadURLBytes = 2048
	// maxReadResponseBytes bounds a response.
	maxReadResponseBytes = 1 << 20
)

// readStatus is the closed set of answers to a read.
type readStatus string

const (
	readReady                   readStatus = "READY"
	readReauthorizationRequired readStatus = "REAUTHORIZATION_REQUIRED"
	readConnectionNotSelected   readStatus = "CONNECTION_NOT_SELECTED"
)

type readRequest struct {
	SharedConnectionID string `json:"sharedConnectionId"`
	RejectedRevision   uint64 `json:"rejectedRevision,omitempty"`
}

type readResponse struct {
	Status     readStatus                 `json:"status"`
	Credential map[string]json.RawMessage `json:"credential,omitempty"`
	Revision   uint64                     `json:"revision,omitempty"`
	IssuedAt   *time.Time                 `json:"issuedAt,omitempty"`
	ExpiresAt  *time.Time                 `json:"expiresAt,omitempty"`
}

// ReadEndpoint serves CredentialReadPath.
type ReadEndpoint struct {
	reader *CredentialReader
	slots  chan struct{}
	// scopes counts the reads in flight of each Project scope that has one.
	mutex  sync.Mutex
	scopes map[string]int
}

// NewReadEndpoint serves reads with reader.
func NewReadEndpoint(reader *CredentialReader) *ReadEndpoint {
	return &ReadEndpoint{reader: reader, slots: make(chan struct{}, maxConcurrentReads), scopes: map[string]int{}}
}

// admitScope takes one of a Project scope's read slots, if one is free, and
// returns its release.
func (endpoint *ReadEndpoint) admitScope(scope string) (func(), bool) {
	endpoint.mutex.Lock()
	defer endpoint.mutex.Unlock()
	if endpoint.scopes[scope] >= maxScopeReads {
		return nil, false
	}
	endpoint.scopes[scope]++
	return func() {
		endpoint.mutex.Lock()
		defer endpoint.mutex.Unlock()
		if endpoint.scopes[scope]--; endpoint.scopes[scope] == 0 {
			delete(endpoint.scopes, scope)
		}
	}, true
}

// ServeHTTP answers one read. Every outcome of an authenticated request is
// HTTP 200 with its status; errors carry no token, credential or body.
func (endpoint *ReadEndpoint) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != CredentialReadPath {
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
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxReadRequestBytes))
	if maxBytes := (*http.MaxBytesError)(nil); errors.As(err, &maxBytes) {
		http.Error(response, "the request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	var read readRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err != nil || decoder.Decode(&read) != nil || read.SharedConnectionID == "" {
		http.Error(response, "the request is malformed", http.StatusBadRequest)
		return
	}
	select {
	case endpoint.slots <- struct{}{}:
		defer func() { <-endpoint.slots }()
	default:
		http.Error(response, "too many credential reads", http.StatusTooManyRequests)
		return
	}
	answer, status := endpoint.read(request.Context(), ScopeTokenHash(token), read)
	switch status {
	case http.StatusOK:
	case http.StatusConflict:
		http.Error(response, errConnectorVersionUnsupported.Error()+"; select a connection of a newer release", status)
		return
	default:
		http.Error(response, http.StatusText(status), status)
		return
	}
	encoded, err := json.Marshal(answer)
	if err != nil || len(encoded) > maxReadResponseBytes {
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(encoded)
}

// read authorizes a read by its scope token's hash and returns the answer, or
// the HTTP status of a request that has none.
func (endpoint *ReadEndpoint) read(ctx context.Context, tokenHash string, read readRequest) (readResponse, int) {
	authorization, err := endpoint.reader.dao.AuthorizeRead(ctx, tokenHash, read.SharedConnectionID)
	switch {
	case errors.Is(err, connectiondao.ErrNotFound):
		return readResponse{}, http.StatusUnauthorized
	case err != nil:
		return readResponse{}, http.StatusServiceUnavailable
	case authorization.Connection == nil:
		return readResponse{Status: readConnectionNotSelected}, http.StatusOK
	}
	release, admitted := endpoint.admitScope(authorization.ProjectID + "/" + authorization.Scope)
	if !admitted {
		return readResponse{}, http.StatusTooManyRequests
	}
	defer release()
	credential, err := endpoint.reader.read(ctx, *authorization.Connection, read.RejectedRevision)
	switch {
	case errors.Is(err, ErrReauthorizationRequired):
		return readResponse{Status: readReauthorizationRequired}, http.StatusOK
	case errors.Is(err, errConnectorVersionUnsupported):
		return readResponse{}, http.StatusConflict
	case errors.Is(err, errNoAnswer), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return readResponse{}, http.StatusServiceUnavailable
	case err != nil:
		return readResponse{}, http.StatusInternalServerError
	}
	values := credential.forApplication()
	if encoded, _ := json.Marshal(values); len(encoded) > maxCredentialBytes {
		return readResponse{}, http.StatusInternalServerError
	}
	return readResponse{Status: readReady, Credential: values, Revision: credential.revision, IssuedAt: credential.issuedAt, ExpiresAt: credential.expiresAt}, http.StatusOK
}

// newScopeToken returns a random scope token and the hash the database
// stores.
func newScopeToken() (string, string) {
	value := make([]byte, 32)
	_, _ = rand.Read(value) // crypto/rand.Read never fails; it crashes the program instead.
	token := base64.RawURLEncoding.EncodeToString(value)
	return token, ScopeTokenHash(token)
}

// ScopeTokenHash is the hash the database stores of a scope token.
func ScopeTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// ValidateReadURL admits the endpoint URL that configurations carry: an
// absolute https URL, or http to a loopback, private, single-label or
// cluster-local host, at CredentialReadPath, without user information, a
// query or a fragment. Applications refuse any other.
func ValidateReadURL(value string) error {
	target, err := url.Parse(value)
	if err != nil || len(value) > maxReadURLBytes || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" ||
		target.Path != CredentialReadPath || (target.Scheme != "https" && target.Scheme != "http") {
		return errors.New("must be an absolute http or https URL of " + CredentialReadPath + " without user information, a query or a fragment")
	}
	if target.Scheme == "http" && !isPrivateHost(target.Hostname()) {
		return errors.New("may use http only for a loopback, private, single-label or cluster-local host")
	}
	return nil
}

// isPrivateHost reports hosts only a trusted network resolves, the rule of
// the applications' broker client.
func isPrivateHost(hostname string) bool {
	if address := net.ParseIP(hostname); address != nil {
		return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
	}
	return hostname == "localhost" || hostname == "host.docker.internal" || !strings.Contains(hostname, ".") ||
		strings.HasSuffix(hostname, ".svc") || strings.HasSuffix(hostname, ".svc.cluster.local")
}
