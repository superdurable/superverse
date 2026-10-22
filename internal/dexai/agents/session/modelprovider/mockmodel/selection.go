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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// The markers a conversation carries. Studio sends [[scenario:name#n]] with
// the text of input n of a scenario, a message or a request for changes, and
// [[scenario-arg:name=value]] for each argument; [[scenario:name]] is input 1.
// The mock compaction summary writes the scenario, the input and the reply it
// reached, marked by [[scenario-step:k]], and the arguments into the next
// segment's first message.
var (
	scenarioMarker = regexp.MustCompile(`\[\[scenario:([a-z0-9][a-z0-9-]*)(?:#([0-9]{1,4}))?\]\]`)
	// subagentMarker names the script of a subagent in its task, the prompt of
	// the start_agent call that started it.
	subagentMarker = regexp.MustCompile(`\[\[subagent:([a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*)\]\]`)
	stepMarker     = regexp.MustCompile(`\[\[scenario-step:([0-9]{1,6})\]\]`)
	argumentMarker = regexp.MustCompile(`\[\[scenario-arg:([a-z][a-z_]*)=([A-Za-z0-9 ._:/-]{1,128})\]\]`)
	argumentValue  = regexp.MustCompile(`^[A-Za-z0-9 ._:/-]{1,128}$`)
)

// WithoutMarkers is text without the markers the mock model reads, which are
// not the person's words.
func WithoutMarkers(text string) string {
	for _, marker := range []*regexp.Regexp{scenarioMarker, subagentMarker, stepMarker, argumentMarker} {
		text = marker.ReplaceAllString(text, " ")
	}
	return text
}

// defaultReply answers a conversation that names no scenario, so a person can
// still use Studio while the Session runs the mock model.
const defaultReply = "The mock model answers only scenario inputs: choose a scenario and send its next input."

// selection is the scenario in force at a point of a conversation and the
// reply the next assistant message stands for. It depends on the messages alone.
type selection struct {
	// script is nil while no user message has started a scenario.
	script    *script
	arguments map[string]string
	// at is the reply the next assistant message stands at: the last input
	// the scenario received and how many replies answered it so far.
	at position
	// previous is the position of the previous assistant message when that
	// message was a reply of the script, whose tool results the next user
	// message answers; nil otherwise.
	previous *position
	// stopped is the text every later reply of the scenario is once a tool of
	// its script failed; empty while the script runs.
	stopped string
	// rejected is the reply to a user input that was not the next one the
	// scenario expects. It holds until the next user message and changes
	// nothing else, so the scenario still waits for the input it expected.
	rejected string
	// segmentStart is the text of the request's first message, the
	// segment_start, which every reply of the request is checked against.
	segmentStart string
	// contextInStart reports whether the segment_start block holds the
	// repository context, and contextNotices counts the later user text
	// blocks that do, up to the reply.
	contextInStart bool
	contextNotices int
	// run names this run of the scenario: the segment and the position of the
	// message that started it, so a Session that runs scenarios one after
	// another, or one scenario again, never repeats a tool use ID.
	run string
}

// requestMessage is one message of a request.
type requestMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

// texts returns the text of every top-level text block of a message.
func (message requestMessage) texts() []string {
	var texts []string
	for _, raw := range message.Content {
		var header textJSON
		if json.Unmarshal(raw, &header) == nil && header.Type == conversation.BlockTypeText.String() {
			text := header.Text
			if strings.HasPrefix(text, conversation.SenderEnvelopePrefix) {
				var envelope struct {
					Sender string `json:"sender"`
					Text   string `json:"text"`
				}
				if json.Unmarshal([]byte(strings.TrimPrefix(text, conversation.SenderEnvelopePrefix)), &envelope) == nil {
					text = envelope.Text
				}
			}
			texts = append(texts, text)
		}
	}
	return texts
}

// markerTexts reads input markers without letting a fully validated canonical
// handoff's older summary select an input. Content remains untouched for exact
// assistant replay and handoff-boundary checks. Prefix-only or malformed notices
// remain ordinary input text; genuine compaction carry blocks remain unchanged.
func (message requestMessage) markerTexts() []string {
	var texts []string
	for _, raw := range message.Content {
		if canonicalHandoffBlock(raw) {
			continue
		}
		texts = append(texts, (requestMessage{Content: []json.RawMessage{raw}}).texts()...)
	}
	return texts
}

func canonicalHandoffBlock(raw json.RawMessage) bool {
	var text textJSON
	if json.Unmarshal(raw, &text) != nil || text.Type != conversation.BlockTypeText.String() {
		return false
	}
	if first, _, _ := strings.Cut(text.Text, "\n"); first == conversation.CredentialContextNotice && len(text.Text) <= remoteexec.MaximumHandoffBytes {
		return true
	}
	_, err := remoteexec.ParseHandoffNotice(text.Text)
	return err == nil
}

// walk reads a conversation in order. Each user message first settles the
// tool results of the reply before it: a tool result that is an error and
// answers a reply of the script stops the scenario unless the reply expects
// the failure, and its replies are the stop text from there on, until the
// scenario starts again. Then the message is an input when it carries a
// marker or answers the requirements card, the questions
// question; any other user message, such as a task notification or a
// continuation notice, continues the replies of the last input. onAssistant
// sees each assistant message with the selection it was produced under,
// before the selection moves on. walk returns the selection in force after
// the last message. An unknown scenario or a bad argument is an error.
func walk(messages []requestMessage, onAssistant func(requestMessage, selection) error) (selection, error) {
	current := selection{segmentStart: strings.Join(messages[0].texts(), "\n")}
	interactive := interactiveReplay{seen: map[conversation.CallID]bool{}, pending: map[conversation.CallID]bool{}}
	var assistant requestMessage
	for index, message := range messages {
		switch conversation.EntryRole(message.Role) {
		case conversation.EntryRoleUser:
			if current.script == nil {
				if err := interactive.user(message); err != nil {
					return selection{}, err
				}
			}
			current = current.settleTools(assistant, message)
			received, err := readInput(assistant, message)
			if err != nil {
				return selection{}, err
			}
			current = current.receive(received, index).withMessageArgumentsOf(message).countRepositoryContext(message, index)
			if err := current.checkHandoffBoundary(message); err != nil {
				return selection{}, err
			}
		case conversation.EntryRoleAssistant:
			assistant = message
			if current.script == nil {
				if err := interactive.assistant(message); err != nil {
					return selection{}, err
				}
			} else if onAssistant != nil {
				if err := onAssistant(message, current); err != nil {
					return selection{}, err
				}
			}
			current = current.replied()
		case conversation.EntryRoleSystem:
			if current.script == nil {
				if err := interactive.user(message); err != nil {
					return selection{}, err
				}
			}
			// A system message names no scenario and answers no step.
		}
	}
	if len(interactive.pending) != 0 {
		return selection{}, errors.New("interactive remote tool uses lack their complete result unit")
	}
	return current, nil
}

// countRepositoryContext records whether the segment_start, the first text
// block of the first message, holds the repository context, and counts the
// later user text blocks that hold it: the notices the Session appends.
func (sel selection) countRepositoryContext(message requestMessage, index int) selection {
	for position, text := range message.texts() {
		switch {
		case index == 0 && position == 0:
			sel.contextInStart = agentloop.HoldsRepositoryContext(text)
		case agentloop.HoldsRepositoryContext(text):
			sel.contextNotices++
		}
	}
	return sel
}

// repositoryContextStop is the stop text of a step that expects the
// repository context in exactly one notice after a segment_start without it,
// when the request carries it otherwise; "" when it may reply.
func (sel selection) repositoryContextStop() string {
	if step := sel.script.stepAt(sel.at); step == nil || !step.ExpectRepositoryContextNotice || !sel.contextInStart && sel.contextNotices == 1 {
		return ""
	}
	return fmt.Sprintf("Mock scenario %s stopped at %s: the repository context is in the segment_start (%t) and in %d notices, not in exactly one notice",
		sel.script.Name, sel.at, sel.contextInStart, sel.contextNotices)
}

// settleTools checks the tool results of answer against the reply of the
// script that made the calls.
func (sel selection) settleTools(assistant, answer requestMessage) selection {
	at := sel.previous
	sel.previous = nil
	if at == nil || sel.stopped != "" {
		return sel
	}
	tool, result, failed := failedTool(assistant, answer)
	want, contains := sel.script.expectedFailureText(*at)
	missing := slices.IndexFunc(contains, func(text string) bool { return !strings.Contains(result, text) })
	switch detail := firstLine(result); {
	case failed && !sel.script.expectsFailure(*at):
		sel.stopped = stopText(sel.script.Name, *at, tool, detail)
	case want != "" && !strings.Contains(detail, want):
		sel.stopped = fmt.Sprintf("Mock scenario %s stopped at %s: no tool failed with %q", sel.script.Name, *at, want)
	case missing >= 0:
		sel.stopped = fmt.Sprintf("Mock scenario %s stopped at %s: no failed tool result holds %q", sel.script.Name, *at, contains[missing])
	default:
		sel.stopped = sel.script.missingResultText(*at, allResults(assistant, answer))
	}
	return sel
}

// replied moves past an assistant message: a reply of the script makes the
// next message the input's next reply, and its tool results are settled with
// the next user message.
func (sel selection) replied() selection {
	if sel.script == nil || sel.rejected != "" || sel.stopped != "" {
		return sel
	}
	if sel.script.stepAt(sel.at) != nil {
		at := sel.at
		sel.previous = &at
	}
	sel.at.reply++
	return sel
}

// received is what one user message gives the scenario: nothing, an input or
// the carry-over of a compaction summary.
type received struct {
	kind InputKind
	// script and number name the input a marker names, from 1; an answer
	// names neither and is the next input of the scenario in force.
	script *script
	number int
	// arguments are the values the marker's message sets.
	arguments map[string]string
	// carry is the reply a compaction summary reached, with carried set.
	carry   int
	carried bool
}

// readInput reads what a user message gives the scenario. The newest text
// block with a scenario marker names its input: a message that follows a
// rejected model call also holds the input that call never answered. A marker
// followed by a step marker is a compaction summary's carry-over. An answer
// carries no marker: answers to questions are known by the call of the reply
// before them that they answer.
func readInput(assistant, message requestMessage) (received, error) {
	texts := message.markerTexts()
	text := strings.Join(texts, "\n")
	if match := subagentMarker.FindStringSubmatch(text); match != nil {
		// A subagent's task, or a compaction summary of its conversation.
		s, found := subagentScripts[match[1]]
		if !found {
			return received{}, fmt.Errorf("unknown subagent script %q", match[1])
		}
		in := received{kind: InputMessage, script: s, number: 1}
		if step := stepMarker.FindStringSubmatch(text); step != nil {
			in.carry, in.carried = comm.Must(strconv.Atoi(step[1])), true
		}
		return in, nil
	}
	if block, found := newestScenarioBlock(texts); found {
		match := scenarioMarker.FindStringSubmatch(block)
		s, found := scripts[match[1]]
		if !found {
			return received{}, fmt.Errorf("unknown scenario %q", match[1])
		}
		in := received{kind: InputMessage, script: s, number: 1}
		if match[2] != "" {
			in.number = comm.Must(strconv.Atoi(match[2]))
		}
		arguments, err := markerArguments(s, block)
		if err != nil {
			return received{}, err
		}
		in.arguments = arguments
		if step := stepMarker.FindStringSubmatch(text); step != nil {
			in.carry, in.carried = comm.Must(strconv.Atoi(step[1])), true
		}
		return in, nil
	}
	// A notification is never a user's input.
	userTexts := slices.DeleteFunc(slices.Clone(texts), func(text string) bool { return strings.HasPrefix(text, conversation.NotificationPreamble) })
	switch {
	case len(userTexts) == 0:
		return received{}, nil
	case answersCall(assistant, message, conversation.ToolRequestUserInput) && strings.HasPrefix(userTexts[0], "**"):
		return received{kind: InputAnswers}, nil
	}
	return received{}, nil
}

// markerArguments reads the [[scenario-arg:name=value]] markers of a text;
// the first value of a name wins.
func markerArguments(s *script, text string) (map[string]string, error) {
	markers := argumentMarker.FindAllStringSubmatch(text, -1)
	if strings.Count(text, "[[scenario-arg:") != len(markers) {
		return nil, errors.New("a [[scenario-arg:name=value]] marker is malformed; a value holds letters, digits, space and ._:/- up to 128 characters")
	}
	arguments := map[string]string{}
	for _, marker := range markers {
		if _, declared := s.Arguments[marker[1]]; !declared {
			return nil, fmt.Errorf("scenario %s has no argument %q", s.Name, marker[1])
		}
		if _, seen := arguments[marker[1]]; !seen {
			arguments[marker[1]] = marker[2]
		}
	}
	return arguments, nil
}

// newestScenarioBlock is the last text block that holds a scenario marker.
func newestScenarioBlock(texts []string) (string, bool) {
	for _, block := range slices.Backward(texts) {
		if scenarioMarker.MatchString(block) {
			return block, true
		}
	}
	return "", false
}

// answersCall reports whether answer holds the result of a call of tool that
// the assistant message before it made.
func answersCall(assistant, answer requestMessage, tool conversation.ToolName) bool {
	calls := map[string]bool{}
	for _, raw := range assistant.Content {
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolUse.String() && block.Name == string(tool) {
			calls[block.ID] = true
		}
	}
	return slices.ContainsFunc(answer.Content, func(raw json.RawMessage) bool {
		var block struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
		}
		return json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolResult.String() && calls[block.ToolUseID]
	})
}

// receive applies what a user message gives. An input must be exactly the
// next one: input 1 of any scenario starts it (again), any other input must
// follow the last input the scenario in force received, in the kind the
// scenario declares. Any other input is answered with a text that names the
// input the scenario expects, and the selection stays where it was.
func (sel selection) receive(in received, index int) selection {
	sel.rejected = ""
	if in.carried {
		return selection{script: in.script, arguments: withDefaults(in.script, in.arguments), at: position{input: in.number - 1, reply: in.carry},
			segmentStart: sel.segmentStart, run: runOf(sel.segmentStart, index)}
	}
	s, number := in.script, in.number
	switch {
	case in.kind == "":
		return sel
	case s == nil && sel.script == nil:
		// An answer outside any scenario continues the default replies.
		return sel
	case s == nil:
		s, number = sel.script, sel.at.input+2
	}
	if number < 1 || number > len(s.Inputs) || number > 1 && (sel.script != s || number != sel.at.input+2) {
		sel.rejected = sel.outOfOrder(s, number)
		return sel
	}
	// A marker names its input, so any text the user writes may be it.
	expected := s.Inputs[number-1]
	matches := expected.kind() == in.kind || in.script != nil && expected.kind().carriesMarker()
	if !matches {
		sel.rejected = fmt.Sprintf("Mock scenario %s: input %d (%s) is %s, not %s.", s.Name, number, expected.Label, describe(expected), describeReceived(in))
		return sel
	}
	if number == 1 {
		return selection{script: s, arguments: withDefaults(s, in.arguments), segmentStart: sel.segmentStart, run: runOf(sel.segmentStart, index)}
	}
	sel.arguments = maps.Clone(sel.arguments)
	maps.Copy(sel.arguments, in.arguments)
	sel.at = position{input: number - 1}
	return sel
}

// withDefaults is the script's argument defaults with the given values.
func withDefaults(s *script, values map[string]string) map[string]string {
	arguments := maps.Clone(s.Arguments)
	if arguments == nil {
		arguments = map[string]string{}
	}
	maps.Copy(arguments, values)
	return arguments
}

// outOfOrder is the reply to input number of s when s expects another one.
func (sel selection) outOfOrder(s *script, number int) string {
	next := 1
	if sel.script == s {
		next = sel.at.input + 2
	}
	if next > len(s.Inputs) {
		return fmt.Sprintf("Mock scenario %s: input %d is out of order; the scenario received all its inputs, so send input 1 to start it again.", s.Name, number)
	}
	return fmt.Sprintf("Mock scenario %s: input %d is out of order; send input %d first: %s.", s.Name, number, next, s.Inputs[next-1].Label)
}

// describe says what an input is, for a rejection.
func describe(in input) string {
	return describeKind(in.kind())
}

// describeReceived says what a user message gave, for a rejection.
func describeReceived(in received) string {
	return describeKind(in.kind)
}

func describeKind(kind InputKind) string {
	switch kind {
	case InputAnswers:
		return "answers to the questions"
	default:
		return "a message"
	}
}

// messageArguments are the script arguments that a message sets, each to the
// last match of its pattern: task, the task ID of a background command or of a
// subagent in a tool result or a notification, and release_candidate, the
// version of the connector release candidate a notification names. Both
// depend on the Session or the library, so no script can name them; the
// pattern's first group, when it has one, is the value.
var messageArguments = map[string]*regexp.Regexp{
	"task":              regexp.MustCompile(`cmd-[a-z2-7]{12}|agent-[a-z0-9]{8}`),
	"release_candidate": regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*`),
}

// withMessageArgumentsOf sets each message argument a script declares to the
// last value the message holds, so that a later step reads or stops the task a
// command started, or uses the candidate a contribution published.
func (sel selection) withMessageArgumentsOf(message requestMessage) selection {
	if sel.script == nil {
		return sel
	}
	for name, pattern := range messageArguments {
		if _, declared := sel.script.Arguments[name]; !declared {
			continue
		}
		var last string
		for _, raw := range message.Content {
			if canonicalHandoffBlock(raw) {
				continue
			}
			for _, found := range pattern.FindAllSubmatch(raw, -1) {
				last = string(found[len(found)-1])
			}
		}
		if last != "" {
			sel.arguments = maps.Clone(sel.arguments)
			sel.arguments[name] = last
		}
	}
	return sel
}

// scriptStep is the step of the script the next reply stands at; nil when the
// reply is a rejection, a stop text or outside a script.
func (sel selection) scriptStep() *step {
	if sel.script == nil || sel.rejected != "" || sel.stopped != "" {
		return nil
	}
	return sel.script.stepAt(sel.at)
}

// nextReply is the reply the selection stands at.
func (sel selection) nextReply() (reply, error) {
	switch {
	case sel.rejected != "":
		return textReply(sel.rejected), nil
	case sel.stopped != "":
		return textReply(sel.stopped), nil
	case sel.script == nil:
		return textReply(defaultReply), nil
	}
	if stop := sel.script.segmentStartMismatch(sel.at, sel.arguments, sel.segmentStart); stop != "" {
		return textReply(stop), nil
	}
	if stop := sel.repositoryContextStop(); stop != "" {
		return textReply(stop), nil
	}
	return sel.script.replyAt(sel.at, sel.arguments, sel.run)
}

// runOf names the run of a scenario that the message at index of the segment
// that starts with segmentStart began.
func runOf(segmentStart string, index int) string {
	sum := sha256.Sum256([]byte(segmentStart))
	return hex.EncodeToString(sum[:4]) + "_" + strconv.Itoa(index)
}

// missingNotification is why the next reply cannot be given: it answers a
// notification of a kind that the newest user message does not hold; "" when
// it can. A scenario that declares a notification therefore proves it arrived.
func (sel selection) missingNotification(messages []requestMessage) string {
	if sel.script == nil || sel.stopped != "" || sel.rejected != "" {
		return ""
	}
	step := sel.script.stepAt(sel.at)
	if step == nil || step.Notification == "" {
		return ""
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if conversation.EntryRole(messages[index].Role) != conversation.EntryRoleUser {
			continue
		}
		for _, text := range messages[index].texts() {
			var notification struct {
				Kind conversation.NotificationKind `json:"kind"`
			}
			body, isNotification := strings.CutPrefix(text, conversation.NotificationPreamble)
			if isNotification && json.Unmarshal([]byte(body), &notification) == nil && notification.Kind == step.Notification {
				return ""
			}
		}
		break
	}
	return fmt.Sprintf("mock scenario %s at %s answers a %s notification, which the newest user message does not hold", sel.script.Name, sel.at, step.Notification)
}

// progress is where the next reply leaves the scenario: the input it
// answers and the input the scenario takes next, 0 after the last; nil
// outside a scenario. A rejected input leaves it where it was.
func (sel selection) progress() *conversation.MockProgress {
	if sel.script == nil {
		return nil
	}
	next := sel.at.input + 2
	if next > len(sel.script.Inputs) {
		next = 0
	}
	return &conversation.MockProgress{Scenario: sel.script.Name, Input: sel.at.input + 1, NextInput: next}
}

// isCompaction reports whether the request ends with the compaction
// instruction, as a system message or, for a model without those, as a
// reminder in the last user message.
func isCompaction(messages []requestMessage) bool {
	last := messages[len(messages)-1]
	for _, text := range last.texts() {
		if strings.Contains(text, conversation.CompactionInstruction) {
			return true
		}
	}
	return false
}

// compactionFailure is the error that answers the compaction request the
// selection stands at instead of the summary: the one the step of the reply
// before the request declares. It is nil for a request answered with the
// summary, and outside a running scenario.
func (sel selection) compactionFailure() *stepError {
	if sel.script == nil || sel.stopped != "" || sel.rejected != "" {
		return nil
	}
	if step := sel.script.stepAt(position{input: sel.at.input, reply: sel.at.reply - 1}); step != nil {
		return step.CompactionError
	}
	return nil
}

// compactionSummary answers a compaction request: the scenario in force, the
// input and the reply it reached and its arguments, so the next segment
// continues the script, then the summary.
func (sel selection) compactionSummary() string {
	if sel.script == nil {
		return "<summary>The conversation named no mock scenario.</summary>"
	}
	var summary strings.Builder
	if sel.script.isSubagent() {
		fmt.Fprintf(&summary, "<summary>[[subagent:%s]] [[scenario-step:%d]]", sel.script.Name, sel.at.reply)
	} else {
		fmt.Fprintf(&summary, "<summary>[[scenario:%s#%d]] [[scenario-step:%d]]", sel.script.Name, sel.at.input+1, sel.at.reply)
	}
	for _, name := range slices.Sorted(maps.Keys(sel.arguments)) {
		fmt.Fprintf(&summary, " [[scenario-arg:%s=%s]]", name, sel.arguments[name])
	}
	text := sel.script.CompactionSummary
	if text == "" {
		text = "Mock summary of the conversation so far."
	}
	return summary.String() + " " + text + "</summary>"
}
