// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// fewRuns is the run count below which a value is shown as "few runs" and never headlines a proposal.
const fewRuns = 20

// Metrics is the evaluation of every goal over one export.
type Metrics struct {
	EvaluatedAt time.Time   `json:"evaluated_at"`
	Runs        int         `json:"runs"`
	Goals       []GoalValue `json:"goals"`
}

// GoalValue is one goal's value overall, per release at start and per trial arm.
type GoalValue struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	Statement string          `json:"statement"`
	Metric    string          `json:"metric"`
	Direction Direction       `json:"direction,omitempty"`
	Guardrail *GuardrailValue `json:"guardrail,omitempty"`
	Target    *float64        `json:"target,omitempty"`
	Value     *float64        `json:"value"`
	Runs      int             `json:"runs"`
	FewRuns   bool            `json:"few_runs"`
	ByRelease map[string]cell `json:"by_release,omitempty"`
	ByVariant map[string]cell `json:"by_variant,omitempty"`
}

// cell is a value over a subset of the runs.
type cell struct {
	Value *float64 `json:"value"`
	Runs  int      `json:"runs"`
}

// GuardrailValue is a guardrail and whether the value keeps it; Holds is absent without a value.
type GuardrailValue struct {
	Max   *float64 `json:"max,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Holds *bool    `json:"holds,omitempty"`
}

// Evaluate computes every goal over the records; at is the time open runs are measured to.
func Evaluate(spec Spec, records []runexport.Record, at time.Time) (Metrics, error) {
	result := Metrics{EvaluatedAt: at.UTC(), Runs: len(records)}
	for _, goal := range spec.Goals {
		m, err := compileMetric(goal.Metric, spec)
		if err != nil {
			return Metrics{}, fmt.Errorf("goal %s: %w", goal.ID, err)
		}
		e := evaluator{spec: spec, at: at}
		value, runs := e.metric(m, records)
		gv := GoalValue{
			ID: goal.ID, Kind: goal.Kind, Statement: goal.Statement, Metric: goal.Metric, Direction: goal.Direction,
			Target: goal.Target, Value: value, Runs: runs, FewRuns: runs < fewRuns,
			ByRelease: e.split(m, records, func(r runexport.Record) string { return r.ReleaseAtStart }),
			ByVariant: e.split(m, records, func(r runexport.Record) string {
				if r.Variant == nil {
					return ""
				}
				return *r.Variant
			}),
		}
		if goal.Guardrail != nil {
			gv.Guardrail = &GuardrailValue{Max: goal.Guardrail.Max, Min: goal.Guardrail.Min}
			if value != nil {
				holds := (goal.Guardrail.Max == nil || *value <= *goal.Guardrail.Max) && (goal.Guardrail.Min == nil || *value >= *goal.Guardrail.Min)
				gv.Guardrail.Holds = &holds
			}
		}
		result.Goals = append(result.Goals, gv)
	}
	return result, nil
}

type evaluator struct {
	spec Spec
	at   time.Time
}

// split evaluates the metric per key; runs without a key are left out, and a single key is no split.
func (e evaluator) split(m metric, records []runexport.Record, key func(runexport.Record) string) map[string]cell {
	groups := map[string][]runexport.Record{}
	for _, record := range records {
		if k := key(record); k != "" {
			groups[k] = append(groups[k], record)
		}
	}
	if len(groups) < 2 {
		return nil
	}
	cells := make(map[string]cell, len(groups))
	for k, group := range groups {
		value, runs := e.metric(m, group)
		cells[k] = cell{Value: value, Runs: runs}
	}
	return cells
}

// metric returns the value and the run count it rests on: a ratio rests on its denominator's runs.
func (e evaluator) metric(m metric, records []runexport.Record) (*float64, int) {
	value, runs := e.term(m.numerator, records)
	if m.denominator == nil {
		return value, runs
	}
	denominator, denominatorRuns := e.term(*m.denominator, records)
	if value == nil || denominator == nil || *denominator == 0 {
		return nil, denominatorRuns
	}
	ratio := *value / *denominator
	return &ratio, denominatorRuns
}

func (e evaluator) term(t term, records []runexport.Record) (*float64, int) {
	var selected []runexport.Record
	for _, record := range records {
		if t.where == nil || e.holds(*t.where, record) {
			selected = append(selected, record)
		}
	}
	switch t.fn {
	case aggregateCount:
		n := 0
		for _, record := range selected {
			if e.outcome(t.outcome, record) {
				n++
			}
		}
		count := float64(n)
		return &count, len(selected)
	case aggregateRate:
		hits, base := 0, 0
		for _, record := range selected {
			if e.outcome(t.of, record) {
				base++
				if e.outcome(t.outcome, record) {
					hits++
				}
			}
		}
		if base == 0 {
			return nil, 0
		}
		rate := float64(hits) / float64(base)
		return &rate, base
	}
	var values []float64
	for _, record := range selected {
		if v, ok := e.number(t.number, record); ok {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return nil, 0
	}
	result := summarize(t.fn, values)
	return &result, len(values)
}

func summarize(fn aggregate, values []float64) float64 {
	switch fn {
	case aggregateSum, aggregateMean:
		total := 0.0
		for _, v := range values {
			total += v
		}
		if fn == aggregateMean {
			return total / float64(len(values))
		}
		return total
	case aggregateMax:
		best := math.Inf(-1)
		for _, v := range values {
			best = math.Max(best, v)
		}
		return best
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	share := 0.5
	if fn == aggregateP90 {
		share = 0.9
	}
	rank := int(math.Ceil(share*float64(len(sorted)))) - 1
	return sorted[max(rank, 0)]
}

func (e evaluator) holds(c condition, record runexport.Record) bool {
	if c.outcome != "" {
		return e.outcome(c.outcome, record) != c.negate
	}
	value, ok := record.Fields[c.field]
	return ok && sameValue(value, c.value)
}

func (e evaluator) outcome(name string, record runexport.Record) bool {
	o := e.spec.Outcomes[name]
	switch {
	case o.Step != "":
		return record.Steps[o.Step] > 0
	case o.Action != "":
		return record.Actions[o.Action] > 0
	}
	value, ok := record.Fields[o.Attribute]
	if !ok {
		return false
	}
	if o.Equals != nil {
		return sameValue(value, o.Equals)
	}
	for _, candidate := range o.In {
		if sameValue(value, candidate) {
			return true
		}
	}
	return false
}

// number is a run's value of a per-run number; Steps, Actions and waits a run never had count as zero.
func (e evaluator) number(n number, record runexport.Record) (float64, bool) {
	switch n.kind {
	case numberDuration:
		end := e.at
		if record.ClosedAt != nil {
			end = *record.ClosedAt
		}
		return end.Sub(record.StartedAt).Hours(), true
	case numberSteps:
		return float64(record.Steps[n.name]), true
	case numberActions:
		return float64(record.Actions[n.name]), true
	case numberWait:
		return record.WaitHours[n.name], true
	case numberAI:
		switch n.name {
		case "calls":
			return float64(record.AI.Calls), true
		case "input_tokens":
			return float64(record.AI.InputTokens), true
		case "output_tokens":
			return float64(record.AI.OutputTokens), true
		}
		if record.AI.USD == nil {
			return 0, false
		}
		return *record.AI.USD, true
	}
	return numeric(record.Fields[n.name])
}

func numeric(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

// sameValue compares a record's value with a goals value: numbers by value, everything else by its text.
func sameValue(a, b any) bool {
	if x, ok := numeric(a); ok {
		if y, ok := numeric(b); ok {
			return x == y
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
