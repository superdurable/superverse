// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform"
)

// ErrOutcomeUnknown never authorizes replay without observing the owning operation.
var ErrOutcomeUnknown = errors.New("GitHub request outcome is uncertain")

// HTTPError is a GitHub response outside 2xx.
type HTTPError struct {
	Status                            int
	Method                            string
	path, requiredPermissions, reason string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GitHub %s %s returned HTTP %d (%s; required: %s)", e.Method, e.path, e.Status, e.reason, e.requiredPermissions)
}

// HTTPStatusCode lets platform classify the response without knowing GitHub.
func (e *HTTPError) HTTPStatusCode() int { return e.Status }

// apiClient bounds every call, including its body read, and never follows a
// redirect, which would carry the bearer token to another host.
var apiClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// request sends one JSON call to the GitHub API authenticated by token. It also
// runs in goroutines that nothing recovers, so it reports every failure as an
// error. A lost or unreadable response is an OutcomeUnknownError: the call may
// have taken effect.
func request(ctx context.Context, method, path, token string, input, output any) error {
	var data []byte
	if input != nil {
		var err error
		if data, err = json.Marshal(input); err != nil {
			return err
		}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(data)) //nolint:gosec // G704: the host is the fixed GitHub API
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Accept", "application/vnd.github+json")
	httpRequest.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	httpRequest.Header.Set("User-Agent", "superverse-v2")
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := apiClient.Do(httpRequest) //nolint:gosec // G704: the host is the fixed GitHub API
	if err != nil {
		return &platform.OutcomeUnknownError{Sentinel: ErrOutcomeUnknown, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		return &HTTPError{Status: response.StatusCode, Method: method, path: path, reason: comm.TruncateUTF8(failure.Message, 512), requiredPermissions: response.Header.Get("X-Accepted-GitHub-Permissions")}
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	} else {
		err = json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(output)
	}
	if err != nil {
		return &platform.OutcomeUnknownError{Sentinel: ErrOutcomeUnknown, Message: "GitHub response could not be read", Cause: err}
	}
	return nil
}
