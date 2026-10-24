// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// Client calls one Kubernetes API server. Bootstrap validates ServerURL once;
// every path is built by code from the object toolkit or literal prefixes.
type Client struct {
	ServerURL string
	// TokenFile holds the projected service account token. It is reread for every
	// call so a rotated token applies at once. Without it, HTTP authenticates.
	TokenFile string
	HTTP      *http.Client
}

// StatusError is a non-2xx, non-404 API response. Reason and Message come from
// the Kubernetes Status the server answered with, when it sent one.
type StatusError struct {
	method, path    string
	Status          int
	reason, message string
}

func (e *StatusError) Error() string {
	text := fmt.Sprintf("Kubernetes %s %s returned %d", e.method, e.path, e.Status)
	if e.reason != "" {
		text += " " + e.reason
	}
	if e.message != "" {
		text += ": " + e.message
	}
	return text
}

// HTTPStatusCode lets platform classify the response without knowing Kubernetes.
func (e *StatusError) HTTPStatusCode() int { return e.Status }

// Call sends one request. A 404 is not an error: it returns a nil object so the
// caller decides what absence means. A transport failure leaves the effect of
// a mutation unconfirmed; the caller reads the resource again before retrying.
func (c *Client) Call(ctx context.Context, method, path string, body any) (Object, int, error) {
	contentType := "application/json"
	if method == http.MethodPatch {
		contentType = "application/apply-patch+yaml"
	}
	return c.call(ctx, method, path, body, contentType)
}

// MergePatch updates only the named fields. A resourceVersion in metadata
// makes a stale scale request conflict instead of overwriting another writer.
func (c *Client) MergePatch(ctx context.Context, path string, body Object) (Object, int, error) {
	return c.call(ctx, http.MethodPatch, path, body, "application/merge-patch+json")
}

func (c *Client) call(ctx context.Context, method, path string, body any, contentType string) (Object, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	const limit = 8 << 20
	data, code, err := c.send(ctx, method, path, body, limit, contentType)
	if err != nil || data == nil {
		return nil, code, err
	}
	if len(data) > limit {
		return nil, code, fmt.Errorf("the Kubernetes %s %s response exceeds its size limit", method, path)
	}
	var result Object
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, code, fmt.Errorf("decode the Kubernetes %s %s response: %w", method, path, err)
	}
	return result, code, nil
}

// ReadText reads the text a GET of path answers, such as a container log,
// keeping at most limit bytes of it. A 404 returns "" with its status.
func (c *Client) ReadText(ctx context.Context, path string, limit int) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, code, err := c.send(ctx, http.MethodGet, path, nil, limit, "application/json")
	if len(data) > limit {
		data = data[:limit]
	}
	return string(data), code, err
}

// send sends one request and reads up to limit+1 bytes of a successful
// response, so the caller can tell that it was cut. A 404 returns nil data.
func (c *Client) send(ctx context.Context, method, path string, body any, limit int, contentType string) ([]byte, int, error) {
	var data []byte
	if body != nil {
		data = comm.Must(json.Marshal(body))
	}
	request := comm.Must(http.NewRequestWithContext(ctx, method, strings.TrimRight(c.ServerURL, "/")+path, bytes.NewReader(data)))
	request.Header.Set("Content-Type", contentType)
	if c.TokenFile != "" {
		token, err := os.ReadFile(c.TokenFile)
		if err != nil {
			return nil, 0, fmt.Errorf("read the projected Kubernetes credential: %w", err)
		}
		token = bytes.TrimSpace(token)
		if len(token) == 0 {
			return nil, 0, errors.New("the projected Kubernetes credential is empty")
		}
		request.Header.Set("Authorization", "Bearer "+string(token))
	}
	// A followed redirect would carry the bearer token to another host.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("the effect of Kubernetes %s is unconfirmed: %w", method, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, response.StatusCode, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, statusError(method, path, response)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read the Kubernetes %s %s response: %w", method, path, err)
	}
	return data, response.StatusCode, nil
}

// statusError describes a failed response with the reason and message of the
// Kubernetes Status it carries. A body that is not a Status leaves both empty.
func statusError(method, path string, response *http.Response) *StatusError {
	const messageLimit = 1 << 10
	var status struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status)
	if len(status.Message) > messageLimit {
		status.Message = comm.TruncateUTF8(status.Message, messageLimit) + "..."
	}
	return &StatusError{method: method, path: path, Status: response.StatusCode, reason: status.Reason, message: status.Message}
}

// GetOrCreate returns the object named by desired, creating it when absent. A
// concurrent create (409) resolves to the winner, so the caller must verify
// that the returned object is its own before relying on it. The object is
// never nil without an error: a create answered 404, such as one into a
// deleted namespace, or a winner deleted before its re-read is an error.
func (c *Client) GetOrCreate(ctx context.Context, desired Object) (Object, error) {
	path, name := CollectionPath(desired), String(Child(desired, "metadata"), "name")
	found, code, err := c.Call(ctx, http.MethodGet, path+"/"+name, nil)
	if err != nil || code != http.StatusNotFound {
		return found, err
	}
	found, code, err = c.Call(ctx, http.MethodPost, path, desired)
	if code == http.StatusConflict {
		found, code, err = c.Call(ctx, http.MethodGet, path+"/"+name, nil)
	}
	if err == nil && code == http.StatusNotFound {
		return nil, fmt.Errorf("create %s/%s: the collection or object is absent", path, name)
	}
	return found, err
}

// IsForbiddenIn reports whether err is a 403 for a resource inside namespace.
func IsForbiddenIn(err error, namespace string) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Status == http.StatusForbidden && strings.Contains(status.path, "/namespaces/"+namespace+"/")
}

// authorizerLag bounds how long an authorizer may still deny a new RoleBinding.
const authorizerLag = 15 * time.Second

// WithinAuthorizerLag reports whether err is a 403 inside namespace that may only
// mean the authorizer has not yet observed the RoleBinding created at created.
// Kubernetes authorizers briefly lag a binding's create response; a denial
// outside that window, or outside the namespace, stays conclusive.
func WithinAuthorizerLag(err error, created time.Time, namespace string) bool {
	age := time.Since(created)
	return !created.IsZero() && age >= 0 && age < authorizerLag && IsForbiddenIn(err, namespace)
}
