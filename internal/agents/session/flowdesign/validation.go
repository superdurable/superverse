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
	"slices"
)

// maximumDesignBytes bounds a design's canonical JSON, and so each instance of
// the Session's FlowDesigns.
const maximumDesignBytes = 24 << 10

// Validate checks what the schema cannot: that the names a design uses are
// declared, that every Step is reachable, and that the design fits its bound.
// featureIDs are the card's features. The error names the field and the rule.
// Group shape is not checked: Dex's analyzer owns that rule and only warns.
func (design Design) Validate(featureIDs []string) error {
	if size := len(design.canonicalJSON()); size > maximumDesignBytes {
		return fmt.Errorf("design: %d bytes of canonical JSON exceed the limit of %d; list fewer or shorter Flows", size, maximumDesignBytes)
	}
	flowTypes := map[string]bool{}
	for index, flow := range design.Flows {
		field := fmt.Sprintf("design.flows[%d]", index)
		if flowTypes[flow.FlowType] {
			return fmt.Errorf("%s.flow_type: %s is listed twice", field, flow.FlowType)
		}
		flowTypes[flow.FlowType] = true
		if slices.Contains(design.RemovedFlows, flow.FlowType) {
			return fmt.Errorf("%s.flow_type: %s is both designed and in removed_flows", field, flow.FlowType)
		}
		if err := flow.validate(field, featureIDs); err != nil {
			return err
		}
	}
	removed := map[string]bool{}
	for index, flowType := range design.RemovedFlows {
		if removed[flowType] {
			return fmt.Errorf("design.removed_flows[%d]: %s is listed twice", index, flowType)
		}
		removed[flowType] = true
	}
	return nil
}

func (flow Flow) validate(field string, featureIDs []string) error {
	steps, err := uniqueNames(field, "steps", "step_type", flow.Steps, func(step Step) string { return step.StepType })
	if err != nil {
		return err
	}
	channels, err := uniqueNames(field, "channels", "name", flow.Channels, func(channel Channel) string { return channel.Name })
	if err != nil {
		return err
	}
	actions, err := uniqueNames(field, "actions", "rpc", flow.Actions, func(action Action) string { return action.RPC })
	if err != nil {
		return err
	}
	if _, err := uniqueNames(field, "attributes", "name", flow.Attributes, func(attribute Attribute) string { return attribute.Name }); err != nil {
		return err
	}
	if !steps[flow.StartStep] {
		return fmt.Errorf("%s.start_step: %s is not a Step of %s", field, flow.StartStep, flow.FlowType)
	}
	for index, action := range flow.Actions {
		if action.Publishes != "" && !channels[action.Publishes] {
			return fmt.Errorf("%s.actions[%d].publishes: %s is not a Channel of %s", field, index, action.Publishes, flow.FlowType)
		}
	}
	for index, attribute := range flow.Attributes {
		if attribute.IsMap && attribute.Key == "" {
			return fmt.Errorf("%s.attributes[%d].key: the AttributeMap %s needs its instance key format", field, index, attribute.Name)
		}
	}
	for index, step := range flow.Steps {
		if err := step.validate(fmt.Sprintf("%s.steps[%d]", field, index), flow.FlowType, steps, channels, actions, featureIDs); err != nil {
			return err
		}
	}
	reached := flow.reachable(func(Transition) bool { return true })
	for index, step := range flow.Steps {
		if !reached[step.StepType] {
			return fmt.Errorf("%s.steps[%d]: %s is not reachable from the start Step %s", field, index, step.StepType, flow.StartStep)
		}
	}
	return nil
}

func (step Step) validate(field, flowType string, steps, channels, actions map[string]bool, featureIDs []string) error {
	switch step.WaitsFor.Kind {
	case WaitKindAction:
		if !actions[step.WaitsFor.Name] {
			return fmt.Errorf("%s.waits_for.name: %s is not the rpc of an Action of %s", field, step.WaitsFor.Name, flowType)
		}
	case WaitKindChannel:
		if !channels[step.WaitsFor.Name] {
			return fmt.Errorf("%s.waits_for.name: %s is not a Channel of %s", field, step.WaitsFor.Name, flowType)
		}
	case WaitKindTimer:
		if step.WaitsFor.Name == "" {
			return fmt.Errorf("%s.waits_for.name: a timer wait says its business time", field)
		}
	case WaitKindNone:
	}
	for index, next := range step.Next {
		if _, isEnd := next.End(); !isEnd && !steps[next.To] {
			return fmt.Errorf("%s.next[%d].to: %s is not a Step of %s, complete or fail; use lowercase complete or fail for an end", field, index, next.To, flowType)
		}
		if len(step.Next) > 1 && next.When == "" {
			return fmt.Errorf("%s.next[%d].when: set a nonempty when on every next transition of a Step with several ways out", field, index)
		}
	}
	for index, feature := range step.Features {
		if !slices.Contains(featureIDs, feature) {
			return fmt.Errorf("%s.features[%d]: %s is not a feature of this card", field, index, feature)
		}
	}
	return nil
}

// reachable is the set of Steps the start Step reaches through the
// transitions follow admits, in no particular order.
func (flow Flow) reachable(follow func(Transition) bool) map[string]bool {
	order := flow.readingOrder(follow)
	reached := make(map[string]bool, len(order))
	for _, step := range order {
		reached[step] = true
	}
	return reached
}

func uniqueNames[T any](field, list, key string, items []T, name func(T) string) (map[string]bool, error) {
	names := make(map[string]bool, len(items))
	for index, item := range items {
		if names[name(item)] {
			return nil, fmt.Errorf("%s.%s[%d].%s: %s is listed twice", field, list, index, key, name(item))
		}
		names[name(item)] = true
	}
	return names, nil
}
