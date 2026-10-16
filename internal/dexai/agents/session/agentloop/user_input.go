// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// maximumAnsweredMessageBytes bounds the user message that joins the answers
// to one question batch.
const maximumAnsweredMessageBytes = 1_000_000

// UserInputQuestionID identifies one question within a pending input batch.
type UserInputQuestionID string

// UserInputOption describes one suggested answer.
type UserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// UserInputQuestion describes one question within a pending input batch.
type UserInputQuestion struct {
	ID       UserInputQuestionID `json:"id"`
	Header   string              `json:"header"`
	Question string              `json:"question"`
	Options  []UserInputOption   `json:"options"`
}

// PendingUserInput describes one durable wait for the user: a batch of
// questions. A Session that waited at a requirements card before cards stopped
// waiting still holds that card until a message answers it.
type PendingUserInput struct {
	CallID    conversation.CallID `json:"call_id"`
	Questions []UserInputQuestion `json:"questions"`
	StartedAt *time.Time          `json:"started_at,omitempty"`
	// Kind is empty for the questions of a request_user_input call.
	Kind PendingUserInputKind `json:"kind,omitempty"`
	// Requirements is the card of an old requirements wait; it has no questions.
	Requirements *Requirements `json:"requirements,omitempty"`
}

// PendingUserInputKind names the model's questions (empty) or an old requirements card.
type PendingUserInputKind string

// PendingUserInputKindRequirements is the kind of a card a Session waited at
// before cards stopped waiting; SendMessage answers it with the message.
const PendingUserInputKindRequirements PendingUserInputKind = "requirements"

// CurrentKind maps a question left by the removed prelaunch turn limit to an
// ordinary question while its open Session finishes it.
func (kind PendingUserInputKind) CurrentKind() PendingUserInputKind {
	if kind == "turn_limit" {
		return ""
	}
	return kind
}

// PendingUserInputKinds lists every kind of pending input a Session creates:
// the questions of request_user_input (the empty kind). The mock scenario
// coverage check requires a scenario that answers each.
var PendingUserInputKinds = []PendingUserInputKind{""}

// AnswerQuestionsRequest identifies and answers one pending question batch.
type AnswerQuestionsRequest struct {
	Actor   *conversation.Actor `json:"actor,omitempty"`
	CallID  conversation.CallID `json:"call_id"`
	Answers []UserInputAnswer   `json:"answers"`
}

// UserInputAnswer answers one question in a pending input batch.
type UserInputAnswer struct {
	QuestionID UserInputQuestionID `json:"question_id"`
	Answer     string              `json:"answer"`
}

// AnsweredUserInput is one accepted answer awaiting Agent consumption.
type AnsweredUserInput struct {
	CallID  conversation.CallID      `json:"call_id"`
	Message conversation.UserMessage `json:"message"`
	// Description is the content-free activity message that records the answer.
	Description string `json:"description"`
}

// RequestedUserInput is the question batch a request_user_input call asks. The
// schema bounds the counts and lengths; the model must also give non-blank
// text, unique question IDs and unique option labels.
func RequestedUserInput(call conversation.ToolCall, startedAt time.Time) (PendingUserInput, error) {
	input, err := toolinput.RequestUserInput.Decode(call.Arguments.String())
	if err != nil {
		return PendingUserInput{}, invalidToolArguments(call, err)
	}
	questions := make([]UserInputQuestion, 0, len(input.Questions))
	questionIDs := make(map[UserInputQuestionID]bool, len(input.Questions))
	for index, item := range input.Questions {
		question := UserInputQuestion{
			ID:       UserInputQuestionID(strings.TrimSpace(item.ID)),
			Header:   strings.TrimSpace(item.Header),
			Question: strings.TrimSpace(item.Question),
		}
		if question.ID == "" || question.Header == "" || question.Question == "" {
			return PendingUserInput{}, fmt.Errorf("question %d requires id, header, and question", index+1)
		}
		if questionIDs[question.ID] {
			return PendingUserInput{}, fmt.Errorf("question IDs must be unique: %q", question.ID)
		}
		questionIDs[question.ID] = true
		labels := make(map[string]bool, len(item.Options))
		for _, option := range item.Options {
			label, description := strings.TrimSpace(option.Label), strings.TrimSpace(option.Description)
			if label == "" || description == "" {
				return PendingUserInput{}, fmt.Errorf("question %d options require label and description", index+1)
			}
			if labels[label] {
				return PendingUserInput{}, fmt.Errorf("question %d option labels must be unique", index+1)
			}
			labels[label] = true
			question.Options = append(question.Options, UserInputOption{Label: label, Description: description})
		}
		questions = append(questions, question)
	}
	pending := PendingUserInput{CallID: call.ID, Questions: questions, StartedAt: &startedAt}
	encoded, err := json.Marshal(pending)
	if err != nil || len(encoded) > MaximumProgressJSONBytes {
		return PendingUserInput{}, invalidToolArguments(call, fmt.Errorf("the question batch exceeds 256 KiB of encoded state"))
	}
	return pending, nil
}

// answeredUserMessage joins the answers to every pending question into the
// user message of the next turn. False rejects answers that are not exactly
// one per pending question.
func answeredUserMessage(pending PendingUserInput, answers []UserInputAnswer) (conversation.UserMessage, bool) {
	if len(answers) != len(pending.Questions) {
		return conversation.UserMessage{}, false
	}
	byQuestion := make(map[UserInputQuestionID]string, len(answers))
	for _, answer := range answers {
		questionID := UserInputQuestionID(strings.TrimSpace(string(answer.QuestionID)))
		if _, duplicate := byQuestion[questionID]; duplicate {
			return conversation.UserMessage{}, false
		}
		byQuestion[questionID] = strings.TrimSpace(answer.Answer)
	}
	var content strings.Builder
	for index, question := range pending.Questions {
		answer, found := byQuestion[question.ID]
		if !found {
			return conversation.UserMessage{}, false
		}
		if index > 0 {
			content.WriteString("\n\n")
		}
		fmt.Fprintf(&content, "**%s**: %s", question.Header, answer)
	}
	if content.Len() > maximumAnsweredMessageBytes {
		return conversation.UserMessage{}, false
	}
	callID := pending.CallID
	return conversation.UserMessage{Content: content.String(), AnsweredInputCallID: &callID}, true
}

// answeredQuestionsDescription is the activity message of an answered batch.
func answeredQuestionsDescription(questionCount int) string {
	noun := "questions"
	if questionCount == 1 {
		noun = "question"
	}
	return fmt.Sprintf("Answered %d %s.", questionCount, noun)
}

// The requirements card records what the Agent builds as a version the user
// reads while the Agent goes on; the user changes it with a message.

// RequirementFeature is one behavior the user will get. Its ID, R<n>, keeps
// its number in every later version of the requirements, and the
// commit_and_push requirement trace names it.
type RequirementFeature struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// RequirementConnectionStatus says whether the Project already has a
// connection or the user must create it.
type RequirementConnectionStatus string

const (
	RequirementConnectionExisting RequirementConnectionStatus = "existing"
	RequirementConnectionNew      RequirementConnectionStatus = "new"
)

// validate rejects unknown connection statuses.
func (status RequirementConnectionStatus) validate() error {
	switch status {
	case RequirementConnectionExisting, RequirementConnectionNew:
		return nil
	default:
		return conversation.NewEnumValidationError("RequirementConnectionStatus", string(status))
	}
}

// UnmarshalJSON decodes and validates a connection status.
func (status *RequirementConnectionStatus) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, status, RequirementConnectionStatus.validate)
}

// RequirementConnection is one external system the application talks to.
type RequirementConnection struct {
	Need       string                      `json:"need"`
	Connector  string                      `json:"connector"`
	Operations []string                    `json:"operations"`
	Status     RequirementConnectionStatus `json:"status"`
	UserSetup  string                      `json:"user_setup"`
}

// Requirements is what a requirements card shows the user.
type Requirements struct {
	Title       string                  `json:"title"`
	Summary     string                  `json:"summary"`
	Features    []RequirementFeature    `json:"features"`
	Connections []RequirementConnection `json:"connections"`
	UserSetup   []string                `json:"user_setup"`
	Acceptance  []string                `json:"acceptance"`
	// ReplacesRequirements marks the card of a completely different request
	// after a recorded version: it replaces the requirements instead of
	// revising them, so its features are numbered from R1 again.
	ReplacesRequirements bool `json:"replaces_requirements,omitempty"`
	// Design is the reference of the card's Flow design, whose body the
	// Session's FlowDesigns holds; nil for a card that changes no Flow.
	Design *flowdesign.Reference `json:"design,omitempty"`
}

// ConfirmedRequirements is the current version of the requirements, under its
// persisted name. SessionAgentFlow is its only owner: RouteTool records each
// card as the next version in the commit that answers the call.
type ConfirmedRequirements struct {
	// ConfirmedBy is the user who confirmed a version recorded while cards
	// waited for a confirmation; nil on a recorded version.
	ConfirmedBy *conversation.Actor `json:"confirmed_by,omitempty"`
	Revision    int64               `json:"revision"`
	Spec        Requirements        `json:"spec"`
	ConfirmedAt time.Time           `json:"confirmed_at"`
	// LastFeatureNumber is the highest feature number that any version has used
	// since the requirements were last replaced, removed features included; a
	// revised card numbers a new feature above it. A version stored before it
	// existed holds 0, which lastFeatureNumber reads as Spec's highest number.
	LastFeatureNumber int64 `json:"last_feature_number,omitempty"`
}

// maximumRequirementsBytes bounds the requirements as segment_start renders
// them, which every segment after a recorded version carries.
const maximumRequirementsBytes = 16 << 10

// RequestedRequirements is the card a confirm_requirements call shows, and
// its Flow design or nil. The schema bounds the sizes; the model must also give
// non-blank text, keep the card within maximumRequirementsBytes and give its
// features stable IDs (checkFeatureIDs) against stored, the current version
// or nil, so that the IDs of a version are exactly those the commit trace can
// name. Only a card that replaces recorded requirements may set
// ReplacesRequirements, and a design must pass flowdesign's validation.
func RequestedRequirements(call conversation.ToolCall, stored *ConfirmedRequirements) (Requirements, *flowdesign.Design, error) {
	input, err := toolinput.ConfirmRequirements.Decode(call.Arguments.String())
	if err != nil {
		return Requirements{}, nil, invalidToolArguments(call, err)
	}
	spec := Requirements{Features: make([]RequirementFeature, 0, len(input.Features)), Connections: make([]RequirementConnection, 0, len(input.Connections)),
		ReplacesRequirements: input.ReplacesRequirements.Value}
	if spec.ReplacesRequirements && stored == nil {
		return Requirements{}, nil, errors.New("replaces_requirements is only for a card that replaces recorded requirements, and none are recorded")
	}
	if spec.Title, err = nonBlankText("title", input.Title); err != nil {
		return Requirements{}, nil, err
	}
	if spec.Summary, err = nonBlankText("summary", input.Summary); err != nil {
		return Requirements{}, nil, err
	}
	for index, feature := range input.Features {
		text, textErr := nonBlankText(fmt.Sprintf("features[%d].text", index), feature.Text)
		if textErr != nil {
			return Requirements{}, nil, textErr
		}
		spec.Features = append(spec.Features, RequirementFeature{ID: feature.ID, Text: text})
	}
	if err = checkFeatureIDs(spec, stored); err != nil {
		return Requirements{}, nil, err
	}
	for index, connection := range input.Connections {
		item, itemErr := requirementConnection(index, connection.Need, connection.Connector, connection.Operations, string(connection.Status), connection.UserSetup)
		if itemErr != nil {
			return Requirements{}, nil, itemErr
		}
		spec.Connections = append(spec.Connections, item)
	}
	if spec.UserSetup, err = nonBlankTexts("user_setup", input.UserSetup); err != nil {
		return Requirements{}, nil, err
	}
	if spec.Acceptance, err = nonBlankTexts("acceptance", input.Acceptance); err != nil {
		return Requirements{}, nil, err
	}
	var design *flowdesign.Design
	if value, isSet := input.Design.Get(); isSet {
		designed := flowdesign.FromToolInput(value)
		featureIDs := make([]string, 0, len(spec.Features))
		for _, feature := range spec.Features {
			featureIDs = append(featureIDs, feature.ID)
		}
		if err = designed.Validate(featureIDs); err != nil {
			return Requirements{}, nil, err
		}
		reference := designed.Reference()
		design, spec.Design = &designed, &reference
	}
	if len(requirementsText(1, spec)) > maximumRequirementsBytes {
		return Requirements{}, nil, fmt.Errorf("the requirements are too long: keep them within %d KiB in all", maximumRequirementsBytes>>10)
	}
	return spec, design, nil
}

// checkFeatureIDs requires stable feature IDs: R<n> in increasing order, gaps
// allowed. A card that revises recorded requirements keeps the ID of every
// feature it keeps or changes and numbers a new feature above every number
// the requirements have used, so a removed feature's number is never reused;
// only a card that replaces them numbers from R1 again.
func checkFeatureIDs(spec Requirements, stored *ConfirmedRequirements) error {
	var previous int64
	for index, feature := range spec.Features {
		number := featureNumber(feature.ID)
		switch {
		case number == 0:
			return fmt.Errorf("features[%d].id %q is not R followed by the feature's number, such as R1", index, feature.ID)
		case number <= previous:
			return fmt.Errorf("features[%d].id %s does not follow %s: list the features with increasing IDs", index, feature.ID, spec.Features[index-1].ID)
		case stored != nil && !spec.ReplacesRequirements && number <= stored.lastFeatureNumber() && !slices.ContainsFunc(stored.Spec.Features, func(confirmed RequirementFeature) bool { return confirmed.ID == feature.ID }):
			return fmt.Errorf("features[%d].id %s is the number of a feature that an earlier version removed, and a removed number is never reused: number a new feature R%d or above, or set replaces_requirements on the card of a completely different request", index, feature.ID, stored.lastFeatureNumber()+1)
		}
		previous = number
	}
	return nil
}

// featureNumber is the n of a feature ID R<n>, or 0 for an ID of another form.
func featureNumber(id string) int64 {
	digits, isFeatureID := strings.CutPrefix(id, "R")
	number, err := strconv.ParseInt(digits, 10, 64)
	if !isFeatureID || err != nil || number < 1 || strconv.FormatInt(number, 10) != digits {
		return 0
	}
	return number
}

// lastFeatureNumber is the highest feature number the requirements have used.
func (confirmed *ConfirmedRequirements) lastFeatureNumber() int64 {
	last := confirmed.LastFeatureNumber
	for _, feature := range confirmed.Spec.Features {
		last = max(last, featureNumber(feature.ID))
	}
	return last
}

func requirementConnection(index int, need, connector string, operations []string, status, userSetup string) (RequirementConnection, error) {
	name := fmt.Sprintf("connections[%d]", index)
	connection := RequirementConnection{Status: RequirementConnectionStatus(status), UserSetup: strings.TrimSpace(userSetup)}
	var err error
	if connection.Need, err = nonBlankText(name+".need", need); err != nil {
		return RequirementConnection{}, err
	}
	if connection.Connector, err = nonBlankText(name+".connector", connector); err != nil {
		return RequirementConnection{}, err
	}
	if connection.Operations, err = nonBlankTexts(name+".operations", operations); err != nil {
		return RequirementConnection{}, err
	}
	return connection, nil
}

// nonBlankText trims text and rejects it when nothing is left.
func nonBlankText(name, text string) (string, error) {
	if text = strings.TrimSpace(text); text == "" {
		return "", fmt.Errorf("%s must not be blank", name)
	}
	return text, nil
}

func nonBlankTexts(name string, texts []string) ([]string, error) {
	trimmed := make([]string, 0, len(texts))
	for index, text := range texts {
		value, err := nonBlankText(fmt.Sprintf("%s[%d]", name, index), text)
		if err != nil {
			return nil, err
		}
		trimmed = append(trimmed, value)
	}
	return trimmed, nil
}

// RecordRequirements is the version a card records over stored, the current
// version or nil: the stored revision plus one, which carries the stored design
// forward when a revising card has none, and the highest feature number used.
func RecordRequirements(stored *ConfirmedRequirements, card Requirements, at time.Time) ConfirmedRequirements {
	recorded := ConfirmedRequirements{Revision: 1, Spec: card, ConfirmedAt: at}
	if stored != nil {
		recorded.Revision = stored.Revision + 1
		if !card.ReplacesRequirements {
			recorded.LastFeatureNumber = stored.lastFeatureNumber()
			if card.Design == nil {
				recorded.Spec.Design = stored.Spec.Design
			}
		}
	}
	recorded.LastFeatureNumber = recorded.lastFeatureNumber()
	return recorded
}

// RecordedRequirementsResult answers a confirm_requirements call that recorded
// version: the turn goes on, and the user changes the card with a message.
func RecordedRequirementsResult(version int64) ToolResultPayload {
	return ToolResultPayload{Status: ToolResultStatusRecorded, Version: version,
		Reason: fmt.Sprintf("Shown to the user as version %d. Continue building it; the user changes it by sending a message, and a change is recorded as the next version.", version)}
}

// PendingInputAnswer is what answering the pending input decides: the user
// message that begins the next turn and the activity message that records it.
type PendingInputAnswer struct {
	Message     conversation.UserMessage
	Description string
}

// AnswerPendingInput validates the answers to a pending question batch: one
// per question; false rejects any other shape and an old requirements card,
// which only a message answers (AnswerRequirementsCard).
func AnswerPendingInput(pending PendingUserInput, request AnswerQuestionsRequest) (PendingInputAnswer, bool) {
	if pending.Kind == PendingUserInputKindRequirements {
		return PendingInputAnswer{}, false
	}
	message, isValid := answeredUserMessage(pending, request.Answers)
	message.Actor = request.Actor
	return PendingInputAnswer{Message: message, Description: answeredQuestionsDescription(len(pending.Questions))}, isValid
}

// AnswerRequirementsCard answers the old requirements card a Session waits at
// with the user's message, which the Agent reads as the reply to the card that
// SendMessage recorded as version.
func AnswerRequirementsCard(pending PendingUserInput, message conversation.UserMessage, version int64) AnsweredUserInput {
	callID := pending.CallID
	message.AnsweredInputCallID = &callID
	return AnsweredUserInput{CallID: callID, Message: message, Description: fmt.Sprintf("Replied to the requirements, recorded as version %d.", version)}
}

// TraceScope is what commit_and_push checks its requirement trace against: the
// current version and its feature IDs; nil when nothing is recorded.
func (confirmed *ConfirmedRequirements) TraceScope() *sandboxtools.ConfirmedRequirements {
	if confirmed == nil {
		return nil
	}
	scope := &sandboxtools.ConfirmedRequirements{Revision: confirmed.Revision, IDs: make([]string, 0, len(confirmed.Spec.Features))}
	for _, feature := range confirmed.Spec.Features {
		scope.IDs = append(scope.IDs, feature.ID)
	}
	return scope
}

// confirmedRequirementsText renders the current requirements for a
// segment_start entry, or "" when none are recorded. It names the number
// of the next new feature, which a removed last feature does not show.
func confirmedRequirementsText(confirmed *ConfirmedRequirements) string {
	if confirmed == nil {
		return ""
	}
	return requirementsText(confirmed.Revision, confirmed.Spec) +
		fmt.Sprintf("\n\nA new feature is numbered R%d or above; a removed feature's number is never reused.", confirmed.lastFeatureNumber()+1)
}

// requirementsText renders one version of the requirements as plain text.
func requirementsText(version int64, spec Requirements) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Version %d: %s\n%s\n\nFeatures:\n", version, spec.Title, spec.Summary)
	for _, feature := range spec.Features {
		fmt.Fprintf(&text, "- %s: %s\n", feature.ID, feature.Text)
	}
	if len(spec.Connections) > 0 {
		text.WriteString("\nConnections:\n")
	}
	for _, connection := range spec.Connections {
		fmt.Fprintf(&text, "- %s: %s (%s), operations %s", connection.Need, connection.Connector, connection.Status, strings.Join(connection.Operations, ", "))
		if connection.UserSetup != "" {
			text.WriteString("; the user sets up: " + connection.UserSetup)
		}
		text.WriteString("\n")
	}
	for _, section := range []struct {
		title string
		items []string
	}{{"The user prepares", spec.UserSetup}, {"Acceptance", spec.Acceptance}} {
		if len(section.items) > 0 {
			text.WriteString("\n" + section.title + ":\n- " + strings.Join(section.items, "\n- ") + "\n")
		}
	}
	if spec.Design != nil {
		fmt.Fprintf(&text, "\nFlow design: %s\n%s\n", spec.Design.Summary, spec.Design.Outline)
	}
	return strings.TrimSuffix(text.String(), "\n")
}
