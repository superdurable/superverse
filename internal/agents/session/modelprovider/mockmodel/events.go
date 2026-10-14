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
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm"
)

// deltaRunes is the size of one text, thinking or tool input delta.
const deltaRunes = 16

// requestID is the request-id of every answer.
const requestID = "req_mock"

// typeField is the JSON field that names the type of a message, of a content
// block and of a server-sent event.
const typeField = "type"

// blockDelta is one delta of a content block: its type and the one field it
// fills, never empty.
type blockDelta struct {
	Type        string `json:"type"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// writeStream streams a reply as the server-sent events of the Messages API:
// message_start, then per block a content_block_start, its deltas and a
// content_block_stop, then message_delta with the stop reason and the usage,
// and message_stop. The events follow the shapes the SDK's stream decoder and
// BetaMessage.Accumulate read, with a ping after the first block as the API
// sends one. It returns when the reply ends or the client is gone.
func writeStream(ctx context.Context, w http.ResponseWriter, id, model string, answer reply) error {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("request-id", requestID)
	w.WriteHeader(http.StatusOK)
	send := func(event string, data map[string]any) error {
		data[typeField] = event
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, comm.Must(json.Marshal(data))); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	usage := map[string]any{"input_tokens": answer.inputTokens, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	started := map[string]any{"id": id, typeField: "message", "role": conversation.EntryRoleAssistant, "model": model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": withOutput(usage, 1)}
	if err := send("message_start", map[string]any{"message": started}); err != nil {
		return err
	}
	for index, block := range answer.blocks {
		if err := writeBlock(ctx, send, index, block, answer.chunkDelay); err != nil {
			return err
		}
		if index == 0 {
			if err := send("ping", map[string]any{}); err != nil {
				return err
			}
		}
	}
	delta := map[string]any{"stop_reason": answer.stopReason, "stop_sequence": nil, "stop_details": nil}
	if err := send("message_delta", map[string]any{"delta": delta, "usage": withOutput(usage, answer.outputTokens)}); err != nil {
		return err
	}
	return send("message_stop", map[string]any{})
}

// withOutput copies usage with the output token count.
func withOutput(usage map[string]any, output int64) map[string]any {
	copied := map[string]any{"output_tokens": output}
	for key, value := range usage {
		copied[key] = value
	}
	return copied
}

// writeBlock streams one content block: its start with the empty shape the API
// sends, the deltas that fill it and its stop.
func writeBlock(ctx context.Context, send func(string, map[string]any) error, index int, b block, delay time.Duration) error {
	empty := map[string]any{typeField: b.kind.String()}
	var deltas []blockDelta
	switch b.kind {
	case conversation.BlockTypeThinking:
		empty["thinking"], empty["signature"] = "", ""
		for _, piece := range chunks(b.text, deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "thinking_delta", Thinking: piece})
		}
		deltas = append(deltas, blockDelta{Type: "signature_delta", Signature: b.signature})
	case conversation.BlockTypeText:
		empty["text"] = ""
		for _, piece := range chunks(b.text, deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "text_delta", Text: piece})
		}
	default:
		empty["id"], empty["name"], empty["input"] = b.id, b.name, map[string]any{}
		for _, piece := range chunks(string(b.input), deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "input_json_delta", PartialJSON: piece})
		}
	}
	sendOfBlock := func(event string, data map[string]any) error {
		data["index"] = index
		return send(event, data)
	}
	if err := sendOfBlock("content_block_start", map[string]any{"content_block": empty}); err != nil {
		return err
	}
	for _, delta := range deltas {
		if err := pause(ctx, delay); err != nil {
			return err
		}
		if err := sendOfBlock("content_block_delta", map[string]any{"delta": delta}); err != nil {
			return err
		}
	}
	return sendOfBlock("content_block_stop", map[string]any{})
}

// pause waits between two deltas unless the client is gone.
func pause(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
