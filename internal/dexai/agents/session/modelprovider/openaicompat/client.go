// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Policy is the catalog-owned request policy, frozen by the router for a call.
// No credentials or provider-selected conversation state belong here.
type Policy struct {
	Provider               conversation.Provider
	WireMode               conversation.APIStyle
	MaxTokens              int64
	Images, Strict, Effort bool
	Replay                 conversation.ReasoningReplayMode
	ChatThinking           conversation.ChatThinkingMode
}

// Client owns no HTTP pool or durable state; its owner closes the shared transport.
// The only credential admitted before BYOK is the mock model's placeholder.
type Client struct {
	http    *http.Client
	baseURL string
	policy  Policy
	logger  *slog.Logger
}

// NewMock connects the real adapter to the existing loopback scenario engine.
// Real providers stay unavailable until the separately reviewed BYOK boundary.
func NewMock(client *http.Client, baseURL string, policy Policy, logger *slog.Logger) (*Client, error) {
	if client == nil || policy.MaxTokens <= 0 {
		return nil, errors.New("the adapter requires an owned HTTP client and a positive output budget")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("the mock provider endpoint must be loopback HTTP")
	}
	if policy.WireMode != conversation.OpenAIResponses && policy.WireMode != conversation.OpenAIChatCompletions {
		return nil, errors.New("the adapter requires an explicit OpenAI wire mode")
	}
	if policy.Provider.APIStyle() != policy.WireMode {
		return nil, errors.New("the mock policy must use the catalog wire mode")
	}
	return &Client{http: client, baseURL: strings.TrimRight(baseURL, "/"), policy: policy, logger: logger}, nil
}

// At most 32 compatible model calls run in a replica; admission queues no goroutines.
const maximumConcurrentCalls = 32

var compatibleCalls = make(chan struct{}, maximumConcurrentCalls)
var errModelCapacity = errors.New("compatible model call capacity is exhausted")

// Complete performs one bounded streamed request, with no automatic retry.
func (client *Client) Complete(ctx context.Context, request conversation.ModelRequest) (reply conversation.ModelReply, err error) {
	defer func() {
		if rejection, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
			client.logger.LogAttrs(ctx, slog.LevelWarn, "Model provider rejected the request", slog.String("call_id", string(request.CallID)),
				slog.String("model", request.Model.ProviderModel()), slog.String("code", rejection.Code), slog.Int("status", rejection.Status), slog.String("detail", rejection.Detail))
		}
	}()
	if !request.Model.IsMock() {
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
	started := time.Now()
	body, err := encodeRequest(request, client.policy)
	if err != nil {
		return conversation.ModelReply{}, err
	}
	path := ChatPath
	if client.policy.WireMode == conversation.OpenAIResponses {
		path = ResponsesPath
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return conversation.ModelReply{}, errors.New("construct model request")
	}
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Accept", "text/event-stream")
	call.Header.Set("Authorization", "Bearer mock-model-not-a-secret")
	call.Header.Set(MockProviderHeader, client.policy.Provider.String())
	var headers http.Header
	reply, headers, err = client.streamRequest(ctx, call, request, started)
	if err != nil {
		return conversation.ModelReply{}, err
	}
	for _, call := range reply.ToolCalls {
		toolInputProgress(request, string(call.ID), string(call.Name), call.Arguments.String(), true)
		if request.WriteActivity != nil {
			target := ""
			if request.ToolTarget != nil {
				target = request.ToolTarget(call)
			}
			if err := request.WriteActivity(conversation.AgentEvent{Kind: conversation.EventKindModelToolCall, Message: "Model requested " + string(call.Name) + ".", CallID: &call.ID, ToolName: &call.Name, Target: target}); err != nil {
				return conversation.ModelReply{}, err
			}
		}
	}
	reply.Metrics.Model = request.Model.ProviderModel()
	reply.Metrics.Effort = requestEffort(request, client.policy)
	reply.Metrics.DurationMS = time.Since(started).Milliseconds()
	if value := headers.Get("Superverse-Mock-Progress"); value != "" {
		var progress conversation.MockProgress
		if json.Unmarshal([]byte(value), &progress) != nil {
			return conversation.ModelReply{}, rejected(failureInvalidMockProgress)
		}
		reply.MockProgress = &progress
	}
	return reply, nil
}

// streamRequest owns admission only while the HTTP request and stream are active.
func (client *Client) streamRequest(ctx context.Context, call *http.Request, request conversation.ModelRequest, started time.Time) (conversation.ModelReply, http.Header, error) {
	select {
	case compatibleCalls <- struct{}{}:
		defer func() { <-compatibleCalls }()
	default:
		return conversation.ModelReply{}, nil, errModelCapacity
	}
	response, err := client.http.Do(call)
	if err != nil {
		if ctx.Err() != nil {
			return conversation.ModelReply{}, nil, ctx.Err()
		}
		// net/url errors can carry an endpoint. Keep request bodies and credentials out.
		return conversation.ModelReply{}, nil, &transportError{cause: err}
	}
	if response.StatusCode != http.StatusOK {
		failure := httpFailure(response)
		_ = response.Body.Close()
		if retry, ok := failure.(*retryableError); ok && retry.retryAfter > 0 {
			return conversation.ModelReply{}, nil, dex.RetryAfter(retry.retryAfter, failure)
		}
		return conversation.ModelReply{}, nil, failure
	}
	defer response.Body.Close()
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return conversation.ModelReply{}, nil, rejected(failureInvalidStream)
	}
	reply, err := readStream(ctx, response.Body, request, client.policy, started)
	return reply, response.Header, err
}

func rejected(code failureCode) error {
	return &conversation.ModelRejectedError{Code: string(code), Hint: "The model returned an unsupported protocol response."}
}

// httpFailure reads only a bounded body and retains allowlisted code/type, never message text.
// Dex owns retry delays and the total deadline; no HTTP slot sleeps for backoff.
func httpFailure(response *http.Response) error {
	status := response.StatusCode

	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return &transportError{cause: err}
	}
	failure := providerFailure(body, status)
	if _, rejected := failure.(*conversation.ModelRejectedError); rejected {
		return failure
	}
	retryAfter := time.Duration(0)
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		retryAfter = time.Duration(min(seconds, 60)) * time.Second
	} else if when, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
		retryAfter = min(max(time.Until(when), time.Duration(0)), time.Minute)
	}
	retry := failure.(*retryableError)
	retry.retryAfter = retryAfter
	return retry
}

// retryableError carries bounded provider retry metadata without response text.
// The existing Dex Step retry policy and total deadline remain authoritative.
type retryableError struct {
	status     int
	retryAfter time.Duration
	code       string
}

func (failure *retryableError) Error() string {
	return fmt.Sprintf("model provider HTTP %d (code %s)", failure.status, failure.code)
}

// transportError retains the typed cause for classification without formatting its URL.
type transportError struct{ cause error }

func (failure *transportError) Error() string {
	cause := failure.cause
	if endpoint, ok := errors.AsType[*url.Error](cause); ok {
		cause = endpoint.Err
	}
	return fmt.Sprintf("model provider transport failed: %s", cause)
}
func (failure *transportError) Unwrap() error { return failure.cause }
