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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ExportBinding names the runs a backtest or an open-run check read, so a
// review accepts only one computed over its own Live export: ReviewID,
// GoalsCommit, RunsSHA256 and Records come from the placed folder (the review,
// the Live build's commit, the SHA-256 of runs.jsonl and its run count),
// BeforeCommit is the Live build commit the before side's code came from and
// BaseCommit the worktree's base. A binding of runs outside the platform names
// no review and no goals commit.
type ExportBinding struct {
	ReviewID     string `json:"review_id,omitempty"`
	GoalsCommit  string `json:"goals_commit,omitempty"`
	RunsSHA256   string `json:"runs_sha256"`
	Records      int    `json:"records"`
	BeforeCommit string `json:"before_commit,omitempty"`
	BaseCommit   string `json:"base_commit,omitempty"`
}

// BindRuns is the binding of the runs file runs, which holds records runs:
// its SHA-256 and run count, without a review.
func BindRuns(runs []byte, records int) ExportBinding {
	sum := sha256.Sum256(runs)
	return ExportBinding{RunsSHA256: hex.EncodeToString(sum[:]), Records: records}
}

// A review report's backtest and open-run check are refused for one of two
// reasons: they are not bound to the review's own export and goals, or they
// are not a backtest the review can offer (unreadable, or a gate refuses).
var (
	ErrExportMismatch  = errors.New("the backtest is not bound to this review's Live export and goals")
	errInvalidBacktest = errors.New("the backtest cannot back a proposal")
)

// maximumSummaryMetricBytes cuts a goal's metric in a summary; its
// definition digest names it in full.
const maximumSummaryMetricBytes = 120

// BacktestSummary is what a review report carries of a proposal's
// backtest.json and what the proposal keeps, with the JSON keys of Backtest:
// the export binding, the runs by label, the calibration, the estimation's
// interval and sample sizes, the codes of the gates that fired with their
// rule's summary, and per goal its comparison. KnownValue serves the review's
// guardrail gate and is not kept (Kept).
type BacktestSummary struct {
	Export      ExportBinding      `json:"export"`
	EvaluatedAt time.Time          `json:"evaluated_at"`
	Runs        backtestRuns       `json:"runs"`
	Calibration summaryCalibration `json:"calibration"`
	Estimation  summaryEstimation  `json:"estimation"`
	Goals       []summaryGoal      `json:"goals"`
	Gates       []summaryGate      `json:"gates"`
}

type summaryCalibration struct {
	Share   *float64 `json:"share"`
	Floor   float64  `json:"floor"`
	Checked int      `json:"checked"`
	Matched int      `json:"matched"`
}

type summaryEstimation struct {
	MinRuns      int     `json:"min_runs"`
	Interval     float64 `json:"interval"`
	Groups       int     `json:"groups"`
	SampleMin    int     `json:"sample_min"`
	SampleMedian int     `json:"sample_median"`
	SampleMax    int     `json:"sample_max"`
}

// summaryGoal is one goal's comparison; its metric is cut to
// maximumSummaryMetricBytes, and its digest names the full definition.
type summaryGoal struct {
	ID         string     `json:"id"`
	Digest     string     `json:"digest"`
	Metric     string     `json:"metric"`
	Direction  Direction  `json:"direction,omitempty"`
	Guardrail  *Guardrail `json:"guardrail,omitempty"`
	Targeted   bool       `json:"targeted"`
	Before     *float64   `json:"before"`
	Baseline   *float64   `json:"baseline"`
	After      *float64   `json:"after"`
	AfterLow   *float64   `json:"after_low"`
	AfterHigh  *float64   `json:"after_high"`
	Known      int        `json:"known"`
	Estimated  int        `json:"estimated"`
	Unknown    int        `json:"unknown"`
	KnownValue *float64   `json:"known_value,omitempty"`
}

// summaryGate is a gate that fired, with its rule's summary as its message.
type summaryGate struct {
	Code    GateCode `json:"code"`
	Refuses bool     `json:"refuses"`
	Message string   `json:"message"`
}

// OpenRunsSummary is what a review report carries of a proposal's
// open-runs.json: its export binding, the open runs and the gates that fired.
type OpenRunsSummary struct {
	Export   ExportBinding `json:"export"`
	OpenRuns int           `json:"open_runs"`
	Gates    []summaryGate `json:"gates"`
}

// Kept is the summary a proposal keeps: without each goal's known value,
// which the review's guardrail gate has read.
func (s BacktestSummary) Kept() BacktestSummary {
	s.Goals = slices.Clone(s.Goals)
	for i := range s.Goals {
		s.Goals[i].KnownValue = nil
	}
	return s
}

// ParseBacktest decodes backtest.json strictly and returns its summary; a
// file that is not a bound backtest is errInvalidBacktest.
func ParseBacktest(data []byte) (BacktestSummary, error) {
	var backtest Backtest
	if err := decodeStrict(data, &backtest); err != nil {
		return BacktestSummary{}, fmt.Errorf("%w: backtest.json: %w", errInvalidBacktest, err)
	}
	if backtest.Export == nil || len(backtest.Goals) == 0 {
		return BacktestSummary{}, fmt.Errorf("%w: backtest.json names no export or no goal; write it with compare", errInvalidBacktest)
	}
	summary := BacktestSummary{
		Export: *backtest.Export, EvaluatedAt: backtest.EvaluatedAt, Runs: backtest.Runs,
		Calibration: summaryCalibration{Share: backtest.Calibration.Share, Floor: backtest.Calibration.Floor, Checked: backtest.Calibration.Checked, Matched: backtest.Calibration.Matched},
		Estimation: summaryEstimation{MinRuns: backtest.Estimation.MinRuns, Interval: backtest.Estimation.Interval, Groups: backtest.Estimation.Groups,
			SampleMin: backtest.Estimation.SampleMin, SampleMedian: backtest.Estimation.SampleMedian, SampleMax: backtest.Estimation.SampleMax},
		Gates: summaryGates(backtest.Gates),
	}
	for _, goal := range backtest.Goals {
		summary.Goals = append(summary.Goals, summaryGoal{
			ID: goal.ID, Digest: goal.Digest, Metric: cutUTF8(goal.Metric, maximumSummaryMetricBytes), Direction: goal.Direction, Guardrail: goal.Guardrail,
			Targeted: goal.Targeted, Before: goal.Before, Baseline: goal.Baseline, After: goal.After, AfterLow: goal.AfterLow, AfterHigh: goal.AfterHigh,
			Known: goal.Known, Estimated: goal.Estimated, Unknown: goal.Unknown, KnownValue: goal.KnownValue,
		})
	}
	return summary, nil
}

// ParseOpenRuns decodes open-runs.json strictly and returns its summary; a
// file that is not a bound open-run check is errInvalidBacktest.
func ParseOpenRuns(data []byte) (OpenRunsSummary, error) {
	var check OpenRunCheck
	if err := decodeStrict(data, &check); err != nil {
		return OpenRunsSummary{}, fmt.Errorf("%w: open-runs.json: %w", errInvalidBacktest, err)
	}
	if check.Export == nil {
		return OpenRunsSummary{}, fmt.Errorf("%w: open-runs.json names no export; write it with open-runs", errInvalidBacktest)
	}
	return OpenRunsSummary{Export: *check.Export, OpenRuns: check.OpenRuns, Gates: summaryGates(check.Gates)}, nil
}

// ReviewFacts are what a review recorded of its export: the review, the Live
// build's goals commit, the runs digest and count, and each goal's value by
// goal ID (absent for a goal the metrics summary left out).
type ReviewFacts struct {
	ReviewID, GoalsCommit, RunsSHA256 string
	Records                           int
	Values                            map[string]*float64
}

// CheckReview admits a proposal's backtest and open-run check for the review
// whose goals copy is spec and whose export facts recorded: both are bound to
// the review's export, before side and goals commit; the backtest's goals are
// exactly the copy's, each under the copy's definition digest and with the
// review's value before; and then no gate refuses it. The guardrail gate is
// applied from the copy's guardrails and each goal's known value, never from
// the backtest's own verdict. A mismatch is ErrExportMismatch, a refused
// backtest errInvalidBacktest with the gate's code; trial is whether the
// proposal is a trial, which a trial_required gate needs.
func CheckReview(spec Spec, facts ReviewFacts, backtest BacktestSummary, openRuns OpenRunsSummary, trial bool) error {
	for name, binding := range map[string]ExportBinding{"backtest.json": backtest.Export, "open-runs.json": openRuns.Export} {
		if err := binding.check(facts); err != nil {
			return fmt.Errorf("%w: %s %w", ErrExportMismatch, name, err)
		}
	}
	if err := checkGoalSet(spec, facts, backtest.Goals); err != nil {
		return fmt.Errorf("%w: %w", ErrExportMismatch, err)
	}
	if share := backtest.Calibration.Share; share == nil || *share < calibrationFloor {
		return fmt.Errorf("%w: %s: the calibration is below %.0f%% or was not checked", errInvalidBacktest, GateBacktestInvalid, 100*calibrationFloor)
	}
	for _, goal := range spec.Goals {
		compared := backtest.Goals[slices.IndexFunc(backtest.Goals, func(g summaryGoal) bool { return g.ID == goal.ID })]
		if goal.Guardrail != nil && compared.KnownValue != nil && !keeps(*goal.Guardrail, *compared.KnownValue) {
			return fmt.Errorf("%w: %s: goal %s is %s over the runs the backtest knows exactly, outside %s", errInvalidBacktest, GateGuardrailBroken,
				goal.ID, formatValue(*compared.KnownValue), describeGuardrail(*goal.Guardrail))
		}
	}
	for _, gate := range append(slices.Clone(backtest.Gates), openRuns.Gates...) {
		if rule, known := gateRules[gate.Code]; !known || rule.refuses || (gate.Code == GateTrialRequired && !trial) {
			return fmt.Errorf("%w: %s: %s", errInvalidBacktest, gate.Code, cutUTF8(gate.Message, 300))
		}
	}
	return nil
}

// check is nil when the binding names the review's own export and goals commit.
func (b ExportBinding) check(facts ReviewFacts) error {
	switch {
	case b.ReviewID != facts.ReviewID:
		return fmt.Errorf("names review %q, not %s", b.ReviewID, facts.ReviewID)
	case b.GoalsCommit != facts.GoalsCommit || b.BeforeCommit != facts.GoalsCommit:
		return fmt.Errorf("names goals commit %q and before commit %q, not the Live build %s", b.GoalsCommit, b.BeforeCommit, facts.GoalsCommit)
	case b.RunsSHA256 != facts.RunsSHA256 || b.Records != facts.Records:
		return fmt.Errorf("read %d runs with SHA-256 %q, not the review's %d runs %s", b.Records, b.RunsSHA256, facts.Records, facts.RunsSHA256)
	}
	return nil
}

// checkGoalSet is nil when the compared goals are exactly the copy's, each
// under its definition digest and with the review's value before.
func checkGoalSet(spec Spec, facts ReviewFacts, compared []summaryGoal) error {
	if len(compared) != len(spec.Goals) {
		return fmt.Errorf("the backtest compares %d goals, not the review's %d", len(compared), len(spec.Goals))
	}
	for _, goal := range spec.Goals {
		index := slices.IndexFunc(compared, func(g summaryGoal) bool { return g.ID == goal.ID })
		if index < 0 {
			return fmt.Errorf("the backtest leaves out goal %s", goal.ID)
		}
		if digest := definitionDigest(goal); compared[index].Digest != digest {
			return fmt.Errorf("goal %s has definition %q, not the review's %s", goal.ID, compared[index].Digest, digest)
		}
		if value, measured := facts.Values[goal.ID]; measured && !sameMeasure(value, compared[index].Before) {
			return fmt.Errorf("goal %s is %s before the change, not the review's %s", goal.ID, describeValue(compared[index].Before), describeValue(value))
		}
	}
	return nil
}

// sameMeasure reports whether two values are equal within rounding.
func sameMeasure(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) <= 1e-9*math.Max(1, math.Abs(*b))
}

func describeValue(v *float64) string {
	if v == nil {
		return "unmeasured"
	}
	return formatValue(*v)
}

// keeps reports whether value keeps the guardrail.
func keeps(guardrail Guardrail, value float64) bool {
	return (guardrail.Max == nil || value <= *guardrail.Max) && (guardrail.Min == nil || value >= *guardrail.Min)
}

func summaryGates(gates []Gate) []summaryGate {
	summary := make([]summaryGate, 0, len(gates))
	for _, gate := range gates {
		summary = append(summary, summaryGate{Code: gate.Code, Refuses: gate.Refuses, Message: gateRules[gate.Code].summary})
	}
	return summary
}

// decodeStrict decodes the one JSON value of data, refusing unknown fields.
func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("data follows the JSON value")
	}
	return nil
}

// cutUTF8 cuts text to at most limit bytes at a character boundary.
func cutUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return strings.TrimSpace(text)
}
