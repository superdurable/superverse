// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package platform is the Bridge's client of the remote agent endpoint.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/comm"
)

// Response body limits. A sync answer may hold one entry beyond its soft
// budget; activate carries the tool schemas.
const (
	maxSyncResponseBytes     = bridgeapi.MaxSyncResponseBytes
	maxActivateResponseBytes = 2 << 20
	maxSmallResponseBytes    = 64 << 10
)

// Each attempt is bounded; a sync attempt outlasts the server's long poll.
const (
	attemptTimeout     = 30 * time.Second
	syncAttemptTimeout = bridgeapi.SyncWait + 15*time.Second
	firstBackoff       = time.Second
	maxBackoff         = 60 * time.Second
	// maxRetryAfter bounds the wait a 429's Retry-After may ask for.
	maxRetryAfter = 5 * time.Minute
)

// Client calls the remote agent endpoint as one connection.
type Client struct {
	endpoint *url.URL
	token    string
	agent    string
	http     *http.Client
	logger   *slog.Logger
}

// RefusalError is an answer of the endpoint other than success: its status and
// coded body. A refusal is final for its request; the caller decides.
type RefusalError struct {
	Status int
	Body   bridgeapi.ErrorBody
	// RetryAfter is a 429's Retry-After in seconds, the least wait before
	// the retry; zero when absent.
	RetryAfter time.Duration
}

func (refusal *RefusalError) Error() string {
	return fmt.Sprintf("remote agent endpoint refused the request: %s (HTTP %d): %s",
		refusal.Body.Code, refusal.Status, refusal.Body.Message)
}

// ErrInvalidRequest marks a request refused before it was sent: it fails its
// local validation or size limit, so it never reached the endpoint and
// sending it again fails the same way.
var ErrInvalidRequest = errors.New("the request is invalid and was not sent")

// RefusalCode returns the code of a refusal in err's chain.
func RefusalCode(err error) (bridgeapi.ErrorCode, bool) {
	var refusal *RefusalError
	if errors.As(err, &refusal) {
		return refusal.Body.Code, true
	}
	return "", false
}

// New returns the client of one connection. endpoint is the environment's
// Studio origin; it must be HTTPS unless it is a loopback address.
func New(endpoint, token string, bridge bridgeapi.BridgeInfo, logger *slog.Logger) (*Client, error) {
	parsed, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{
		endpoint: parsed,
		token:    token,
		agent:    "superverse-bridge/" + bridge.Version,
		// The endpoint never redirects; following one would replay the token
		// and the body to another URL.
		http:   &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		logger: logger,
	}, nil
}

// ParseEndpoint validates an endpoint origin.
func ParseEndpoint(endpoint string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("endpoint %q must be an origin such as https://studio.example.com", endpoint)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(parsed.Hostname()); parsed.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("endpoint %q must use https unless it is a loopback address", endpoint)
		}
	default:
		return nil, fmt.Errorf("endpoint %q must use https", endpoint)
	}
	parsed.Path = ""
	return parsed, nil
}

// Pair redeems a pairing code. It carries no bearer token: the answer is the
// connection's token.
func (client *Client) Pair(ctx context.Context, code string, bridge bridgeapi.BridgeInfo) (bridgeapi.PairResponse, error) {
	var response bridgeapi.PairResponse
	err := client.post(ctx, call{
		path: bridgeapi.PathPair, request: bridgeapi.PairRequest{Code: code, Bridge: bridge},
		requestLimit: bridgeapi.MaxPairRequestBytes, response: &response, responseLimit: maxSmallResponseBytes,
	})
	if err == nil && (response.ConnectionID == "" || response.Token == "" || response.ProjectID == "") {
		err = errors.New("pair: the answer lacks the connection, token or Project")
	}
	return response, err
}

// Activate makes the connection the Session's executor, or repeats it.
func (client *Client) Activate(ctx context.Context, binding bridgeapi.Binding) (bridgeapi.ActivateResponse, error) {
	var response bridgeapi.ActivateResponse
	err := client.post(ctx, call{
		path: bridgeapi.PathActivate, request: bridgeapi.ActivateRequest{Binding: binding}, bearer: true,
		requestLimit: bridgeapi.MaxActivateRequestBytes, response: &response, responseLimit: maxActivateResponseBytes,
	})
	return response, err
}

// Sync long-polls the Session for the entries after the cursor, and for the
// state when its digest differs from stateDigest.
func (client *Client) Sync(ctx context.Context, epoch, after uint64, stateDigest string, withContext bool) (bridgeapi.SyncResponse, error) {
	var response bridgeapi.SyncResponse
	err := client.post(ctx, call{
		path: bridgeapi.PathSync, request: bridgeapi.SyncRequest{Epoch: epoch, After: after, StateDigest: stateDigest, Context: withContext}, bearer: true,
		requestLimit: bridgeapi.MaxSyncRequestBytes, response: &response, responseLimit: maxSyncResponseBytes,
		attemptTimeout: syncAttemptTimeout,
	})
	if err == nil && len(response.Entries) > bridgeapi.MaxSyncEntries {
		err = fmt.Errorf("sync: %d entries exceed the limit of %d", len(response.Entries), bridgeapi.MaxSyncEntries)
	}
	if err == nil && response.ExaminedThrough < after {
		err = fmt.Errorf("sync: examined through %d, before the cursor %d", response.ExaminedThrough, after)
	}
	if err == nil && overSoftBudget(response.Entries) {
		// The contract is lenient here: a large answer is applied, never refused.
		client.logger.WarnContext(ctx, "a sync answer kept adding entries past its soft budget", "entries", len(response.Entries))
	}
	return response, err
}

// overSoftBudget reports an answer that added an entry after its earlier
// entries already exceeded the soft budget. One large entry, alone or last,
// is within the contract.
func overSoftBudget(entries []bridgeapi.Entry) bool {
	total := 0
	for _, entry := range entries[:max(len(entries), 1)-1] {
		total += len(entry.Text)
		for _, result := range entry.ToolResults {
			total += len(result.Content)
		}
	}
	return total > bridgeapi.SyncSoftBudgetBytes
}

// Report sends one report. A network failure or a 429 (reports_pending:
// the Session's inbox is full) resends the same report, with the same
// sequence, until it is answered or ctx ends.
func (client *Client) Report(ctx context.Context, report bridgeapi.Report) (bridgeapi.ReportResponse, error) {
	if err := report.Validate(); err != nil {
		return bridgeapi.ReportResponse{}, fmt.Errorf("%w: report %d: %w", ErrInvalidRequest, report.Sequence, err)
	}
	var response bridgeapi.ReportResponse
	err := client.post(ctx, call{
		path: bridgeapi.PathReport, request: report, bearer: true,
		requestLimit: bridgeapi.MaxReportRequestBytes, response: &response, responseLimit: maxSmallResponseBytes,
	})
	return response, err
}

// Disconnect revokes the connection's own credential.
func (client *Client) Disconnect(ctx context.Context) error {
	return client.post(ctx, call{
		path: bridgeapi.PathDisconnect, request: bridgeapi.DisconnectRequest{}, bearer: true,
		requestLimit: bridgeapi.MaxDisconnectRequestBytes, response: &bridgeapi.DisconnectResponse{}, responseLimit: maxSmallResponseBytes,
	})
}

// DownloadsManifest reads the builds and the minimum Bridge version the
// environment serves.
func (client *Client) DownloadsManifest(ctx context.Context) (bridgeapi.DownloadsManifest, error) {
	var manifest bridgeapi.DownloadsManifest
	err := client.retry(ctx, bridgeapi.PathDownloadsManifest, func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint.JoinPath(bridgeapi.PathDownloadsManifest).String(), nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		return client.send(request, &manifest, maxSmallResponseBytes)
	}, attemptTimeout)
	if err != nil {
		return manifest, err
	}
	for _, artifact := range manifest.Artifacts {
		if err := artifact.Validate(); err != nil {
			return manifest, fmt.Errorf("downloads manifest: %w", err)
		}
	}
	return manifest, nil
}

type call struct {
	path           string
	request        any
	bearer         bool
	requestLimit   int
	response       any
	responseLimit  int64
	attemptTimeout time.Duration
}

func (client *Client) post(ctx context.Context, c call) error {
	body := comm.Must(json.Marshal(c.request))
	if len(body) > c.requestLimit {
		return fmt.Errorf("%w: %s: request of %d bytes exceeds the limit of %d", ErrInvalidRequest, c.path, len(body), c.requestLimit)
	}
	timeout := c.attemptTimeout
	if timeout == 0 {
		timeout = attemptTimeout
	}
	return client.retry(ctx, c.path, func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint.JoinPath(c.path).String(), bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		if c.bearer {
			request.Header.Set("Authorization", "Bearer "+client.token)
		}
		return client.send(request, c.response, c.responseLimit)
	}, timeout)
}

// retry runs attempt until it succeeds, is refused, or ctx ends. Network
// errors and transient refusals back off from 1 s to 60 s.
func (client *Client) retry(ctx context.Context, path string, attempt func(context.Context) error, timeout time.Duration) error {
	backoff := firstBackoff
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := attempt(attemptCtx)
		cancel()
		if err == nil || ctx.Err() != nil || !retryable(err) {
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			return nil
		}
		wait := backoff
		var refusal *RefusalError
		if errors.As(err, &refusal) {
			wait = max(wait, refusal.RetryAfter)
		}
		client.logger.WarnContext(ctx, "remote agent endpoint unreachable; retrying", "path", path, "after", wait, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", path, ctx.Err())
		case <-time.After(wait):
		}
		backoff = min(2*backoff, maxBackoff)
	}
}

// retryable is a network failure, a gateway failure or a transient refusal.
func retryable(err error) bool {
	if code, ok := RefusalCode(err); ok {
		return code.Transient()
	}
	var gateway *gatewayError
	if errors.As(err, &gateway) {
		return true
	}
	var protocol *protocolError
	return !errors.As(err, &protocol)
}

// gatewayError is a 502, 503 or 504 without a coded body, from a proxy.
type gatewayError struct{ status int }

func (err *gatewayError) Error() string { return fmt.Sprintf("gateway answered HTTP %d", err.status) }

// protocolError is an answer the Bridge does not understand; it is not retried.
type protocolError struct{ err error }

func (err *protocolError) Error() string { return err.err.Error() }
func (err *protocolError) Unwrap() error { return err.err }

func (client *Client) send(request *http.Request, response any, limit int64) error {
	request.Header.Set("User-Agent", client.agent)
	answer, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("call the endpoint: %w", err)
	}
	defer func() { _ = answer.Body.Close() }()
	if answer.StatusCode == http.StatusOK {
		return decodeLimited(answer.Body, limit, response)
	}
	if answer.StatusCode >= 300 && answer.StatusCode < 400 {
		return &protocolError{err: fmt.Errorf("HTTP %d: the endpoint does not redirect", answer.StatusCode)}
	}
	var refusal bridgeapi.ErrorBody
	if err := decodeLimited(answer.Body, bridgeapi.MaxErrorBodyBytes, &refusal); err != nil {
		switch answer.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return &gatewayError{status: answer.StatusCode}
		}
		return &protocolError{err: fmt.Errorf("HTTP %d without a coded body: %w", answer.StatusCode, err)}
	}
	if refusal.Code.Status() != answer.StatusCode {
		return &protocolError{err: fmt.Errorf("HTTP %d does not match the code %s", answer.StatusCode, refusal.Code)}
	}
	return &RefusalError{Status: answer.StatusCode, Body: refusal, RetryAfter: retryAfter(answer)}
}

// retryAfter is a 429's Retry-After in its seconds form, at most
// maxRetryAfter; the date form and other statuses give zero.
func retryAfter(answer *http.Response) time.Duration {
	if answer.StatusCode != http.StatusTooManyRequests {
		return 0
	}
	seconds, err := strconv.ParseUint(strings.TrimSpace(answer.Header.Get("Retry-After")), 10, 32)
	if err != nil {
		return 0
	}
	return min(time.Duration(seconds)*time.Second, maxRetryAfter)
}

// decodeLimited strictly decodes a body of at most limit bytes: unknown
// fields and trailing data are refused.
func decodeLimited(body io.Reader, limit int64, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return fmt.Errorf("read the answer: %w", err)
	}
	if int64(len(data)) > limit {
		return &protocolError{err: fmt.Errorf("the answer exceeds %d bytes", limit)}
	}
	if err := comm.DecodeStrictJSON(data, target); err != nil {
		return &protocolError{err: fmt.Errorf("decode the answer: %w", err)}
	}
	return nil
}
