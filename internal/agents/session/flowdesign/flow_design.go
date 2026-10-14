// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowdesign

import (
	"fmt"

	"github.com/superdurable/superversev2/internal/agents/session/toolinput/generated"
)

// Design is the agent's proposed Flows before any code: what a requirements
// card adds or changes, which Studio draws until the build is complete. Its
// JSON keeps the schema's field order, so that its canonical form and digest
// are stable.
type Design struct {
	Summary      string   `json:"summary"`
	Flows        []Flow   `json:"flows"`
	RemovedFlows []string `json:"removed_flows"`
}

// Flow is one designed Flow, listed in full.
type Flow struct {
	FlowType   string      `json:"flow_type"`
	Purpose    string      `json:"purpose"`
	StartStep  string      `json:"start_step"`
	Steps      []Step      `json:"steps"`
	Channels   []Channel   `json:"channels"`
	Actions    []Action    `json:"actions"`
	Attributes []Attribute `json:"attributes"`
	Starts     []string    `json:"starts"`
}

// Step is one designed Step with its group, what it waits for and where it
// goes.
type Step struct {
	StepType    string       `json:"step_type"`
	Group       string       `json:"group"`
	Explanation string       `json:"explanation"`
	WaitsFor    Wait         `json:"waits_for"`
	Next        []Transition `json:"next"`
	Features    []string     `json:"features"`
}

// Wait is what a Step waits for before it runs.
type Wait struct {
	Kind WaitKind `json:"kind"`
	Name string   `json:"name"`
}

// WaitKind is what a designed Step waits for: nothing, an Action of its Flow
// (Name is the Action's RPC), a Channel of its Flow (Name is the Channel) or a
// business time (Name says it).
type WaitKind string

const (
	WaitKindNone    WaitKind = "none"
	WaitKindAction  WaitKind = "action"
	WaitKindChannel WaitKind = "channel"
	WaitKindTimer   WaitKind = "timer"
)

// waitKinds lists every wait kind.
var waitKinds = []WaitKind{WaitKindNone, WaitKindAction, WaitKindChannel, WaitKindTimer}

// Transition is one way out of a Step: to a Step of the same Flow, or to an
// end. A failure route is where the Step's failure leads.
type Transition struct {
	To             string `json:"to"`
	When           string `json:"when"`
	IsFailureRoute bool   `json:"is_failure_route"`
}

// TransitionEnd is a way out of a Flow that a Transition's To may name
// instead of a Step.
type TransitionEnd string

const (
	TransitionEndComplete TransitionEnd = "complete"
	TransitionEndFail     TransitionEnd = "fail"
)

// transitionEnds lists every end.
var transitionEnds = []TransitionEnd{TransitionEndComplete, TransitionEndFail}

// End returns the end the transition leads to, or false when it leads to a
// Step.
func (transition Transition) End() (TransitionEnd, bool) {
	for _, end := range transitionEnds {
		if transition.To == string(end) {
			return end, true
		}
	}
	return "", false
}

// Channel is a designed Channel of a Flow.
type Channel struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
}

// Action is a designed Action: an RPC that a person invokes from Studio.
type Action struct {
	Label      string `json:"label"`
	RPC        string `json:"rpc"`
	Publishes  string `json:"publishes"`
	Permission string `json:"permission"`
}

// Attribute is a designed Attribute or AttributeMap; it is shown, never
// compared.
type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	IsMap bool   `json:"is_map"`
	Key   string `json:"key"`
}

// Slot names the two designs a Session keeps: the waiting card's and the
// confirmed one. Each is a fixed instance key of the Session's FlowDesigns.
type Slot string

const (
	SlotPending   Slot = "pending"
	SlotConfirmed Slot = "confirmed"
)

// Slots lists both slots; the business API maps each.
var Slots = []Slot{SlotPending, SlotConfirmed}

// FromToolInput is the design of a confirm_requirements call, whose schema
// the decoder already enforced. Absent lists become empty ones and an absent
// wait is a wait for nothing, so that equal designs have one canonical form.
func FromToolInput(input generated.ConfirmRequirementsDesign) Design {
	design := Design{Summary: input.Summary, Flows: make([]Flow, 0, len(input.Flows)), RemovedFlows: nonNil(input.RemovedFlows)}
	for _, flow := range input.Flows {
		designed := Flow{
			FlowType: flow.FlowType, Purpose: flow.Purpose, StartStep: flow.StartStep,
			Steps: make([]Step, 0, len(flow.Steps)), Channels: make([]Channel, 0, len(flow.Channels)),
			Actions: make([]Action, 0, len(flow.Actions)), Attributes: make([]Attribute, 0, len(flow.Attributes)),
			Starts: nonNil(flow.Starts),
		}
		for _, step := range flow.Steps {
			designed.Steps = append(designed.Steps, fromToolInputStep(step))
		}
		for _, channel := range flow.Channels {
			designed.Channels = append(designed.Channels, Channel{Name: channel.Name, Payload: channel.Payload})
		}
		for _, action := range flow.Actions {
			designed.Actions = append(designed.Actions, Action{Label: action.Label, RPC: action.RPC, Publishes: action.Publishes.Or(""), Permission: action.Permission})
		}
		for _, attribute := range flow.Attributes {
			designed.Attributes = append(designed.Attributes, Attribute{Name: attribute.Name, Value: attribute.Value, IsMap: attribute.IsMap.Or(false), Key: attribute.Key.Or("")})
		}
		design.Flows = append(design.Flows, designed)
	}
	return design
}

func fromToolInputStep(step generated.ConfirmRequirementsDesignFlowsItemStepsItem) Step {
	designed := Step{
		StepType: step.StepType, Group: step.Group, Explanation: step.Explanation,
		WaitsFor: Wait{Kind: WaitKindNone}, Next: make([]Transition, 0, len(step.Next)), Features: nonNil(step.Features),
	}
	if wait, isSet := step.WaitsFor.Get(); isSet {
		designed.WaitsFor = Wait{Kind: waitKindOf(wait.Kind), Name: wait.Name.Or("")}
	}
	for _, next := range step.Next {
		designed.Next = append(designed.Next, Transition{To: next.To, When: next.When.Or(""), IsFailureRoute: next.IsFailureRoute.Or(false)})
	}
	return designed
}

func waitKindOf(kind generated.ConfirmRequirementsDesignFlowsItemStepsItemWaitsForKind) WaitKind {
	for _, known := range waitKinds {
		if string(kind) == string(known) {
			return known
		}
	}
	panic(fmt.Sprintf("the schema admitted the unknown wait kind %q", kind))
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
