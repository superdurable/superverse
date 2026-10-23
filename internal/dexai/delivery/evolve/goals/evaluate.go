// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"crypto/sha256"
	"encoding/hex"
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
	EvaluatedAt time.Time `json:"evaluated_at"`
	Runs        int       `json:"runs"`
	// Superseded counts the records left out because the export holds a newer run of the same FlowID.
	Superseded int         `json:"superseded,omitempty"`
	Goals      []GoalValue `json:"goals"`
}

// GoalValue is one goal's value overall, per release at start and per trial arm.
type GoalValue struct {
	ID string `json:"id"`
	// Digest names the definition the value was measured under (definitionDigest).
	Digest    string          `json:"digest"`
	Kind      Kind            `json:"kind"`
	Statement string          `json:"statement"`
	Metric    string          `json:"metric"`
	Within    string          `json:"within,omitempty"`
	Direction Direction       `json:"direction,omitempty"`
	Guardrail *GuardrailValue `json:"guardrail,omitempty"`
	Target    *float64        `json:"target,omitempty"`
	Value     *float64        `json:"value"`
	Runs      int             `json:"runs"`
	FewRuns   bool            `json:"few_runs"`
	// Maturing counts the runs, open or closed, left out because within has not passed since their decision point, and the open ones that have not reached it.
	Maturing int `json:"maturing,omitempty"`
	// Undated counts, per term, the runs left out because a time or an event the goal needs is not in their record.
	Undated   int             `json:"undated,omitempty"`
	ByRelease map[string]cell `json:"by_release,omitempty"`
	ByVariant map[string]cell `json:"by_variant,omitempty"`
}

// cell is a value over a subset of the runs.
type cell struct {
	Value    *float64 `json:"value"`
	Runs     int      `json:"runs"`
	Maturing int      `json:"maturing,omitempty"`
	Undated  int      `json:"undated,omitempty"`
}

// GuardrailValue is a guardrail and whether the value keeps it; Holds is absent without a value.
type GuardrailValue struct {
	Max   *float64 `json:"max,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Holds *bool    `json:"holds,omitempty"`
}

// Evaluate computes every goal over the records, keeping each FlowID's newest
// run (runexport.Canonical); at is the time open runs are measured to, the
// export's time, since the records know nothing later.
func Evaluate(spec Spec, records []runexport.Record, at time.Time) (Metrics, error) {
	newest := runexport.Canonical(records)
	result := Metrics{EvaluatedAt: at.UTC(), Runs: len(newest), Superseded: len(records) - len(newest)}
	for _, goal := range spec.Goals {
		m, err := compileMetric(goal.Metric, spec)
		if err != nil {
			return Metrics{}, fmt.Errorf("goal %s: %w", goal.ID, err)
		}
		e := evaluator{spec: spec, at: at}
		if goal.Within != "" {
			// The goals' reader checked the within; a local profile's accepts one from a minute.
			if e.within, err = parseWithin(goal.Within, MinuteDurations); err != nil {
				return Metrics{}, fmt.Errorf("goal %s: %w", goal.ID, err)
			}
			if !m.dated() {
				return Metrics{}, fmt.Errorf("goal %s: within %q does not apply to %s", goal.ID, goal.Within, goal.Metric)
			}
		}
		v := e.metric(m, newest)
		gv := GoalValue{
			ID: goal.ID, Digest: definitionDigest(goal), Kind: goal.Kind, Statement: goal.Statement, Metric: goal.Metric, Within: goal.Within,
			Direction: goal.Direction, Target: goal.Target, Value: v.value, Runs: v.runs, FewRuns: v.runs < fewRuns, Maturing: v.maturing, Undated: v.undated,
			ByRelease: e.split(m, newest, func(r runexport.Record) string { return r.ReleaseAtStart }),
			ByVariant: e.split(m, newest, func(r runexport.Record) string {
				if r.Variant == nil {
					return ""
				}
				return *r.Variant
			}),
		}
		if goal.Guardrail != nil {
			gv.Guardrail = &GuardrailValue{Max: goal.Guardrail.Max, Min: goal.Guardrail.Min}
			if v.value != nil {
				holds := keeps(*goal.Guardrail, *v.value)
				gv.Guardrail.Holds = &holds
			}
		}
		result.Goals = append(result.Goals, gv)
	}
	return result, nil
}

// definitionDigest is a short digest of what a goal's value means: its metric,
// its within, its direction or guardrail and its target. Two goals with one ID
// and the same digest measure the same thing; another digest is another
// definition, such as a looser guardrail, whose values never stand for this
// one's. A goal without within has the digest it had before within existed.
func definitionDigest(goal Goal) string {
	definition, err := json.Marshal(struct {
		Metric    string     `json:"metric"`
		Within    string     `json:"within,omitempty"`
		Direction Direction  `json:"direction,omitempty"`
		Guardrail *Guardrail `json:"guardrail,omitempty"`
		Target    *float64   `json:"target,omitempty"`
	}{goal.Metric, goal.Within, goal.Direction, goal.Guardrail, goal.Target})
	if err != nil {
		panic("goals: a goal definition does not encode: " + err.Error())
	}
	sum := sha256.Sum256(definition)
	return hex.EncodeToString(sum[:8])
}

// DefinitionDigestForTest is definitionDigest, which tests compute as a review does.
var DefinitionDigestForTest = definitionDigest

type evaluator struct {
	spec Spec
	at   time.Time
	// within is the goal's within, zero without one; it applies to a dated metric's one term.
	within time.Duration
}

// measured is a goal's value, the runs it rests on and the runs within or a missing time left out.
type measured struct {
	value    *float64
	runs     int
	maturing int
	undated  int
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
		v := e.metric(m, group)
		cells[k] = cell{Value: v.value, Runs: v.runs, Maturing: v.maturing, Undated: v.undated}
	}
	return cells
}

// metric evaluates a goal's metric over the records from each run's goalShare:
// a ratio rests on its denominator's runs, and a run left out of a term counts
// once for each term.
func (e evaluator) metric(m metric, records []runexport.Record) measured {
	var result measured
	result.value, result.runs = ratio(m, func(t term) []share {
		shares := make([]share, 0, len(records))
		for _, record := range records {
			s, status := e.goalShare(t, record)
			switch status {
			case runMaturing:
				result.maturing++
			case runUndated:
				result.undated++
			case runCounts:
				shares = append(shares, s)
			}
		}
		return shares
	})
	return result
}

// share is one run's part in a term: the chance the term selects it, the chance that it is also
// the term's outcome, and its number. A recorded run's chances are 0 or 1; the backtest weighs
// an estimated run by the runs that took its new path.
type share struct {
	selected float64
	hit      float64
	value    float64
	counted  bool
}

// ratio combines the shares of a metric's terms into its value and the runs it rests on.
func ratio(m metric, shares func(term) []share) (*float64, int) {
	return ratioSides(m, shares, shares)
}

// ratioSides is ratio with the numerator's and the denominator's shares taken apart.
func ratioSides(m metric, numerator, denominator func(term) []share) (*float64, int) {
	value, weight := combine(m.numerator.fn, numerator(m.numerator))
	if m.denominator == nil {
		return value, int(math.Round(weight))
	}
	denominatorValue, denominatorWeight := combine(m.denominator.fn, denominator(*m.denominator))
	runs := int(math.Round(denominatorWeight))
	if value == nil || denominatorValue == nil || *denominatorValue == 0 {
		return nil, runs
	}
	result := *value / *denominatorValue
	return &result, runs
}

// share is a run's part in a term from its recorded facts as they are at the export, with numbers
// replacing recorded per-run numbers by name; the backtest reads runs with it.
func (e evaluator) share(t term, record runexport.Record, numbers map[string]float64) share {
	if t.where != nil && !e.holds(*t.where, record) {
		return share{}
	}
	switch t.fn {
	case aggregateCount:
		return share{selected: 1, hit: indicator(e.outcome(t.outcome, record))}
	case aggregateRate:
		if !e.outcome(t.of, record) {
			return share{}
		}
		return share{selected: 1, hit: indicator(e.outcome(t.outcome, record))}
	default:
		value, counted := e.numberWith(t.number, record, numbers)
		return share{selected: 1, value: value, counted: counted}
	}
}

func indicator(holds bool) float64 {
	if holds {
		return 1
	}
	return 0
}

// combine aggregates a term's shares and returns the weight of runs the value rests on.
func combine(fn aggregate, shares []share) (*float64, float64) {
	switch fn {
	case aggregateCount:
		hits, runs := 0.0, 0.0
		for _, s := range shares {
			hits += s.hit
			runs += s.selected
		}
		return &hits, runs
	case aggregateRate:
		hits, base := 0.0, 0.0
		for _, s := range shares {
			hits += s.hit
			base += s.selected
		}
		if base == 0 {
			return nil, 0
		}
		rate := hits / base
		return &rate, base
	default:
		var values []share
		for _, s := range shares {
			if s.counted && s.selected > 0 {
				values = append(values, s)
			}
		}
		if len(values) == 0 {
			return nil, 0
		}
		return summarize(fn, values)
	}
}

// summarize aggregates numbers weighted by the chance each run is selected; a percentile is the
// first value whose cumulative weight reaches its share of the total.
func summarize(fn aggregate, values []share) (*float64, float64) {
	total, weight := 0.0, 0.0
	for _, v := range values {
		total += v.selected * v.value
		weight += v.selected
	}
	var result float64
	switch fn {
	case aggregateMean:
		result = total / weight
	case aggregateMax:
		result = math.Inf(-1)
		for _, v := range values {
			result = math.Max(result, v.value)
		}
	case aggregateP50, aggregateP90:
		sorted := append([]share(nil), values...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].value < sorted[j].value })
		target := 0.5 * weight
		if fn == aggregateP90 {
			target = 0.9 * weight
		}
		cumulative := 0.0
		for _, v := range sorted {
			cumulative += v.selected
			result = v.value
			if cumulative >= target {
				break
			}
		}
	default:
		result = total
	}
	return &result, weight
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
	return ok && matches(o, value)
}

// numberWith is a run's value of a per-run number, taken from numbers by its name when present
// there; Steps, Actions and waits a run never had count as zero. Fields always come from the record.
func (e evaluator) numberWith(n number, record runexport.Record, numbers map[string]float64) (float64, bool) {
	if value, ok := numbers[n.key()]; ok && n.kind != numberField {
		return value, true
	}
	return e.number(n, record)
}

// number is a run's recorded value of a per-run number; a run whose start its
// record does not hold has no duration.
func (e evaluator) number(n number, record runexport.Record) (float64, bool) {
	switch n.kind {
	case numberDuration:
		if record.HistoryFrom != nil {
			return 0, false
		}
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
	case int64:
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
