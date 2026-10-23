// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// maxDecisionLineBytes bounds one decision line, as runexport bounds a record line.
const maxDecisionLineBytes = 1 << 20

// DecisionLine is one run's decisions as a backtest program writes them, one JSON line per run:
// the decisions by name, where on the run's path each is taken, the per-run numbers they
// determine and, optionally, a key of the path they take, which the comparison does not read.
type DecisionLine struct {
	FlowID    string                   `json:"flow_id"`
	Decisions map[string]any           `json:"decisions"`
	Points    map[string]decisionPoint `json:"points,omitempty"`
	Numbers   map[string]float64       `json:"numbers"`
	Path      string                   `json:"path,omitempty"`
}

// decisionPoint is where a run takes a decision: its Entry-th entry into the Step, counted from 1
// over the run's whole path, which the paths before and after a change share up to their first
// changed decision.
type decisionPoint struct {
	Step  string `json:"step"`
	Entry int    `json:"entry"`
}

// decisionLineJSON is the strict wire form, so a null number is an error instead of a zero.
type decisionLineJSON struct {
	FlowID    string                   `json:"flow_id"`
	Decisions map[string]any           `json:"decisions"`
	Points    map[string]decisionPoint `json:"points"`
	Numbers   map[string]*float64      `json:"numbers"`
	Path      string                   `json:"path"`
}

// ReadDecisions decodes every decision line of a JSON Lines stream, strictly; a FlowID appears once.
func ReadDecisions(r io.Reader) ([]DecisionLine, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxDecisionLineBytes)
	var lines []DecisionLine
	seen := map[string]int{}
	for at := 1; scanner.Scan(); at++ {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		line, err := decodeDecisionLine(scanner.Bytes())
		if err != nil {
			return nil, fmt.Errorf("decision line %d: %w", at, err)
		}
		if first, repeated := seen[line.FlowID]; repeated {
			return nil, fmt.Errorf("decision line %d: flow_id %q is also on line %d", at, line.FlowID, first)
		}
		seen[line.FlowID] = at
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("a decision line exceeds %d bytes: %w", maxDecisionLineBytes, err)
		}
		return nil, err
	}
	return lines, nil
}

func decodeDecisionLine(data []byte) (DecisionLine, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire decisionLineJSON
	if err := decoder.Decode(&wire); err != nil {
		return DecisionLine{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return DecisionLine{}, errors.New("unexpected data after the JSON object")
	}
	if wire.FlowID == "" {
		return DecisionLine{}, errors.New("flow_id is required")
	}
	for _, name := range sortedKeys(wire.Decisions) {
		switch wire.Decisions[name].(type) {
		case string, float64, bool, nil:
		default:
			return DecisionLine{}, fmt.Errorf("decision %q is not a scalar (string, number, bool or null)", name)
		}
	}
	for _, name := range sortedKeys(wire.Points) {
		point := wire.Points[name]
		switch _, decided := wire.Decisions[name]; {
		case !decided:
			return DecisionLine{}, fmt.Errorf("point %q names no decision of the line", name)
		case point.Step == "" || point.Entry < 1:
			return DecisionLine{}, fmt.Errorf("point %q needs a step and an entry of at least 1", name)
		}
	}
	line := DecisionLine{FlowID: wire.FlowID, Decisions: wire.Decisions, Points: wire.Points, Path: wire.Path, Numbers: make(map[string]float64, len(wire.Numbers))}
	for _, name := range sortedKeys(wire.Numbers) {
		if _, err := decisionNumber(name); err != nil {
			return DecisionLine{}, err
		}
		value := wire.Numbers[name]
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return DecisionLine{}, fmt.Errorf("number %q needs a finite value", name)
		}
		line.Numbers[name] = *value
	}
	return line, nil
}

// decisionNumber parses a per-run number a decision line may carry: every number of the metric
// language except field.<name>, since the backtest reads fields as recorded facts of the run.
func decisionNumber(name string) (number, error) {
	if strings.HasPrefix(name, string(numberField)+".") {
		return number{}, fmt.Errorf("number %q: a decision line carries no field.<name>; fields are read from the record", name)
	}
	n, err := parseNumber(name, Spec{})
	if err != nil {
		return number{}, fmt.Errorf("number %q: %w", name, err)
	}
	return n, nil
}

// changedDecisions are the names, in order, that two lines decide differently: present in one
// only, or with unequal scalar values. Two lines decide the same when none is.
func changedDecisions(a, b map[string]any) []string {
	var changed []string
	for _, name := range sortedKeys(a) {
		if other, ok := b[name]; !ok || other != a[name] {
			changed = append(changed, name)
		}
	}
	for _, name := range sortedKeys(b) {
		if _, ok := a[name]; !ok {
			changed = append(changed, name)
		}
	}
	return changed
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
