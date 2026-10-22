// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"
)

// Limits of a goals block.
const (
	maxFlows        = 4
	maxFields       = 24
	maxOutcomes     = 16
	maxGoals        = 8
	maxStatementLen = 200
)

var (
	slug     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	typeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)
	attrName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)
	kinds    = map[Kind]bool{KindOutcome: true, KindExperience: true, KindCost: true}
)

func validate(spec Spec, at lines) []Problem {
	var problems []Problem
	add := func(line int, format string, args ...any) {
		problems = append(problems, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	if spec.Version != 1 {
		add(at.top("version"), "version must be 1")
	}
	if _, ok := periodLengths[spec.Review]; !ok {
		add(at.top("review"), "review must be weekly, biweekly or off")
	}
	if len(spec.Flows) == 0 || len(spec.Flows) > maxFlows {
		add(at.top("flows"), "flows names 1 to %d Flow types", maxFlows)
	}
	for _, flow := range spec.Flows {
		if !typeName.MatchString(flow) {
			add(at.top("flows"), "%q is not a Flow type name", flow)
		}
	}
	if duplicate := firstDuplicate(spec.Flows); duplicate != "" {
		add(at.top("flows"), "flows names %q twice", duplicate)
	}
	if len(spec.Fields) > maxFields {
		add(at.top("fields"), "fields names at most %d Attributes", maxFields)
	}
	for _, field := range spec.Fields {
		if !attrName.MatchString(field) {
			add(at.top("fields"), "%q is not an Attribute name", field)
		}
	}
	if duplicate := firstDuplicate(spec.Fields); duplicate != "" {
		add(at.top("fields"), "fields names %q twice", duplicate)
	}
	if len(spec.Outcomes) > maxOutcomes {
		add(at.top("outcomes"), "at most %d outcomes", maxOutcomes)
	}
	names := make([]string, 0, len(spec.Outcomes))
	for name := range spec.Outcomes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if message := outcomeProblem(name, spec.Outcomes[name]); message != "" {
			add(at.outcome(name), "outcome %s: %s", name, message)
		}
	}
	if len(spec.Goals) == 0 || len(spec.Goals) > maxGoals {
		add(at.top("goals"), "goals lists 1 to %d goals", maxGoals)
	}
	ids := map[string]bool{}
	for i, goal := range spec.Goals {
		if !slug.MatchString(goal.ID) {
			add(at.goal(i, "id"), "goal id %q must be a lower-case slug", goal.ID)
		} else if ids[goal.ID] {
			add(at.goal(i, "id"), "goal id %q is used twice", goal.ID)
		}
		ids[goal.ID] = true
		if !kinds[goal.Kind] {
			add(at.goal(i, "kind"), "kind must be outcome, experience or cost")
		}
		if goal.Statement == "" || utf8.RuneCountInString(goal.Statement) > maxStatementLen {
			add(at.goal(i, "statement"), "statement has 1 to %d characters", maxStatementLen)
		}
		if _, err := compileMetric(goal.Metric, spec); err != nil {
			add(at.goal(i, "metric"), "metric: %s", err)
		}
		switch {
		case goal.Direction != "" && goal.Guardrail != nil:
			add(at.goal(i, "guardrail"), "a goal has a direction or a guardrail, not both")
		case goal.Direction == "" && goal.Guardrail == nil:
			add(at.goal(i, "metric"), "a goal needs a direction (up or down) or a guardrail")
		case goal.Direction != "" && goal.Direction != DirectionUp && goal.Direction != DirectionDown:
			add(at.goal(i, "direction"), "direction must be up or down")
		case goal.Guardrail != nil && (goal.Guardrail.Max == nil) == (goal.Guardrail.Min == nil):
			add(at.goal(i, "guardrail"), "a guardrail has exactly one of max and min")
		}
	}
	return problems
}

func outcomeProblem(name string, outcome outcomeDefinition) string {
	if !slug.MatchString(name) {
		return "the name must be a lower-case slug"
	}
	set := 0
	for _, value := range []string{outcome.Attribute, outcome.Step, outcome.Action} {
		if value != "" {
			set++
		}
	}
	switch {
	case set != 1:
		return "use exactly one of attribute, step and action"
	case outcome.Attribute != "" && (outcome.Equals == nil) == (len(outcome.In) == 0):
		return "an attribute outcome has exactly one of equals and in"
	case outcome.Attribute == "" && (outcome.Equals != nil || len(outcome.In) > 0):
		return "equals and in apply only to an attribute"
	case outcome.Attribute != "" && !attrName.MatchString(outcome.Attribute):
		return fmt.Sprintf("%q is not an Attribute name", outcome.Attribute)
	case outcome.Step != "" && !typeName.MatchString(outcome.Step):
		return fmt.Sprintf("%q is not a Step type name", outcome.Step)
	case outcome.Action != "" && !typeName.MatchString(outcome.Action):
		return fmt.Sprintf("%q is not an RPC name", outcome.Action)
	}
	return ""
}

func firstDuplicate(list []string) string {
	seen := map[string]bool{}
	for _, item := range list {
		if seen[item] {
			return item
		}
		seen[item] = true
	}
	return ""
}
