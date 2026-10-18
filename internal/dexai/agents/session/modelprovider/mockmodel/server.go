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
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
)

// APIKey is the fixed, non-secret key the Claude client sends to the mock
// model; the server refuses any other, so a real key is never expected here.
const APIKey = "mock-model-not-a-secret"

// maximumRequestBytes bounds one request body. A Session context stays far
// below it.
const maximumRequestBytes = 64 << 20

// Start serves the mock model on a random loopback port and returns the
// base URL for ClaudeClientConfig.BaseURL. The owner calls stop once; it also
// runs when ctx ends. Requests are served over real HTTP, so the Claude client
// runs unchanged.
func Start(ctx context.Context, logger *slog.Logger) (baseURL string, stop func(), err error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("listen for the mock model: %w", err)
	}
	server := &http.Server{Handler: &handler{logger: logger}, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "the mock model stopped serving", "error", err)
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

// handler implements POST /v1/messages with stream: true. It keeps no state:
// the reply depends only on the request.
type handler struct{ logger *slog.Logger }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		writeError(w, http.StatusNotFound, notFoundError, onlyStreamedMessages)
		return
	}
	if r.Header.Get("X-Api-Key") != APIKey {
		writeError(w, http.StatusUnauthorized, authenticationError, "The mock model accepts only its own placeholder key.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maximumRequestBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, requestTooLarge, "The request body could not be read within the limit.")
		return
	}
	request, err := parseRequest(r, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, invalidRequestError, err.Error())
		return
	}
	if !request.Stream {
		writeError(w, http.StatusNotFound, notFoundError, onlyStreamedMessages)
		return
	}
	answer, id, err := h.plan(r.Context(), request)
	if err != nil {
		h.logger.WarnContext(r.Context(), "the mock model rejected a request", "model", request.Model, "reason", err.Error())
		writeError(w, http.StatusBadRequest, invalidRequestError, err.Error())
		return
	}
	if answer.failure != nil {
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
	selected, err := followReplay(request.Messages)
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
		return textReply(selected.compactionSummary()), id + "_compaction", nil
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
	answer.progress = selected.progress()
	return answer, id, nil
}

// onlyStreamedMessages answers a request for anything but a streamed message.
const onlyStreamedMessages = "The mock model implements only streamed POST /v1/messages."

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
