// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// maximumMessagesCalls bounds in-flight compatible calls per shared router.
const maximumMessagesCalls = 64

var errMessagesCapacity = errors.New("compatible model call capacity is exhausted")

// messagesClient encodes reviewed Messages-compatible profiles. It owns no
// credentials: the caller supplies a key for this request after admission.
type messagesClient struct {
	httpClient *http.Client
	slots      chan struct{}
	logger     *slog.Logger
}

func newMessagesClient(httpClient *http.Client, logger *slog.Logger) *messagesClient {
	return &messagesClient{httpClient: httpClient, logger: logger, slots: make(chan struct{}, maximumMessagesCalls)}
}

func (client *messagesClient) complete(ctx context.Context, request conversation.ModelRequest, key, baseURL string, mock bool) (reply conversation.ModelReply, err error) {
	defer func() {
		if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
			client.logger.LogAttrs(ctx, slog.LevelWarn, "Model provider rejected the request",
				slog.String("call_id", string(request.CallID)), slog.String("provider", request.Model.Details().Provider.String()),
				slog.String("model", request.Model.ProviderModel()), slog.String("code", rejected.Code), slog.Int("status", rejected.Status))
		}
	}()
	profile := ProfileOf(request.Model)
	if request.Model.Details().Provider.APIStyle() != conversation.AnthropicMessages || profile.Capabilities.Replay == conversation.ReplayPlaintextRequired || profile.Capabilities.Replay == conversation.ReplayClaudeSigned {
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
	if key == "" {
		return conversation.ModelReply{}, conversation.NoProviderKey.Rejected()
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return conversation.ModelReply{}, conversation.InvalidProviderKey.Rejected()
		}
	}
	select {
	case client.slots <- struct{}{}:
		defer func() { <-client.slots }()
	default:
		return conversation.ModelReply{}, errMessagesCapacity
	}
	params, err := compatibleMessageParams(profile, request)
	if err != nil {
		return conversation.ModelReply{}, err
	}
	options := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithHTTPClient(client.httpClient), option.WithMaxRetries(0), option.WithBaseURL(baseURL)}
	// Meta documents bearer auth; DeepSeek and MiniMax support SDK API-key auth.
	if request.Model.Details().Provider == conversation.ProviderMeta {
		options = append(options, option.WithAuthToken(key))
	} else {
		options = append(options, option.WithAPIKey(key))
	}
	if request.Model.IsCustom() {
		root, err := url.Parse(baseURL)
		if err != nil {
			return conversation.ModelReply{}, errors.New("invalid model endpoint")
		}
		options = append(options, option.WithMiddleware(func(call *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			call.URL.Path = strings.TrimRight(root.Path, "/") + "/messages"
			call.URL.RawPath, call.URL.RawQuery = "", ""
			return next(call)
		}))
	}
	sdk := anthropic.NewClient(options...)
	var response *http.Response
	stream := sdk.Beta.Messages.NewStreaming(ctx, params, option.WithResponseInto(&response))
	defer func() { _ = stream.Close() }()
	reply, err = readCompatibleStream(stream, request, profile, time.Now())
	if err != nil {
		return conversation.ModelReply{}, classifyMessagesError(err)
	}
	if mock {
		reply.MockProgress = mockProgress(response)
	}
	return reply, nil
}

// compatibleMessageParams deliberately starts with only common wire fields.
// Frozen Claude tool schemas retain their JSON schema, without Claude flags.
func compatibleMessageParams(profile ModelProfile, request conversation.ModelRequest) (anthropic.BetaMessageNewParams, error) {
	body := map[string]any{
		"model": request.APIModelID(), "max_tokens": profile.maxTokens,
		"system":   []any{map[string]any{"type": conversation.BlockTypeText.String(), "text": request.Prompts.System}},
		"messages": []any{}, "tools": []any{}, "tool_choice": map[string]any{"type": "auto"},
	}
	if request.Model.IsCustom() {
		body["tool_choice"] = map[string]any{"type": "auto", "disable_parallel_tool_use": true}
	}
	if request.ToolChoiceNone {
		body["tool_choice"] = map[string]any{"type": "none"}
	}
	switch profile.Capabilities.messagesThinking {
	case (protocolMode{}):
		if !request.Model.IsCustom() {
			return anthropic.BetaMessageNewParams{}, conversation.ModelUnavailable.Rejected()
		}
	case thinkingDisabled:
		body["thinking"] = map[string]any{"type": "disabled"}
	case thinkingAdaptive:
		body["thinking"] = map[string]any{"type": "adaptive", "display": "omitted"}
	default:
		return anthropic.BetaMessageNewParams{}, conversation.ModelUnavailable.Rejected()
	}
	for _, tool := range request.Prompts.Tools {
		var fields map[string]json.RawMessage
		comm.MustDo(json.Unmarshal(tool, &fields))
		// Only the three documented custom-tool fields are sent. Strict and
		// eager streaming are Claude policy, not implicit compatible features.
		kept := map[string]json.RawMessage{}
		for _, field := range []string{"name", "description", "input_schema"} {
			if value, exists := fields[field]; exists {
				kept[field] = value
			}
		}
		body["tools"] = append(body["tools"].([]any), kept)
	}
	for _, message := range request.Messages {
		if message.Role != conversation.EntryRoleUser && message.Role != conversation.EntryRoleAssistant {
			return anthropic.BetaMessageNewParams{}, conversation.ProviderProtocolRejected.Rejected(0)
		}
		blocks := []json.RawMessage{}
		seenOpaque := map[string]bool{}
		opaqueBytes := 0
		for position, block := range message.Blocks {
			if conversation.IsOpaqueReplay(block) {
				if profile.Capabilities.Replay != conversation.ReplayEncryptedOpaque {
					return anthropic.BetaMessageNewParams{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				replay, err := conversation.ReadOpaqueReplay(block, request.Model.Details().Provider, conversation.AnthropicMessages, position)
				opaqueBytes += len(block)
				if err != nil || seenOpaque[replay.Ciphertext] || opaqueBytes > conversation.MaximumOpaqueReplayBytes {
					return anthropic.BetaMessageNewParams{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				seenOpaque[replay.Ciphertext] = true
				blocks = append(blocks, comm.Must(json.Marshal(map[string]string{"type": conversation.BlockTypeRedactedThinking.String(), "data": replay.Ciphertext})))
				continue
			}
			var fields map[string]json.RawMessage
			comm.MustDo(json.Unmarshal(block, &fields))
			var kind string
			comm.MustDo(json.Unmarshal(fields["type"], &kind))
			switch kind {
			case conversation.BlockTypeText.String(), conversation.BlockTypeToolUse.String(), conversation.BlockTypeToolResult.String(), conversation.BlockTypeImage.String():
			default:
				return anthropic.BetaMessageNewParams{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			delete(fields, "cache_control")
			blocks = append(blocks, comm.Must(json.Marshal(fields)))
		}
		body["messages"] = append(body["messages"].([]any), map[string]any{"role": message.Role, "content": blocks})
	}
	return param.Override[anthropic.BetaMessageNewParams](body), nil
}

// classifyMessagesError never includes an SDK error body, URL or request ID:
// compatible providers may echo prompts or credentials in any of those fields.
func classifyMessagesError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		return err
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		if status == http.StatusPaymentRequired {
			return conversation.ProviderCreditExhausted.Rejected(status)
		}
		if apiErr.Type() == shared.ErrorTypeBillingError {
			return conversation.ProviderBillingProblem.Rejected(status)
		}
		if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
			return conversation.ProviderRequestRejected.Rejected(status)
		}
		if status == http.StatusOK {
			switch apiErr.Type() {
			case shared.ErrorTypeAuthenticationError, shared.ErrorTypePermissionError, shared.ErrorTypeInvalidRequestError, shared.ErrorTypeNotFoundError:
				return conversation.ProviderRequestRejected.Rejected(status)
			}
		}
		cause := fmt.Errorf("model provider request failed (HTTP %d)", status)
		if (status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable) && apiErr.Response != nil {
			header := apiErr.Response.Header.Get("Retry-After")
			var delay time.Duration
			if seconds, err := strconv.ParseInt(header, 10, 64); err == nil && seconds > 0 {
				delay = time.Duration(min(seconds, int64(time.Duration(1<<63-1)/time.Second))) * time.Second
			} else if when, err := http.ParseTime(header); err == nil {
				delay = time.Until(when)
			}
			if delay > 0 {
				return dex.RetryAfter(max(30*time.Second, delay), cause)
			}
		}
		return cause
	}
	// No wrapped transport error: net/url errors can contain secret URL data.
	return errors.New("model provider stream or transport failed")
}

// validateCredential shares the bounded inference admission, thinking policy and provider authentication.
func (client *messagesClient) validateCredential(ctx context.Context, key string, model conversation.Model, base string) error {
	select {
	case client.slots <- struct{}{}:
		defer func() { <-client.slots }()
	default:
		return errMessagesCapacity
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	params, err := compatibleMessageParams(ProfileOf(model), conversation.ModelRequest{Model: model,
		Messages: []conversation.RequestMessage{{Role: conversation.EntryRoleUser, Blocks: []json.RawMessage{comm.Must(json.Marshal(map[string]string{"type": "text", "text": "Reply OK."}))}}}})
	if err != nil {
		return err
	}
	options := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithHTTPClient(client.httpClient), option.WithMaxRetries(0), option.WithBaseURL(base), option.WithJSONSet("max_tokens", 64)}
	if model.Details().Provider == conversation.ProviderMeta {
		options = append(options, option.WithAuthToken(key))
	} else {
		options = append(options, option.WithAPIKey(key))
	}
	sdk := anthropic.NewClient(options...)
	_, err = sdk.Beta.Messages.New(ctx, params)
	if err != nil {
		return classifyMessagesError(err)
	}
	return nil
}
