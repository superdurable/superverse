// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// apiVersion is the Docker Engine API version every request names. 1.44 is
// the first that attaches a new container to several networks at once.
const apiVersion = "v1.44"

// queryTrue is the value that turns a boolean query parameter of the API on.
const queryTrue = "true"

// callTimeout bounds an ordinary call; a call that waits for the engine, such
// as a stop or a pull, sets its own bound.
const callTimeout = 30 * time.Second

// Client calls one Docker Engine over its unix socket. Every path is built by
// this package from typed arguments, so no caller selects an endpoint.
type Client struct {
	http      *http.Client
	transport *http.Transport
}

// New reaches the Docker Engine listening on the unix socket at socket.
func New(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		},
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     time.Minute,
	}
	// A followed redirect would leave the socket's API.
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{http: client, transport: transport}
}

// Close releases the idle connections of the client.
func (c *Client) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}

// responseError is a response the engine refused, with the message it sent.
type responseError struct {
	method, path string
	status       int
	message      string
}

func (e *responseError) Error() string {
	text := fmt.Sprintf("Docker %s %s returned %d", e.method, e.path, e.status)
	if e.message != "" {
		text += ": " + e.message
	}
	return text
}

// HTTPStatusCode lets platform classify the response without knowing Docker.
func (e *responseError) HTTPStatusCode() int { return e.status }

// IsConflict reports whether the engine refused a request because of the
// current state of an object: a name that is taken, a volume or network that
// is in use, or a removal already in progress.
func IsConflict(err error) bool {
	var status *responseError
	return errors.As(err, &status) && status.status == http.StatusConflict
}

// Filters select the objects a list returns, as the engine's filters
// parameter spells them: label=key=value, name=..., volume=...
type Filters map[string][]string

// Labels selects objects that carry every label of labels.
func Labels(labels map[string]string) Filters {
	filters := Filters{}
	for key, value := range labels {
		filters["label"] = append(filters["label"], key+"="+value)
	}
	return filters
}

func (f Filters) query() url.Values {
	if len(f) == 0 {
		return url.Values{}
	}
	return url.Values{"filters": {string(comm.Must(json.Marshal(f)))}}
}

// call sends one request and decodes a successful JSON response into out. A
// 404 and a 304 are no errors: call returns their status and leaves out
// untouched, so the caller decides what absence or an unchanged state means. A
// transport failure leaves the effect of a mutation unconfirmed; the caller
// reads the object again before it retries.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) (int, error) {
	data, status, err := c.send(ctx, method, path, query, body, "application/json", 8<<20)
	if err != nil || data == nil || out == nil {
		return status, err
	}
	if err = json.Unmarshal(data, out); err != nil {
		return status, fmt.Errorf("decode the Docker %s %s response: %w", method, path, err)
	}
	return status, nil
}

// send sends one request with a body of contentType: JSON-encoded unless it
// is already bytes. It returns at most limit bytes of a successful response;
// a 404 and a 304 return no data.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any, contentType string, limit int64) ([]byte, int, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	var payload io.Reader
	switch value := body.(type) {
	case nil:
	case []byte:
		payload = bytes.NewReader(value)
	default:
		payload = bytes.NewReader(comm.Must(json.Marshal(value)))
	}
	// path is escaped already; the host is a placeholder of the socket.
	target := "http://docker/" + apiVersion + path
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	request := comm.Must(http.NewRequestWithContext(ctx, method, target, payload))
	if payload != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("the effect of Docker %s %s is unconfirmed: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusNotModified:
		return nil, response.StatusCode, nil
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, response.StatusCode, statusError(method, path, response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read the Docker %s %s response: %w", method, path, err)
	}
	if int64(len(data)) > limit {
		return nil, response.StatusCode, fmt.Errorf("the Docker %s %s response exceeds %d bytes", method, path, limit)
	}
	return data, response.StatusCode, nil
}

// statusError describes a refused response with the message the engine sent.
func statusError(method, path string, response *http.Response) *responseError {
	var refusal struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&refusal)
	return &responseError{method: method, path: path, status: response.StatusCode, message: comm.TruncateUTF8(refusal.Message, 1<<10)}
}

// segment escapes one path segment, such as an object name.
func segment(value string) string { return url.PathEscape(value) }

// reference escapes an image reference, whose repository keeps its slashes.
func reference(value string) string { return strings.ReplaceAll(url.PathEscape(value), "%2F", "/") }
