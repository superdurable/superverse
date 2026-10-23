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
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// wilsonZ is the standard normal quantile of a two-sided 90% interval.
const wilsonZ = 1.6448536269514722

// bound picks the point estimate or an end of its interval.
type bound int

const (
	boundPoint bound = iota
	boundLow
	boundHigh
)

// opposite is the other end of an interval; the point stays the point.
func (b bound) opposite() bound {
	switch b {
	case boundLow:
		return boundHigh
	case boundHigh:
		return boundLow
	case boundPoint:
	}
	return b
}

// literal is an outcome, or its negation, that a term reads of a run.
type literal struct {
	outcome string
	negate  bool
}

// landmark is where a changed run's path leaves its recorded one: the point of its first changed
// decision, the time its recorded path entered that point, and the pool of recorded runs that
// reached the same point in the same stratum and took the run's new answer there.
type landmark struct {
	decision string
	point    decisionPoint
	at       time.Time
	pool     string
	// final is set when nothing can follow the landmark, so the change moves no Step outcome.
	final bool
}

// poolRun is a recorded closed run at a decision point, with its before line and the time it entered the point.
type poolRun struct {
	record *runexport.Record
	line   DecisionLine
	at     time.Time
}

// chance is how the backtest knows the chance that a set of outcome literals holds for a changed
// run after the change: decided at the landmark, estimated from at-risk pools, or unknown.
type chance struct {
	status goalStatus
	// value is the decided chance, 0 or 1.
	value float64
	// point, low and high are an estimated chance and the ends of its Wilson interval.
	point, low, high float64
	// pools are the at-risk pools the estimate drew on, with their sizes.
	pools map[string]int
}

// at is a chance's decided value, or its estimate at the bound.
func (ch chance) at(b bound) float64 {
	switch {
	case ch.status != statusEstimated:
		return ch.value
	case b == boundLow:
		return ch.low
	case b == boundHigh:
		return ch.high
	}
	return ch.point
}

// index files every recorded closed run with a before line under each decision point its path
// reached, by stratum, decision and the answer it took there.
func (c *comparison) index(records []runexport.Record, before map[string]DecisionLine) {
	c.pools, c.chances, c.untimed = map[string][]poolRun{}, map[string]chance{}, map[string]bool{}
	for i := range records {
		record := &records[i]
		line, ok := before[record.FlowID]
		if !ok || record.ClosedAt == nil {
			continue
		}
		for _, name := range sortedKeys(line.Points) {
			point := line.Points[name]
			if at, reached := pointTime(*record, point); reached {
				key := c.poolKey(*record, name, point, line.Decisions[name])
				c.pools[key] = append(c.pools[key], poolRun{record: record, line: line, at: at})
			}
		}
	}
}

// poolKey is a run's stratum, the values of the strata fields, with a decision point and an answer there.
func (c *comparison) poolKey(record runexport.Record, decision string, point decisionPoint, answer any) string {
	values := make([]any, 0, len(c.strata)+4)
	for _, field := range c.strata {
		values = append(values, record.Fields[field])
	}
	values = append(values, decision, point.Step, point.Entry, answer)
	key, err := json.Marshal(values)
	if err != nil {
		panic(err) // Record fields and decisions are decoded JSON scalars.
	}
	return string(key)
}

// policyOf keys an after line's decisions and points, which every later step of an estimate follows.
func policyOf(line DecisionLine) string {
	key, err := json.Marshal([]any{line.Decisions, line.Points})
	if err != nil {
		panic(err) // Decisions are decoded JSON scalars.
	}
	return string(key)
}

// landmarkOf places a changed run's landmark at the earliest point on its recorded path of the
// decisions the two lines answer differently. It is nil, so every outcome the run's goals read is
// unknown, when a changed decision has no point, when the recorded path lacks the point of a
// decision the before line took (a truncated path), when two changed decisions share the earliest
// point, or when the after line does not take the first one. A changed decision only the after
// line takes, at a point the recorded path never reached, comes after the paths part; the at-risk
// pools check it on the runs that did reach it.
func (c *comparison) landmarkOf(record runexport.Record, before, after DecisionLine) *landmark {
	var first *landmark
	tied := false
	for _, name := range changedDecisions(before.Decisions, after.Decisions) {
		point, placed := before.Points[name]
		if !placed {
			point, placed = after.Points[name]
		}
		if !placed {
			return nil
		}
		at, reached := pointTime(record, point)
		_, taken := before.Decisions[name]
		switch {
		case !reached && taken:
			return nil
		case !reached:
			continue
		case first == nil || at.Before(first.at):
			first, tied = &landmark{decision: name, point: point, at: at}, false
		case at.Equal(first.at):
			tied = true
		}
	}
	if first == nil || tied {
		return nil
	}
	answer, answered := after.Decisions[first.decision]
	if !answered {
		return nil
	}
	first.pool = c.poolKey(record, first.decision, first.point, answer)
	first.final = finalAt(record, before, after, first.point, first.at)
	return first
}

// finalAt reports whether nothing can follow a landmark: it is the last entry on the closed
// record's path, the after line takes no decision at a point past it, and the after line carries
// steps.* numbers equal to the before line's, so the after path enters no Step the record lacks.
func finalAt(record runexport.Record, before, after DecisionLine, point decisionPoint, at time.Time) bool {
	if record.ClosedAt == nil || len(record.Path) == 0 {
		return false
	}
	if last := record.Path[len(record.Path)-1]; last.Step != point.Step || !last.At.Equal(at) {
		return false
	}
	for _, name := range sortedKeys(after.Points) {
		if t, reached := pointTime(record, after.Points[name]); !reached || t.After(at) {
			return false
		}
	}
	prefix, afterSteps, beforeSteps := string(numberSteps)+".", 0, 0
	for name, value := range after.Numbers {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		afterSteps++
		if recorded, ok := before.Numbers[name]; !ok || !closeEnough(recorded, value) {
			return false
		}
	}
	for name := range before.Numbers {
		if strings.HasPrefix(name, prefix) {
			beforeSteps++
		}
	}
	return afterSteps > 0 && afterSteps == beforeSteps
}

// pointTime is when a run entered the point's Step for the point's Entry-th time; false when its
// path does not hold that entry. A truncated path keeps the newest entries, so the entries the
// record counts in Steps and the path lacks are the oldest.
func pointTime(record runexport.Record, point decisionPoint) (time.Time, bool) {
	kept := 0
	for _, entry := range record.Path {
		if entry.Step == point.Step {
			kept++
		}
	}
	position := point.Entry - max(0, record.Steps[point.Step]-kept)
	if position < 1 || position > kept {
		return time.Time{}, false
	}
	for _, entry := range record.Path {
		if entry.Step != point.Step {
			continue
		}
		if position--; position == 0 {
			return entry.At, true
		}
	}
	return time.Time{}, false
}

// heldBefore reports whether an outcome already held for a run before time at, and whether the
// record can tell. It is the one place outcome times enter the backtest. A Step outcome holds from
// the run's first entry into the Step, which its path records (an entry a truncated path dropped
// is older than every entry it kept, so older than any landmark on it). The backtest does not read
// the record's Action first times or field timelines yet, so a landmark cannot place an Attribute or
// Action outcome.
func (e evaluator) heldBefore(name string, record runexport.Record, at time.Time) (held, timed bool) {
	step := e.spec.Outcomes[name].Step
	if step == "" {
		return false, false
	}
	kept := 0
	for _, entry := range record.Path {
		if entry.Step != step {
			continue
		}
		if entry.At.Before(at) {
			return true, true
		}
		kept++
	}
	return record.Steps[step] > kept, true
}

// standing is where a run stood on each literal's outcome before time at (held or not yet), and
// whether that decides the literals: 0 once a negated outcome held, 1 once every outcome held
// unnegated. timed is false when an outcome the record cannot time leaves them open.
func (c *comparison) standing(literals []literal, record runexport.Record, at time.Time) (stood []bool, decided bool, value float64, timed bool) {
	stood, decided, timed = make([]bool, len(literals)), true, true
	for i, l := range literals {
		held, ok := c.e.heldBefore(l.outcome, record, at)
		switch {
		case !ok:
			timed = false
		case held && l.negate:
			return stood, true, 0, true
		}
		stood[i] = held
		decided = decided && ok && held
	}
	if !timed {
		return nil, false, 0, false
	}
	return stood, decided, indicator(decided), true
}

// literals are the outcome literals of a term a run's share depends on: those that select it, and
// those that also make it a hit of a count or rate; selected is false when a field condition
// leaves the run out, which the change cannot alter.
func (c *comparison) literals(t term, record runexport.Record) (selection, hit []literal, selected bool) {
	if t.where != nil {
		if t.where.outcome == "" {
			if !c.e.holds(*t.where, record) {
				return nil, nil, false
			}
		} else {
			selection = append(selection, literal{outcome: t.where.outcome, negate: t.where.negate})
		}
	}
	if t.fn == aggregateRate {
		selection = append(selection, literal{outcome: t.of})
	}
	if t.fn == aggregateCount || t.fn == aggregateRate {
		hit = append(append([]literal{}, selection...), literal{outcome: t.outcome})
	}
	return selection, hit, true
}

// outcomeStatus is how the backtest knows a changed run's value of a goal whose metric reads an
// outcome: known when the landmark decides every chance the metric needs, estimated when at-risk
// pools of at least minEstimateRuns runs give each other one, unknown otherwise.
func (c *comparison) outcomeStatus(plan goalPlan, run *comparedRun) goalStatus {
	status := statusKnown
	for _, t := range plan.metric.terms() {
		selection, hit, selected := c.literals(t, run.record)
		if !selected {
			continue
		}
		for _, set := range [][]literal{selection, hit} {
			switch ch := c.chanceOf(*run, set); ch.status {
			case statusUnknown:
				return statusUnknown
			case statusEstimated:
				status = statusEstimated
				for key, n := range ch.pools {
					run.pools[key] = n
				}
			case statusKnown:
			}
		}
	}
	return status
}

// chanceOf is the chance that every literal holds for a changed run after the change. A literal
// whose outcome held before the landmark keeps its recorded value, since the change cannot undo
// it, and so do all of them when nothing can follow the landmark. Otherwise the chance comes from
// the run's at-risk pool at its landmark.
func (c *comparison) chanceOf(run comparedRun, literals []literal) chance {
	if len(literals) == 0 {
		return chance{status: statusKnown, value: 1}
	}
	if run.landmark == nil {
		return chance{status: statusUnknown}
	}
	stood, decided, value, timed := c.standing(literals, run.record, run.landmark.at)
	switch {
	case !timed:
		for _, l := range literals {
			if _, ok := c.e.heldBefore(l.outcome, run.record, run.landmark.at); !ok {
				c.untimed[l.outcome] = true
			}
		}
		return chance{status: statusUnknown}
	case decided:
		return chance{status: statusKnown, value: value}
	case run.landmark.final:
		return chance{status: statusKnown, value: indicator(c.allHold(literals, run.record))}
	}
	return c.atRisk(run.landmark.pool, run.landmark.point, literals, stood, run.after, run.policy, pointKey(run.landmark.point))
}

// atRisk estimates the chance that every literal holds, after the change, for a run that stood
// as stood says at a decision point, from the at-risk pool there: the recorded closed runs of its
// stratum that reached the point, took the after line's answer there and stood alike (each
// outcome held, or not yet, by their own time at the point). A pool run whose record later
// answers a decision of the after line otherwise (it deviates) counts as recorded up to that
// point and, while the literals are still open there, by the chance from the at-risk pool at
// that point in turn, so every run counted follows the after line. visited lists the points of
// the chain, which cannot repeat one.
func (c *comparison) atRisk(pool string, at decisionPoint, literals []literal, stood []bool, after DecisionLine, policy, visited string) chance {
	key := strings.Join([]string{pool, literalsKey(literals, stood), policy, visited}, "\x00")
	if cached, ok := c.chances[key]; ok {
		return cached
	}
	unknown := chance{status: statusUnknown}
	n, sum, low, high := 0, 0.0, 0.0, 0.0
	pools := map[string]int{}
	for _, member := range c.pools[pool] {
		if !c.stoodAlike(literals, stood, member) {
			continue
		}
		n++
		name, point, when, deviates := deviation(member, at, after)
		if !deviates {
			hit := indicator(c.allHold(literals, *member.record))
			sum, low, high = sum+hit, low+hit, high+hit
			continue
		}
		next := pointKey(point)
		memberStood, decided, value, timed := c.standing(literals, *member.record, when)
		switch {
		case name == "" || !timed || strings.Contains(visited, next):
			c.chances[key] = unknown
			return unknown
		case decided:
			sum, low, high = sum+value, low+value, high+value
			continue
		}
		nested := c.atRisk(c.poolKey(*member.record, name, point, after.Decisions[name]), point, literals, memberStood, after, policy, visited+next)
		if nested.status != statusEstimated {
			c.chances[key] = unknown
			return unknown
		}
		sum, low, high = sum+nested.point, low+nested.low, high+nested.high
		for nestedKey, size := range nested.pools {
			pools[nestedKey] = size
		}
	}
	if n < minEstimateRuns {
		c.chances[key] = unknown
		return unknown
	}
	pools[pool] = n
	result := chance{status: statusEstimated, point: sum / float64(n), low: wilson(low/float64(n), n, boundLow), high: wilson(high/float64(n), n, boundHigh), pools: pools}
	c.chances[key] = result
	return result
}

// deviation finds where a pool run first leaves the after line past the pool's point: the earliest
// point on its record, from its own time at the pool's point, at which the after line takes a
// decision that its before line did not take there with the same answer. name is empty when the
// deviation cannot be placed: two such points tie, or the after line takes without a point a
// decision the pool run answered otherwise.
func deviation(member poolRun, from decisionPoint, after DecisionLine) (name string, point decisionPoint, at time.Time, deviates bool) {
	tied := false
	for _, decision := range sortedKeys(after.Decisions) {
		answer := after.Decisions[decision]
		theirs, took := member.line.Decisions[decision]
		q, placed := after.Points[decision]
		if !placed {
			if took && theirs != answer {
				return "", decisionPoint{}, time.Time{}, true
			}
			continue
		}
		t, reached := pointTime(*member.record, q)
		if q == from || !reached || t.Before(member.at) || (took && member.line.Points[decision] == q && theirs == answer) {
			continue
		}
		switch {
		case !deviates || t.Before(at):
			name, point, at, deviates, tied = decision, q, t, true, false
		case t.Equal(at):
			tied = true
		}
	}
	if tied {
		return "", decisionPoint{}, time.Time{}, true
	}
	return name, point, at, deviates
}

func pointKey(point decisionPoint) string { return fmt.Sprintf("|%s#%d|", point.Step, point.Entry) }

// literalsKey names a set of literals with where a run stood on each.
func literalsKey(literals []literal, stood []bool) string {
	parts := make([]string, 0, len(literals))
	for i, l := range literals {
		part := l.outcome
		if l.negate {
			part = "!" + part
		}
		if stood[i] {
			part += "@held"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}

// stoodAlike reports whether a pool run stood at its landmark where the estimated run stood at its own.
func (c *comparison) stoodAlike(literals []literal, stood []bool, member poolRun) bool {
	for i, l := range literals {
		held, timed := c.e.heldBefore(l.outcome, *member.record, member.at)
		if !timed || held != stood[i] {
			return false
		}
	}
	return true
}

func (c *comparison) allHold(literals []literal, record runexport.Record) bool {
	for _, l := range literals {
		if c.e.outcome(l.outcome, record) == l.negate {
			return false
		}
	}
	return true
}

// wilson is an end of the Wilson interval of a share p of n runs.
func wilson(p float64, n int, b bound) float64 {
	size := float64(n)
	z2 := wilsonZ * wilsonZ
	center := (p + z2/(2*size)) / (1 + z2/size)
	half := wilsonZ / (1 + z2/size) * math.Sqrt(math.Max(0, p*(1-p))/size+z2/(4*size*size))
	if b == boundLow {
		return math.Max(0, center-half)
	}
	return math.Min(1, center+half)
}

// boundedChance is a chance at a bound: decided, estimated, or fallback when the backtest cannot tell.
func (c *comparison) boundedChance(run comparedRun, literals []literal, b bound, fallback float64) float64 {
	if ch := c.chanceOf(run, literals); ch.status != statusUnknown {
		return ch.at(b)
	}
	return fallback
}

// estimatedShare is a changed run's share of a term after the change, decided at its landmark or
// estimated from its at-risk pools, with the run's own after numbers. A count or rate takes its
// selection at the point and its hit at the bound, so a rate's harmful end does not shrink its
// denominator with its numerator. A chance the backtest cannot tell, for an unknown run at the
// harmful end, is at its worst: hit 0 at the low end and 1 at the high end, selected for a count
// or rate, and selected as recorded for any other aggregate.
func (c *comparison) estimatedShare(t term, run comparedRun, b bound) share {
	selection, hit, selected := c.literals(t, run.record)
	if !selected {
		return share{}
	}
	if !overOutcome[t.fn] {
		s := share{selected: c.boundedChance(run, selection, b, indicator(c.allHold(selection, run.record)))}
		s.value, s.counted = c.e.numberWith(t.number, run.record, run.after.Numbers)
		return s
	}
	s := share{selected: c.boundedChance(run, selection, boundPoint, 1), hit: c.boundedChance(run, hit, b, indicator(b == boundHigh))}
	if t.fn == aggregateRate {
		s.hit = math.Min(s.hit, s.selected)
	} else {
		s.selected = math.Max(s.selected, s.hit)
	}
	return s
}

// estimation summarizes the pools the changed_estimated runs drew on, with the runs the backtest
// could not place and the outcomes it could not time.
func (c *comparison) estimation() estimation {
	result := estimation{Method: estimationMethod, MinRuns: minEstimateRuns, Interval: estimateInterval, UntimedOutcomes: sortedKeys(c.untimed)}
	pools := map[string]bool{}
	var sizes []int
	for _, run := range c.runs {
		if run.changed && run.landmark == nil {
			result.Unplaced++
		}
		if runLabelOf(run) != labelChangedEstimated {
			continue
		}
		smallest := 0
		for key, n := range run.pools {
			pools[key] = true
			if smallest == 0 || n < smallest {
				smallest = n
			}
		}
		sizes = append(sizes, smallest)
	}
	if len(sizes) == 0 {
		return result
	}
	sort.Ints(sizes)
	result.Groups = len(pools)
	result.SampleMin, result.SampleMedian, result.SampleMax = sizes[0], sizes[len(sizes)/2], sizes[len(sizes)-1]
	return result
}
