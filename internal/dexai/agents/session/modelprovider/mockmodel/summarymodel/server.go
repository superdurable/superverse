// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package summarymodel

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// Path shares the local platform's credential listener; hosted servers never register it.
const Path = "/internal/mock-llm/v1/responses"

// APIKeyForTest and ModelForTest are fixed non-secret verification identities.
const APIKeyForTest = "superverse-local-mock-llm-not-a-secret"
const ModelForTest = "superverse-mock-summary"

// New serves only deterministic Responses API generation with no outbound client or durable state.
func New(logger *slog.Logger) http.Handler {
	slots := make(chan struct{}, 16)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+APIKeyForTest {
			http.Error(w, "synthetic credential required", http.StatusUnauthorized)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Input  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || request.Model != ModelForTest {
			http.Error(w, "mock model request required", http.StatusBadRequest)
			return
		}
		var text string
		for _, input := range request.Input {
			if input.Role == "user" {
				text = input.Content
				break
			}
		}
		words := strings.Fields(text)
		if len(words) == 0 {
			http.Error(w, "source text required", http.StatusBadRequest)
			return
		}
		limit := min(len(words), 40)
		summary := "Local mock summary: " + strings.Join(words[:limit], " ")
		id := fmt.Sprintf("resp_mock_%x", sha256.Sum256([]byte(text)))
		response := map[string]any{
			"id": id, "object": "response", "model": ModelForTest, "status": "completed",
			"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": summary}}}},
			"usage":  map[string]int{"input_tokens": len(words), "output_tokens": limit + 3, "total_tokens": len(words) + limit + 3},
		}
		logger.InfoContext(r.Context(), "local mock LLM answered", "response", id, "stream", request.Stream)
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []map[string]any{
			{"type": "response.output_text.delta", "delta": summary},
			{"type": "response.completed", "response": response},
		} {
			data, err := json.Marshal(event)
			if err != nil {
				panic(err)
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data); err != nil {
				return
			}
		}
	})
}
