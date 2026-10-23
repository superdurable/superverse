// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// Thresholds of the backtest.
const (
	// calibrationFloor is the least share of runs whose before decisions reproduce their record.
	calibrationFloor = 0.98
	// minEstimateRuns is the least number of at-risk recorded runs an estimate rests on.
	minEstimateRuns = 10
	// estimateInterval is the confidence of an estimate's Wilson interval.
	estimateInterval = 0.9
	// calibrationTolerance absorbs float rounding in recorded hours and dollars.
	calibrationTolerance = 1e-6
	// rateMargin is how much worse a rate goal may get at the harmful end of its interval: 2
	// percentage points; a count may rise or fall by the same share of the runs it counts over.
	rateMargin = 0.02
	// relativeMargin is the same for any other goal, as a share of its value before: 10%, and for
	// a ratio at least one run's worth (1 over the runs it rests on).
	relativeMargin = 0.1
)

// GateCode names a gate of a proposal's backtest; backtest.json and open-runs.json list the gates that fired.
type GateCode string

// The gates, each with its own message.
const (
	// GateBacktestInvalid: calibration is below 98%, so the decision code does not reproduce history.
	GateBacktestInvalid GateCode = "backtest_invalid"
	// GateGuardrailBroken: a guardrail breaks over the runs the backtest knows exactly.
	GateGuardrailBroken GateCode = "guardrail_broken"
	// GateTrialRequired: more than half of the runs bearing on the targeted goal are unknown, or a
	// goal's unknown runs cannot be bounded; more than half bearing on a goal that reads an outcome
	// are estimated and move it; or a directional goal that reads an outcome, targeted or not, may
	// get worse than its margin at the harmful end, with its unknown runs at their worst.
	GateTrialRequired GateCode = "trial_required"
	// GateOpenRunsBlocked: the change removes a Step that open runs are in, or changes an Attribute's value type.
	GateOpenRunsBlocked GateCode = "open_runs_blocked"
)

type gateRule struct {
	summary string
	// refuses is false for a gate that only limits the proposal's kind.
	refuses bool
}

var gateRules = map[GateCode]gateRule{
	GateBacktestInvalid: {summary: "the backtest is invalid: the before decisions do not reproduce the recorded runs", refuses: true},
	GateGuardrailBroken: {summary: "a guardrail breaks over the runs the backtest knows exactly", refuses: true},
	GateTrialRequired:   {summary: "history cannot judge the change, so the proposal must be a trial", refuses: false},
	GateOpenRunsBlocked: {summary: "open runs would fail on the new code", refuses: true},
}

// Gate is a gate that fired, with its message.
type Gate struct {
	Code    GateCode `json:"code"`
	Refuses bool     `json:"refuses"`
	Message string   `json:"message"`
}

func (c GateCode) fire(format string, args ...any) Gate {
	rule, ok := gateRules[c]
	if !ok {
		panic(fmt.Sprintf("gate %q has no rule", c))
	}
	return Gate{Code: c, Refuses: rule.refuses, Message: rule.summary + ": " + fmt.Sprintf(format, args...)}
}

// Refusal is the error of the gates that refuse a proposal; nil when none does.
func Refusal(gates []Gate) error {
	var messages []string
	for _, gate := range gates {
		if gate.Refuses {
			messages = append(messages, fmt.Sprintf("%s: %s", gate.Code, gate.Message))
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return errors.New(strings.Join(messages, "\n"))
}

// CompareOptions choose the strata of the estimates and the goal the proposal targets.
type CompareOptions struct {
	// Strata are fields whose values a matching run shares with the run estimated.
	Strata []string
	// Target is the goal the proposal sets out to move; empty targets every goal with a direction.
	Target string
	// At is the time open runs are measured to.
	At time.Time
}

// Backtest is the comparison of a proposal's decisions with the recorded runs. It holds
// aggregates only, since the application repository commits it with the proposal.
type Backtest struct {
	// Export names the runs the comparison read; a review accepts only its own.
	Export      *ExportBinding   `json:"export,omitempty"`
	EvaluatedAt time.Time        `json:"evaluated_at"`
	Strata      []string         `json:"strata"`
	Runs        backtestRuns     `json:"runs"`
	Calibration calibration      `json:"calibration"`
	Estimation  estimation       `json:"estimation"`
	Goals       []goalComparison `json:"goals"`
	Gates       []Gate           `json:"gates"`
}

// backtestRuns counts the runs by how the backtest knows their after value.
type backtestRuns struct {
	Records int `json:"records"`
	// Compared runs have a record, a before line and an after line; only they are labelled.
	Compared         int `json:"compared"`
	Same             int `json:"same"`
	ChangedExact     int `json:"changed_exact"`
	ChangedEstimated int `json:"changed_estimated"`
	Unknown          int `json:"unknown"`
	MissingBefore    int `json:"missing_before"`
	MissingAfter     int `json:"missing_after"`
	// LinesWithoutRecord are decision lines, before or after, whose FlowID no record has.
	LinesWithoutRecord int `json:"lines_without_record"`
}

// calibration is how well the before decisions reproduce the recorded numbers.
type calibration struct {
	// Share is of the checked runs whose every before number equals the record; null when none was checked.
	Share   *float64 `json:"share"`
	Floor   float64  `json:"floor"`
	Checked int      `json:"checked"`
	Matched int      `json:"matched"`
	// Mismatches count the runs whose before number differs from the record, by number name.
	Mismatches map[string]int `json:"mismatches"`
}

// estimation describes how changed_estimated runs are estimated, with the sample sizes used.
type estimation struct {
	Method   string  `json:"method"`
	MinRuns  int     `json:"min_runs"`
	Interval float64 `json:"interval"`
	// Groups are the distinct at-risk pools the estimated runs drew on.
	Groups int `json:"groups"`
	// SampleMin, SampleMedian and SampleMax spread the smallest pool each estimated run drew on.
	SampleMin    int `json:"sample_min"`
	SampleMedian int `json:"sample_median"`
	SampleMax    int `json:"sample_max"`
	// Unplaced counts the changed runs whose first changed decision has no point on their recorded path.
	Unplaced int `json:"unplaced,omitempty"`
	// UntimedOutcomes are the outcomes an estimate needed whose times the record lacks.
	UntimedOutcomes []string `json:"untimed_outcomes,omitempty"`
}

const estimationMethod = "landmark: recorded runs of the stratum at risk at the first changed decision " +
	"that took the new answer, chained at later ones; Wilson"

// goalComparison is one goal before and after the change.
type goalComparison struct {
	ID string `json:"id"`
	// Digest names the goal's definition, as metrics.json does (definitionDigest).
	Digest    string     `json:"digest"`
	Metric    string     `json:"metric"`
	Direction Direction  `json:"direction,omitempty"`
	Guardrail *Guardrail `json:"guardrail,omitempty"`
	Targeted  bool       `json:"targeted"`
	// Before is the goal's value over every record, as metrics.json has it.
	Before *float64 `json:"before"`
	// Baseline is the before value over the runs the after value covers: compared runs not unknown for the goal.
	Baseline  *float64 `json:"baseline"`
	After     *float64 `json:"after"`
	AfterLow  *float64 `json:"after_low"`
	AfterHigh *float64 `json:"after_high"`
	// Known, Estimated and Unknown count the compared runs that bear on the goal.
	Known     int `json:"known"`
	Estimated int `json:"estimated"`
	Unknown   int `json:"unknown"`
	// KnownValue is the after value over the known runs only: same, and changed but exact for the goal.
	KnownValue      *float64 `json:"known_value"`
	GuardrailBreaks bool     `json:"guardrail_breaks"`
	// Margin and WorstEffect are set for a goal with a direction whose metric reads an outcome:
	// WorstEffect is the harmful end of the after value over every compared run, each unknown run
	// at its worst, minus the before value of the same runs; worse than Margin requires a trial.
	Margin      *float64 `json:"margin,omitempty"`
	WorstEffect *float64 `json:"worst_effect,omitempty"`
	// unbounded is set when an unknown run lacks an after number the goal reads, so no worst case bounds it.
	unbounded bool
}

// runLabel is how the backtest knows a compared run's after value.
type runLabel int

const (
	labelSame runLabel = iota
	labelChangedExact
	labelChangedEstimated
	labelUnknown
)

// goalStatus is how the backtest knows one goal's value of one compared run.
type goalStatus int

const (
	statusKnown goalStatus = iota
	statusEstimated
	statusUnknown
)

// goalPlan is a goal's compiled metric with what it reads of a run.
type goalPlan struct {
	goal     Goal
	metric   metric
	numbers  []string
	outcomes bool
	targeted bool
}

// comparedRun is a run with a record, a before line and an after line.
type comparedRun struct {
	record  runexport.Record
	after   DecisionLine
	changed bool
	// landmark is nil for an unchanged run and for a changed run the backtest cannot place.
	landmark *landmark
	// policy keys the after line, which an estimate's pool runs must follow past the landmark.
	policy   string
	statuses []goalStatus
	// pools are the at-risk pools the run's estimates drew on, with their sizes.
	pools map[string]int
}

// Compare compares a backtest program's before and after decision lines with the records: it
// checks calibration, labels each run, computes every goal before and after and fires the gates.
func Compare(spec Spec, records []runexport.Record, before, after []DecisionLine, options CompareOptions) (Backtest, error) {
	if options.At.IsZero() {
		return Backtest{}, errors.New("compare needs the time open runs are measured to")
	}
	for _, field := range options.Strata {
		if !contains(spec.Fields, field) {
			return Backtest{}, fmt.Errorf("stratum %q is not in fields, so the export does not carry it", field)
		}
	}
	plans, err := planGoals(spec, options.Target)
	if err != nil {
		return Backtest{}, err
	}
	beforeLines, err := byFlowID(before, "before")
	if err != nil {
		return Backtest{}, err
	}
	afterLines, err := byFlowID(after, "after")
	if err != nil {
		return Backtest{}, err
	}
	c := comparison{e: evaluator{spec: spec, at: options.At}, strata: options.Strata, plans: plans}
	result := Backtest{EvaluatedAt: options.At.UTC(), Strata: append([]string{}, options.Strata...), Runs: backtestRuns{Records: len(records)}}
	result.Runs.LinesWithoutRecord = linesWithoutRecord(records, beforeLines) + linesWithoutRecord(records, afterLines)
	result.Calibration = c.calibrate(records, beforeLines)
	c.index(records, beforeLines)
	for _, record := range records {
		b, hasBefore := beforeLines[record.FlowID]
		a, hasAfter := afterLines[record.FlowID]
		if !hasBefore {
			result.Runs.MissingBefore++
		}
		if !hasAfter {
			result.Runs.MissingAfter++
		}
		if hasBefore && hasAfter {
			c.runs = append(c.runs, c.label(record, b, a))
		}
	}
	result.Runs.Compared = len(c.runs)
	for _, run := range c.runs {
		switch runLabelOf(run) {
		case labelSame:
			result.Runs.Same++
		case labelChangedExact:
			result.Runs.ChangedExact++
		case labelChangedEstimated:
			result.Runs.ChangedEstimated++
		case labelUnknown:
			result.Runs.Unknown++
		}
	}
	result.Estimation = c.estimation()
	metrics, err := Evaluate(spec, records, options.At)
	if err != nil {
		return Backtest{}, err
	}
	for i := range plans {
		goal := c.goal(i)
		goal.Before = metrics.Goals[i].Value
		result.Goals = append(result.Goals, goal)
	}
	result.Gates = c.gates(result)
	return result, nil
}

func planGoals(spec Spec, target string) ([]goalPlan, error) {
	if target != "" && !containsGoal(spec.Goals, target) {
		return nil, fmt.Errorf("target %q is not a goal of the goals file", target)
	}
	plans := make([]goalPlan, 0, len(spec.Goals))
	for _, goal := range spec.Goals {
		m, err := compileMetric(goal.Metric, spec)
		if err != nil {
			return nil, fmt.Errorf("goal %s: %w", goal.ID, err)
		}
		plan := goalPlan{goal: goal, metric: m, targeted: goal.ID == target || (target == "" && goal.Direction != "")}
		for _, t := range m.terms() {
			if t.fn == aggregateCount || t.fn == aggregateRate || (t.where != nil && t.where.outcome != "") {
				plan.outcomes = true
			}
			if !overOutcome[t.fn] && t.number.kind != numberField && !contains(plan.numbers, t.number.key()) {
				plan.numbers = append(plan.numbers, t.number.key())
			}
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func containsGoal(goals []Goal, id string) bool {
	for _, goal := range goals {
		if goal.ID == id {
			return true
		}
	}
	return false
}

// terms are the metric's numerator and, for a ratio, its denominator.
func (m metric) terms() []term {
	if m.denominator == nil {
		return []term{m.numerator}
	}
	return []term{m.numerator, *m.denominator}
}

func byFlowID(lines []DecisionLine, side string) (map[string]DecisionLine, error) {
	indexed := make(map[string]DecisionLine, len(lines))
	for _, line := range lines {
		if _, repeated := indexed[line.FlowID]; repeated {
			return nil, fmt.Errorf("the %s decisions have two lines for one run", side)
		}
		indexed[line.FlowID] = line
	}
	return indexed, nil
}

func linesWithoutRecord(records []runexport.Record, lines map[string]DecisionLine) int {
	known := make(map[string]bool, len(records))
	for _, record := range records {
		known[record.FlowID] = true
	}
	n := 0
	for flowID := range lines {
		if !known[flowID] {
			n++
		}
	}
	return n
}

// comparison holds the evaluator, the compared runs and the estimate pools of one Compare.
type comparison struct {
	e      evaluator
	strata []string
	plans  []goalPlan
	runs   []comparedRun
	// pools are the recorded closed runs at each decision point, by stratum, decision and answer.
	pools map[string][]poolRun
	// chances caches each at-risk pool's size and hits for a set of outcome literals.
	chances map[string]chance
	// untimed are the outcomes an estimate needed whose times the record lacks.
	untimed map[string]bool
}

// calibrate checks every before number that has a recorded counterpart against the record.
func (c *comparison) calibrate(records []runexport.Record, before map[string]DecisionLine) calibration {
	result := calibration{Floor: calibrationFloor, Mismatches: map[string]int{}}
	for _, record := range records {
		line, ok := before[record.FlowID]
		if !ok {
			continue
		}
		checked, matched := false, true
		for _, name := range sortedKeys(line.Numbers) {
			n, err := decisionNumber(name)
			if err != nil {
				panic(err) // ReadDecisions validated every name.
			}
			if n.kind == numberDuration && record.ClosedAt == nil {
				continue
			}
			recorded, ok := c.e.number(n, record)
			if !ok {
				continue
			}
			checked = true
			if !closeEnough(line.Numbers[name], recorded) {
				matched = false
				result.Mismatches[name]++
			}
		}
		if checked {
			result.Checked++
			if matched {
				result.Matched++
			}
		}
	}
	if result.Checked > 0 {
		share := float64(result.Matched) / float64(result.Checked)
		result.Share = &share
	}
	return result
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) <= calibrationTolerance*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// label decides how the backtest knows each goal's value of a run, and so the run's label.
func (c *comparison) label(record runexport.Record, before, after DecisionLine) comparedRun {
	run := comparedRun{record: record, after: after, changed: len(changedDecisions(before.Decisions, after.Decisions)) > 0, pools: map[string]int{}}
	if run.changed {
		run.landmark, run.policy = c.landmarkOf(record, before, after), policyOf(after)
	}
	run.statuses = make([]goalStatus, len(c.plans))
	for i, plan := range c.plans {
		switch {
		case !run.changed:
			run.statuses[i] = statusKnown
		case !hasNumbers(after, plan.numbers):
			run.statuses[i] = statusUnknown
		case !plan.outcomes:
			run.statuses[i] = statusKnown
		default:
			run.statuses[i] = c.outcomeStatus(plan, &run)
		}
	}
	return run
}

func hasNumbers(line DecisionLine, names []string) bool {
	for _, name := range names {
		if _, ok := line.Numbers[name]; !ok {
			return false
		}
	}
	return true
}

func runLabelOf(run comparedRun) runLabel {
	if !run.changed {
		return labelSame
	}
	label := labelChangedExact
	for _, status := range run.statuses {
		switch status {
		case statusUnknown:
			return labelUnknown
		case statusEstimated:
			label = labelChangedEstimated
		case statusKnown:
		}
	}
	return label
}

// goal computes one goal over the compared runs.
func (c *comparison) goal(i int) goalComparison {
	plan := c.plans[i]
	result := goalComparison{
		ID: plan.goal.ID, Digest: definitionDigest(plan.goal), Metric: plan.goal.Metric, Direction: plan.goal.Direction, Guardrail: plan.goal.Guardrail, Targeted: plan.targeted,
	}
	for _, run := range c.runs {
		if !c.bears(plan, run.record) {
			continue
		}
		switch run.statuses[i] {
		case statusKnown:
			result.Known++
		case statusEstimated:
			result.Estimated++
		case statusUnknown:
			result.Unknown++
		}
	}
	covered := func(run comparedRun) bool { return run.statuses[i] != statusUnknown }
	known := func(run comparedRun) bool { return run.statuses[i] == statusKnown }
	result.Baseline, _ = ratio(plan.metric, c.shares(i, covered, c.recorded))
	result.KnownValue, _ = c.value(i, known, boundPoint)
	result.After, _ = c.value(i, covered, boundPoint)
	low, _ := c.value(i, covered, boundLow)
	high, _ := c.value(i, covered, boundHigh)
	result.AfterLow, result.AfterHigh = spread(result.After, low, high)
	if g := plan.goal.Guardrail; g != nil && result.KnownValue != nil {
		v := *result.KnownValue
		result.GuardrailBreaks = (g.Max != nil && v > *g.Max) || (g.Min != nil && v < *g.Min)
	}
	if plan.outcomes && plan.goal.Direction != "" {
		c.harm(i, &result)
	}
	return result
}

// harm sets a directional goal's margin and worst effect over every compared run, each unknown
// run at its worst case, or marks the goal unbounded when an unknown run lacks a number it reads.
func (c *comparison) harm(i int, result *goalComparison) {
	plan := c.plans[i]
	for _, run := range c.runs {
		if run.statuses[i] == statusUnknown && !hasNumbers(run.after, plan.numbers) {
			result.unbounded = true
			return
		}
	}
	every := func(comparedRun) bool { return true }
	before, runs := ratio(plan.metric, c.shares(i, every, c.recorded))
	pull := boundLow
	if plan.goal.Direction == DirectionDown {
		pull = boundHigh
	}
	worst, _ := c.value(i, every, pull)
	if before == nil || worst == nil {
		return
	}
	margin, effect := plan.margin(*before, runs), *worst-*before
	result.Margin, result.WorstEffect = &margin, &effect
}

// margin is how much worse a goal may get at the harmful end: 2 percentage points for a rate,
// and for a count that share of the runs it counts over; 10% of its value before for any other
// metric, and for a ratio at least one run's worth, 1 over the runs it rests on.
func (p goalPlan) margin(before float64, runs int) float64 {
	m := p.metric
	switch {
	case m.denominator == nil && m.numerator.fn == aggregateRate:
		return rateMargin
	case m.denominator == nil && m.numerator.fn == aggregateCount:
		return rateMargin * float64(runs)
	case m.denominator != nil && runs > 0:
		return math.Max(relativeMargin*math.Abs(before), 1/float64(runs))
	}
	return relativeMargin * math.Abs(before)
}

// worseThanMargin reports whether a goal's worst effect is worse, in its direction, than its margin.
func (g goalComparison) worseThanMargin() bool {
	if g.WorstEffect == nil {
		return false
	}
	if g.Direction == DirectionDown {
		return *g.WorstEffect > *g.Margin
	}
	return *g.WorstEffect < -*g.Margin
}

// moves reports whether the estimated runs move a goal's after value: its interval has a width.
func (g goalComparison) moves() bool {
	return g.AfterLow != nil && g.AfterHigh != nil && !closeEnough(*g.AfterLow, *g.AfterHigh)
}

// bears reports whether a run's recorded facts select it for any term of the goal's metric.
func (c *comparison) bears(plan goalPlan, record runexport.Record) bool {
	for _, t := range plan.metric.terms() {
		if c.e.share(t, record, nil).selected > 0 {
			return true
		}
	}
	return false
}

// shares returns, for a term, the shares of the compared runs that include admits.
func (c *comparison) shares(i int, include func(comparedRun) bool, of func(term, comparedRun) share) func(term) []share {
	return func(t term) []share {
		shares := make([]share, 0, len(c.runs))
		for _, run := range c.runs {
			if include(run) {
				shares = append(shares, of(t, run))
			}
		}
		return shares
	}
}

// recorded is a run's share of a term from its record.
func (c *comparison) recorded(t term, run comparedRun) share { return c.e.share(t, run.record, nil) }

// value is the goal's after value over the runs include admits, with its numerator at the bound
// and a ratio's denominator at the other end, which moves the ratio the same way.
func (c *comparison) value(i int, include func(comparedRun) bool, b bound) (*float64, int) {
	return ratioSides(c.plans[i].metric, c.shares(i, include, c.afterShare(i, b)), c.shares(i, include, c.afterShare(i, b.opposite())))
}

// afterShare is a run's share of a term after the change: recorded when unchanged, with the
// after numbers when known exactly, and from its chances when estimated, or unknown at its worst.
func (c *comparison) afterShare(i int, b bound) func(term, comparedRun) share {
	return func(t term, run comparedRun) share {
		switch {
		case !run.changed:
			return c.e.share(t, run.record, nil)
		case run.statuses[i] == statusKnown:
			return c.e.share(t, run.record, run.after.Numbers)
		}
		return c.estimatedShare(t, run, b)
	}
}

// spread orders the value at the point, low and high estimates into the interval's ends.
func spread(point, low, high *float64) (*float64, *float64) {
	values := []float64{}
	for _, v := range []*float64{point, low, high} {
		if v != nil {
			values = append(values, *v)
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	sort.Float64s(values)
	return &values[0], &values[len(values)-1]
}

// gates fires every gate the result calls for; none stops the comparison early.
func (c *comparison) gates(result Backtest) []Gate {
	gates := []Gate{}
	switch share := result.Calibration.Share; {
	case share == nil:
		gates = append(gates, GateBacktestInvalid.fire("no before line carries a number with a recorded counterpart to check"))
	case *share < calibrationFloor:
		gates = append(gates, GateBacktestInvalid.fire("calibration is %.1f%% (%d of %d runs), below %.0f%%",
			100**share, result.Calibration.Matched, result.Calibration.Checked, 100*calibrationFloor))
	}
	for _, goal := range result.Goals {
		if goal.GuardrailBreaks {
			gates = append(gates, GateGuardrailBroken.fire("goal %s is %s over %d known runs, outside %s",
				goal.ID, formatValue(*goal.KnownValue), goal.Known, describeGuardrail(*goal.Guardrail)))
		}
	}
	var unknown, estimated, worse []string
	for _, goal := range result.Goals {
		bearing := goal.Known + goal.Estimated + goal.Unknown
		if bearing == 0 {
			continue
		}
		if (goal.Targeted && 2*goal.Unknown > bearing) || goal.unbounded {
			unknown = append(unknown, fmt.Sprintf("%s (%d of %d runs)", goal.ID, goal.Unknown, bearing))
		}
		if 2*goal.Estimated > bearing && goal.moves() {
			estimated = append(estimated, fmt.Sprintf("%s (%d of %d runs)", goal.ID, goal.Estimated, bearing))
		}
		if goal.worseThanMargin() {
			worse = append(worse, fmt.Sprintf("%s by %s (margin %s)", goal.ID, formatValue(*goal.WorstEffect), formatValue(*goal.Margin)))
		}
	}
	if len(unknown) > 0 {
		gates = append(gates, GateTrialRequired.fire("unknown after the change: %s", strings.Join(unknown, ", ")))
	}
	if len(estimated) > 0 {
		gates = append(gates, GateTrialRequired.fire("mostly estimated: %s", strings.Join(estimated, ", ")))
	}
	if len(worse) > 0 {
		gates = append(gates, GateTrialRequired.fire("worse than the margin at the harmful end of the %.0f%% interval: %s",
			100*estimateInterval, strings.Join(worse, ", ")))
	}
	return gates
}

func formatValue(v float64) string { return fmt.Sprintf("%.4g", v) }

func describeGuardrail(g Guardrail) string {
	if g.Max != nil {
		return "max " + formatValue(*g.Max)
	}
	return "min " + formatValue(*g.Min)
}
