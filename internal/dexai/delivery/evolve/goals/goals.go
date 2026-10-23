// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FilePath is where an application keeps its goals, relative to the repository root.
const FilePath = ".superverse/goals.md"

// Period is how often a scheduled review runs.
type Period string

// The review periods; off keeps the goals for Studio and schedules no review.
const (
	PeriodWeekly   Period = "weekly"
	PeriodBiweekly Period = "biweekly"
	PeriodOff      Period = "off"
)

var periodLengths = map[Period]time.Duration{PeriodWeekly: 7 * 24 * time.Hour, PeriodBiweekly: 14 * 24 * time.Hour, PeriodOff: 0}

// Length is the time between scheduled reviews; zero for off.
func (p Period) Length() time.Duration { return periodLengths[p] }

// Kind groups goals in Studio; reviews weigh outcome goals first.
type Kind string

// The goal kinds.
const (
	KindOutcome    Kind = "outcome"
	KindExperience Kind = "experience"
	KindCost       Kind = "cost"
)

// Direction is which way a goal should move.
type Direction string

// The goal directions.
const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

// Spec is the goals block of a goals file.
type Spec struct {
	Version  int                          `yaml:"version" json:"version"`
	Review   Period                       `yaml:"review" json:"review"`
	Flows    []string                     `yaml:"flows" json:"flows"`
	Fields   []string                     `yaml:"fields" json:"fields,omitempty"`
	Outcomes map[string]outcomeDefinition `yaml:"outcomes" json:"outcomes,omitempty"`
	Goals    []Goal                       `yaml:"goals" json:"goals"`
}

// outcomeDefinition is a named fact read from each run: an Attribute's value, a Step entered or an Action accepted.
type outcomeDefinition struct {
	Attribute string `yaml:"attribute,omitempty" json:"attribute,omitempty"`
	Equals    any    `yaml:"equals,omitempty" json:"equals,omitempty"`
	In        []any  `yaml:"in,omitempty" json:"in,omitempty"`
	Step      string `yaml:"step,omitempty" json:"step,omitempty"`
	Action    string `yaml:"action,omitempty" json:"action,omitempty"`
}

// Goal is one thing to grow, protect or hold.
type Goal struct {
	ID        string     `yaml:"id" json:"id"`
	Kind      Kind       `yaml:"kind" json:"kind"`
	Statement string     `yaml:"statement" json:"statement"`
	Metric    string     `yaml:"metric" json:"metric"`
	Direction Direction  `yaml:"direction,omitempty" json:"direction,omitempty"`
	Guardrail *Guardrail `yaml:"guardrail,omitempty" json:"guardrail,omitempty"`
	Target    *float64   `yaml:"target,omitempty" json:"target,omitempty"`
	// Within, such as 7d, 12h or 90m, is how long after its decision point a
	// run's outcome may come, or when its per-run number is read.
	Within string `yaml:"within,omitempty" json:"within,omitempty"`
}

// Guardrail is a limit a goal's metric must keep.
type Guardrail struct {
	Max *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	Min *float64 `yaml:"min,omitempty" json:"min,omitempty"`
}

// Problem is one error of a goals file, at its line in the file.
type Problem struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func (p Problem) String() string { return fmt.Sprintf("line %d: %s", p.Line, p.Message) }

// ExportFields are the Attributes an export reads: the listed fields and every outcome's Attribute.
func (s Spec) ExportFields() []string {
	seen := map[string]bool{}
	for _, field := range s.Fields {
		seen[field] = true
	}
	for _, outcome := range s.Outcomes {
		if outcome.Attribute != "" {
			seen[outcome.Attribute] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var (
	fenceOpen   = regexp.MustCompile("^\\s*(```+|~~~+)\\s*yaml\\s+goals\\s*$")
	yamlLineRef = regexp.MustCompile(`line (\d+)`)
)

// Parse reads the one goals block of a goals file, decodes it strictly and
// validates it; a within shorter than durations accepts is a problem.
func Parse(file []byte, durations Durations) (Spec, []Problem) {
	spec, at, problems := decode(file)
	if len(problems) > 0 {
		return Spec{}, problems
	}
	return spec, validate(spec, at, durations)
}

// Warnings lists what a check reports of a goals file without refusing it:
// each within under an hour, which only the server of a local profile accepts
// (evolve.allowShortDurations).
func Warnings(file []byte) []Problem {
	spec, at, problems := decode(file)
	if len(problems) > 0 {
		return nil
	}
	var warnings []Problem
	for i, goal := range spec.Goals {
		if within, err := parseWithin(goal.Within, MinuteDurations); err == nil && within < HourDurations.shortest {
			warnings = append(warnings, Problem{Line: at.goal(i, "within"), Message: "only test profiles accept durations under an hour"})
		}
	}
	return warnings
}

// decode reads the one goals block of a goals file strictly, with the lines of its keys.
func decode(file []byte) (Spec, lines, []Problem) {
	block, offset, problems := goalsBlock(file)
	if len(problems) > 0 {
		return Spec{}, lines{}, problems
	}
	var spec Spec
	decoder := yaml.NewDecoder(bytes.NewReader(block))
	decoder.KnownFields(true)
	if err := decoder.Decode(&spec); err != nil {
		if errors.Is(err, io.EOF) {
			return Spec{}, lines{}, []Problem{{Line: offset, Message: "the goals block is empty"}}
		}
		return Spec{}, lines{}, decodeProblems(err, offset)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(block, &root); err != nil {
		return Spec{}, lines{}, decodeProblems(err, offset)
	}
	return spec, lines{root: &root, offset: offset}, nil
}

// goalsBlock returns the body of the one ```yaml goals fence and the file line before its first body line.
func goalsBlock(file []byte) ([]byte, int, []Problem) {
	all := strings.Split(string(file), "\n")
	var body []string
	start, fence := 0, ""
	found := 0
	for i, line := range all {
		if fence == "" {
			if match := fenceOpen.FindStringSubmatch(line); match != nil {
				found++
				if found > 1 {
					return nil, 0, []Problem{{Line: i + 1, Message: "a goals file has exactly one ```yaml goals block; this is a second one"}}
				}
				fence, start = match[1], i+1
			}
			continue
		}
		if strings.TrimSpace(line) == fence {
			fence = ""
			continue
		}
		body = append(body, line)
	}
	if found == 0 {
		return nil, 0, []Problem{{Line: 1, Message: "no ```yaml goals block"}}
	}
	if fence != "" {
		return nil, 0, []Problem{{Line: start, Message: "the ```yaml goals block is not closed"}}
	}
	return []byte(strings.Join(body, "\n")), start, nil
}

// decodeProblems maps the YAML decoder's errors, whose lines count from the block, to file lines.
func decodeProblems(err error, offset int) []Problem {
	var typeErr *yaml.TypeError
	messages := []string{err.Error()}
	if errors.As(err, &typeErr) {
		messages = typeErr.Errors
	}
	problems := make([]Problem, 0, len(messages))
	for _, message := range messages {
		line := offset
		if match := yamlLineRef.FindStringSubmatch(message); match != nil {
			n, _ := strconv.Atoi(match[1])
			line = offset + n
			message = strings.Replace(strings.TrimPrefix(message, "yaml: "), match[0]+":", "", 1)
		}
		message = strings.TrimSpace(strings.TrimPrefix(message, "yaml: "))
		problems = append(problems, Problem{Line: line, Message: message})
	}
	return problems
}

// lines finds the file line of a key in the decoded block.
type lines struct {
	root   *yaml.Node
	offset int
}

func (l lines) mapping() *yaml.Node {
	if l.root.Kind == yaml.DocumentNode && len(l.root.Content) > 0 {
		return l.root.Content[0]
	}
	return l.root
}

// top is the line of a top-level key, or of the block when the key is absent.
func (l lines) top(key string) int {
	if node := valueOf(l.mapping(), key); node != nil {
		return l.offset + node.Line
	}
	return l.offset
}

// outcome is the line of one outcome's entry.
func (l lines) outcome(name string) int {
	if node := valueOf(valueOf(l.mapping(), "outcomes"), name); node != nil {
		return l.offset + node.Line
	}
	return l.top("outcomes")
}

// goal is the line of a key of the index-th goal, or of the goal itself.
func (l lines) goal(index int, key string) int {
	list := valueOf(l.mapping(), "goals")
	if list == nil || list.Kind != yaml.SequenceNode || index >= len(list.Content) {
		return l.top("goals")
	}
	item := list.Content[index]
	if node := valueOf(item, key); node != nil {
		return l.offset + node.Line
	}
	return l.offset + item.Line
}

func valueOf(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
