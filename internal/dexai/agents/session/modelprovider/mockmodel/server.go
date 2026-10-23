// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// apiKey is the fixed non-secret key of every mock model protocol.
// The server refuses any other, so a real key is never expected here.
const apiKey = "mock-model-not-a-secret"

// APIKeyForTest is the fixed synthetic key for external protocol fixtures.
const APIKeyForTest = apiKey

const customHeader = "Superverse-Mock-Custom"
const customDiscoveredModelID = "self-hosted-qwen"
const customManualModelID = "custom-model"

const CustomDiscoveredModelIDForTest = customDiscoveredModelID
const CustomManualModelIDForTest = customManualModelID

// maximumRequestBytes bounds one request body. A Session context stays far
// below it.
const maximumRequestBytes = 64 << 20

// Start serves the mock model on a random loopback port and returns the
// base URL for the model adapters. The owner calls stop once; it also
// runs when ctx ends. Requests are served over real HTTP, so the Claude client
// runs unchanged.
func StartForTest(ctx context.Context, logger *slog.Logger, faults ...WireFaultForTest) (baseURL string, stop func(), err error) {
	fault := WireFaultForTest{}
	if len(faults) > 1 {
		panic("mock model accepts at most one wire fault")
	}
	if len(faults) == 1 {
		fault = faults[0]
	}
	return startHandler(ctx, &handler{logger: logger, wireFault: fault}, "")
}

func startHandler(ctx context.Context, h *handler, listen string) (baseURL string, stop func(), err error) {
	address := "127.0.0.1:0"
	if listen != "" {
		address = listen
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return "", nil, fmt.Errorf("listen for the mock model: %w", err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			h.logger.ErrorContext(ctx, "the mock model stopped serving", "error", err)
		}
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			// Close ends open streams too; a Session retries its call.
			_ = server.Close()
			<-served
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-served:
		}
	}()
	return "http://" + listener.Addr().String(), stop, nil
}

// handler serves model protocols with fixed-size counters for retry fixtures.
type handler struct {
	retry     [16]atomic.Int32
	custom    bool
	logger    *slog.Logger
	probe     *CredentialProbeForTest
	wireFault WireFaultForTest
}

// StartBuiltin owns a mock listener and its exact-origin HTTP client. Hosted
// administrators may use this built-in mock without granting the endpoint pool
// a loopback exception. No caller-supplied URL enters this client.
func StartBuiltin(ctx context.Context, logger *slog.Logger, custom bool, listen string) (*modelprovider.MockClientConfig, func(), error) {
	base, stop, err := startConfigured(ctx, logger, custom, listen)
	if err != nil {
		return nil, nil, err
	}
	pool := modelendpoint.NewPool(modelendpoint.Config{AllowLocalLoopback: true})
	client, err := pool.Client(ctx, base)
	if err != nil {
		stop()
		_ = pool.Close()
		return nil, nil, err
	}
	return &modelprovider.MockClientConfig{ClaudeClientConfig: modelprovider.ClaudeClientConfig{APIKey: apiKey, BaseURL: base}, HTTPClient: client}, func() { stop(); _ = pool.Close() }, nil
}

// startConfigured is used only after local provider configuration validation.
func startConfigured(ctx context.Context, logger *slog.Logger, custom bool, listen string) (string, func(), error) {
	return startHandler(ctx, &handler{logger: logger, custom: custom}, listen)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if probeID := r.Header.Get("Superverse-Mock-Probe"); h.custom && probeID != "" {
		if len(probeID) <= 20 && strings.Trim(probeID, "0123456789") == "" {
			h.logger.InfoContext(r.Context(), "mock credential probe received", "probe_id", probeID)
			defer h.logger.InfoContext(r.Context(), "mock credential probe responded", "probe_id", probeID)
		}
	}
	if strings.HasPrefix(r.URL.Path, "/custom/") {
		if !h.custom {
			http.NotFound(w, r)
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/custom/"), "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		if h.probeRetryFixture(w, r, parts[0]) {
			return
		}
		if r.Method == http.MethodGet && parts[1] == "v1/models" {
			h.discover(w, r, parts[0])
			return
		}
		if parts[0] == "held-stream" && r.Header.Get("Authorization") == "Bearer "+apiKey {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()
			<-r.Context().Done()
			return
		}
		if parts[0] == "probe-timeout" || parts[0] == "fast-list-slow-probe" {
			// Let net/http observe client disconnects while the fixture waits.
			_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, maximumRequestBytes))
			<-r.Context().Done()
			return
		}
		for _, code := range []int{401, 403, 404, 429, 500, 503} {
			if parts[0] == fmt.Sprintf("probe-%d", code) {
				w.WriteHeader(code)
				return
			}
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/" + parts[1]
		r.Header.Set(customHeader, "true")
	}
	if r.Method == http.MethodPost && (r.URL.Path == openaicompat.ResponsesPath || r.URL.Path == openaicompat.ChatPath) {
		h.serveOpenAI(w, r)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		writeError(w, http.StatusNotFound, notFoundError, onlyStreamedMessages)
		return
	}
	accepted := r.Header.Get("X-Api-Key") == apiKey || r.Header.Get("Authorization") == "Bearer "+apiKey
	if h.probe != nil {
		accepted = h.probe.accept(r.Header.Get("X-Api-Key"))
	}
	if !accepted {
		writeError(w, http.StatusUnauthorized, authenticationError, "The mock model accepts only its own placeholder key.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maximumRequestBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, requestTooLarge, "The request body could not be read within the limit.")
		return
	}
	if h.custom && r.Header.Get(customHeader) == "true" {
		var probe struct {
			Model     string `json:"model"`
			Stream    bool   `json:"stream"`
			MaxTokens int    `json:"max_tokens"`
		}
		if json.Unmarshal(body, &probe) == nil && !probe.Stream {
			if probe.MaxTokens != 64 || (probe.Model != customDiscoveredModelID && probe.Model != customManualModelID) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"model": probe.Model, "content": []any{map[string]string{"type": "text", "text": "OK"}}})
			return
		}
	}
	var acceptedModels []string
	if h.probe != nil {
		acceptedModels = []string{conversation.ModelOpus55.ProviderModel(), conversation.ModelSonnet55.ProviderModel(), conversation.ModelHaiku45.ProviderModel()}
	}
	request, err := parseRequest(r, body, acceptedModels...)
	request.RequestBytes = len(body)
	var output struct {
		MaxTokens int64 `json:"max_tokens"`
		MaxOutput int64 `json:"max_output_tokens"`
	}
	if json.Unmarshal(body, &output) == nil {
		request.OutputBudget = max(output.MaxTokens, output.MaxOutput)
	}
	request.Custom = h.custom && r.Header.Get(customHeader) == "true"
	if (request.Model == customDiscoveredModelID || request.Model == customManualModelID) && !request.Custom {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, invalidRequestError, err.Error())
		return
	}
	if !request.Stream && (h.probe != nil || request.Custom || request.catalogModel() != "") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg_credential_probe", "type": "message", "role": "assistant", "model": request.Model, "content": []map[string]string{{"type": "text", "text": "ok"}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}})
		return
	}
	if !request.Stream {
		writeError(w, http.StatusNotFound, notFoundError, onlyStreamedMessages)
		return
	}
	if h.probe != nil && !h.probe.await(r.Context()) {
		return
	}
	answer, id, err := h.plan(r.Context(), request)
	if err != nil {
		h.logger.WarnContext(r.Context(), "the mock model rejected a request", "model", request.Model, "reason", err.Error())
		writeError(w, http.StatusBadRequest, invalidRequestError, err.Error())
		return
	}
	if answer.failure != nil {
		if answer.failure.Status == http.StatusTooManyRequests || answer.failure.Status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		writeErrorWithCode(w, answer.failure.Status, answer.failure.Type, answer.failure.Message, answer.failure.ErrorCode)
		return
	}
	// The Session stores the progress with the reply, where Studio reads which input comes next.
	if answer.progress != nil {
		w.Header().Set(modelprovider.MockProgressHeader, string(comm.Must(json.Marshal(answer.progress))))
	}
	// The client may leave mid-stream when its Step is retried or cancelled.
	if err = writeStream(r.Context(), w, id, request.Model, answer); err != nil {
		h.logger.InfoContext(r.Context(), "the mock model stopped streaming", "message", id, "error", err.Error())
	}
}

// plan follows the conversation to the scenario and reply the request stands at,
// checking on the way that its assistant messages replay what this server
// returned, and returns the reply with its message ID. A compaction request is
// answered with the mock summary, or with the error the step before it declares.
func (h *handler) plan(ctx context.Context, request messagesRequest) (reply, string, error) {
	if h.probe != nil && request.Custom {
		h.probe.observeCustom(request)
	}
	model := request.catalogModel()
	var selected selection
	var err error
	if request.Custom {
		selected, err = followCustomReplay(request.Messages)
	} else if request.WireMode == conversation.OpenAIResponses || request.WireMode == conversation.OpenAIChatCompletions {
		selected, err = followWireReplay(request.Messages, request.WireMode, request.WireProvider, request.Custom)
	} else {
		selected, err = followMessagesReplay(request.Messages, model, isCompaction(request.Messages))
	}

	if err != nil {
		return reply{}, "", err
	}
	at := selected.at
	name := "default"
	if selected.script != nil {
		name = selected.script.Name
	}
	// A subagent's script name holds a slash, which no message ID does.
	id := fmt.Sprintf("msg_mock_%s_%d_%d", strings.ReplaceAll(name, "/", "_"), at.input, at.reply)
	if step := selected.scriptStep(); step != nil && step.Remote {
		return (stepError{Status: http.StatusBadRequest, Type: invalidRequestError, Message: "This scenario expects an external remote report; a platform model request is forbidden."}).reply(), id, nil
	}
	if isCompaction(request.Messages) {
		failure := selected.compactionFailure()
		h.logger.InfoContext(ctx, "the mock model summarizes", "scenario", name, "at", at.String(), "model", request.Model, "failure", failure != nil)
		if failure != nil {
			return failure.reply(), id + "_compaction", nil
		}
		return messagesReply(textReply(selected.compactionSummary()), model), id + "_compaction", nil
	}
	if missing := selected.missingNotification(request.Messages); missing != "" {
		return reply{}, "", errors.New(missing)
	}
	if err := request.checkLongRemoteHandoff(selected); err != nil {
		return reply{}, "", err
	}
	if step := selected.scriptStep(); step != nil && step.ExpectSenderEnvelope {
		if err := request.checkSenderEnvelope(selected); err != nil {
			return reply{}, "", err
		}
	}
	if step := selected.scriptStep(); step != nil && step.ExpectPendingRequestBudget {
		if err := request.checkPendingRequestBudget(selected); err != nil {
			return reply{}, "", err
		}
	}
	if step := selected.scriptStep(); step != nil && len(step.ExpectAttachments) > 0 {
		if err := request.checkAttachments(step.ExpectAttachments); err != nil {
			return reply{}, "", err
		}
	}
	answer, err := selected.nextReply()
	if err != nil {
		return reply{}, "", err
	}
	for _, block := range answer.blocks {
		switch {
		case block.kind != conversation.BlockTypeToolUse:
		case request.allowsNoTool():
			return reply{}, "", fmt.Errorf("scenario %s at %s calls the tool %q, but the request allows no tool", name, at, block.name)
		case !request.offersTool(block.name):
			return reply{}, "", fmt.Errorf("scenario %s at %s calls the tool %q, which the request does not offer", name, at, block.name)
		}
	}
	h.logger.InfoContext(ctx, "the mock model answers", "scenario", name, "at", at.String(), "model", request.Model, "stop_reason", answer.stopReason,
		"rejected", selected.rejected != "", "failure", answer.failure != nil)
	if request.Custom && h.wireFault == WireFaultPlaintextForTest {
		answer.protocolFault = faultPlaintext
	}
	answer = messagesReply(answer, model)
	answer.progress = selected.progress()
	return answer, id, nil
}

// onlyStreamedMessages answers a request for anything but a streamed message.
const onlyStreamedMessages = "The mock model accepts streamed Messages, Responses and Chat Completions calls."

// errorBody is an Anthropic error answer.
type errorBody struct {
	Type  string `json:"type"`
	Error struct {
		Type    errorType `json:"type"`
		Message string    `json:"message"`
		Details *struct {
			ErrorCode string `json:"error_code"`
		} `json:"details,omitempty"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

// writeError answers with an Anthropic error body. A write error means the
// client is gone, which leaves nothing to report.
func writeError(w http.ResponseWriter, status int, class errorType, message string) {
	writeErrorWithCode(w, status, class, message, "")
}

func writeErrorWithCode(w http.ResponseWriter, status int, class errorType, message, errorCode string) {
	body := errorBody{Type: "error", RequestID: requestID}
	body.Error.Type, body.Error.Message = class, message
	if errorCode != "" {
		body.Error.Details = &struct {
			ErrorCode string `json:"error_code"`
		}{ErrorCode: errorCode}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("request-id", requestID)
	w.WriteHeader(status)
	_, _ = w.Write(comm.Must(json.Marshal(body)))
}

// CredentialProbeRequestIDForTest names the existing model endpoint's synthetic request identity.
const CredentialProbeRequestIDForTest = requestID

// CredentialProbeForTest is a test-only counter for synthetic credentials at the existing model endpoint.
// It never stores request headers, request bodies, history or an unbounded list.
type CredentialProbeForTest struct {
	retry                 *[16]atomic.Int32
	mu                    sync.Mutex
	key                   string
	requests              int
	firstCustomBytes      int
	firstCustomCompaction bool
	customCompactions     int
	customOutputBudget    int64
	gate                  *credentialGate
}

func (p *CredentialProbeForTest) SetKeyForTest(key string) {
	if !strings.HasPrefix(key, "synthetic-byok-") {
		panic("credential probe accepts only synthetic test keys")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key = key
}
func (p *CredentialProbeForTest) RequestsForTest() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests
}
func (p *CredentialProbeForTest) accept(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key != p.key {
		return false
	}
	p.requests++
	return true
}

type credentialGate struct {
	entered chan struct{}
	release chan struct{}
}

// HoldNextForTest pauses one actual streaming request until its owner releases it.
func (p *CredentialProbeForTest) HoldNextForTest() (<-chan struct{}, func()) {
	gate := &credentialGate{entered: make(chan struct{}), release: make(chan struct{})}
	p.mu.Lock()
	if p.gate != nil {
		p.mu.Unlock()
		panic("credential request gate already owned")
	}
	p.gate = gate
	p.mu.Unlock()
	var once sync.Once
	return gate.entered, func() { once.Do(func() { close(gate.release) }) }
}

func (p *CredentialProbeForTest) await(ctx context.Context) bool {
	p.mu.Lock()
	gate := p.gate
	p.gate = nil
	p.mu.Unlock()
	if gate == nil {
		return true
	}
	close(gate.entered)
	select {
	case <-gate.release:
		return true
	case <-ctx.Done():
		return false
	}
}

// StartCredentialProbeForTest serves the same adapter protocol and scenarios, accepting one synthetic key.
func StartCredentialProbeForTest(ctx context.Context, logger *slog.Logger, key string) (string, *CredentialProbeForTest, func(), error) {
	probe := &CredentialProbeForTest{}
	probe.SetKeyForTest(key)
	url, stop, err := startHandler(ctx, &handler{logger: logger, probe: probe}, "")
	return url, probe, stop, err
}

// serveOpenAI drives the same ordered scripts through the real compatible adapter.
func (h *handler) serveOpenAI(w http.ResponseWriter, r *http.Request) {
	accepted := r.Header.Get("Authorization") == "Bearer "+apiKey
	if h.probe != nil {
		accepted = h.probe.accept(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	if !accepted {
		writeOpenAIError(w, http.StatusUnauthorized, authenticationError, "Only the mock placeholder key is accepted.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maximumRequestBytes))
	if err != nil {
		writeOpenAIError(w, http.StatusRequestEntityTooLarge, requestTooLarge, "Request exceeds the mock limit.")
		return
	}
	var admission struct {
		Stream *bool `json:"stream"`
	}
	if json.Unmarshal(body, &admission) == nil && admission.Stream != nil && !*admission.Stream {
		model, valid := credentialAdmission(body, r.URL.Path == openaicompat.ResponsesPath, h.custom && r.Header.Get(customHeader) == "true")
		if !valid {
			writeOpenAIError(w, http.StatusBadRequest, invalidRequestError, "Invalid credential admission request.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "mock_credential_admission", "model": model})
		return
	}

	request, err := parseOpenAIRequest(body, r.URL.Path == openaicompat.ResponsesPath)
	request.RequestBytes = len(body)
	var output struct {
		MaxTokens int64 `json:"max_tokens"`
		MaxOutput int64 `json:"max_output_tokens"`
	}
	if json.Unmarshal(body, &output) == nil {
		request.OutputBudget = max(output.MaxTokens, output.MaxOutput)
	}
	request.Custom = h.custom && r.Header.Get(customHeader) == "true"
	if (request.Model == customDiscoveredModelID || request.Model == customManualModelID) && !request.Custom {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err != nil {
		h.logger.WarnContext(r.Context(), "the mock compatible request was rejected", "reason", err.Error())
		writeOpenAIError(w, http.StatusBadRequest, invalidRequestError, "Invalid compatible request.")
		return
	}
	request.WireProvider, err = mockWirePolicy(r.Header.Get(openaicompat.MockProviderHeader), request.WireMode, body, request.Custom)
	if err != nil {
		h.logger.WarnContext(r.Context(), "the mock compatible policy was rejected", "reason", err.Error())
		writeOpenAIError(w, http.StatusBadRequest, invalidRequestError, "Invalid compatible policy.")
		return
	}
	if h.probe != nil && !h.probe.await(r.Context()) {
		return
	}
	answer, id, err := h.plan(r.Context(), request)
	if err != nil {
		h.logger.WarnContext(r.Context(), "the mock compatible scenario was rejected", "reason", err.Error())
		writeOpenAIError(w, http.StatusBadRequest, invalidRequestError, err.Error())
		return
	}
	switch h.wireFault {
	case WireFaultCreditExhaustedForTest:
		writeOpenAIError(w, http.StatusTooManyRequests, rateLimitError, "Synthetic provider credit message must be discarded.", "insufficient_quota")
		return
	case WireFaultContextRejectedForTest:
		writeOpenAIError(w, http.StatusBadRequest, invalidRequestError, "Synthetic provider context message must be discarded.", "context_length_exceeded")
		return
	case WireFaultHTTP401ForTest:
		writeOpenAIError(w, http.StatusUnauthorized, authenticationError, "Synthetic model authentication refusal.")
		return
	case WireFaultHTTP429ForTest:
		w.Header().Set("Retry-After", "1")
		writeOpenAIError(w, http.StatusTooManyRequests, rateLimitError, "Synthetic model rate limit.")
		return
	case WireFaultHTTP503ForTest:
		writeOpenAIError(w, http.StatusServiceUnavailable, overloadedError, "Synthetic model overload.")
		return
	}
	if answer.failure != nil {
		writeOpenAIError(w, answer.failure.Status, answer.failure.Type, answer.failure.Message, answer.failure.ErrorCode)
		return
	}
	if answer.progress != nil {
		w.Header().Set(modelprovider.MockProgressHeader, string(comm.Must(json.Marshal(answer.progress))))
	}
	// Cancellation is owned by the real HTTP request, just as on Messages.
	if err := writeOpenAIStream(r.Context(), w, id, request.Model, answer, request.WireMode, request.WireProvider, h.wireFault, request.WireTools); err != nil {
		h.logger.InfoContext(r.Context(), "the mock compatible stream stopped", "message", id)
	}
}

// mockWirePolicy checks the real encoder's reviewed controls before the shared
// scenario runs. Unknown providers or a protocol mismatch never pick a fallback.
func mockWirePolicy(id string, mode conversation.APIStyle, body []byte, custom bool) (conversation.Provider, error) {
	var provider conversation.Provider
	for _, candidate := range []conversation.Provider{conversation.ProviderOpenAI, conversation.ProviderXAI, conversation.ProviderGemini, conversation.ProviderKimi, conversation.ProviderQwen, conversation.ProviderGLM} {
		if candidate.String() == id {
			provider = candidate
			break
		}
	}
	if provider.String() == "" || provider.APIStyle() != mode {
		return provider, errors.New("mock provider and wire mode differ")
	}
	var controls struct {
		Include  []string `json:"include"`
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
		EnableThinking   *bool `json:"enable_thinking"`
		PreserveThinking *bool `json:"preserve_thinking"`
	}
	if json.Unmarshal(body, &controls) != nil {
		return provider, errors.New("invalid mock controls")
	}
	if custom {
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil {
			return provider, errors.New("invalid custom fields")
		}
		for _, field := range []string{"reasoning", "include", "thinking", "enable_thinking", "preserve_thinking"} {
			if _, found := fields[field]; found {
				return provider, errors.New("unexpected custom reasoning control")
			}
		}
		return provider, nil
	}
	switch provider {
	case conversation.ProviderOpenAI, conversation.ProviderXAI:
		if len(controls.Include) != 1 || controls.Include[0] != "reasoning.encrypted_content" {
			return provider, errors.New("missing encrypted Responses control")
		}
	case conversation.ProviderKimi, conversation.ProviderGLM:
		if controls.Thinking == nil || controls.Thinking.Type != "disabled" {
			return provider, errors.New("thinking is not explicitly disabled")
		}
	case conversation.ProviderQwen:
		if controls.EnableThinking == nil || *controls.EnableThinking || controls.PreserveThinking == nil || *controls.PreserveThinking {
			return provider, errors.New("Qwen thinking and preservation are not disabled")
		}
	case conversation.ProviderGemini:
		if controls.Thinking != nil || controls.EnableThinking != nil || controls.PreserveThinking != nil {
			return provider, errors.New("unexpected Gemini thinking control")
		}
	}
	return provider, nil
}

// compatibleMockModel admits reviewed wire fixtures with the matching protocol.
// Protocol coverage does not make a profile selectable in the server catalog.
func compatibleMockModel(apiModel string, responses bool) bool {
	if apiModel == mockModel || apiModel == customDiscoveredModelID || apiModel == customManualModelID {
		return true
	}
	for _, model := range append(modelprovider.CompatibleModels(), conversation.ModelKimiK26, conversation.ModelQwen38Max) {
		if model.ProviderModel() == apiModel {
			return (model.Details().Provider.APIStyle() == conversation.OpenAIResponses) == responses
		}
	}
	return false
}

func parseOpenAIRequest(body []byte, responses bool) (messagesRequest, error) {
	var wire struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Store    *bool             `json:"store"`
		Previous json.RawMessage   `json:"previous_response_id"`
		Input    []json.RawMessage `json:"input"`
		Messages []json.RawMessage `json:"messages"`
		Tools    []struct {
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Choice string `json:"tool_choice"`
	}
	if json.Unmarshal(body, &wire) != nil || !compatibleMockModel(wire.Model, responses) || !wire.Stream || len(wire.Previous) > 0 {
		return messagesRequest{}, errors.New("invalid mock compatible request")
	}
	request := messagesRequest{Model: wire.Model, Stream: true, WireMode: conversation.OpenAIChatCompletions}
	var echo struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &echo); err != nil {
		return request, errors.New("invalid mock tool definitions")
	}
	request.WireTools = echo.Tools
	if responses {
		request.WireMode = conversation.OpenAIResponses
		if wire.Store == nil || *wire.Store {
			return request, errors.New("Responses must be stateless")
		}
	}
	if wire.Choice == toolChoiceNone {
		request.ToolChoice = &requestToolChoice{Type: toolChoiceNone}
	}
	for _, tool := range wire.Tools {
		name := tool.Function.Name
		if responses {
			name = tool.Name
		}
		request.Tools = append(request.Tools, requestTool{Name: name})
	}
	items := wire.Messages
	if responses {
		items = wire.Input
	}
	appendBlock := func(role string, block json.RawMessage) {
		last := len(request.Messages) - 1
		if last < 0 || request.Messages[last].Role != role {
			request.Messages = append(request.Messages, requestMessage{Role: role, Content: []json.RawMessage{block}})
		} else {
			request.Messages[last].Content = append(request.Messages[last].Content, block)
		}
	}
	for index, raw := range items {
		var item struct {
			Type       string          `json:"type"`
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			CallID     string          `json:"call_id"`
			ToolCallID string          `json:"tool_call_id"`
			Name       string          `json:"name"`
			Arguments  string          `json:"arguments"`
			Output     string          `json:"output"`
			Calls      []struct {
				Extra struct {
					Google struct {
						Signature string `json:"thought_signature"`
					} `json:"google"`
				} `json:"extra_content"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return request, errors.New("invalid wire item")
		}
		switch item.Type {
		case openaicompat.ItemReasoning.String():
			appendBlock("assistant", raw)
			continue
		case openaicompat.ItemFunctionCall.String():
			appendBlock("assistant", comm.Must(json.Marshal(map[string]any{"type": conversation.BlockTypeToolUse.String(), "id": item.CallID, "name": item.Name, "input": json.RawMessage(item.Arguments)})))
			continue
		case openaicompat.ItemFunctionOutput.String():
			var output openaicompat.ToolOutput
			if json.Unmarshal([]byte(item.Output), &output) != nil {
				return request, errors.New("invalid compatible tool output")
			}
			appendBlock("user", conversation.ToolResultBlock(conversation.CallID(item.CallID), output.Content, output.IsError))
			continue
		}
		if item.Role == "system" && index == 0 && !responses {
			continue
		}
		if item.Role == "tool" {
			var text string
			if json.Unmarshal(item.Content, &text) != nil {
				return request, errors.New("invalid tool content")
			}
			var output openaicompat.ToolOutput
			if json.Unmarshal([]byte(text), &output) != nil {
				return request, errors.New("invalid compatible tool output")
			}
			appendBlock("user", conversation.ToolResultBlock(conversation.CallID(item.ToolCallID), output.Content, output.IsError))
			continue
		}
		var parts []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			ImageURL json.RawMessage `json:"image_url"`
		}
		if string(item.Content) != "null" && json.Unmarshal(item.Content, &parts) != nil {
			return request, errors.New("invalid message content")
		}
		for _, part := range parts {
			if part.Type != "text" && part.Type != openaicompat.ItemInputText.String() && part.Type != openaicompat.ItemOutputText.String() {
				var imageURL string
				if part.Type == openaicompat.ItemInputImage.String() {
					if json.Unmarshal(part.ImageURL, &imageURL) != nil {
						return request, errors.New("invalid mock image URL")
					}
				} else if part.Type == openaicompat.ItemImageURL.String() {
					var image struct {
						URL string `json:"url"`
					}
					if json.Unmarshal(part.ImageURL, &image) != nil {
						return request, errors.New("invalid mock image URL")
					}
					imageURL = image.URL
				} else {
					return request, errors.New("unsupported mock content")
				}
				media, data, found := strings.Cut(strings.TrimPrefix(imageURL, "data:"), ";base64,")
				if !strings.HasPrefix(imageURL, "data:") || !found {
					return request, errors.New("mock images require inline data")
				}
				source := comm.Must(json.Marshal(map[string]string{"type": "base64", "media_type": media, "data": data}))
				if !validImageSource(source) {
					return request, errors.New("invalid mock inline image")
				}
				appendBlock(item.Role, comm.Must(json.Marshal(map[string]any{"type": conversation.BlockTypeImage.String(), "source": source})))
				continue
			}
			appendBlock(item.Role, comm.Must(json.Marshal(map[string]string{"type": conversation.BlockTypeText.String(), "text": part.Text})))
		}
		for _, call := range item.Calls {
			if call.Extra.Google.Signature != "" {
				appendBlock("assistant", comm.Must(json.Marshal(map[string]string{"type": "mock_signature", "callID": call.ID, "signature": call.Extra.Google.Signature})))
			}
			appendBlock("assistant", comm.Must(json.Marshal(map[string]any{"type": conversation.BlockTypeToolUse.String(), "id": call.ID, "name": call.Function.Name, "input": json.RawMessage(call.Function.Arguments)})))
		}
	}
	if len(request.Messages) == 0 {
		return request, errors.New("empty mock messages")
	}
	return request, nil
}

// writeOpenAIError preserves the shared engine's failure in compatible error shape.
func writeOpenAIError(w http.ResponseWriter, status int, kind errorType, message string, codes ...string) {
	code := ""
	if len(codes) > 0 {
		code = codes[0]
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-request-id", requestID)
	w.WriteHeader(status)
	_, _ = w.Write(comm.Must(json.Marshal(map[string]any{"error": map[string]any{"type": kind, "message": message, "param": nil, "code": code}})))
}

// credentialAdmission verifies the exact bounded inference probe, including privacy controls.
func credentialAdmission(body []byte, responses bool, custom bool) (string, bool) {
	var request struct {
		Model     string   `json:"model"`
		Stream    *bool    `json:"stream"`
		MaxOutput int      `json:"max_output_tokens"`
		MaxTokens int      `json:"max_tokens"`
		Store     *bool    `json:"store"`
		Include   []string `json:"include"`
		Input     string   `json:"input"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !compatibleMockModel(request.Model, responses) || request.Stream == nil || *request.Stream {
		return "", false
	}
	if responses {
		if custom {
			return request.Model, request.MaxOutput == 64 && request.MaxTokens == 0 && request.Store != nil && !*request.Store && len(request.Include) == 0 && request.Input == "Reply OK."
		}
		return request.Model, request.MaxOutput == 64 && request.MaxTokens == 0 && request.Store != nil && !*request.Store && len(request.Include) == 1 && request.Include[0] == "reasoning.encrypted_content" && request.Input == "Reply OK." && len(request.Messages) == 0
	}
	return request.Model, request.MaxTokens == 64 && request.MaxOutput == 0 && request.Store == nil && len(request.Include) == 0 && request.Input == "" && len(request.Messages) == 1 && request.Messages[0].Role == "user" && request.Messages[0].Content == "Reply OK."
}

// discover serves deterministic model-only fixtures on the model listener.
func (h *handler) discover(w http.ResponseWriter, r *http.Request, fixture string) {
	accepted := r.Header.Get("Authorization") == "Bearer "+apiKey
	if h.probe != nil {
		accepted = h.probe.accept(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	if !accepted {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ids := []string{customDiscoveredModelID}
	switch fixture {
	case "empty":
		ids = nil
	case "many":
		ids = []string{customDiscoveredModelID, customManualModelID}
	case "models-401", "models-403", "models-429", "models-501":
		code, _ := strconv.Atoi(strings.TrimPrefix(fixture, "models-"))
		w.WriteHeader(code)
		return
	case "models-405":
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case "fast-list-slow-probe":
	case "unsupported":
		w.WriteHeader(http.StatusNotFound)
		return
	case "count", "max-count":
		count := 101
		if fixture == "max-count" {
			count = 100
		}
		ids = make([]string, count)
		for i := range ids {
			ids[i] = fmt.Sprintf("model-%d", i)
		}
	case "max-id":
		ids = []string{strings.Repeat("x", 256)}
	case "key":
		ids = []string{apiKey}
	case "pagination":
	case "id":
		ids = []string{strings.Repeat("x", 257)}
	case "bytes":
		ids = []string{strings.Repeat("x", (1<<20)+1)}
	case "timeout":
		<-r.Context().Done()
		return
	case "context":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"id": customDiscoveredModelID, "context_length": 131072},
			map[string]any{"id": customManualModelID, "context_window": 262144},
			map[string]any{"id": "invalid-hint", "context_length": "999999999999999999999"},
			map[string]any{"id": "conflict-hint", "context_length": 131072, "max_model_len": 65536},
			map[string]any{"id": "duplicate-hint", "context_length": 131072},
			map[string]any{"id": "duplicate-hint", "context_length": 65536},
		}})
		return
	case "one":
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	data := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]any{"id": id})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "has_more": fixture == "pagination"})
}

// StartCustomEndpointProbeForTest exposes only model fixture counters, never credentials.
func StartCustomEndpointProbeForTest(ctx context.Context, logger *slog.Logger, faults ...WireFaultForTest) (string, *CredentialProbeForTest, func(), error) {
	fault := WireFaultForTest{}
	if len(faults) > 1 {
		panic("custom endpoint accepts at most one fixture fault")
	}
	if len(faults) == 1 {
		fault = faults[0]
	}
	probe := &CredentialProbeForTest{key: apiKey}
	h := &handler{logger: logger, probe: probe, custom: true, wireFault: fault}
	probe.retry = &h.retry
	base, stop, err := startHandler(ctx, h, "")
	return base, probe, stop, err
}

func (p *CredentialProbeForTest) observeCustom(request messagesRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	compact := isCompaction(request.Messages)
	p.customOutputBudget = request.OutputBudget
	if p.firstCustomBytes == 0 {
		p.firstCustomBytes, p.firstCustomCompaction = request.RequestBytes, compact
	}
	if compact {
		p.customCompactions++
	}
}

// CustomOutputBudgetForTest reports the most recent custom inference output limit.
func (p *CredentialProbeForTest) CustomOutputBudgetForTest() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.customOutputBudget
}

// CustomBudgetForTest returns bounded numeric wire measurements without payloads.
func (p *CredentialProbeForTest) CustomBudgetForTest() (bytes int, firstWasCompaction bool, compactions int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.firstCustomBytes, p.firstCustomCompaction, p.customCompactions
}
