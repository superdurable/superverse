// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider"
)

// checkLongRemoteHandoff asserts the actual first request of this acceptance
// fixture, after followReplay has checked every selected assistant with
// sameBlocks. It never exempts a message or block from replay validation and
// does not record provider bodies or private reasoning.
func (request messagesRequest) checkLongRemoteHandoff(selected selection) error {
	if selected.script == nil || selected.script.Name != "remote-long-continuation" || selected.at.reply != 0 {
		return nil
	}
	if selected.arguments["bytecase"] == "1" {
		return request.checkLongRemoteByteHandoff(selected)
	}
	if selected.at.input != 0 || len(request.Messages) != 5 || request.Messages[0].Role != "user" {
		return errors.New("long handoff must replay the exact user-first five-message retained suffix")
	}
	if selected.arguments["task"] != "cmd-cccccccccccc" {
		return errors.New("long handoff let its older summary replace a current message-derived argument")
	}
	first := request.Messages[0].texts()
	if len(first) != 994 {
		return errors.New("long handoff lost imported entries or reconstructed a truncated tool unit")
	}
	for index := 0; index < 993; index++ {
		want := fmt.Sprintf("Long remote entry %04d ", index+1) + strings.Repeat("x", 128)
		if first[index] != want {
			return errors.New("long handoff reordered, altered or omitted an imported public entry")
		}
	}
	if !strings.Contains(first[993], "[[scenario:remote-long-tail#1]]") {
		return errors.New("long handoff lacks its exact scripted remote-tail input")
	}
	uses := []string{"remote-1-36", "remote-1-37"}
	for index, id := range uses {
		assistant := request.Messages[1+index*2]
		answer := request.Messages[2+index*2]
		var use struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		var result struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if assistant.Role != "assistant" || len(assistant.Content) != 1 || json.Unmarshal(assistant.Content[0], &use) != nil || use.Type != "tool_use" || use.ID != id || answer.Role != "user" || len(answer.Content) == 0 || json.Unmarshal(answer.Content[0], &result) != nil || result.Type != "tool_result" || result.ToolUseID != id || result.IsError {
			return errors.New("long handoff does not contain chronological complete successful remote tool units")
		}
	}
	if len(request.Messages[2].Content) != 1 || len(request.Messages[4].Content) != 3 {
		return errors.New("long handoff contains an unexpected result, input or notice")
	}
	budget, err := strconv.ParseInt(selected.arguments["budget"], 10, 64)
	if err != nil || budget != 400000 {
		return errors.New("long fixture requires the default 200000-token compaction threshold")
	}
	bytes, notices := int64(0), 0
	for _, message := range request.Messages {
		for _, raw := range message.Content {
			fields, kind, err := interactiveFields(raw, message.Role == "user")
			if err != nil {
				return err
			}
			if kind == "text" {
				var text string
				if json.Unmarshal(fields["text"], &text) != nil {
					return errors.New("long handoff contains malformed text")
				}
				if strings.HasPrefix(text, remoteexec.HandoffNoticePrefix) {
					notices++
					if err := checkLongHandoffNotice(text, selected); err != nil {
						return err
					}
					continue // Only this separately bounded canonical notice is outside the suffix budget.
				}
			}
			// Includes the provider's cache anchor, a conservative upper bound
			// on the immutable entry blocks. No block is shortened or edited.
			bytes += int64(len(raw))
		}
	}
	if notices != 1 || bytes > budget {
		return errors.New("long handoff duplicated its notice or exceeded half of the configured context budget")
	}
	return nil
}

func checkLongHandoffNotice(text string, selected selection) error {
	parsed, err := remoteexec.ParseHandoffNotice(text)
	if err != nil || parsed.Head != selected.arguments["head"] {
		return errors.New("long handoff has invalid bounded metadata or replaced the independently captured HEAD")
	}
	lines := strings.SplitN(text, "\n", 3)
	if len(lines) < 2 || lines[1] != "Only the recent part of that work is in the context above; older work is in the repository and its commits." {
		return errors.New("long handoff changed the exact older-work notice")
	}
	digest := sha256.Sum256([]byte(strings.Join(lines[:2], "\n")))
	if hex.EncodeToString(digest[:]) != selected.arguments["notice"] {
		return errors.New("long handoff changed authenticated owners, interval, reason or original HEAD")
	}
	_, summary, found := strings.Cut(text, "\n\nOlder platform summary:\n")
	if selected.arguments["summary"] == "none" {
		if found || len(lines) != 2 {
			return errors.New("fresh long handoff invented an older platform summary or optional section")
		}
		return nil
	}
	if !found || summary == "" || len(summary) > 8<<10 {
		return errors.New("long handoff lost its genuine bounded pre-remote platform summary")
	}
	digest = sha256.Sum256([]byte(summary))
	if hex.EncodeToString(digest[:]) != selected.arguments["summary"] {
		return errors.New("long handoff altered or replaced its genuine pre-remote platform summary")
	}
	return nil
}

// messagesRequest is the part of a Messages API request the mock model
// reads; the client sends much more, which it ignores.
type messagesRequest struct {
	Model      string             `json:"model"`
	Stream     bool               `json:"stream"`
	Messages   []requestMessage   `json:"messages"`
	Tools      []requestTool      `json:"tools"`
	ToolChoice *requestToolChoice `json:"tool_choice"`
}

// requestToolChoice is how a request lets the model use its tools.
type requestToolChoice struct {
	Type string `json:"type"`
}

// toolChoiceNone is the tool choice that lets the model call no tool, as the
// API spells it: a subagent's last call before its limit.
const toolChoiceNone = "none"

type requestTool struct {
	Name string `json:"name"`
}

// mockModel is the API name of the mock model. The server routes only the
// requests of mock Sessions here, so any other name is a routing defect.
var mockModel = conversation.ModelMock.ProviderModel()

// parseRequest decodes and checks a request: a streamed call of the mock model
// with well-formed messages.
func parseRequest(r *http.Request, body []byte) (messagesRequest, error) {
	var request messagesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return request, fmt.Errorf("the request body is not valid JSON: %w", err)
	}
	if request.Model != mockModel {
		return request, fmt.Errorf("model %q is not the mock model %q", request.Model, mockModel)
	}
	for _, header := range r.Header.Values("anthropic-beta") {
		for _, beta := range strings.Split(header, ",") {
			if beta = strings.TrimSpace(beta); !slices.Contains(modelprovider.RequestBetas, beta) {
				return request, fmt.Errorf("the beta %q is not one the client sends", beta)
			}
		}
	}
	if len(request.Messages) == 0 {
		return request, errors.New("messages must not be empty")
	}
	for index, message := range request.Messages {
		if conversation.EntryRole(message.Role).Validate() != nil || message.Content == nil {
			return request, fmt.Errorf("messages.%d needs a role of user, assistant or system and a content array", index)
		}
	}
	return request, nil
}

// allowsNoTool reports whether the request lets the model call no tool.
func (request messagesRequest) allowsNoTool() bool {
	return request.ToolChoice != nil && request.ToolChoice.Type == toolChoiceNone
}

// offersTool reports whether the request declares a tool of this name.
func (request messagesRequest) offersTool(name string) bool {
	return slices.ContainsFunc(request.Tools, func(tool requestTool) bool { return tool.Name == name })
}

// expectedSenderMarker is supplied by the real authenticated API test.
var expectedSenderMarker = regexp.MustCompile(`\[\[expected-sender:([^\]\r\n]+)\]\]`)

// checkSenderEnvelope checks exactly the selected input on the actual provider
// request, either directly or as one whole canonical line of the segment quote.
func (request messagesRequest) checkSenderEnvelope(selected selection) error {
	newest := -1
	for index := len(request.Messages) - 1; index >= 0; index-- {
		if request.Messages[index].Role == string(conversation.EntryRoleUser) {
			newest = index
			break
		}
	}
	if newest < 0 {
		return errors.New("the request has no user message")
	}
	type candidate struct {
		encoded      string
		quoted       bool
		sectionBytes int
	}
	var candidates []candidate
	for _, raw := range request.Messages[newest].Content {
		var block textJSON
		if err := json.Unmarshal(raw, &block); err != nil {
			return fmt.Errorf("invalid newest user block: %w", err)
		}
		if block.Type == conversation.BlockTypeText.String() && strings.HasPrefix(block.Text, conversation.SenderEnvelopePrefix) {
			candidates = append(candidates, candidate{encoded: strings.TrimPrefix(block.Text, conversation.SenderEnvelopePrefix)})
		}
	}
	// Only the first text block is the actual segment_start; arbitrary later
	// text blocks and quoted repository/context-summary strings cannot qualify.
	if len(request.Messages[0].Content) > 0 {
		var start textJSON
		if err := json.Unmarshal(request.Messages[0].Content[0], &start); err != nil {
			return fmt.Errorf("invalid segment-start block: %w", err)
		}
		if start.Type == conversation.BlockTypeText.String() {
			pending, found := finalPendingRequest(start.Text)
			if found {
				for _, line := range strings.Split(pending, "\n") {
					if encoded, ok := strings.CutPrefix(line, conversation.SenderEnvelopePrefix); ok {
						candidates = append(candidates, candidate{encoded: encoded, quoted: true, sectionBytes: len(pending)})
					}
				}
			}
		}
	}
	matches := 0
	for _, candidate := range candidates {
		var envelope struct {
			Sender string  `json:"sender"`
			Via    *string `json:"via"`
			Text   string  `json:"text"`
		}
		err := json.Unmarshal([]byte(candidate.encoded), &envelope)
		if err != nil {
			// Broken JSON cannot reliably identify its input, including escaped
			// markers. Never skip it in favor of another qualifying candidate.
			return errors.New("invalid sender-envelope candidate JSON")
		}
		if !selected.senderInputMatches(envelope.Text) {
			continue
		}
		matches++
		var canonical strings.Builder
		encoder := json.NewEncoder(&canonical)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(envelope); err != nil || strings.TrimSuffix(canonical.String(), "\n") != candidate.encoded || envelope.Via != nil {
			return errors.New("current-input sender envelope is not the canonical outer object with null via")
		}
		expected := expectedSenderMarker.FindAllStringSubmatch(envelope.Text, -1)
		if len(expected) != 1 || envelope.Sender == "" || expected[0][1] != envelope.Sender {
			return errors.New("current-input sender envelope does not match the exact authenticated sender expectation")
		}
		input := selected.script.Inputs[selected.at.input]
		if !strings.Contains(envelope.Text, input.Message) {
			return errors.New("current-input sender envelope changed the complete forged body, delimiters, quote or backslash")
		}
		if !strings.Contains(envelope.Text, "<b>a && b</b>") || !strings.Contains(candidate.encoded, "<b>a && b</b>") || strings.Contains(candidate.encoded, `\u003c`) || strings.Contains(candidate.encoded, `\u003e`) || strings.Contains(candidate.encoded, `\u0026`) {
			return errors.New("current-input sender envelope HTML-like coding text was escaped or changed")
		}
		if candidate.quoted != (selected.at.input == 0) {
			return errors.New("current-input sender envelope is not in the expected quoted or top-level form")
		}
		if selected.arguments["maximum"] == "true" {
			if !candidate.quoted || candidate.sectionBytes > conversation.MaximumUserMessageContentBytes || !strings.HasSuffix(envelope.Text, " [truncated]") || !utf8.ValidString(envelope.Text) || strings.ContainsRune(envelope.Text, '\ufffd') {
				return errors.New("maximum input did not produce a bounded canonical quote with a UTF-8-safe visible truncation suffix")
			}
		} else if strings.HasSuffix(envelope.Text, " [truncated]") {
			return errors.New("ordinary sender test input was unexpectedly truncated")
		}
	}
	if matches != 1 {
		return fmt.Errorf("current input has %d matching sender envelopes, want exactly one", matches)
	}
	return nil
}

// senderInputMatches uses both the script and selected input position, so a
// still-retained input one cannot satisfy the check for input two.
func (selected selection) senderInputMatches(text string) bool {
	for _, match := range scenarioMarker.FindAllStringSubmatch(text, -1) {
		number := 1
		if match[2] != "" {
			number, _ = strconv.Atoi(match[2])
		}
		if match[1] == selected.script.Name && number == selected.at.input+1 {
			return true
		}
	}
	return false
}

// finalPendingRequest recognizes whole-line delimiters of only the final
// section. Escaped newlines and delimiters inside a JSON string stay inert.
func finalPendingRequest(start string) (string, bool) {
	lines := strings.Split(start, "\n")
	if len(lines) < 3 || lines[len(lines)-1] != "</pending_request>" {
		return "", false
	}
	for index := len(lines) - 2; index >= 0; index-- {
		if lines[index] == "</pending_request>" {
			return "", false
		}
		if lines[index] == "<pending_request>" {
			return strings.Join(lines[index+1:len(lines)-1], "\n"), true
		}
	}
	return "", false
}

// checkPendingRequestBudget validates the real provider request after the
// bounded question continuation and its one forced compaction.
func (request messagesRequest) checkPendingRequestBudget(selected selection) error {
	if selected.at.input != len(selected.script.Inputs)-1 || selected.at.reply != 1 {
		return errors.New("multipart quote check did not reach the current final input after compaction")
	}
	var start textJSON
	if len(request.Messages[0].Content) == 0 || json.Unmarshal(request.Messages[0].Content[0], &start) != nil || start.Type != conversation.BlockTypeText.String() {
		return errors.New("multipart quote has no actual segment-start block")
	}
	pending, found := finalPendingRequest(start.Text)
	if !found || len(pending) > conversation.MaximumUserMessageContentBytes {
		return errors.New("multipart quote is absent or exceeds its total byte budget")
	}
	parts := strings.Split(pending, "\n\n")
	if len(parts) != 3 || parts[1] != "[2 pending request entries omitted]" {
		return errors.New("multipart quote must retain two whole parts and exactly one omitted-count line")
	}
	for index, part := range []string{parts[0], parts[2]} {
		encoded, ok := strings.CutPrefix(part, conversation.SenderEnvelopePrefix)
		var envelope struct {
			Sender string  `json:"sender"`
			Via    *string `json:"via"`
			Text   string  `json:"text"`
		}
		if !ok || json.Unmarshal([]byte(encoded), &envelope) != nil {
			return errors.New("multipart quote retained a broken sender envelope")
		}
		var canonical strings.Builder
		encoder := json.NewEncoder(&canonical)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(envelope); err != nil || strings.TrimSuffix(canonical.String(), "\n") != encoded || envelope.Via != nil {
			return errors.New("multipart quote changed canonical envelope bytes or via")
		}
		expected := expectedSenderMarker.FindAllStringSubmatch(envelope.Text, -1)
		if len(expected) != 1 || envelope.Sender == "" || expected[0][1] != envelope.Sender {
			return errors.New("multipart quote changed an authenticated sender")
		}
		marker := "[[budget-part:first]]"
		if index == 1 {
			marker = "[[budget-part:newest]]"
		}
		if !strings.Contains(envelope.Text, marker) || strings.Contains(envelope.Text, "[[budget-part:older]]") || strings.Contains(envelope.Text, "[[budget-part:middle]]") || strings.HasSuffix(envelope.Text, " [truncated]") {
			return errors.New("multipart quote did not preserve first/newest whole parts in chronological order")
		}
	}
	return nil
}

// Handoff is an assertion at one exact script boundary, never a replay exemption.
func (selected selection) checkHandoffBoundary(message requestMessage) error {
	if selected.script == nil {
		return nil
	}
	var notice string
	for _, raw := range message.Content {
		var text textJSON
		if json.Unmarshal(raw, &text) != nil || text.Type != conversation.BlockTypeText.String() || !strings.HasPrefix(text.Text, remoteexec.HandoffNoticePrefix) {
			continue
		}
		if notice != "" {
			return errors.New("a scripted boundary contains more than one canonical handoff")
		}
		notice = text.Text
	}
	step := selected.scriptStep()
	expected := step != nil && step.ExpectHandoff
	if (notice != "") != expected {
		return errors.New("the canonical handoff is missing or at an unscripted boundary")
	}
	if !expected {
		return nil
	}
	parsed, err := remoteexec.ParseHandoffNotice(notice)
	if err != nil {
		return err
	}
	if selected.arguments["head"] != parsed.Head {
		return errors.New("the handoff HEAD differs from the independently selected fixture HEAD")
	}
	return nil
}

// Interactive history has no script to bind replies to. Validate its public
// blocks and complete units explicitly; a selected script never enters this path.
type interactiveReplay struct {
	seen    map[conversation.CallID]bool
	pending map[conversation.CallID]bool
}

func (replay *interactiveReplay) assistant(message requestMessage) error {
	if len(replay.pending) != 0 || len(message.Content) == 0 {
		return errors.New("interactive assistant history lacks a complete preceding unit")
	}
	for _, raw := range message.Content {
		_, kind, err := interactiveFields(raw, false)
		if err != nil {
			return err
		}
		switch kind {
		case conversation.BlockTypeText.String():
			var block textJSON
			if json.Unmarshal(raw, &block) != nil {
				return errors.New("invalid interactive public text block")
			}
			report := remoteexec.RemoteReport{Sequence: 1, Events: []remoteexec.RemoteEvent{{Kind: remoteexec.EventAssistantMessage, Text: block.Text}}}
			if err := report.Validate(); err != nil {
				return err
			}
		case conversation.BlockTypeToolUse.String():
			var use struct {
				ID    conversation.CallID   `json:"id"`
				Name  conversation.ToolName `json:"name"`
				Input json.RawMessage       `json:"input"`
			}
			if json.Unmarshal(raw, &use) != nil {
				return errors.New("invalid interactive tool use")
			}
			parts := strings.Split(string(use.ID), "-")
			if len(parts) != 3 {
				return errors.New("interactive tool use lacks its canonical remote ID")
			}
			epoch, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil || epoch == 0 || !remoteexec.IsCallID(epoch, use.ID) || replay.seen[use.ID] || len(replay.pending) >= 4 {
				return errors.New("interactive tool IDs must be positive, unique and bounded to one batch")
			}
			arguments, err := conversation.ParseJSONObject(string(use.Input))
			if err != nil {
				return err
			}
			report := remoteexec.RemoteReport{Sequence: 1, ToolCall: &remoteexec.RemoteToolCall{Name: use.Name, Arguments: arguments}}
			if err := report.Validate(); err != nil {
				return err
			}
			if err := checkToolInput(agentloop.RemoteTools(), use.Name, use.Input); err != nil {
				return err
			}
			replay.seen[use.ID], replay.pending[use.ID] = true, true
		default:
			return errors.New("interactive assistant history has thinking or an unknown block")
		}
	}
	return nil
}

func (replay *interactiveReplay) user(message requestMessage) error {
	for _, raw := range message.Content {
		_, kind, err := interactiveFields(raw, true)
		if err != nil {
			return err
		}
		switch kind {
		case conversation.BlockTypeText.String():
			var block textJSON
			if json.Unmarshal(raw, &block) != nil || len(block.Text) > maximumRequestBytes || !utf8.ValidString(block.Text) {
				return errors.New("interactive user text is malformed or unbounded")
			}
			if strings.HasPrefix(block.Text, remoteexec.HandoffNoticePrefix) {
				if _, err := remoteexec.ParseHandoffNotice(block.Text); err != nil {
					return err
				}
			}
		case conversation.BlockTypeToolResult.String():
			var result struct {
				ID      conversation.CallID `json:"tool_use_id"`
				IsError bool                `json:"is_error"`
				Content json.RawMessage     `json:"content"`
			}
			if message.Role != string(conversation.EntryRoleUser) || json.Unmarshal(raw, &result) != nil || !replay.pending[result.ID] || len(result.Content) > 16<<20 {
				return errors.New("interactive result does not answer one exact pending remote tool")
			}
			var text string
			if json.Unmarshal(result.Content, &text) != nil {
				var blocks []json.RawMessage
				if json.Unmarshal(result.Content, &blocks) != nil || len(blocks) == 0 {
					return errors.New("interactive result lacks public content")
				}
				for _, block := range blocks {
					_, kind, err := interactiveFields(block, false)
					var item textJSON
					if err != nil || kind != conversation.BlockTypeText.String() || json.Unmarshal(block, &item) != nil || !utf8.ValidString(item.Text) {
						return errors.New("interactive result contains a non-public block")
					}
				}
			} else if !utf8.ValidString(text) {
				return errors.New("interactive result is not UTF-8")
			}
			delete(replay.pending, result.ID)
		default:
			return errors.New("interactive user history contains an unknown block")
		}
	}
	return nil
}

// Each public block has its exact known fields, with only the actual user cache anchor allowed.
func interactiveFields(raw json.RawMessage, user bool) (map[string]json.RawMessage, string, error) {
	var fields map[string]json.RawMessage
	var kind string
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(fields["type"], &kind) != nil {
		return nil, "", errors.New("interactive content is not a typed block")
	}
	allowed := []string{"type"}
	switch kind {
	case conversation.BlockTypeText.String():
		allowed = append(allowed, "text")
	case conversation.BlockTypeToolUse.String():
		allowed = append(allowed, "id", "name", "input")
	case conversation.BlockTypeToolResult.String():
		allowed = append(allowed, "tool_use_id", "content", "is_error")
	default:
		return nil, "", errors.New("interactive content contains thinking or an unknown block")
	}
	for _, field := range allowed {
		if _, found := fields[field]; !found && field != "is_error" {
			return nil, "", errors.New("interactive public block lacks a required field")
		}
	}
	if cache, found := fields["cache_control"]; found && user {
		var value map[string]string
		if json.Unmarshal(cache, &value) != nil || len(value) != 2 || value["type"] != "ephemeral" || value["ttl"] != "1h" {
			return nil, "", errors.New("interactive cache anchor is invalid")
		}
		allowed = append(allowed, "cache_control")
	}
	for field := range fields {
		if !slices.Contains(allowed, field) {
			return nil, "", errors.New("interactive public block contains an unknown field")
		}
	}
	return fields, kind, nil
}

// LongHandoffByteFillerForTest is the immutable public body imported through
// the real remote endpoint, 473..632 bytes rather than a near-limit payload.
func LongHandoffByteFillerForTest(index int) string {
	return fmt.Sprintf("Long remote entry %04d ", index) + strings.Repeat("x", 450+index%160)
}

// These assertions inspect the provider's actual chosen suffix. They neither
// call the production selector nor search for an expected selection boundary.
func (request messagesRequest) checkLongRemoteByteHandoff(selected selection) error {
	if selected.at.input != 0 || len(request.Messages) != 5 || request.Messages[0].Role != "user" || selected.arguments["task"] != "cmd-cccccccccccc" || selected.arguments["budget"] != "400000" {
		return errors.New("byte handoff lacks the exact user-first five-message boundary and current arguments")
	}
	// NewSegmentFor resets CacheAnchor to zero. System/top-level automatic
	// cache controls do not alter entry blocks; no content anchor is allowed.
	for _, message := range request.Messages {
		for _, raw := range message.Content {
			fields, _, err := interactiveFields(raw, message.Role == "user")
			if err != nil {
				return err
			}
			if _, found := fields["cache_control"]; found {
				return errors.New("first byte handoff request unexpectedly carries a content cache anchor")
			}
		}
	}
	first := request.Messages[0]
	texts := first.texts()
	if len(texts) < 2 || len(texts) >= 994 || len(texts) != len(first.Content) {
		return errors.New("byte handoff did not remove older fillers as whole entries")
	}
	var firstIndex int
	if _, err := fmt.Sscanf(texts[0], "Long remote entry %04d ", &firstIndex); err != nil || firstIndex <= 1 || firstIndex > 993 || len(texts) != 995-firstIndex {
		return errors.New("byte handoff does not start at a newest numbered filler suffix")
	}
	// The sender envelope is already frozen in UserMessage.Entry. Reuse
	// that encoding, and bind all reconstructed blocks to actual Dex digests.
	var textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(first.Content[0], &textBlock) != nil {
		return errors.New("byte handoff first filler is not a text block")
	}
	encodedEnvelope, found := strings.CutPrefix(textBlock.Text, conversation.SenderEnvelopePrefix)
	var envelope struct {
		Sender string  `json:"sender"`
		Via    *string `json:"via"`
		Text   string  `json:"text"`
	}
	if !found || json.Unmarshal([]byte(encodedEnvelope), &envelope) != nil || envelope.Sender == "" || envelope.Via == nil || *envelope.Via == "" {
		return errors.New("byte handoff first filler lost its immutable authenticated envelope")
	}
	actor := &conversation.Actor{Email: envelope.Sender}
	var fillers []json.RawMessage
	for index := 1; index <= 993; index++ {
		entry := (conversation.UserMessage{Content: LongHandoffByteFillerForTest(index), Actor: actor, Via: envelope.Via}).Entry()
		fillers = append(fillers, entry.Blocks[0])
		if index >= firstIndex && !bytes.Equal(first.Content[index-firstIndex], entry.Blocks[0]) {
			return errors.New("byte handoff changed, reordered or omitted an immutable numbered imported entry")
		}
	}
	tailInput := scripts["remote-long-tail"].describe().Inputs[0].Content
	tailEntry := (conversation.UserMessage{Content: tailInput, Actor: actor, Via: envelope.Via}).Entry()
	if !bytes.Equal(first.Content[len(first.Content)-1], tailEntry.Blocks[0]) {
		return errors.New("byte handoff altered the exact original remote-tail input or envelope")
	}
	for index, id := range []string{"remote-1-36", "remote-1-37"} {
		assistant, answer := request.Messages[1+index*2], request.Messages[2+index*2]
		var use struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		var result struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if assistant.Role != "assistant" || len(assistant.Content) != 1 || json.Unmarshal(assistant.Content[0], &use) != nil || use.Type != "tool_use" || use.ID != id || answer.Role != "user" || len(answer.Content) == 0 || json.Unmarshal(answer.Content[0], &result) != nil || result.Type != "tool_result" || result.ToolUseID != id || result.IsError {
			return errors.New("byte handoff lost a complete original chronological successful tool unit")
		}
	}
	if len(request.Messages[2].Content) != 1 || len(request.Messages[4].Content) != 3 {
		return errors.New("byte handoff contains an extra result, notice or triggering input")
	}
	last := request.Messages[4]
	var notice struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(last.Content[2], &notice) != nil || notice.Type != "text" {
		return errors.New("byte handoff lacks its separately bounded canonical notice")
	}
	if err := checkLongHandoffNotice(notice.Text, selected); err != nil {
		return err
	}
	if selected.arguments["summary"] == "none" {
		return errors.New("byte handoff requires its genuine pre-remote summary")
	}
	noticeEntry := conversation.NoticeEntry(conversation.EntryRoleUser, notice.Text, "")
	if !bytes.Equal(last.Content[2], noticeEntry.Blocks[0]) {
		return errors.New("byte handoff notice changed its immutable text-block encoding")
	}
	content := selected.script.describe().Inputs[0].Content
	for _, name := range []string{"budget", "bytecase", "candidate_bytes", "candidate_count", "candidate_digest", "head", "notice", "predecessors_count", "predecessors_digest", "summary", "task"} {
		content += " " + ArgumentMarkerForTest(name, selected.arguments[name])
	}
	current := conversation.UserMessageEntry(content, nil)
	if !bytes.Equal(last.Content[1], current.Blocks[0]) {
		return errors.New("byte handoff changed its exact pre-selection platform input or current arguments")
	}
	// The tail blocks are still under unchanged followReplay/sameBlocks and
	// result checks; the canonical digest also binds every byte of each unit.
	tail := []json.RawMessage{tailEntry.Blocks[0], request.Messages[1].Content[0], request.Messages[2].Content[0], request.Messages[3].Content[0], last.Content[0]}
	candidate := append(slices.Clone(fillers), tail...)
	candidateBytes, candidateDigest := longHandoffBlockAccounting(candidate)
	_, predecessorsDigest := longHandoffBlockAccounting(fillers)
	if selected.arguments["candidate_count"] != strconv.Itoa(len(candidate)) || selected.arguments["candidate_bytes"] != strconv.FormatInt(candidateBytes, 10) || selected.arguments["candidate_digest"] != candidateDigest || selected.arguments["predecessors_count"] != strconv.Itoa(len(fillers)) || selected.arguments["predecessors_digest"] != predecessorsDigest || candidateBytes <= 400000 {
		return errors.New("byte handoff candidate or immediate predecessor differs from canonical stored blocks, or candidate fits")
	}
	chosen := append(slices.Clone(first.Content), tail[1:]...)
	chosen = append(chosen, last.Content[1]) // AwaitUser stores current input before suffix selection.
	chosenBytes, _ := longHandoffBlockAccounting(chosen)
	predecessorBytes, _ := longHandoffBlockAccounting(fillers[firstIndex-2 : firstIndex-1])
	if chosenBytes > 400000 || chosenBytes+predecessorBytes <= 400000 {
		return errors.New("byte handoff actual suffix exceeds its immutable byte budget or is not maximal")
	}
	return nil
}

func longHandoffBlockAccounting(blocks []json.RawMessage) (int64, string) {
	digest := sha256.New()
	total := int64(0)
	for _, raw := range blocks {
		total += int64(len(raw))
		digest.Write(raw)
		digest.Write([]byte{'\n'})
	}
	return total, hex.EncodeToString(digest.Sum(nil))
}
