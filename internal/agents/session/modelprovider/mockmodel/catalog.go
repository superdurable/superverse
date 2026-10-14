// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"fmt"
	"maps"
	"slices"

	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm"
)

// Scenario is the public description of one embedded scenario: what Studio
// lists and sends for it, and what the coverage check reads. It holds no
// secret; the scenarios are compiled into the binary.
type Scenario struct {
	Name        string
	Title       string
	Description string
	Category    ScenarioCategory
	// NeedsNewProject reports that the scenario changes the Project: its
	// source, by an edit, a merge of a subagent's changes or a push, or its
	// requirements, by a confirmed card. It expects a new Project's state and
	// runs once per Project.
	NeedsNewProject bool
	// TestAction is what only the scenario's Dex test can do between its
	// inputs; Studio does not offer a scenario that has one.
	TestAction string
	// Arguments are the values a person may set for the scenario, with their
	// defaults. The argument task is not among them: the mock model takes it
	// from the conversation.
	Arguments []Argument
	Inputs    []Input
	// Tools are the tools the scenario's replies and its subagents' replies
	// call, in name order.
	Tools []conversation.ToolName
	// SubagentKinds are the kinds of the subagents the scenario starts, in
	// name order.
	SubagentKinds []agenttasks.SubagentKind
	// Notifications are the kinds of the notifications the scenario's replies
	// answer; the mock model rejects such a reply's call without one.
	Notifications []conversation.NotificationKind
}

// ScenarioCategory is the group Studio lists a scenario under, which its
// script declares as category.
type ScenarioCategory struct{ name string }

// The categories, in the order Studio lists them.
var (
	// categoryProduct builds a complete product application.
	categoryProduct = ScenarioCategory{"product"}
	// categoryAgentFeature exercises a capability of the Agent itself, such
	// as background commands, subagents, compaction or Stop.
	categoryAgentFeature = ScenarioCategory{"agent_feature"}
	// categoryProjectFeature exercises building the Project: source changes,
	// requirements, connectors, deployments and platform reads.
	categoryProjectFeature = ScenarioCategory{"project_feature"}
	// categoryPlatformError is a platform or provider failure that the
	// platform reports and recovers from.
	categoryPlatformError = ScenarioCategory{"platform_error"}
	// categoryUserError is a mistake in the user's configuration or
	// application source that the platform reports and the Agent fixes.
	categoryUserError = ScenarioCategory{"user_error"}
	// categoryAgentError is a mistake in the Agent's own call that the tool
	// reports and the Agent corrects.
	categoryAgentError = ScenarioCategory{"agent_error"}
)

var scenarioCategories = []ScenarioCategory{categoryProduct, categoryAgentFeature, categoryProjectFeature,
	categoryPlatformError, categoryUserError, categoryAgentError}

// String is the spelling scripts and the API use.
func (category ScenarioCategory) String() string { return category.name }

// UnmarshalText reads a script's spelling.
func (category *ScenarioCategory) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, category, ScenarioCategory.String, scenarioCategories...)
}

// Argument is one value a scenario's placeholders take.
type Argument struct {
	Name    string
	Default string
}

// Input is one user input of a scenario, in order.
type Input struct {
	// Number counts the inputs from 1.
	Number int
	Label  string
	Kind   InputKind
	// Content is the exact text to send: a message or a request for changes
	// with its marker. A confirmation and answers
	// have none.
	Content string
	// Answers are the answers to the pending questions, in question order.
	Answers []string
}

// Answers is the kind of pending input an input of this kind answers; false
// for a message, which answers none.
func (kind InputKind) Answers() (agentloop.PendingUserInputKind, bool) {
	switch kind {
	case InputAnswers:
		return "", true
	case InputRequirementsConfirm, InputRequirementsChange:
		return agentloop.PendingUserInputKindRequirements, true
	default:
		return "", false
	}
}

// Catalog lists every embedded scenario in name order.
func Catalog() []Scenario {
	catalog := make([]Scenario, 0, len(scripts))
	for _, name := range scenarioNames() {
		catalog = append(catalog, scripts[name].describe())
	}
	return catalog
}

// inputMarker is the marker that names input number of a scenario.
func inputMarker(scenario string, number int) string {
	return fmt.Sprintf("[[scenario:%s#%d]]", scenario, number)
}

// ArgumentMarkerForTest is the marker that sets an argument of a scenario.
func ArgumentMarkerForTest(name, value string) string {
	return fmt.Sprintf("[[scenario-arg:%s=%s]]", name, value)
}

// projectChangingTools are the tools whose effect a second run on the same
// Project would not repeat: an edit's old text is gone, merged changes are in,
// a pushed commit leaves nothing to push and confirmed requirements stay, with
// their versions and the numbers of removed features.
var projectChangingTools = []conversation.ToolName{conversation.ToolEdit, conversation.ToolMergeAgentChanges,
	conversation.ToolCommitAndPush, conversation.ToolConfirmRequirements}

// describe is the public description of a script.
func (s *script) describe() Scenario {
	scenario := Scenario{Name: s.Name, Title: s.Title, Description: s.Description, Category: s.Category, TestAction: s.TestAction}
	notifications := map[conversation.NotificationKind]bool{}
	for _, name := range slices.Sorted(maps.Keys(s.Arguments)) {
		if _, set := messageArguments[name]; !set {
			scenario.Arguments = append(scenario.Arguments, Argument{Name: name, Default: s.Arguments[name]})
		}
	}
	tools := map[conversation.ToolName]bool{}
	for index, in := range s.Inputs {
		input := Input{Number: index + 1, Label: in.Label, Kind: in.kind(), Answers: in.Answers}
		switch input.Kind {
		case InputMessage:
			input.Content = in.Message + " " + inputMarker(s.Name, input.Number)
		case InputRequirementsChange:
			input.Content = in.RequirementsChange + " " + inputMarker(s.Name, input.Number)
		default:
		}
		scenario.Inputs = append(scenario.Inputs, input)
		for _, reply := range in.Replies {
			for _, use := range reply.ToolUses {
				tools[use.Name] = true
				scenario.NeedsNewProject = scenario.NeedsNewProject || slices.Contains(projectChangingTools, use.Name)
			}
			if reply.Notification != "" {
				notifications[reply.Notification] = true
			}
		}
	}
	kinds := map[string]agenttasks.SubagentKind{}
	for _, definition := range s.Subagents {
		kinds[definition.Kind.String()] = definition.Kind
		for _, reply := range definition.Replies {
			for _, use := range reply.ToolUses {
				tools[use.Name] = true
			}
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		scenario.SubagentKinds = append(scenario.SubagentKinds, kinds[kind])
	}
	scenario.Tools = slices.Sorted(maps.Keys(tools))
	scenario.Notifications = slices.Sorted(maps.Keys(notifications))
	return scenario
}

// RemoteReplyForTest reads an exact embedded remote script, not a provider or app dependency.
func RemoteReplyForTest(name string, input, number int) conversation.ModelReply {
	script := scripts[name]
	at := position{input: input - 1, reply: number - 1}
	if script == nil || script.stepAt(at) == nil || !script.stepAt(at).Remote {
		panic("the fixture requested no declared remote step")
	}
	expected := comm.Must(script.replyAt(at, script.Arguments, "remote-fixture"))
	reply := conversation.ModelReply{}
	for _, block := range expected.blocks {
		switch block.kind {
		case conversation.BlockTypeText:
			reply.Text = block.text
		case conversation.BlockTypeToolUse:
			reply.ToolCalls = append(reply.ToolCalls, conversation.ToolCall{ID: conversation.CallID(block.id), Name: conversation.ToolName(block.name), Arguments: comm.Must(conversation.ParseJSONObject(string(block.input)))})
		default:
			panic("a remote script contains a non-public block")
		}
	}
	return reply
}
