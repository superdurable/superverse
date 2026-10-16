// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowdesign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// maximumOutlineBytes bounds the outline that the card, the confirmed
// requirements and every segment_start carry.
const maximumOutlineBytes = 2 << 10

// Reference is what the card and the confirmed requirements carry of a
// design: its digest, its summary and its outline. The body stays in the
// Session's FlowDesigns.
type Reference struct {
	Digest  string `json:"digest"`
	Summary string `json:"summary"`
	Outline string `json:"outline"`
}

// Reference returns the design's reference.
func (design Design) Reference() Reference {
	return Reference{Digest: design.Digest(), Summary: design.Summary, Outline: design.outline()}
}

// canonicalJSON is the design's JSON with the keys in schema order and no
// insignificant whitespace.
func (design Design) canonicalJSON() []byte {
	data, err := json.Marshal(design)
	if err != nil {
		panic(fmt.Sprintf("encode the flow design: %v", err))
	}
	return data
}

// Digest identifies the design across the card, the confirmation and the
// read: sha256: and the lowercase hex SHA-256 of its canonical JSON.
func (design Design) Digest() string {
	sum := sha256.Sum256(design.canonicalJSON())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// outline is one line per Flow with its main-story Steps in reading order,
// waits in brackets, and the number of its failure-handling Steps, then the
// removed Flows. It stays within maximumOutlineBytes by leaving out whole
// Flows, which its last Flow line counts.
func (design Design) outline() string {
	var removed string
	if len(design.RemovedFlows) > 0 {
		removed = "Removed: " + strings.Join(design.RemovedFlows, ", ")
	}
	lines := make([]string, 0, len(design.Flows)+2)
	for _, flow := range design.Flows {
		lines = append(lines, flow.outlineLine())
	}
	for kept := len(lines); kept >= 0; kept-- {
		outline := lines[:kept:kept]
		if left := len(lines) - kept; left > 0 {
			outline = append(outline, fmt.Sprintf("and %d more %s", left, plural(left, "Flow")))
		}
		if removed != "" {
			outline = append(outline, removed)
		}
		if text := strings.Join(outline, "\n"); len(text) <= maximumOutlineBytes {
			return text
		}
	}
	panic("the removed Flows alone exceed the outline's bound")
}

func (flow Flow) outlineLine() string {
	story := flow.mainStory()
	labels := make(map[string]string, len(flow.Actions))
	for _, action := range flow.Actions {
		labels[action.RPC] = action.Label
	}
	steps := make(map[string]Step, len(flow.Steps))
	for _, step := range flow.Steps {
		steps[step.StepType] = step
	}
	names := make([]string, 0, len(story))
	for _, name := range story {
		switch wait := steps[name].WaitsFor; wait.Kind {
		case WaitKindAction:
			names = append(names, fmt.Sprintf("%s [%s]", name, labels[wait.Name]))
		case WaitKindChannel, WaitKindTimer:
			names = append(names, fmt.Sprintf("%s [%s]", name, wait.Name))
		case WaitKindNone:
			names = append(names, name)
		}
	}
	line := flow.FlowType + ": " + strings.Join(names, ", ")
	if hidden := len(flow.Steps) - len(story); hidden > 0 {
		line += fmt.Sprintf("; +%d failure-handling %s", hidden, plural(hidden, "Step"))
	}
	return line
}

// mainStory is the Flow's main story in reading order: the Steps its start
// Step reaches through ordinary transitions, breadth-first in next order.
// Every other Step is failure handling, which only failure routes reach,
// directly or through other such Steps: the split Dex's analyzer and the Flow
// canvas apply to implemented Flows.
func (flow Flow) mainStory() []string {
	return flow.readingOrder(func(transition Transition) bool { return !transition.IsFailureRoute })
}

func (flow Flow) readingOrder(follow func(Transition) bool) []string {
	steps := make(map[string]Step, len(flow.Steps))
	for _, step := range flow.Steps {
		steps[step.StepType] = step
	}
	if _, isStep := steps[flow.StartStep]; !isStep {
		return nil
	}
	order, seen := []string{flow.StartStep}, map[string]bool{flow.StartStep: true}
	for index := 0; index < len(order); index++ {
		for _, next := range steps[order[index]].Next {
			if _, isStep := steps[next.To]; isStep && follow(next) && !seen[next.To] {
				seen[next.To] = true
				order = append(order, next.To)
			}
		}
	}
	return order
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
