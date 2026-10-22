// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

const maximumStreamBytes = 16 << 20
const maximumEventBytes = 1 << 20
const maximumOutputItems = 256

// readStream bounds aggregate memory and requires a terminal frame before releasing calls.
// Only ordinary assistant text is provisionally forwarded; reasoning has no writer.
func readStream(ctx context.Context, body io.Reader, request conversation.ModelRequest, policy Policy, startedAt time.Time) (conversation.ModelReply, error) {
	var firstEventMS *int64
	reader := bufio.NewScanner(io.LimitReader(body, maximumStreamBytes+1))
	reader.Buffer(make([]byte, 4096), maximumEventBytes)
	var data []string
	eventBytes := 0
	total := 0
	responses := responseStream{items: map[int]responseItem{}, started: map[int]responseItem{}, arguments: map[int]*strings.Builder{}, texts: map[int]*strings.Builder{}, refusals: map[int]*strings.Builder{}, lastSequence: -1}
	chat := chatStream{calls: map[int]*chatCall{}}
	dispatch := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		if firstEventMS == nil {
			elapsed := time.Since(startedAt).Milliseconds()
			firstEventMS = &elapsed
		}
		encoded := strings.Join(data, "\n")
		data = nil
		eventBytes = 0
		if !utf8.ValidString(encoded) {
			return false, rejected(failureInvalidStreamEvent)
		}
		if policy.WireMode == conversation.OpenAIResponses {
			return responses.event([]byte(encoded), request)
		}
		return chat.event([]byte(encoded), request)
	}
	for reader.Scan() {
		if ctx.Err() != nil {
			return conversation.ModelReply{}, ctx.Err()
		}
		line := reader.Text()
		total += len(line) + 1
		if total > maximumStreamBytes {
			return conversation.ModelReply{}, rejected(failureResponseTooLarge)
		}
		if line == "" {
			done, err := dispatch()
			if err != nil {
				return conversation.ModelReply{}, err
			}
			if done {
				if policy.WireMode == conversation.OpenAIResponses {
					reply, err := responses.reply(request, policy)
					reply.Metrics.FirstEventMS = firstEventMS
					return reply, err
				}
				reply, err := chat.reply(request, policy)
				reply.Metrics.FirstEventMS = firstEventMS
				return reply, err
			}
		} else if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			eventBytes += len(value) + 1
			if eventBytes > maximumEventBytes {
				return conversation.ModelReply{}, rejected(failureResponseTooLarge)
			}
			data = append(data, value)
		}
	}
	if ctx.Err() != nil {
		return conversation.ModelReply{}, ctx.Err()
	}
	if reader.Err() != nil {
		if errors.Is(reader.Err(), bufio.ErrTooLong) {
			return conversation.ModelReply{}, rejected(failureResponseTooLarge)
		}
		return conversation.ModelReply{}, &transportError{cause: reader.Err()}
	}
	return conversation.ModelReply{}, errors.New("model response stream ended without its terminal frame")
}

type responseItem struct {
	Type      string                            `json:"type"`
	ID        string                            `json:"id"`
	Status    *conversation.ProviderReplyStatus `json:"status"`
	CallID    string                            `json:"call_id"`
	Name      string                            `json:"name"`
	Arguments string                            `json:"arguments"`
	Encrypted string                            `json:"encrypted_content"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

// UnmarshalJSON discards readable reasoning before an item reaches aggregation or replay.
func (item *responseItem) UnmarshalJSON(raw []byte) error {
	type wireItem responseItem
	var decoded wireItem
	envelope := struct {
		*wireItem
		Content []json.RawMessage `json:"content"`
	}{wireItem: &decoded}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if decoded.Type != ItemReasoning.String() {
		for _, rawPart := range envelope.Content {
			var identity struct {
				Type    string `json:"type"`
				Thought bool   `json:"thought"`
			}
			if err := json.Unmarshal(rawPart, &identity); err != nil {
				return err
			}
			if identity.Type == "reasoning_text" || identity.Type == "summary_text" || identity.Thought {
				continue
			}
			var visible struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			}
			if err := json.Unmarshal(rawPart, &visible); err != nil {
				return err
			}
			decoded.Content = append(decoded.Content, visible)
		}
	}
	*item = responseItem(decoded)
	return nil
}

type usage struct {
	Input        *int64 `json:"input_tokens"`
	Output       *int64 `json:"output_tokens"`
	Prompt       *int64 `json:"prompt_tokens"`
	Completion   *int64 `json:"completion_tokens"`
	InputDetails struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	PromptDetails struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	OutputDetails struct {
		Reasoning *int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	CompletionDetails struct {
		Reasoning *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type responseStream struct {
	outputLimit                bool
	items, started             map[int]responseItem
	arguments, texts, refusals map[int]*strings.Builder
	lastSequence               int
	usage                      usage
	model                      string
}

func (stream *responseStream) event(raw []byte, request conversation.ModelRequest) (bool, error) {
	var identity struct {
		Type     string `json:"type"`
		Sequence *int   `json:"sequence_number"`
		Index    int    `json:"output_index"`
	}
	if json.Unmarshal(raw, &identity) != nil {
		return false, rejected(failureInvalidStreamEvent)
	}
	switch identity.Type {
	case EventResponseReasoningTextDelta.String(), EventResponseReasoningSummaryDelta.String(), EventResponseReasoningTextDone.String(), EventResponseReasoningSummaryDone.String(), EventResponseReasoningPartAdded.String(), EventResponseReasoningPartDone.String():
		if identity.Sequence == nil || *identity.Sequence != stream.lastSequence+1 {
			return false, rejected(failureInvalidStreamOrder)
		}
		if identity.Index < 0 || identity.Index >= maximumOutputItems {
			return false, rejected(failureInvalidOutputIndex)
		}
		stream.lastSequence = *identity.Sequence
		return false, nil
	}
	var event struct {
		Type     string       `json:"type"`
		Sequence *int         `json:"sequence_number"`
		Index    int          `json:"output_index"`
		ItemID   string       `json:"item_id"`
		Delta    string       `json:"delta"`
		Item     responseItem `json:"item"`
		Response struct {
			Status            conversation.ProviderReplyStatus `json:"status"`
			Model             string                           `json:"model"`
			Output            []responseItem                   `json:"output"`
			Usage             usage                            `json:"usage"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false, rejected(failureInvalidStreamEvent)
	}
	if event.Sequence == nil || *event.Sequence != stream.lastSequence+1 {
		return false, rejected(failureInvalidStreamOrder)
	}
	stream.lastSequence = *event.Sequence
	if event.Index < 0 || event.Index >= maximumOutputItems {
		return false, rejected(failureInvalidOutputIndex)
	}
	switch event.Type {
	case EventResponseOutputItemAdded.String():
		if _, exists := stream.started[event.Index]; exists {
			return false, rejected(failureDuplicateOutputItem)
		}
		if event.Index != len(stream.started) || event.Item.ID == "" {
			return false, rejected(failureInvalidOutputOrder)
		}
		for _, existing := range stream.started {
			if existing.ID == event.Item.ID {
				return false, rejected(failureInvalidOutputIdentity)
			}
		}
		stream.started[event.Index] = event.Item
		stream.arguments[event.Index] = &strings.Builder{}
		stream.texts[event.Index] = &strings.Builder{}
		stream.refusals[event.Index] = &strings.Builder{}
	case EventResponseFunctionArgumentsDelta.String(), EventResponseOutputTextDelta.String(), EventResponseRefusalDelta.String():
		item, exists := stream.started[event.Index]
		if !exists || item.ID != event.ItemID {
			return false, rejected(failureInvalidDeltaIdentity)
		}
		if _, done := stream.items[event.Index]; done {
			return false, rejected(failureDeltaAfterItemDone)
		}
		if event.Type == EventResponseFunctionArgumentsDelta.String() {
			if item.Type != ItemFunctionCall.String() {
				return false, rejected(failureInvalidDeltaType)
			}
			stream.arguments[event.Index].WriteString(event.Delta)
			toolInputProgress(request, item.CallID, item.Name, stream.arguments[event.Index].String(), false)
		} else {
			if item.Type != ItemMessage.String() {
				return false, rejected(failureInvalidDeltaType)
			}
			if event.Type == EventResponseRefusalDelta.String() {
				stream.refusals[event.Index].WriteString(event.Delta)
			} else {
				stream.texts[event.Index].WriteString(event.Delta)
			}
			if request.WriteAssistant != nil && event.Delta != "" {
				if err := request.WriteAssistant(event.Delta); err != nil {
					return false, err
				}
			}
		}
	case EventResponseOutputItemDone.String():
		initial, exists := stream.started[event.Index]
		if !exists || initial.ID != event.Item.ID || initial.Type != event.Item.Type {
			return false, rejected(failureInvalidOutputIdentity)
		}
		if _, done := stream.items[event.Index]; done {
			return false, rejected(failureDuplicateOutputItem)
		}
		if event.Item.Type == ItemFunctionCall.String() && (event.Item.CallID != initial.CallID || event.Item.Name != initial.Name || event.Item.Arguments != stream.arguments[event.Index].String()) {
			return false, rejected(failureInvalidToolAggregation)
		}
		if event.Item.Type == ItemMessage.String() {
			var text, refusal strings.Builder
			for _, part := range event.Item.Content {
				if part.Type == ItemOutputText.String() {
					text.WriteString(part.Text)
				} else if part.Type == ItemRefusal.String() {
					refusal.WriteString(part.Refusal)
				}
			}
			if text.String() != stream.texts[event.Index].String() || refusal.String() != stream.refusals[event.Index].String() {
				return false, rejected(failureInvalidTextAggregation)
			}
		}
		stream.items[event.Index] = event.Item
	case EventResponseCompleted.String():
		if event.Response.Status != conversation.ProviderReplyCompleted || len(stream.items) != len(stream.started) || len(event.Response.Output) != len(stream.items) {
			return false, rejected(failureIncompleteResponse)
		}
		for index, item := range event.Response.Output {
			finished := stream.items[index]
			// Both terminal proofs now exist; absence is optional, an explicit
			// non-completed status remains a deterministic protocol rejection.
			completed := conversation.ProviderReplyCompleted
			if item.Status == nil {
				item.Status = &completed
			}
			if finished.Status == nil {
				finished.Status = &completed
			}
			if *item.Status != completed || *finished.Status != completed {
				return false, rejected(failureIncompleteResponse)
			}
			if !reflect.DeepEqual(item, finished) {
				return false, rejected(failureInconsistentResponse)
			}
			stream.items[index] = finished
		}
		stream.usage = event.Response.Usage
		stream.model = event.Response.Model
		return true, nil
	case EventResponseFailed.String(), EventProviderError.String():
		return false, providerFailure(raw, 0)
	case EventResponseIncomplete.String():
		if event.Response.Status != conversation.ProviderReplyIncomplete || event.Response.IncompleteDetails.Reason != "max_output_tokens" || len(event.Response.Output) != len(stream.started) {
			return false, rejected(failureIncompleteResponse)
		}
		for index, item := range event.Response.Output {
			initial := stream.started[index]
			if initial.ID != item.ID || initial.Type != item.Type {
				return false, rejected(failureInvalidOutputIdentity)
			}
			finished, done := stream.items[index]
			if done && finished.Status != nil && *finished.Status == conversation.ProviderReplyCompleted && item.Status != nil && *item.Status == conversation.ProviderReplyCompleted {
				if !reflect.DeepEqual(item, finished) {
					return false, rejected(failureInconsistentResponse)
				}
				continue
			}
			delete(stream.items, index)
			if item.Type == ItemFunctionCall.String() {
				if item.CallID != initial.CallID || item.Name != initial.Name || item.Arguments != stream.arguments[index].String() {
					return false, rejected(failureInvalidToolAggregation)
				}
				completed := conversation.ProviderReplyCompleted
				item.Arguments, item.Status = "{}", &completed
				stream.items[index] = item
			}
		}
		stream.outputLimit, stream.usage, stream.model = true, event.Response.Usage, event.Response.Model
		return true, nil
	// Structural frames carry no business content; no reasoning field is retained.
	case EventResponseCreated.String(), EventResponseInProgress.String(), EventResponseContentPartAdded.String(), EventResponseContentPartDone.String(), EventResponseOutputTextDone.String(), EventResponseFunctionArgumentsDone.String(), EventResponseRefusalDone.String():
	default:
		return false, rejected(failureUnsupportedStreamEvent)
	}
	return false, nil
}

func (stream *responseStream) reply(request conversation.ModelRequest, policy Policy) (conversation.ModelReply, error) {
	reply := conversation.ModelReply{StopReason: conversation.StopReasonEndTurn, ToolCalls: []conversation.ToolCall{}}
	refused := false
	seen := map[string]bool{}
	opaqueBytes := 0
	for index := 0; index < len(stream.started); index++ {
		item, exists := stream.items[index]
		if !exists {
			continue
		}
		switch item.Type {
		case ItemReasoning.String():
			if item.Encrypted == "" {
				continue
			}
			if policy.Replay != conversation.ReplayEncryptedOpaque {
				return conversation.ModelReply{}, rejected(failureInvalidReasoningReplay)
			}
			replay := conversation.OpaqueReplay{Provider: policy.Provider.String(), WireMode: policy.WireMode.String(), Position: len(reply.Blocks), ItemID: item.ID, Status: conversation.ProviderReplyCompleted, Ciphertext: item.Encrypted}
			block, err := replay.Block()
			if err != nil {
				return conversation.ModelReply{}, rejected(failureInvalidReasoningReplay)
			}
			opaqueBytes += len(block)
			reply.Blocks = append(reply.Blocks, block)
		case ItemMessage.String():
			if item.Status == nil || *item.Status != conversation.ProviderReplyCompleted {
				return conversation.ModelReply{}, rejected(failureIncompleteResponse)
			}
			for _, content := range item.Content {
				switch content.Type {
				case ItemOutputText.String():
					if content.Text != "" {
						reply.Blocks = append(reply.Blocks, textBlock(content.Text))
					}
				case ItemRefusal.String():
					refused = true
				default:
					return conversation.ModelReply{}, rejected(failureUnsupportedContent)
				}
			}
		case ItemFunctionCall.String():
			if item.Status == nil || *item.Status != conversation.ProviderReplyCompleted || seen[item.CallID] {
				return conversation.ModelReply{}, rejected(failureInvalidToolIdentity)
			}
			call, block, err := completeCall(item.CallID, item.Name, item.Arguments, request)
			if err != nil {
				return conversation.ModelReply{}, err
			}
			seen[item.CallID] = true
			reply.Blocks = append(reply.Blocks, block)
			reply.ToolCalls = append(reply.ToolCalls, call)
		default:
			return conversation.ModelReply{}, rejected(failureUnsupportedOutputItem)
		}
	}
	// A completed refusal is definitive, including an output-limited response.
	switch {
	case refused:
		reply.StopReason = conversation.StopReasonRefusal
	case stream.outputLimit:
		reply.StopReason = conversation.StopReasonMaxTokens
	}
	if reply.StopReason == conversation.StopReasonRefusal && len(reply.ToolCalls) > 0 {
		return conversation.ModelReply{}, rejected(failureInvalidFinishReason)
	}
	if policy.Replay == conversation.ReplayEncryptedOpaque && len(reply.ToolCalls) > 0 && opaqueBytes == 0 && !stream.outputLimit {
		return conversation.ModelReply{}, rejected(failureMissingReplay)
	}
	if opaqueBytes > conversation.MaximumOpaqueReplayBytes {
		return conversation.ModelReply{}, rejected(failureOpaqueReplayTooLarge)
	}
	return finishReply(reply, stream.usage, stream.model), nil
}

type chatCall struct {
	arguments strings.Builder
	ID        string `json:"id"`
	Type      string `json:"type"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	Extra struct {
		Google struct {
			Signature string `json:"thought_signature"`
		} `json:"google"`
	} `json:"extra_content"`
}

type chatStream struct {
	calls  map[int]*chatCall
	text   strings.Builder
	finish string
	usage  usage
	model  string
}

func (stream *chatStream) event(raw []byte, request conversation.ModelRequest) (bool, error) {
	if string(raw) == "[DONE]" {
		if stream.finish == "" {
			return false, errors.New("model stream ended before its finish reason")
		}
		return true, nil
	}
	var event struct {
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Index  int     `json:"index"`
			Finish *string `json:"finish_reason"`
			Delta  struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index int `json:"index"`
					chatCall
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *usage `json:"usage"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false, rejected(failureInvalidStreamEvent)
	}
	if !emptyProtocolValue(event.Error) {
		return false, providerFailure(raw, 0)
	}
	if len(event.Choices) > 1 {
		return false, rejected(failureUnexpectedChoice)
	}
	if event.Model != "" {
		if stream.model != "" && stream.model != event.Model {
			return false, rejected(failureModelChangedInStream)
		}
		stream.model = event.Model
	}
	if event.Usage != nil {
		stream.usage = *event.Usage
	}
	for _, choice := range event.Choices {
		if choice.Index != 0 || stream.finish != "" {
			return false, rejected(failureInvalidChoiceOrder)
		}
		delta := choice.Delta
		if delta.Content != "" {
			stream.text.WriteString(delta.Content)
			if request.WriteAssistant != nil {
				if err := request.WriteAssistant(delta.Content); err != nil {
					return false, err
				}
			}
		}
		for _, part := range delta.ToolCalls {
			if part.Index < 0 || part.Index >= maximumOutputItems {
				return false, rejected(failureInvalidToolIndex)
			}
			call, found := stream.calls[part.Index]
			if !found {
				if part.Index != len(stream.calls) {
					return false, rejected(failureInvalidToolOrder)
				}
				call = &chatCall{}
				stream.calls[part.Index] = call
			}
			if part.ID != "" {
				if call.ID != "" {
					return false, rejected(failureDuplicateToolIdentity)
				}
				call.ID = part.ID
			}
			if part.Type != "" {
				if part.Type != ItemFunction.String() || call.Type != "" && call.Type != part.Type {
					return false, rejected(failureInvalidToolType)
				}
				call.Type = part.Type
			}
			if len(call.Function.Name)+len(part.Function.Name) > 128 {
				return false, rejected(failureInvalidToolIdentity)
			}
			call.Function.Name += part.Function.Name
			call.arguments.WriteString(part.Function.Arguments)
			toolInputProgress(request, call.ID, call.Function.Name, call.arguments.String(), false)
			if signature := part.Extra.Google.Signature; signature != "" {
				if call.Extra.Google.Signature != "" {
					return false, rejected(failureDuplicateSignature)
				}
				call.Extra.Google.Signature = signature
			}
		}
		if choice.Finish != nil {
			stream.finish = *choice.Finish
		}
	}
	return false, nil
}

func (stream *chatStream) reply(request conversation.ModelRequest, policy Policy) (conversation.ModelReply, error) {
	reply := conversation.ModelReply{StopReason: conversation.StopReasonEndTurn, ToolCalls: []conversation.ToolCall{}}
	switch stream.finish {
	case FinishStop.String():
		if len(stream.calls) > 0 {
			return conversation.ModelReply{}, rejected(failureInvalidFinishReason)
		}
	case FinishToolCalls.String():
		if len(stream.calls) == 0 {
			return conversation.ModelReply{}, rejected(failureInvalidFinishReason)
		}
	case FinishLength.String():
		reply.StopReason = conversation.StopReasonMaxTokens
	case FinishContentFilter.String():
		if len(stream.calls) > 0 {
			return conversation.ModelReply{}, rejected(failureInvalidFinishReason)
		}
		reply.StopReason = conversation.StopReasonRefusal
	default:
		return conversation.ModelReply{}, rejected(failureInvalidFinishReason)
	}
	if text := stream.text.String(); text != "" {
		reply.Blocks = append(reply.Blocks, textBlock(text))
	}
	indices := make([]int, 0, len(stream.calls))
	for index := range stream.calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	seen := map[string]bool{}
	opaqueBytes := 0
	for _, index := range indices {
		item := stream.calls[index]
		if seen[item.ID] {
			return conversation.ModelReply{}, rejected(failureDuplicateToolIdentity)
		}
		seen[item.ID] = true
		arguments := item.arguments.String()
		if reply.StopReason == conversation.StopReasonMaxTokens {
			if _, err := conversation.ParseJSONObject(arguments); err != nil {
				arguments = "{}"
			}
		}
		call, block, err := completeCall(item.ID, item.Function.Name, arguments, request)
		if err != nil {
			return conversation.ModelReply{}, err
		}
		signature := item.Extra.Google.Signature
		if policy.Replay == conversation.ReplayEncryptedOpaque {
			if signature == "" && index == 0 && reply.StopReason != conversation.StopReasonMaxTokens {
				return conversation.ModelReply{}, rejected(failureMissingReplay)
			}
			if signature != "" {
				replay, err := (conversation.OpaqueReplay{Provider: policy.Provider.String(), WireMode: policy.WireMode.String(), Position: len(reply.Blocks), CallID: call.ID, Ciphertext: signature}).Block()
				if err != nil {
					return conversation.ModelReply{}, rejected(failureInvalidReplay)
				}
				opaqueBytes += len(replay)
				reply.Blocks = append(reply.Blocks, replay)
			}
		} else if signature != "" {
			return conversation.ModelReply{}, rejected(failureUnexpectedReplay)
		}
		reply.Blocks = append(reply.Blocks, block)
		reply.ToolCalls = append(reply.ToolCalls, call)
	}
	if opaqueBytes > conversation.MaximumOpaqueReplayBytes {
		return conversation.ModelReply{}, rejected(failureOpaqueReplayTooLarge)
	}
	return finishReply(reply, stream.usage, stream.model), nil
}

func finishReply(reply conversation.ModelReply, counts usage, model string) conversation.ModelReply {
	if len(reply.ToolCalls) > 0 && reply.StopReason == conversation.StopReasonEndTurn {
		reply.StopReason = conversation.StopReasonToolUse
	}
	reply.Text = (conversation.Entry{Blocks: reply.Blocks}).Text()
	reply.Metrics.ServedModel = model
	reply.Metrics.InputTokens = counts.Input
	reply.Metrics.OutputTokens = counts.Output
	reply.Metrics.CacheReadInputTokens = counts.InputDetails.Cached
	reply.Metrics.ThinkingTokens = counts.OutputDetails.Reasoning
	if counts.Prompt != nil {
		reply.Metrics.InputTokens = counts.Prompt
		reply.Metrics.OutputTokens = counts.Completion
		reply.Metrics.CacheReadInputTokens = counts.PromptDetails.Cached
		reply.Metrics.ThinkingTokens = counts.CompletionDetails.Reasoning
	}
	if reply.Metrics.InputTokens != nil && reply.Metrics.CacheReadInputTokens != nil {
		uncached := max(int64(0), *reply.Metrics.InputTokens-*reply.Metrics.CacheReadInputTokens)
		reply.Metrics.InputTokens = &uncached
	}
	return reply
}

func toolInputProgress(request conversation.ModelRequest, id, name, arguments string, complete bool) {
	if request.WriteToolInput != nil && id != "" && name == string(conversation.ToolConfirmRequirements) {
		request.WriteToolInput(conversation.ToolInputProgress{CallID: conversation.CallID(id), Name: conversation.ToolConfirmRequirements, Input: arguments, Complete: complete})
	}
}

func emptyProtocolValue(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text == ""
	}
	var array []json.RawMessage
	return string(raw) == "null" || json.Unmarshal(raw, &array) == nil && len(array) == 0
}
