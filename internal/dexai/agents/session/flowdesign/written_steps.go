// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowdesign

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// WrittenSteps is how far the agent wrote the code of the current design:
// each designed Step, in design order, and whether a successful write or edit
// declared it since the design was recorded. SessionAgentFlow keeps it as its
// WrittenDesignSteps Attribute beside FlowDesigns, so that a tool result marks
// Steps without reading the design.
type WrittenSteps struct {
	// Digest is the digest of the design it follows.
	Digest string        `json:"digest"`
	Steps  []writtenStep `json:"steps"`
}

// writtenStep is one designed Step and whether its declaration was written.
type writtenStep struct {
	FlowType string `json:"flow_type"`
	StepType string `json:"step_type"`
	Written  bool   `json:"written"`
}

// NewWrittenSteps starts the written state of design. A Step that previous,
// the state of the design it replaces or nil, marked written keeps its mark
// when the design keeps it in the same Flow: its code stays in the workspace.
func NewWrittenSteps(design Design, previous *WrittenSteps) WrittenSteps {
	kept := map[writtenStep]bool{}
	if previous != nil {
		for _, step := range previous.Steps {
			kept[writtenStep{FlowType: step.FlowType, StepType: step.StepType}] = step.Written
		}
	}
	written := WrittenSteps{Digest: design.Digest(), Steps: []writtenStep{}}
	for _, flow := range design.Flows {
		for _, step := range flow.Steps {
			identity := writtenStep{FlowType: flow.FlowType, StepType: step.StepType}
			identity.Written = kept[identity]
			written.Steps = append(written.Steps, identity)
		}
	}
	return written
}

// stepDeclarations find the Step types a Go source text declares: an ordinary
// Step by its struct type, a connector Step by the StepType of its options.
var stepDeclarations = []*regexp.Regexp{
	regexp.MustCompile(`\btype\s+([A-Z][A-Za-z0-9]*)\s+struct\b`),
	regexp.MustCompile(`\bStepType:\s*"([A-Z][A-Za-z0-9]*)"`),
}

// Record marks the designed Steps that text declares, the content that a
// write or the new_string that an edit wrote to the Go source file at
// filePath; a test file declares none. A Step type designed in several Flows
// is marked in the Flow whose file filePath names, such as
// text_summary_flow.go or text_summary_steps.go for TextSummaryFlow, and in
// each of them when it names none. It reports whether a mark changed.
func (written *WrittenSteps) Record(filePath, text string) bool {
	if path.Ext(filePath) != ".go" || strings.HasSuffix(filePath, "_test.go") {
		return false
	}
	declared := map[string]bool{}
	for _, declaration := range stepDeclarations {
		for _, match := range declaration.FindAllStringSubmatch(text, -1) {
			declared[match[1]] = true
		}
	}
	isChanged := false
	for index, step := range written.Steps {
		if !declared[step.StepType] || step.Written {
			continue
		}
		if flowType, isNamed := written.namedFlow(step.StepType, filePath); isNamed && flowType != step.FlowType {
			continue
		}
		written.Steps[index].Written, isChanged = true, true
	}
	return isChanged
}

// namedFlow is the Flow that filePath names among the several Flows that
// design a Step of stepType: the one whose snake-case name, without its _flow,
// is the longest that the file's name starts with. It reports false when one
// Flow designs the type or the file names none of them.
func (written WrittenSteps) namedFlow(stepType, filePath string) (string, bool) {
	name := strings.TrimSuffix(path.Base(filePath), ".go")
	designers, named, longest := 0, "", 0
	for _, step := range written.Steps {
		if step.StepType != stepType {
			continue
		}
		designers++
		stem := strings.TrimSuffix(snakeCase(step.FlowType), "_flow")
		if (name == stem || strings.HasPrefix(name, stem+"_")) && len(stem) > longest {
			named, longest = step.FlowType, len(stem)
		}
	}
	return named, designers > 1 && named != ""
}

// snakeCase spells a Go type name in snake case, as Go file names do:
// TextSummaryFlow is text_summary_flow and HTTPRequestFlow http_request_flow.
func snakeCase(name string) string {
	runes := []rune(name)
	var snake strings.Builder
	for index, character := range runes {
		if unicode.IsUpper(character) && index > 0 {
			previous := runes[index-1]
			isNextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && isNextLower {
				snake.WriteByte('_')
			}
		}
		snake.WriteRune(unicode.ToLower(character))
	}
	return snake.String()
}

// Names are the written Steps, each <flow_type>.<step_type>, in design order.
func (written WrittenSteps) Names() []string {
	names := []string{}
	for _, step := range written.Steps {
		if step.Written {
			names = append(names, step.FlowType+"."+step.StepType)
		}
	}
	return names
}
