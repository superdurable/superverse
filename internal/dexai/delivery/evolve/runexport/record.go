// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package runexport

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"
)

// Record is one run of one Flow, compacted from its state and history: one line of an export.
type Record struct {
	FlowType       string     `json:"flow_type"`
	FlowID         string     `json:"flow_id"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	ReleaseAtStart string     `json:"release_at_start,omitempty"`
	MovedAt        []string   `json:"moved_at,omitempty"`
	Variant        *string    `json:"variant"`
	// Fields are the allow-listed Attributes' current values: the last write wins.
	Fields map[string]any `json:"fields"`
	// FieldTimelines are the distinct values each field took, in write order, which FieldAsOf reads.
	FieldTimelines map[string]FieldTimeline `json:"field_timeline,omitempty"`
	Steps          map[string]int           `json:"steps"`
	// StepsFirstAt is when the run first entered each Step.
	StepsFirstAt map[string]time.Time `json:"steps_first_at,omitempty"`
	Actions      map[string]int       `json:"actions"`
	// ActionsFirstAt is when the run first had each Action accepted, for at most 64 Actions.
	ActionsFirstAt map[string]time.Time `json:"actions_first_at,omitempty"`
	WaitHours      map[string]float64   `json:"wait_h"`
	// ActiveSteps are the Step types an open run is in at the export time: entered, not yet finished.
	ActiveSteps []string `json:"active_steps,omitempty"`
	// Indexed are the allow-listed Attributes' values in the engine's search index when the export searched.
	Indexed map[string]any `json:"indexed,omitempty"`
	// IndexedActiveSteps are the Step types the engine's search index listed as active for an open run.
	IndexedActiveSteps []string    `json:"indexed_active_steps,omitempty"`
	Path               []PathEntry `json:"path"`
	PathTruncated      bool        `json:"path_truncated,omitempty"`
	// HistoryTruncated is set when the run's history exceeded the export's event limit.
	HistoryTruncated bool `json:"history_truncated,omitempty"`
	// HistoryCutAt is set with HistoryTruncated: the record holds no event from this time on.
	HistoryCutAt *time.Time `json:"history_cut_at,omitempty"`
	// HistoryFrom is set when the run continues earlier runs of its FlowID that
	// the event limit left unread: the record holds the run from this time
	// only, with the Attributes and active Steps it carried in. Its start is
	// unknown, so StartedAt is this time and ReleaseAtStart is empty.
	HistoryFrom *time.Time `json:"history_from,omitempty"`
	// FieldsFromState is set when Fields and Variant are the run's current Attributes, read from its state because its history was truncated.
	FieldsFromState bool `json:"fields_from_state,omitempty"`
	// Timelines is set on every record that carries field timelines and first
	// times; a record exported before them has neither.
	Timelines bool    `json:"timelines,omitempty"`
	AI        AIUsage `json:"ai"`
}

// HistoryComplete reports whether the record holds every event of the run up
// to the export: none was cut at the event limit or left unread before it.
func (r Record) HistoryComplete() bool { return !r.HistoryTruncated && r.HistoryFrom == nil }

// HistoryCovers reports whether the record holds every event of the run up to
// time at: its history starts with the run and is not cut at or before at.
func (r Record) HistoryCovers(at time.Time) bool {
	if r.HistoryFrom != nil {
		return false
	}
	if r.HistoryTruncated {
		return r.HistoryCutAt != nil && at.Before(*r.HistoryCutAt)
	}
	return true
}

// FieldTimeline is the distinct values one field took, in write order; a nil value is a deletion.
type FieldTimeline struct {
	Values []TimedValue `json:"values"`
	// CutAt is set when the timeline is incomplete: from then on the field's value is unknown.
	CutAt *time.Time `json:"cut_at,omitempty"`
}

// TimedValue is a value with the event time of its write, written as [value, "time"].
type TimedValue struct {
	Value any
	At    time.Time
}

// MarshalJSON writes the value as a two-element array.
func (v TimedValue) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]any{v.Value, v.At.UTC().Format(time.RFC3339Nano)})
}

// UnmarshalJSON reads the two-element array form.
func (v *TimedValue) UnmarshalJSON(data []byte) error {
	var pair [2]json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	var at string
	if err := json.Unmarshal(pair[1], &at); err != nil {
		return fmt.Errorf("field value time: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return fmt.Errorf("field value time: %w", err)
	}
	v.At = parsed
	return json.Unmarshal(pair[0], &v.Value)
}

// FieldAsOf returns a field's value at time at, the value of its latest write
// at or before at (the later one in write order on a tie): present is false
// before its first write and after a deletion, and known is false before
// HistoryFrom and from the time its timeline was cut. A record exported before
// timelines (Timelines false) answers its current value.
func (r Record) FieldAsOf(name string, at time.Time) (value any, present, known bool) {
	if !r.Timelines {
		value, present = r.Fields[name]
		return value, present, true
	}
	timeline := r.FieldTimelines[name]
	if (r.HistoryFrom != nil && at.Before(*r.HistoryFrom)) || (timeline.CutAt != nil && !at.Before(*timeline.CutAt)) {
		return nil, false, false
	}
	latest := -1
	for i, written := range timeline.Values {
		if !written.At.After(at) && (latest < 0 || !written.At.Before(timeline.Values[latest].At)) {
			latest = i
		}
	}
	if latest < 0 {
		return nil, false, true
	}
	return timeline.Values[latest].Value, timeline.Values[latest].Value != nil, true
}

// FieldMatchedBy reports whether a field held a value that match accepts at
// some time at or before at, with the earliest such write the record holds,
// which is the first only when HistoryFrom is nil. known is false when the
// record cannot tell: no such write is held, and the run's history before
// HistoryFrom is unread or the timeline was cut at or before at.
func (r Record) FieldMatchedBy(name string, at time.Time, match func(any) bool) (matched bool, first time.Time, known bool) {
	timeline := r.FieldTimelines[name]
	for _, written := range timeline.Values {
		if written.Value != nil && !written.At.After(at) && match(written.Value) && (!matched || written.At.Before(first)) {
			matched, first = true, written.At
		}
	}
	if matched {
		return true, first, true
	}
	cut := timeline.CutAt != nil && !at.Before(*timeline.CutAt)
	return false, time.Time{}, r.HistoryFrom == nil && !cut
}

// PathEntry is one entry into a Step, written as ["StepType", "time"].
type PathEntry struct {
	Step string
	At   time.Time
}

// MarshalJSON writes the entry as a two-element array.
func (e PathEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]string{e.Step, e.At.UTC().Format(time.RFC3339Nano)})
}

// UnmarshalJSON reads the two-element array form.
func (e *PathEntry) UnmarshalJSON(data []byte) error {
	var pair [2]string
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, pair[1])
	if err != nil {
		return fmt.Errorf("path entry time: %w", err)
	}
	e.Step, e.At = pair[0], at
	return nil
}

// AIUsage sums the model connector Steps of a run.
type AIUsage struct {
	Calls        int            `json:"calls"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	Models       map[string]int `json:"models,omitempty"`
	USD          *float64       `json:"usd"`
}

// SourceKind is where the runs of an export came from.
type SourceKind string

// The sources of an export.
const (
	// SourceLive labels EvolveFlow's export of a Project's Live, published deployment.
	SourceLive SourceKind = "live"
	// SourceExternal labels dexai evolve export's export of a Dex server outside the platform.
	SourceExternal SourceKind = "external"
)

// Source is where the writer of an export says its runs came from: an
// informational label, not a gate, since whoever writes a manifest chooses it.
// A Live export names its review with the manifest's ProjectID, ReviewID and
// GoalsCommit; an external one names the Dex server it read.
type Source struct {
	Kind SourceKind `json:"kind"`
	Dex  string     `json:"dex,omitempty"`
}

// ErrNotLive is CheckLive's refusal of a manifest whose own fields do not
// label it the Live export of the review being read. It checks the label
// only, not the runs or the parts, so it is a consistency check and not a
// gate against runs of the Preview, of tests or of any other engine.
var ErrNotLive = errors.New("the export is not this review's export of the Live runs")

// Manifest describes one export; a part it does not list does not exist.
type Manifest struct {
	// Source is absent from a manifest stored before it was recorded, which
	// only EvolveFlow stored, from the Live runs.
	Source          *Source   `json:"source,omitempty"`
	ReviewID        string    `json:"review_id,omitempty"`
	ProjectID       string    `json:"project_id,omitempty"`
	Window          Window    `json:"window"`
	Flows           []string  `json:"flows"`
	Parts           []Part    `json:"parts"`
	Runs            int       `json:"runs"`
	Truncated       bool      `json:"truncated"`
	Complete        bool      `json:"complete"`
	GeneratedAt     time.Time `json:"generated_at"`
	GoalsCommit     string    `json:"goals_commit,omitempty"`
	Timeline        []Release `json:"timeline,omitempty"`
	PriceTableAsOf  string    `json:"price_table_as_of,omitempty"`
	ExporterVersion string    `json:"exporter_version"`
	// RunsSHA256 is set on the copy place_evolve_export places beside
	// runs.jsonl: the SHA-256 of that file (WriteCanonical), which equals the
	// runs digest its review records.
	RunsSHA256 string `json:"runs_sha256,omitempty"`
}

// CheckLive is nil when m's source, Project and review label it EvolveFlow's
// export of the Live runs for review reviewID of project projectID, else
// ErrNotLive with the reason. It reads only those fields of m.
func (m Manifest) CheckLive(projectID, reviewID string) error {
	switch {
	case m.Source != nil && m.Source.Kind != SourceLive:
		return fmt.Errorf("%w: its runs came from source %q", ErrNotLive, m.Source.Kind)
	case m.ProjectID != projectID || m.ReviewID != reviewID:
		return fmt.Errorf("%w: it is the export of review %q of project %q", ErrNotLive, m.ReviewID, m.ProjectID)
	}
	return nil
}

// CheckPlaced is CheckLive for a manifest a review's folder is placed from or
// read in, which must name its source: a manifest without one, or of another
// review or Project than reviewID of projectID, is ErrNotLive.
func (m Manifest) CheckPlaced(projectID, reviewID string) error {
	switch {
	case m.Source == nil:
		return fmt.Errorf("%w: it names no source", ErrNotLive)
	case projectID == "" || reviewID == "":
		return fmt.Errorf("%w: it is checked without a review", ErrNotLive)
	}
	return m.CheckLive(projectID, reviewID)
}

// Window is the time range an export covers; open runs are included however old.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Part is one file of an export.
type Part struct {
	Name   string `json:"name"`
	Runs   int    `json:"runs"`
	SHA256 string `json:"sha256"`
}

// Release is one Live release of the export's timeline.
type Release struct {
	Release     string    `json:"release"`
	BuildCommit string    `json:"build_commit,omitempty"`
	LiveAt      time.Time `json:"live_at"`
}

// maxLineBytes bounds one record line; a longer line is a defect of the producer.
const maxLineBytes = 1 << 20

// Read decodes every record of a JSON Lines stream, strictly: the server
// reads its own export parts with it.
func Read(r io.Reader) ([]Record, error) {
	return read(r, true)
}

// ReadPlaced decodes every record of a placed or exported runs.jsonl, which a
// reader of an older build than the server that wrote it reads: it accepts
// fields it does not know, which a newer server adds, and still refuses
// trailing data.
func ReadPlaced(r io.Reader) ([]Record, error) {
	return read(r, false)
}

// read decodes every record of a JSON Lines stream, refusing unknown fields
// when strict.
func read(r io.Reader, strict bool) ([]Record, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	var records []Record
	for line := 1; scanner.Scan(); line++ {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record Record
		if err := decodeOne(scanner.Bytes(), &record, strict); err != nil {
			return nil, fmt.Errorf("record on line %d: %w", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("a record exceeds %d bytes: %w", maxLineBytes, err)
		}
		return nil, err
	}
	return records, nil
}

// decodeOne decodes exactly one JSON value of data into target, refusing
// trailing data and, when strict, unknown fields; the package imports only
// the standard library, so that the coding image's superverse-evolve builds
// it.
func decodeOne(data []byte, target any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after a JSON value")
	}
	return nil
}

// Write encodes one record as a JSON line, with <, > and & escaped as every
// earlier export wrote them, so a part or a runs.jsonl that a retry writes
// again has the same bytes, and refuses a line longer than Read accepts.
func Write(w io.Writer, record Record) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(line)+1 > maxLineBytes {
		return fmt.Errorf("the record of %s takes %d bytes, more than the %d a line holds", record.FlowID, len(line)+1, maxLineBytes)
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// Canonical is the runs an export holds for evaluation: of each FlowID only
// its newest run by start, since a later part may hold a FlowID again, ordered
// by start, newest first, then by FlowID. Metrics, a review's runs digest and
// a placed runs.jsonl all read the runs in this order.
func Canonical(records []Record) []Record {
	newest := make(map[string]Record, len(records))
	for _, run := range records {
		if known, seen := newest[run.FlowID]; !seen || run.StartedAt.After(known.StartedAt) {
			newest[run.FlowID] = run
		}
	}
	return slices.SortedFunc(maps.Values(newest), func(a, b Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.FlowID, b.FlowID))
	})
}

// WriteCanonical writes the canonical runs of records (Canonical) to w as JSON
// lines and returns the SHA-256 of the bytes it wrote, hex-encoded, and the
// number of runs. These bytes are a placed export's runs.jsonl, and the digest
// is the runs digest its review records.
func WriteCanonical(w io.Writer, records []Record) (string, int, error) {
	canonical := Canonical(records)
	digest := sha256.New()
	out := io.MultiWriter(w, digest)
	for _, record := range canonical {
		if err := Write(out, record); err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), len(canonical), nil
}
