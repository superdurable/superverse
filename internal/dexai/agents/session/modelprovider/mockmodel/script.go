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
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

//go:embed scenarios/*.yaml scenarios/attachments
var scenarioFiles embed.FS

// AttachmentFixtureForTest is attachmentFixture for the Dex tests, which
// stage the files a scenario input attaches.
func AttachmentFixtureForTest(name string) ([]byte, error) { return attachmentFixture(name) }

// attachmentFixture is the bytes of a fixture file a scenario input attaches,
// under scenarios/attachments/.
func attachmentFixture(name string) ([]byte, error) {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return nil, fmt.Errorf("attachment fixture %q is no file name", name)
	}
	data, err := scenarioFiles.ReadFile(path.Join("scenarios", "attachments", name))
	if err != nil {
		return nil, fmt.Errorf("attachment fixture %q: %w", name, err)
	}
	return data, nil
}

// script is one scenario: the user inputs it expects, in order, and the
// replies the model gives to each.
type script struct {
	Name string `json:"name"`
	// Title and Description are what Studio lists.
	Title       string `json:"title"`
	Description string `json:"description"`
	// Category is the group Studio lists the scenario under.
	Category ScenarioCategory `json:"category"`
	// TestAction is what the scenario's Dex test does between its inputs and a
	// person in Studio cannot, such as removing the Session's instance; empty
	// when Studio can run the scenario.
	TestAction string `json:"test_action"`
	// Arguments are the placeholders {{name}} a script may use and their
	// defaults; a request overrides one with [[scenario-arg:name=value]].
	Arguments map[string]string `json:"arguments"`
	Inputs    []input           `json:"inputs"`
	// CompactionSummary is the summary a compaction request is answered with.
	CompactionSummary string `json:"compaction_summary"`
	// Workspace is the example the scenario's Project was created from, whose
	// composed files its file tools play on; empty for the default example.
	Workspace string `json:"workspace"`
	// Start is how the scenario's Project starts from its first message.
	Start scenarioStart `json:"start"`
	// Subagents are the scripts of the subagents the scenario's start_agent
	// calls start, by name; a call's prompt names one with
	// [[subagent:<scenario>/<name>]], and the subagent's model calls get its
	// replies in order.
	Subagents map[string]subagentScript `json:"subagents"`
	// subagentKind is the kind of a subagent's script, which runs with that
	// kind's tools; the zero kind for a scenario.
	subagentKind agenttasks.SubagentKind
}

// scenarioStart is how a scenario's Project starts, a closed set: empty, from
// the default example, or from its workspace example when a person opens it,
// or startMatched, from the example the server matches to the scenario's first
// message, which must be its workspace.
type scenarioStart string

const startMatched scenarioStart = "matched"

// subagentScript is the script of one subagent: its kind and the replies of
// its model calls, the last of which is its report.
type subagentScript struct {
	Kind    agenttasks.SubagentKind `json:"kind"`
	Replies []step                  `json:"replies"`
}

// isSubagent reports whether the script is a subagent's.
func (s *script) isSubagent() bool { return s.subagentKind != agenttasks.SubagentKind{} }

// input is one user input with the replies that answer it. It sets exactly
// one kind: message or answers.
type input struct {
	Label   string   `json:"label"`
	Message string   `json:"message"`
	Answers []string `json:"answers"`
	// Attachments are the fixture files under scenarios/attachments/ that a
	// message input attaches, in order.
	Attachments []string `json:"attachments"`
	Replies     []step   `json:"replies"`
}

// InputKind is how a user input reaches the Session.
type InputKind string

const (
	// InputMessage is a message the user sends, a change to the requirements
	// card too.
	InputMessage InputKind = "message"
	// InputAnswers answers the questions of request_user_input, in order.
	InputAnswers InputKind = "answers"
)

// carriesMarker reports whether an input of the kind is text the user writes,
// which carries the scenario marker; answers are recognized by the call they
// answer.
func (kind InputKind) carriesMarker() bool {
	return kind == InputMessage
}

// kind is the input's kind; validate requires exactly one.
func (in input) kind() InputKind {
	switch {
	case in.Message != "":
		return InputMessage
	case len(in.Answers) > 0:
		return InputAnswers
	default:
		return InputAnswers
	}
}

// step is one model reply, or one rejected request when Error is set.
type step struct {
	ProtocolFault protocolFault `json:"protocol_fault"`
	// Remote declares exact externally reported blocks, never a provider reply.
	Remote bool `json:"remote"`
	// ExpectHandoff asserts the canonical notice at this scripted user boundary.
	ExpectHandoff bool                    `json:"expect_handoff"`
	Thinking      string                  `json:"thinking"`
	Text          string                  `json:"text"`
	ToolUses      []toolUse               `json:"tool_uses"`
	Usage         *stepUsage              `json:"usage"`
	StopReason    conversation.StopReason `json:"stop_reason"`
	ChunkDelayMS  int                     `json:"chunk_delay_ms"`
	Error         *stepError              `json:"error"`
	// Repeat gives the same reply this many times in a row; 0 and 1 give it once.
	Repeat int `json:"repeat"`
	// Notification is the kind of the notification this reply answers, such as
	// task for the automatic turn a background command's exit opens. The newest
	// user message of the request must hold one; otherwise the call is rejected.
	Notification conversation.NotificationKind `json:"notification"`
	// ExpectFailure lets the scenario continue when a tool of this step fails;
	// without it a failed tool stops the scenario.
	ExpectFailure bool `json:"expect_failure"`
	// InvalidToolArguments deliberately tests correction through the real tool boundary.
	InvalidToolArguments bool `json:"invalid_tool_arguments"`
	// ExpectFailureText, with ExpectFailure, requires a tool of this step to fail
	// with a result whose first line holds it; otherwise the scenario stops.
	ExpectFailureText string `json:"expect_failure_text"`
	// ExpectFailureContains, with ExpectFailure, lists texts that the result of
	// the failed tool must hold anywhere, such as the end of a command's output;
	// otherwise the scenario stops.
	ExpectFailureContains []string `json:"expect_failure_contains"`
	// ExpectResultContains lists texts that the results of this step's tools
	// must hold together, failed or not, such as a task's output; otherwise the
	// scenario stops.
	ExpectResultContains []string `json:"expect_result_contains"`
	// ExpectSegmentStart lists texts that the first message of the request, the
	// segment_start of the context segment, must hold. A step after a
	// compaction uses it to prove what the new segment carries; a request
	// without one of them is answered with a stop text.
	ExpectSegmentStart []string `json:"expect_segment_start"`
	// ExpectSegmentStartLacks lists texts that segment_start must not hold,
	// such as a section a segment no longer carries; a request that holds one
	// is answered with a stop text.
	ExpectSegmentStartLacks []string `json:"expect_segment_start_lacks"`
	// ExpectRepositoryContextNotice requires the repository context in exactly
	// one notice after the segment_start, which lacks it: the Session opened
	// the segment before its workspace was ready and appended the context once.
	// A request without it is answered with a stop text.
	ExpectRepositoryContextNotice bool `json:"expect_repository_context_notice"`
	// ExpectSenderEnvelope checks the actual request block, including a forged nested envelope.
	ExpectSenderEnvelope bool `json:"expect_sender_envelope"`
	// ExpectPendingRequestBudget checks the real compacted multipart quote.
	ExpectPendingRequestBudget bool `json:"expect_pending_request_budget"`
	// ExpectAttachments lists attachments the request must show, each in the
	// segment_start or the newest user message, with an envelope there that
	// lists it (checkAttachments); otherwise the call is rejected.
	ExpectAttachments []expectedAttachment `json:"expect_attachments"`
	// CompactionError answers the compaction request that follows this reply
	// with an error instead of the summary, such as the overload that Dex
	// retries until the Session gives up the compaction.
	CompactionError *stepError `json:"compaction_error"`
}

// position is where a reply stands: the index of its input and its index
// among the input's replies, both from 0.
type position struct{ input, reply int }

// String names the position as a person counts it.
func (at position) String() string {
	return fmt.Sprintf("input %d, reply %d", at.input+1, at.reply+1)
}

// stepAt is the step at a position, or nil past the input's replies.
func (s *script) stepAt(at position) *step {
	if at.input < 0 || at.input >= len(s.Inputs) || at.reply < 0 || at.reply >= len(s.Inputs[at.input].Replies) {
		return nil
	}
	return &s.Inputs[at.input].Replies[at.reply]
}

// expectsFailure reports whether the step at a position lets the script go on
// after a failed tool.
func (s *script) expectsFailure(at position) bool {
	step := s.stepAt(at)
	return step != nil && step.ExpectFailure
}

// expectedFailureText is the text the first line of a failed tool's result of
// the step at a position must hold, or "", and the texts the whole result must hold.
func (s *script) expectedFailureText(at position) (string, []string) {
	step := s.stepAt(at)
	if step == nil {
		return "", nil
	}
	return step.ExpectFailureText, step.ExpectFailureContains
}

// missingResultText is the stop text of the step at a position when the
// results of its tools lack a text it expects, or "".
func (s *script) missingResultText(at position, results string) string {
	step := s.stepAt(at)
	if step == nil {
		return ""
	}
	for _, expected := range step.ExpectResultContains {
		if !strings.Contains(results, expected) {
			return fmt.Sprintf("Mock scenario %s stopped at %s: no tool result holds %q", s.Name, at, expected)
		}
	}
	return ""
}

type toolUse struct {
	ID    conversation.CallID   `json:"id"`
	Name  conversation.ToolName `json:"name"`
	Input json.RawMessage       `json:"input"`
}

// stepUsage is the token usage the reply reports, which drives compaction.
type stepUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// stepError answers the call with an Anthropic error instead of a reply.
type stepError struct {
	Status    int       `json:"status"`
	Type      errorType `json:"type"`
	Message   string    `json:"message"`
	ErrorCode string    `json:"error_code"`
}

// validate requires an HTTP error status and an Anthropic error class.
func (failure stepError) validate() error {
	if failure.Status < 400 || failure.Status > 599 || !slices.Contains(errorTypes, failure.Type) {
		return errors.New("an error needs an HTTP status of 400 to 599 and an Anthropic error type")
	}
	if failure.ErrorCode != "" && (failure.ErrorCode != "enforced_spend_limit_reached" || failure.Type != rateLimitError) {
		return errors.New("an error_code must be the rate limit's enforced spend limit")
	}
	return nil
}

// errorType is an Anthropic error class, as the API spells it.
type errorType string

const (
	invalidRequestError errorType = "invalid_request_error"
	authenticationError errorType = "authentication_error"
	billingError        errorType = "billing_error"
	permissionError     errorType = "permission_error"
	notFoundError       errorType = "not_found_error"
	requestTooLarge     errorType = "request_too_large"
	rateLimitError      errorType = "rate_limit_error"
	apiError            errorType = "api_error"
	overloadedError     errorType = "overloaded_error"
)

// errorTypes are the error classes a step may answer with: every one.
var errorTypes = []errorType{invalidRequestError, authenticationError, billingError, permissionError, notFoundError, requestTooLarge, rateLimitError, apiError, overloadedError}

// maximumRepeat bounds one step's repetitions.
const maximumRepeat = 1000

var (
	scenarioName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	placeholder  = regexp.MustCompile(`\{\{([a-z][a-z_]*)\}\}`)
)

// scripts holds every embedded scenario by name, and subagentScripts the
// scripts of their subagents by <scenario>/<name>. The scripts are compiled
// into the binary, so a broken one stops the process at startup.
var scripts, subagentScripts = func() (map[string]*script, map[string]*script) {
	loaded := comm.Must(loadScripts())
	return loaded, comm.Must(loadSubagentScripts(loaded))
}()

// scenarioNames lists the embedded scenarios in name order.
func scenarioNames() []string { return slices.Sorted(maps.Keys(scripts)) }

func loadScripts() (map[string]*script, error) {
	entries, err := scenarioFiles.ReadDir("scenarios")
	if err != nil {
		return nil, err
	}
	loaded := make(map[string]*script, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		file := path.Join("scenarios", entry.Name())
		s, err := parseScript(comm.Must(scenarioFiles.ReadFile(file)))
		if err == nil && s.Name != strings.TrimSuffix(entry.Name(), ".yaml") {
			err = fmt.Errorf("name %q must equal the file name", s.Name)
		}
		if err == nil {
			err = s.expandRepeats()
		}
		if err == nil {
			err = s.validate()
		}
		if err != nil {
			return nil, fmt.Errorf("scenario %s: %w", file, err)
		}
		loaded[s.Name] = s
	}
	return loaded, nil
}

// loadSubagentScripts builds and validates the script of every subagent of
// the scenarios: one input, the subagent's task, answered by its replies.
func loadSubagentScripts(loaded map[string]*script) (map[string]*script, error) {
	subagents := map[string]*script{}
	for _, name := range slices.Sorted(maps.Keys(loaded)) {
		parent := loaded[name]
		for _, sub := range slices.Sorted(maps.Keys(parent.Subagents)) {
			definition := parent.Subagents[sub]
			s := &script{Name: parent.Name + "/" + sub, Title: parent.Title, Description: parent.Description, Category: parent.Category, Arguments: parent.Arguments,
				Inputs: []input{{Label: "The subagent's task", Message: "The task its start_agent call names.", Replies: slices.Clone(definition.Replies)}}, subagentKind: definition.Kind}
			err := s.expandRepeats()
			if err == nil {
				err = s.validate()
			}
			if err != nil {
				return nil, fmt.Errorf("scenario %s, subagent %s: %w", parent.Name, sub, err)
			}
			subagents[s.Name] = s
		}
	}
	return subagents, nil
}

// parseScript decodes one YAML script, refusing unknown fields.
func parseScript(data []byte) (*script, error) {
	encoded, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var s script
	if err = decoder.Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// expandRepeats replaces each repeated step by its copies, which the
// validation then checks one by one.
func (s *script) expandRepeats() error {
	for index := range s.Inputs {
		var replies []step
		for _, reply := range s.Inputs[index].Replies {
			if reply.Repeat < 0 || reply.Repeat > maximumRepeat {
				return fmt.Errorf("input %d: repeat must be 0 to %d", index+1, maximumRepeat)
			}
			for range max(reply.Repeat, 1) {
				replies = append(replies, reply)
			}
		}
		s.Inputs[index].Replies = replies
	}
	return nil
}

// validate checks a script against the Session tool catalog and the
// application template its edits start from.
func (s *script) validate() error {
	if !s.isSubagent() && !scenarioName.MatchString(s.Name) || strings.TrimSpace(s.Title) == "" || strings.TrimSpace(s.Description) == "" || s.Category == (ScenarioCategory{}) || len(s.Inputs) == 0 {
		return errors.New("a scenario needs a lower-case name, a title, a description, a category and at least one input")
	}
	if err := s.validateSubagents(); err != nil {
		return err
	}
	if err := s.validateStart(); err != nil {
		return err
	}
	for name, value := range s.Arguments {
		if !argumentValue.MatchString(value) {
			return fmt.Errorf("argument %q default %q is not a plain value", name, value)
		}
	}
	// The script's file tools play on a copy of the template, in order, so that
	// an edit may build on an earlier edit or write of the same script; a
	// connector_develop subagent's play on the files it writes into its
	// library worktree.
	files, err := templateFiles(s.Workspace)
	if err != nil {
		return err
	}
	remoteIDs := map[conversation.CallID]bool{}
	if s.isSubagent() && s.subagentKind.ContributesConnector() {
		files = rootFiles{files: map[string]string{}}
	}
	for index, in := range s.Inputs {
		if err := s.validateInput(index, in); err != nil {
			return fmt.Errorf("input %d: %w", index+1, err)
		}
		for reply, step := range in.Replies {
			for _, use := range step.ToolUses {
				if use.ID != "" && remoteIDs[use.ID] {
					return fmt.Errorf("remote tool ID %s is declared twice", use.ID)
				}
				if use.ID != "" {
					remoteIDs[use.ID] = true
				}
			}
			if err := s.validateStep(step, files); err != nil {
				return fmt.Errorf("input %d, reply %d: %w", index+1, reply+1, err)
			}
		}
	}
	return nil
}

// validateStart checks the scenario's start against the server's
// closest-example decision on its first message, which CreateProject makes
// when agent.closestExamplePick is on: a matched scenario's first message
// picks its workspace, and that of a scenario with neither a start nor a
// workspace starts from the default, so that the Project its first message
// creates starts from the tree its file tools play on. A subagent's script is
// sent at no creation.
func (s *script) validateStart() error {
	if s.isSubagent() {
		return nil
	}
	match, isMatch := apptemplate.ClosestExample(s.Inputs[0].Message)
	switch s.Start {
	case startMatched:
		if s.Workspace == "" || !isMatch || match.ExampleID != s.Workspace {
			return fmt.Errorf("start %s needs a workspace that the server matches to the first message, which matches %q", startMatched, match.ExampleID)
		}
	case "":
		if s.Workspace == "" && isMatch {
			return fmt.Errorf("the first message starts a Project from the example %s, not the default its file tools play on; reword it, or declare start: %s with that workspace", match.ExampleID, startMatched)
		}
	default:
		return fmt.Errorf("start %q is not %s", s.Start, startMatched)
	}
	return nil
}

// validateInput checks that an input has a label, one kind and replies, and
// that an answer follows the reply that asks for it.
func (s *script) validateInput(index int, in input) error {
	kinds := 0
	for _, set := range []bool{in.Message != "", len(in.Answers) > 0} {
		if set {
			kinds++
		}
	}
	switch {
	case strings.TrimSpace(in.Label) == "" || kinds != 1 || len(in.Replies) == 0:
		return errors.New("an input needs a label, exactly one of message and answers, and replies")
	case index == 0 && in.kind() != InputMessage:
		return errors.New("a scenario starts with a message")
	case slices.ContainsFunc(in.Answers, func(answer string) bool { return strings.TrimSpace(answer) == "" }):
		return errors.New("answers must not be blank")
	case strings.Contains(in.Message, "[["):
		return errors.New("the text of an input carries no marker; Studio adds it")
	case len(in.Attachments) > 0 && in.kind() != InputMessage, len(in.Attachments) > conversation.MaximumMessageAttachments:
		return errors.New("only a message input attaches files, at most ten")
	}
	for _, name := range in.Attachments {
		if _, err := attachmentFixture(name); err != nil {
			return err
		}
	}
	if in.kind() != InputAnswers {
		return nil
	}
	previous := s.Inputs[index-1].Replies
	if len(previous) == 0 {
		return errors.New("an answer follows an input with replies")
	}
	uses := previous[len(previous)-1].ToolUses
	if len(uses) == 0 || uses[len(uses)-1].Name != conversation.ToolRequestUserInput {
		return fmt.Errorf("an input of kind %s follows a reply whose last tool call is %s", in.kind(), conversation.ToolRequestUserInput)
	}
	var questions struct {
		Questions []json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(uses[len(uses)-1].Input, &questions); err != nil || len(questions.Questions) != len(in.Answers) {
		return errors.New("answers give one answer per question of the request_user_input call before them")
	}
	return nil
}

func (s *script) validateStep(step step, files rootFiles) error {
	if !slices.Contains(protocolFaults, step.ProtocolFault) {
		return errors.New("unknown protocol fault")
	}
	if step.Remote && (step.ProtocolFault != faultNone || s.isSubagent() || step.Thinking != "" || step.Error != nil || step.Usage != nil || step.ChunkDelayMS != 0 || step.CompactionError != nil || step.ExpectHandoff || step.Repeat > 1 || step.StopReason != "" || len(step.ToolUses) > 4 || len(step.Text) > 64<<10 || len(step.ExpectSegmentStart) > 0 || len(step.ExpectSegmentStartLacks) > 0 || step.ExpectRepositoryContextNotice || step.ExpectSenderEnvelope || step.ExpectPendingRequestBudget || len(step.ExpectAttachments) > 0) {
		return errors.New("a remote step has only exact public text/tool uses and never provider thinking, timing, failure, compaction or handoff expectations")
	}
	for _, use := range step.ToolUses {
		if !step.Remote && use.ID != "" {
			return errors.New("only remote steps declare exact tool IDs")
		}
		if step.Remote {
			parts := strings.Split(string(use.ID), "-")
			if len(parts) != 3 {
				return errors.New("a remote tool needs its exact canonical ID")
			}
			epoch, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil || epoch == 0 || !remoteexec.IsCallID(epoch, use.ID) || !slices.Contains(agentloop.RemoteToolNames(), use.Name) {
				return errors.New("a remote tool needs its exact positive ID and a remotely offered tool")
			}
		}
	}

	if step.ChunkDelayMS < 0 || step.ChunkDelayMS > 1000 {
		return errors.New("chunk_delay_ms must be 0 to 1000")
	}
	if step.Error != nil {
		if err := step.Error.validate(); err != nil {
			return err
		}
		if step.Thinking != "" || step.Text != "" || len(step.ToolUses) > 0 || step.Usage != nil || step.StopReason != "" || step.CompactionError != nil {
			return errors.New("a step with an error carries nothing else")
		}
		return nil
	}
	if step.Thinking == "" && step.Text == "" && len(step.ToolUses) == 0 {
		return errors.New("a reply needs thinking, text or a tool use")
	}
	if step.CompactionError != nil {
		if err := step.CompactionError.validate(); err != nil {
			return fmt.Errorf("compaction_error: %w", err)
		}
	}
	if step.Notification != "" {
		if err := step.Notification.Validate(); err != nil {
			return err
		}
	}
	for _, expected := range step.ExpectAttachments {
		if err := expected.validate(); err != nil {
			return err
		}
	}
	if step.ExpectFailure && len(step.ToolUses) == 0 {
		return errors.New("expect_failure needs tool uses")
	}
	if step.ExpectFailureText != "" && !step.ExpectFailure {
		return errors.New("expect_failure_text needs expect_failure")
	}
	if len(step.ExpectFailureContains) > 0 && !step.ExpectFailure {
		return errors.New("expect_failure_contains needs expect_failure")
	}
	if slices.Contains(step.ExpectFailureContains, "") {
		return errors.New("expect_failure_contains must not hold an empty text")
	}
	if len(step.ExpectResultContains) > 0 && len(step.ToolUses) == 0 || slices.Contains(step.ExpectResultContains, "") {
		return errors.New("expect_result_contains needs tool uses and no empty text")
	}
	for _, expected := range step.ExpectSegmentStart {
		if expected == "" {
			return errors.New("expect_segment_start must not hold an empty text")
		}
		for _, match := range placeholder.FindAllStringSubmatch(expected, -1) {
			if _, declared := s.Arguments[match[1]]; !declared {
				return fmt.Errorf("the scenario declares no argument %q", match[1])
			}
		}
	}
	switch stopReason := step.stopReason(); {
	case stopReason != conversation.StopReasonEndTurn && stopReason != conversation.StopReasonToolUse && stopReason != conversation.StopReasonMaxTokens:
		return fmt.Errorf("stop_reason %q is not end_turn, tool_use or max_tokens", stopReason)
	case stopReason != conversation.StopReasonMaxTokens && (stopReason == conversation.StopReasonToolUse) != (len(step.ToolUses) > 0):
		return errors.New("a reply stops with tool_use exactly when it has tool uses")
	}
	if step.Usage != nil && (step.Usage.InputTokens < 0 || step.Usage.OutputTokens < 0) {
		return errors.New("usage must not be negative")
	}
	for _, use := range step.ToolUses {
		if step.InvalidToolArguments {
			input, err := s.renderInput(use.Input, s.Arguments)
			if err != nil || !step.ExpectFailure || use.Name != conversation.ToolStartTimer || toolinput.DecodeInput(use.Name, string(input)) == nil {
				return errors.New("invalid_tool_arguments needs an invalid start_timer input and expect_failure")
			}
			continue
		}
		if err := s.validateToolUse(use, files, step.ExpectFailure, step.Remote); err != nil {
			return fmt.Errorf("tool use %s: %w", use.Name, err)
		}
	}
	return nil
}

// stopReason is the declared stop reason; tool_use when the reply has tool
// uses, else end_turn.
func (step step) stopReason() conversation.StopReason {
	switch {
	case step.StopReason != "":
		return step.StopReason
	case len(step.ToolUses) > 0:
		return conversation.StopReasonToolUse
	default:
		return conversation.StopReasonEndTurn
	}
}

// validateToolUse renders the call with the script's defaults, checks it
// against the catalog's input schema and plays a file tool on files. A tool of a
// step that expects a failure may fail on files.
func (s *script) validateToolUse(use toolUse, files rootFiles, mayFail, remote bool) error {
	input, err := s.renderInput(use.Input, s.Arguments)
	if err != nil {
		return err
	}
	tools := s.tools()
	if remote {
		tools = agentloop.RemoteTools()
		if len(input) > 256<<10 {
			return errors.New("a remote scripted tool input exceeds256KiB")
		}
	}
	if err = checkToolInput(tools, use.Name, input); err != nil {
		return err
	}
	return playFileTool(use.Name, input, files, mayFail)
}

// tools is the frozen tool set the script's replies run with: the Session's,
// or the subagent kind's.
func (s *script) tools() []agentloop.ToolDefinition {
	if s.isSubagent() {
		return agentloop.SubagentTools(s.subagentKind, conversation.ModelMock)
	}
	return agentloop.SessionTools(conversation.ModelMock)
}

// subagentPrompt is the start_agent prompt field, which names the subagent's
// script.
type subagentPrompt struct {
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
}

// validateSubagents checks that a subagent's script ends with its report and
// that a scenario starts exactly the subagents it declares: every start_agent
// call names a declared script of its own kind, and every declared script is
// started.
func (s *script) validateSubagents() error {
	if s.isSubagent() {
		replies := s.Inputs[0].Replies
		if len(replies) == 0 || len(replies[len(replies)-1].ToolUses) > 0 {
			return errors.New("a subagent's script ends with its report, a reply without a tool use")
		}
		return nil
	}
	started := map[string]bool{}
	for _, in := range s.Inputs {
		for _, reply := range in.Replies {
			for _, use := range reply.ToolUses {
				if use.Name != conversation.ToolStartAgent {
					continue
				}
				var call subagentPrompt
				comm.MustDo(json.Unmarshal(use.Input, &call))
				match := subagentMarker.FindStringSubmatch(call.Prompt)
				name, isOwn := "", false
				if match != nil {
					name, isOwn = strings.CutPrefix(match[1], s.Name+"/")
				}
				definition, declared := s.Subagents[name]
				if !isOwn || !declared || definition.Kind.String() != call.Kind {
					return fmt.Errorf("a start_agent prompt names [[subagent:%s/<name>]] of a subagent this scenario declares with the call's kind", s.Name)
				}
				started[name] = true
			}
		}
	}
	for name := range s.Subagents {
		if !started[name] {
			return fmt.Errorf("no start_agent call starts the subagent %s", name)
		}
	}
	return nil
}

// checkToolInput requires an object of the tool's declared fields with every
// required one, for a tool of tools. The tool decodes it strictly when it
// runs.
func checkToolInput(tools []agentloop.ToolDefinition, name conversation.ToolName, input json.RawMessage) error {
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	for _, definition := range tools {
		if definition.Name == name {
			comm.MustDo(json.Unmarshal([]byte(definition.InputSchema), &schema))
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(input, &fields); err != nil {
				return fmt.Errorf("input must be a JSON object: %w", err)
			}
			for field := range fields {
				if _, declared := schema.Properties[field]; !declared {
					return fmt.Errorf("input field %q is not in the tool's schema", field)
				}
			}
			for _, field := range schema.Required {
				if _, present := fields[field]; !present {
					return fmt.Errorf("input lacks the required field %q", field)
				}
			}
			// The tool's own decoder also checks nested fields, enums and bounds.
			if err := toolinput.DecodeInput(name, string(input)); err != nil {
				return fmt.Errorf("the tool's decoder rejects the input: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("the tool set the script runs with has no tool %q", name)
}
