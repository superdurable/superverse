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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ClaudeClientConfig is the process configuration of the Claude adapter.
type ClaudeClientConfig struct {
	// APIKey is an immutable process credential; it never enters a Flow
	// payload, an activity event or a log field.
	APIKey string
	// BaseURL replaces the default Claude API origin; empty means the default.
	// Only the mock test model sets it.
	BaseURL string
}

// claudeClient implements the Flow's ModelClient over the Claude Messages API.
type claudeClient struct {
	client anthropic.Client
	logger *slog.Logger
	// readsMockProgress takes the mock model's progress from each reply's
	// response; only the router's mock client sets it.
	readsMockProgress bool
}

// newClaudeClient constructs the adapter over an explicitly owned HTTP
// client. A request uses exactly the configured key: the SDK reads no
// credential or base URL from the environment and never retries, because Dex
// retries the Step.
func newClaudeClient(httpClient *http.Client, config ClaudeClientConfig, logger *slog.Logger) (*claudeClient, error) {
	for _, character := range config.APIKey {
		if character < 0x20 || character > 0x7e {
			return nil, errors.New("the Claude API key must contain only printable ASCII characters")
		}
	}
	options := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(config.APIKey),
		option.WithHTTPClient(httpClient), option.WithMaxRetries(0)}
	if config.BaseURL != "" {
		options = append(options, option.WithBaseURL(config.BaseURL))
	}
	return &claudeClient{client: anthropic.NewClient(options...), logger: logger}, nil
}

// NewProviderHTTPClient is the model provider transport. Redirects are refused
// so a credential never reaches another origin. It sets no overall timeout: a
// streamed reply can run for many minutes, and the Step's context bounds it.
func NewProviderHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("model provider redirects are disabled")
		},
	}
}

// Complete streams one reply of the request's model.
func (client *claudeClient) Complete(ctx context.Context, request conversation.ModelRequest) (conversation.ModelReply, error) {
	return client.complete(ctx, messageParams(ProfileOf(request.Model), request), request)
}

// complete streams the reply of params, built from request.
func (client *claudeClient) complete(ctx context.Context, params anthropic.BetaMessageNewParams, request conversation.ModelRequest) (conversation.ModelReply, error) {
	startedAt := time.Now()
	var response *http.Response
	stream := client.client.Beta.Messages.NewStreaming(ctx, params, option.WithResponseInto(&response))
	defer func() { _ = stream.Close() }()
	reply, err := readStream(stream, request, startedAt)
	if err == nil && client.readsMockProgress {
		reply.MockProgress = mockProgress(response)
	}
	if err != nil {
		err = classifyError(err)
		if rejected, isRejected := errors.AsType[*conversation.ModelRejectedError](err); isRejected {
			client.logger.LogAttrs(ctx, slog.LevelWarn, "Claude rejected the request", slog.String("call_id", string(request.CallID)),
				slog.String("model", request.Model.ProviderModel()), slog.String("code", rejected.Code), slog.Int("status", rejected.Status), slog.String("detail", rejected.Detail))
		}
		return conversation.ModelReply{}, err
	}
	if transformations := reply.Metrics.InputTransformations; len(transformations) > 0 {
		// Positions and reasons only, never content.
		client.logger.LogAttrs(ctx, slog.LevelInfo, "Claude transformed the request input", slog.String("call_id", string(request.CallID)),
			slog.Int("dropped_thinking_blocks", reply.Metrics.DroppedThinkingBlocks), slog.Any("input_transformations", transformations))
	}
	return reply, nil
}

// The beta headers a request can carry. RequestBetas lists them all, so the
// mock test model accepts exactly what this client sends.
const (
	betaThinkingBinding = anthropic.AnthropicBetaThinkingBindingControls2026_08_01
	betaMidConversation = anthropic.AnthropicBetaMidConversationOutputConfig2026_07_01
	betaServerFallback  = anthropic.AnthropicBetaServerSideFallback2026_07_01
)

// RequestBetas is every beta header value a request can carry.
var RequestBetas = []string{betaThinkingBinding, betaMidConversation, betaServerFallback}

// oneHourCache is the breakpoint every cache marker uses.
var oneHourCache = anthropic.BetaCacheControlEphemeralParam{TTL: anthropic.BetaCacheControlEphemeralTTLTTL1h}

// messageParams builds the request: the frozen tools and system prompt, the
// assembled messages, the profile's thinking, effort and fallback settings,
// and three one-hour cache breakpoints (system, the previous request's last
// user block and the top-level automatic one).
func messageParams(profile ModelProfile, request conversation.ModelRequest) anthropic.BetaMessageNewParams {
	params := anthropic.BetaMessageNewParams{
		Model:        request.Model.ProviderModel(),
		MaxTokens:    profile.maxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: request.Prompts.System, CacheControl: oneHourCache}},
		Messages:     make([]anthropic.BetaMessageParam, 0, len(request.Messages)),
		CacheControl: oneHourCache,
		ToolChoice:   anthropic.BetaToolChoiceUnionParam{OfAuto: &anthropic.BetaToolChoiceAutoParam{}},
	}
	if request.ToolChoiceNone {
		params.ToolChoice = anthropic.BetaToolChoiceUnionParam{OfNone: &anthropic.BetaToolChoiceNoneParam{}}
	}
	for _, tool := range request.Prompts.Tools {
		params.Tools = append(params.Tools, param.Override[anthropic.BetaToolUnionParam](tool))
	}
	for _, message := range request.Messages {
		params.Messages = append(params.Messages, messageParam(message))
	}
	if profile.adaptiveThinking {
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{
			Display:      anthropic.BetaThinkingConfigAdaptiveDisplaySummarized,
			BlockBinding: anthropic.BetaThinkingBlockBindingParam{PrefixMismatchBehavior: anthropic.BetaThinkingPrefixMismatchBehavior(request.PrefixMismatch)},
		}}
		params.Betas = append(params.Betas, betaThinkingBinding)
	} else {
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfEnabled: &anthropic.BetaThinkingConfigEnabledParam{BudgetTokens: profile.thinkingBudget}}
	}
	if profile.Effort && request.Effort != "" {
		params.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(request.Effort)}
		if conversation.HasEffort(request.Messages) {
			params.Betas = append(params.Betas, betaMidConversation)
		}
	}
	if profile.fallbacks {
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
		params.Betas = append(params.Betas, betaServerFallback)
	}
	return params
}

// messageParam replays the stored blocks of one message byte for byte; the
// anchored block gains the explicit cache breakpoint.
func messageParam(message conversation.RequestMessage) anthropic.BetaMessageParam {
	blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(message.Blocks))
	for index, block := range message.Blocks {
		if index == message.CacheAnchor {
			block = withCacheControl(block)
		}
		blocks = append(blocks, param.Override[anthropic.BetaContentBlockParamUnion](block))
	}
	switch message.Role {
	case conversation.EntryRoleUser:
		return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleUser, Content: blocks}
	case conversation.EntryRoleAssistant:
		return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: blocks}
	default:
		var output anthropic.BetaSystemMessageOutputConfigParam
		if message.Effort != "" {
			output.Effort = anthropic.BetaSystemMessageOutputConfigEffort(message.Effort)
		}
		return anthropic.NewBetaSystemMessage(output, blocks...)
	}
}

// withCacheControl adds the one-hour cache breakpoint to a user block.
func withCacheControl(block json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	comm.MustDo(json.Unmarshal(block, &fields))
	fields["cache_control"] = json.RawMessage(`{"type":"ephemeral","ttl":"1h"}`)
	return comm.Must(json.Marshal(fields))
}

// maximumRejectionDetailBytes bounds the API's explanation of a rejection.
const maximumRejectionDetailBytes = 1 << 10

// classifyError keeps a retryable failure an error for Dex to retry and turns
// a rejection a retry cannot change into a ModelRejectedError. The error type is read first: an error event in the
// middle of a stream has HTTP status 200.
func classifyError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("claude API transport: %w", err)
	}
	status, errorType := apiErr.StatusCode, apiErr.Type()
	providerMessage, providerCode := providerErrorFields(apiErr)
	// The enforced monthly cap has a structured code even though its outer
	// error type is the same rate_limit_error used for temporary throttling.
	if errorType == shared.ErrorTypeRateLimitError && providerCode == "enforced_spend_limit_reached" {
		return conversation.ProviderSpendLimitReached.Rejected(status)
	}
	// Anthropic exposes exhausted prepaid credit as invalid_request_error,
	// shared with unrelated request errors. Its documented message is the only
	// discriminator; never retain the provider's response text.
	if errorType == shared.ErrorTypeInvalidRequestError && status == http.StatusBadRequest &&
		strings.HasPrefix(providerMessage, "Your credit balance is too low to access the Anthropic API.") {
		return conversation.ProviderCreditExhausted.Rejected(status)
	}
	if errorType == shared.ErrorTypeInvalidRequestError && status == http.StatusBadRequest &&
		(strings.HasPrefix(providerMessage, "You have reached your specified API usage limits") ||
			strings.HasPrefix(providerMessage, "You have reached your specified workspace API usage limits")) {
		return conversation.ProviderSpendLimitReached.Rejected(status)
	}
	if errorType == shared.ErrorTypeBillingError {
		return conversation.ProviderBillingProblem.Rejected(status)
	}
	switch errorType {
	case shared.ErrorTypeRateLimitError, shared.ErrorTypeOverloadedError, shared.ErrorTypeAPIError, shared.ErrorTypeTimeoutError:
		return fmt.Errorf("claude API %s (HTTP %d, request %s)", errorType, status, apiErr.RequestID)
	case shared.ErrorTypeInvalidRequestError, shared.ErrorTypeAuthenticationError, shared.ErrorTypePermissionError,
		shared.ErrorTypeNotFoundError:
		return &conversation.ModelRejectedError{Code: string(errorType), Status: status, Detail: rejectionDetail(apiErr)}
	}
	if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
		return &conversation.ModelRejectedError{Code: fmt.Sprintf("http_%d", status), Status: status, Detail: rejectionDetail(apiErr)}
	}
	return fmt.Errorf("claude API error %q (HTTP %d, request %s)", errorType, status, apiErr.RequestID)
}

// rejectionDetail stays in server diagnostics; publicModelRejection removes it
// before a model rejection enters Flow state.
func rejectionDetail(apiErr *anthropic.Error) string {
	message, _ := providerErrorFields(apiErr)
	return comm.TruncateUTF8(strings.TrimSpace(message+" (request "+apiErr.RequestID+")"), maximumRejectionDetailBytes)
}

// providerErrorFields reads the fields needed for narrow budget classifications
// and bounded rejection diagnostics. Budget classifications discard its text;
// other rejections use it only in server diagnostics, never Flow payloads.
func providerErrorFields(apiErr *anthropic.Error) (string, string) {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Details struct {
				ErrorCode string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(apiErr.RawJSON()), &body)
	return strings.TrimSpace(body.Error.Message), body.Error.Details.ErrorCode
}
