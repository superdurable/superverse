// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// block is one content block of a reply, as the model returns and a later
// request replays it.
type block struct {
	kind      conversation.BlockType // thinking, text or tool_use
	text      string                 // the thinking or the text
	signature string
	id, name  string
	input     json.RawMessage
}

// reply is everything a call answers with: the blocks, the usage and stop
// reason they report, how fast they stream and where they leave the scenario.
type reply struct {
	protocolFault protocolFault
	blocks        []block
	inputTokens   int64
	outputTokens  int64
	stopReason    conversation.StopReason
	chunkDelay    time.Duration
	failure       *stepError
	// progress is reported with the reply; nil outside a scenario.
	progress *conversation.MockProgress
}

// replyAt is the reply at a position of the scenario's run: a script step, or
// a text that names the next input once the input has no more replies. A reply
// depends only on its scenario, its run, its position and the arguments, so a
// retry gets the same bytes.
func (s *script) replyAt(at position, arguments map[string]string, run string) (reply, error) {
	step := s.stepAt(at)
	switch {
	case step == nil && at.input+1 < len(s.Inputs):
		return textReply(fmt.Sprintf("Mock scenario %s has no further replies to input %d; send input %d next: %s.", s.Name, at.input+1, at.input+2, s.Inputs[at.input+1].Label)), nil
	case step == nil:
		return textReply(fmt.Sprintf("The mock scenario %s has no further steps.", s.Name)), nil
	case step.Error != nil:
		return step.Error.reply(), nil
	}
	var blocks []block
	if step.Thinking != "" {
		blocks = append(blocks, block{kind: conversation.BlockTypeThinking, text: s.render(step.Thinking, arguments), signature: signature(s.Name, at, len(blocks))})
	}
	if step.Text != "" {
		blocks = append(blocks, block{kind: conversation.BlockTypeText, text: s.render(step.Text, arguments)})
	}
	for toolIndex, use := range step.ToolUses {
		input, err := s.renderInput(use.Input, arguments)
		if err != nil {
			return reply{}, err
		}
		id := fmt.Sprintf("toolu_mock_%s_%d_%d_%d", run, at.input, at.reply, toolIndex)
		if step.Remote {
			id = string(use.ID)
		}
		blocks = append(blocks, block{kind: conversation.BlockTypeToolUse, id: id, name: string(use.Name), input: input})
	}
	result := reply{protocolFault: step.ProtocolFault, blocks: blocks, inputTokens: 1000, stopReason: step.stopReason(), chunkDelay: time.Duration(step.ChunkDelayMS) * time.Millisecond}
	result.outputTokens = outputEstimate(blocks)
	if step.Usage != nil {
		result.inputTokens, result.outputTokens = step.Usage.InputTokens, step.Usage.OutputTokens
	}
	return result, nil
}

// reply answers the call with the error; a blank message gets the mock
// model's own.
func (failure stepError) reply() reply {
	if failure.Message == "" {
		failure.Message = "The mock model rejected this request."
	}
	return reply{failure: &failure}
}

// textReply is a reply of one text block that ends the turn.
func textReply(text string) reply {
	blocks := []block{{kind: conversation.BlockTypeText, text: text}}
	return reply{blocks: blocks, inputTokens: 1000, outputTokens: outputEstimate(blocks), stopReason: conversation.StopReasonEndTurn}
}

// outputEstimate is about one token per four characters.
func outputEstimate(blocks []block) int64 {
	var characters int
	for _, block := range blocks {
		characters += utf8.RuneCountInString(block.text) + len(block.input)
	}
	return int64(characters/4 + 1)
}

// signature is a fake thinking signature that depends only on where the block
// is in its scenario.
func signature(scenario string, at position, blockIndex int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%d", scenario, at.input, at.reply, blockIndex)))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// textJSON is a text block as a request carries it.
type textJSON struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// json is the block as a request replays it.
func (b block) json() []byte {
	switch b.kind {
	case conversation.BlockTypeRedactedThinking:
		return comm.Must(json.Marshal(map[string]any{typeField: b.kind.String(), "data": b.signature}))
	case conversation.BlockTypeThinking:
		return comm.Must(json.Marshal(map[string]any{typeField: b.kind.String(), "thinking": b.text, "signature": b.signature}))
	case conversation.BlockTypeText:
		return comm.Must(json.Marshal(map[string]any{typeField: b.kind.String(), "text": b.text}))
	default:
		return comm.Must(json.Marshal(map[string]any{typeField: b.kind.String(), "id": b.id, "name": b.name, "input": b.input}))
	}
}

// segmentStartMismatch is the stop text of the step at a position when the
// segment_start of the request lacks a text the step expects in it or holds
// one the step expects it to lack, or "".
func (s *script) segmentStartMismatch(at position, arguments map[string]string, start string) string {
	step := s.stepAt(at)
	if step == nil {
		return ""
	}
	for _, expected := range step.ExpectSegmentStart {
		if rendered := s.render(expected, arguments); !strings.Contains(start, rendered) {
			return fmt.Sprintf("Mock scenario %s stopped at %s: the first message of the context segment lacks %q", s.Name, at, rendered)
		}
	}
	for _, unexpected := range step.ExpectSegmentStartLacks {
		if rendered := s.render(unexpected, arguments); strings.Contains(start, rendered) {
			return fmt.Sprintf("Mock scenario %s stopped at %s: the first message of the context segment holds %q", s.Name, at, rendered)
		}
	}
	return ""
}

// render fills the {{name}} placeholders of text from arguments.
func (s *script) render(text string, arguments map[string]string) string {
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		return arguments[placeholder.FindStringSubmatch(match)[1]]
	})
}

// renderInput fills the placeholders of every string of a tool input and
// returns it as compact JSON with sorted keys. A placeholder the script does
// not declare is an error.
func (s *script) renderInput(input json.RawMessage, arguments map[string]string) (json.RawMessage, error) {
	if input == nil {
		input = json.RawMessage("{}")
	}
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, err
	}
	rendered, err := s.renderValue(value, arguments)
	if err != nil {
		return nil, err
	}
	return comm.Must(json.Marshal(rendered)), nil
}

func (s *script) renderValue(value any, arguments map[string]string) (any, error) {
	switch typed := value.(type) {
	case string:
		for _, match := range placeholder.FindAllStringSubmatch(typed, -1) {
			if _, declared := s.Arguments[match[1]]; !declared {
				return nil, fmt.Errorf("the scenario declares no argument %q", match[1])
			}
		}
		return s.render(typed, arguments), nil
	case []any:
		for index, item := range typed {
			rendered, err := s.renderValue(item, arguments)
			if err != nil {
				return nil, err
			}
			typed[index] = rendered
		}
	case map[string]any:
		for key, item := range typed {
			rendered, err := s.renderValue(item, arguments)
			if err != nil {
				return nil, err
			}
			typed[key] = rendered
		}
	}
	return value, nil
}

// chunks splits text into pieces of at most size runes.
func chunks(text string, size int) []string {
	var pieces []string
	for text != "" {
		end := 0
		for count := 0; count < size && end < len(text); count++ {
			_, width := utf8.DecodeRuneInString(text[end:])
			end += width
		}
		pieces = append(pieces, text[:end])
		text = text[end:]
	}
	return pieces
}
