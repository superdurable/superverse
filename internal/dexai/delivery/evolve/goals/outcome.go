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
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// maxWithin is the longest within.
const maxWithin = 90 * 24 * time.Hour

// Durations is the shortest within a goals reader accepts.
type Durations struct {
	shortest time.Duration
	// label is the shortest within as a goals file writes it.
	label string
}

// The readers' shortest within values.
var (
	// HourDurations accepts a within from an hour, as the server does in every
	// profile but a local one.
	HourDurations = Durations{shortest: time.Hour, label: "1h"}
	// MinuteDurations accepts a within from a minute: the server of a local
	// profile with evolve.allowShortDurations, and the readers of the coding
	// image and the dexai CLI, whose check warns about a value under an hour
	// (Warnings).
	MinuteDurations = Durations{shortest: time.Minute, label: "1m"}
)

// withinUnits are the units a within is written in.
var withinUnits = map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "m": time.Minute}

var withinForm = regexp.MustCompile(`^([0-9]{1,6})([a-z])$`)

// parseWithin reads a within such as 7d, 12h or 90m.
func parseWithin(text string, durations Durations) (time.Duration, error) {
	match := withinForm.FindStringSubmatch(text)
	if match == nil || withinUnits[match[2]] == 0 {
		return 0, fmt.Errorf("within: %q is not a time; write it as 7d or 12h", text)
	}
	n, _ := strconv.Atoi(match[1])
	within := time.Duration(n) * withinUnits[match[2]]
	if within < durations.shortest || within > maxWithin {
		return 0, fmt.Errorf("within must be between %s and 90d", durations.label)
	}
	return within, nil
}

// dated reports whether the metric takes a within: one term, a rate, a count
// or an aggregate of a per-run number, and not a ratio of two.
func (m metric) dated() bool { return m.denominator == nil }

// runStatus is whether a run counts in a term of a goal's value, or why it is left out.
type runStatus int

const (
	runCounts runStatus = iota
	// runMaturing: a run, open or closed, whose decision point plus within
	// has not passed, or an open run that has not reached its decision point.
	runMaturing
	// runUndated: the run's record does not hold a time or an event the goal needs.
	runUndated
)

// pointState is what a record tells of a count's or a per-run number's
// decision point by the evaluation time.
type pointState int

const (
	// pointPassed: the run left its Flow's first Step at the time given.
	pointPassed pointState = iota
	// pointAhead: the open run is still in its first Step.
	pointAhead
	// pointUnknown: the record does not hold the run's first entries.
	pointUnknown
)

// goalShare is a run's part in a term of a goal's value, or why the run is
// left out. Every term reads a where field as of the run's decision point;
// with within, a run counts once within passed since its decision point,
// open or closed, and its outcome or number is read as it stood then. A run
// that closed sooner waits to count, so a value does not lean toward the runs
// that close fast. The backtest's share reads every outcome and field as it
// is at the export.
func (e evaluator) goalShare(t term, record runexport.Record) (share, runStatus) {
	if t.fn == aggregateRate {
		return e.rateShare(t, record)
	}
	decided, point := e.leftFirstStep(record)
	var selected, known bool
	switch {
	case point == pointAhead && e.within > 0:
		return share{}, runMaturing
	case point == pointAhead:
		// The run has no value at its decision point yet; it is read as it is now.
		selected, known = e.meets(t.where, record)
	default:
		selected, known = e.selectsAt(t.where, record, decided, point == pointPassed)
	}
	switch {
	case !known:
		return share{}, runUndated
	case !selected:
		return share{}, runCounts
	case e.within == 0:
		return e.atExport(t, record)
	case point == pointUnknown:
		return share{}, runUndated
	case e.at.Sub(decided) < e.within:
		return share{}, runMaturing
	}
	by := decided.Add(e.within)
	if t.fn == aggregateCount {
		hit, known := e.heldBy(t.outcome, record, by)
		if !known {
			return share{}, runUndated
		}
		return share{selected: 1, hit: indicator(hit)}, runCounts
	}
	value, counted, known := e.numberBy(t.number, record, by)
	if !known {
		return share{}, runUndated
	}
	return share{selected: 1, value: value, counted: counted}, runCounts
}

// atExport is a selected run's part in a count or an aggregate of a per-run
// number without within: its outcome or number as of the export.
func (e evaluator) atExport(t term, record runexport.Record) (share, runStatus) {
	if t.fn == aggregateCount {
		hit, known := e.held(t.outcome, record)
		if !known {
			return share{}, runUndated
		}
		return share{selected: 1, hit: indicator(hit)}, runCounts
	}
	value, ok := e.number(t.number, record)
	if !ok && t.number.kind == numberDuration {
		// The record does not hold the run's start.
		return share{}, runUndated
	}
	return share{selected: 1, value: value, counted: ok}, runCounts
}

// rateShare is a run's part in rate(o, of p): the run is selected when p ever
// held, and a where field is read as of the time p first held, the run's
// decision point. With within, the run counts once within passed since its
// decision point, open or closed, and is a hit when o held by then; a younger
// run is maturing, however soon it closed.
func (e evaluator) rateShare(t term, record runexport.Record) (share, runStatus) {
	holds, decided, dated, known := e.decision(t.of, record)
	switch {
	case !known:
		return share{}, runUndated
	case !holds:
		return share{}, runCounts
	}
	selected, known := e.selectsAt(t.where, record, decided, dated)
	switch {
	case !known:
		return share{}, runUndated
	case !selected:
		return share{}, runCounts
	}
	var hit bool
	switch {
	case e.within == 0:
		hit, known = e.held(t.outcome, record)
	case !dated:
		known = false
	case e.at.Sub(decided) < e.within:
		return share{}, runMaturing
	default:
		hit, known = e.heldBy(t.outcome, record, decided.Add(e.within))
	}
	if !known {
		return share{}, runUndated
	}
	return share{selected: 1, hit: indicator(hit)}, runCounts
}

// closed reports whether the run had closed by the evaluation time.
func (e evaluator) closed(record runexport.Record) bool {
	return record.ClosedAt != nil && !record.ClosedAt.After(e.at)
}

// leftFirstStep is the decision point of a count or a per-run number: when
// the run first left its Flow's first Step, which is the second entry of its
// path, or its close when it closed in that Step. It is unknown when the
// record does not hold the run's first entries: its history was read from a
// continuation on, its path was cut to its newest entries, or its history
// was cut before the run left the Step.
func (e evaluator) leftFirstStep(record runexport.Record) (time.Time, pointState) {
	switch {
	case record.HistoryFrom != nil || record.PathTruncated:
		return time.Time{}, pointUnknown
	case len(record.Path) > 1 && !record.Path[1].At.After(e.at):
		return record.Path[1].At, pointPassed
	case len(record.Path) > 1:
		return time.Time{}, pointAhead
	case !record.HistoryCovers(e.at):
		return time.Time{}, pointUnknown
	case e.closed(record):
		return *record.ClosedAt, pointPassed
	}
	return time.Time{}, pointAhead
}

// held reports whether the outcome holds for the run as of the export, as
// outcome does, and whether its record can tell: a Step or Action the record
// does not show may lie in the unread part of an incomplete history.
func (e evaluator) held(name string, record runexport.Record) (holds, known bool) {
	holds = e.outcome(name, record)
	return holds, holds || e.spec.Outcomes[name].Attribute != "" || record.HistoryComplete()
}

// meets reports whether a run meets a where condition as of the export, as
// holds does, and whether its record can tell; no condition selects every run.
func (e evaluator) meets(c *condition, record runexport.Record) (selected, known bool) {
	switch {
	case c == nil:
		return true, true
	case c.outcome != "":
		holds, known := e.held(c.outcome, record)
		return holds != c.negate, known
	}
	return e.holds(*c, record), true
}

// decision reports whether outcome p ever held for the run and when it first
// held, the run's decision point: its first entry into the Step, its Action's
// first acceptance, or the first write of a matching value to the Attribute.
// dated is false when the record does not hold that time; known is false when
// it cannot tell whether p held. A record exported before timelines reads an
// Attribute's current value and dates only a Step, from its untruncated path.
func (e evaluator) decision(name string, record runexport.Record) (holds bool, at time.Time, dated, known bool) {
	o := e.spec.Outcomes[name]
	if !record.Timelines {
		holds, known = e.held(name, record)
		if o.Step != "" && holds && !record.PathTruncated {
			for _, entry := range record.Path {
				if entry.Step == o.Step {
					return true, entry.At, true, true
				}
			}
		}
		return holds, time.Time{}, false, known
	}
	var first time.Time
	switch {
	case o.Step != "":
		first, dated = record.StepsFirstAt[o.Step]
	case o.Action != "":
		first, dated = record.ActionsFirstAt[o.Action]
	default:
		matched, earliest, told := record.FieldMatchedBy(o.Attribute, never, func(value any) bool { return matches(o, value) })
		if !matched && !told {
			// The timeline cannot tell; a current match still says p held, at an unknown time.
			holds = e.outcome(name, record)
			return holds, time.Time{}, false, holds
		}
		return matched, earliest, matched && record.HistoryFrom == nil, true
	}
	holds, known = e.held(name, record)
	return holds, first, holds && dated && record.HistoryFrom == nil, known
}

// never is a time after every run, for reading whether an outcome ever held.
var never = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// heldBy reports whether outcome o held at some time at or before by, which
// is the end of a goal's within: a Step entered, an Action accepted or a
// matching value written by then. known is false when the record does not
// hold the run's events up to by and shows no such event; a record exported
// before timelines tells this only for a Step, from its untruncated path.
func (e evaluator) heldBy(name string, record runexport.Record, by time.Time) (hit, known bool) {
	o := e.spec.Outcomes[name]
	if !record.Timelines {
		if o.Step == "" || record.PathTruncated {
			return false, false
		}
		for _, entry := range record.Path {
			if entry.Step == o.Step && !entry.At.After(by) {
				return true, true
			}
		}
		return false, !record.HistoryTruncated
	}
	var first time.Time
	var dated bool
	switch {
	case o.Step != "":
		first, dated = record.StepsFirstAt[o.Step]
	case o.Action != "":
		first, dated = record.ActionsFirstAt[o.Action]
		if !dated && record.Actions[o.Action] > 0 {
			// An Action beyond the ones whose first time the record keeps.
			return false, false
		}
	default:
		hit, _, known = record.FieldMatchedBy(o.Attribute, by, func(value any) bool { return matches(o, value) })
		return hit, known
	}
	if dated && !first.After(by) {
		return true, true
	}
	return false, record.HistoryCovers(by)
}

// numberBy is a run's per-run number as it stood at by, the end of its goal's
// within: its recorded value when the run closed by then, otherwise the time
// it ran until by, its entries into a Step on its path, its acceptances of an
// Action told by the Action's first time, or a field's value in its timeline.
// known is false when the record cannot tell, as for waits and AI usage, whose
// times a record does not keep.
func (e evaluator) numberBy(n number, record runexport.Record, by time.Time) (value float64, counted, known bool) {
	if record.ClosedAt != nil && !record.ClosedAt.After(by) {
		value, counted = e.number(n, record)
		return value, counted, true
	}
	switch n.kind {
	case numberDuration:
		return by.Sub(record.StartedAt).Hours(), true, true
	case numberField:
		if !record.Timelines {
			return 0, false, false
		}
		current, present, known := record.FieldAsOf(n.name, by)
		if !present {
			return 0, false, known
		}
		value, counted = numeric(current)
		return value, counted, true
	case numberSteps:
		if record.PathTruncated || !record.HistoryCovers(by) {
			return 0, false, false
		}
		entries := 0
		for _, entry := range record.Path {
			if entry.Step == n.name && !entry.At.After(by) {
				entries++
			}
		}
		return float64(entries), true, true
	case numberActions:
		first, dated := record.ActionsFirstAt[n.name]
		accepted := record.Actions[n.name]
		switch {
		case !record.HistoryCovers(by):
			return 0, false, false
		case accepted == 0 || dated && first.After(by):
			return 0, true, true
		case accepted == 1 && dated:
			return 1, true, true
		}
	}
	return 0, false, false
}

// matches reports whether a value meets an Attribute outcome's equals or in.
func matches(o outcomeDefinition, value any) bool {
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

// selectsAt reports whether a run meets a where condition at its decision
// point: a field's value as of then, an outcome as it is. known is false when
// the record does not hold the field's value then. A record exported before
// timelines reads the field's current value.
func (e evaluator) selectsAt(c *condition, record runexport.Record, decided time.Time, dated bool) (selected, known bool) {
	switch {
	case c == nil:
		return true, true
	case c.outcome != "" || !record.Timelines:
		return e.meets(c, record)
	case !dated:
		return false, false
	}
	value, present, known := record.FieldAsOf(c.field, decided)
	return present && sameValue(value, c.value), known
}
