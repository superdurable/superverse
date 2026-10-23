// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package history

import (
	"cmp"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/engineindex"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// Bounds of a record.
const (
	// maxFieldBytes bounds a string field's value.
	maxFieldBytes = 256
	// maxPathEntries bounds the path; the newest entries are kept.
	maxPathEntries = 200
	// maxFieldValues bounds a field's timeline; the oldest values are kept.
	maxFieldValues = 16
	// maxActionTimes bounds the Actions whose first time a record keeps.
	maxActionTimes = 64
	// maxTimelineBytes bounds the encoded values of all a record's field
	// timelines, so a record stays far below the 1 MiB a line holds.
	maxTimelineBytes = 256 << 10
)

// variantAttribute is the Attribute a trial sets once at Flow start; the export always reads it.
const variantAttribute = "evolve-variant"

// runIdentity is a run's identity, times and indexed values as the engine's search reports them.
type runIdentity struct {
	flowType  string
	FlowID    string
	status    dexpb.FlowStatus
	StartedAt time.Time
	closedAt  *time.Time
	indexed   []*dexpb.KV
}

// runHistory is what an export read of a run's execution: the events of the
// runs it read, oldest run first, and how many runs that is; whether the
// event limit cut the newest run's events, with the earliest time of an event
// it left unread; and then its current allow-listed Attributes from its state.
type runHistory struct {
	events     []*dexpb.FlowHistoryEvent
	runs       int
	truncated  bool
	unreadFrom time.Time
	state      []*dexpb.KV
}

// Price is a model's list price per million tokens.
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// Settings choose what a record keeps and how it is priced and placed.
type Settings struct {
	// Fields are the Attributes a record may carry; the trial variant is always read.
	Fields map[string]bool
	// Redact replaces configured secret values in a string; nil keeps strings as they are.
	Redact func(string) string
	// Prices are keyed by the model ID a model connector reports.
	Prices map[string]Price
	// Timeline lists the Live releases in order of their Live time.
	Timeline []runexport.Release
	// At is the export time, which bounds the waits and durations of open runs.
	At time.Time
}

// compact turns a run's history into its record; values decodes a Dex value,
// loading a blob within its budget, and answers false for a value it will not
// read. A run read with the runs it continues starts at the first one's start.
// A truncated history cuts every field's timeline at the earlier of its last
// event read and its earliest unread one, and the state read then replaces the
// fields.
func compact(run runIdentity, history runHistory, values *engineValues, s Settings) runexport.Record {
	c := compaction{
		settings: s, values: values, chained: history.runs > 1,
		record: runexport.Record{
			FlowType: run.flowType, FlowID: run.FlowID, Status: statusName(run.status),
			StartedAt: run.StartedAt.UTC(), ClosedAt: run.closedAt, Timelines: true,
			Fields: map[string]any{}, FieldTimelines: map[string]runexport.FieldTimeline{}, Steps: map[string]int{}, StepsFirstAt: map[string]time.Time{},
			Actions: map[string]int{}, ActionsFirstAt: map[string]time.Time{}, WaitHours: map[string]float64{},
		},
		executions: map[string]execution{}, responses: map[string]bool{}, tokens: map[string][2]int64{}, indexKeys: map[string]string{},
	}
	last := run.StartedAt
	for i, event := range history.events {
		c.event(event, i == 0)
		last = event.GetEventTime().AsTime()
	}
	if history.truncated {
		cut := last
		if history.unreadFrom.Before(cut) {
			cut = history.unreadFrom
		}
		c.cut(cut.UTC())
		c.current(history.state)
	}
	c.indexed(run)
	c.finish()
	return c.record
}

type execution struct {
	stepType string
	firstAt  time.Time
	waits    bool
	waited   bool
	ended    bool
}

type compaction struct {
	settings Settings
	values   *engineValues
	record   runexport.Record
	// chained is set when the history holds several runs of the FlowID, the first of which gives the start.
	chained    bool
	executions map[string]execution
	responses  map[string]bool
	tokens     map[string][2]int64
	// indexKeys maps the search index key of an allow-listed Attribute, as its writes name it, to the Attribute.
	indexKeys map[string]string
	// timelineBytes is the encoded size of the field timelines' values so far.
	timelineBytes int
}

// event applies one history event; first is set for the history's first.
func (c *compaction) event(event *dexpb.FlowHistoryEvent, first bool) {
	at := event.GetEventTime().AsTime()
	switch payload := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_FlowStartedOrContinued:
		c.started(payload.FlowStartedOrContinued, at, first)
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		c.waits(payload.StepWaitForPending.GetContext(), at)
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		c.waits(payload.StepWaitForCompleted.GetContext(), at)
		c.attributes(payload.StepWaitForCompleted.GetOutput().GetUpsertAttributes(), at)
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		c.waits(payload.StepWaitForFailed.GetContext(), at)
		c.end(payload.StepWaitForFailed.GetContext())
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		c.executes(payload.StepExecutePending.GetContext(), at)
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		e := payload.StepExecuteCompleted
		c.executes(e.GetContext(), at)
		c.end(e.GetContext())
		c.attributes(e.GetOutput().GetUpsertAttributes(), at)
		c.usage(e.GetContext().GetStepType(), e.GetOutput().GetStepDecision().GetNextSteps())
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		c.executes(payload.StepExecuteFailed.GetContext(), at)
		c.end(payload.StepExecuteFailed.GetContext())
	case *dexpb.FlowHistoryEvent_RpcExecutionCompleted:
		rpc := payload.RpcExecutionCompleted
		if changed(rpc) {
			c.action(rpc.GetRpcName(), at)
		}
		c.attributes(rpc.GetUpsertAttributes(), at)
	}
}

// started applies the start of a run. The first run's start sets the record's
// start; a later run of the chain carries in what the run before it ended
// with, which the record already holds. When the first run read continues an
// unread one, the record starts with what it carried in: its Attributes and
// its Step executions under way (HistoryFrom).
func (c *compaction) started(event *dexpb.FlowStartedOrContinuedHistoryEvent, at time.Time, first bool) {
	if initial := event.GetInitialStart(); initial != nil {
		if first && c.chained {
			c.record.StartedAt = at.UTC()
		}
		c.attributes(kvWrites(initial.GetInitialAttributes()), at)
		return
	}
	continued := event.GetContinuedStart()
	if !first || continued == nil {
		return
	}
	from := at.UTC()
	c.record.HistoryFrom, c.record.StartedAt = &from, from
	c.attributes(kvWrites(continued.GetAttributes()), at)
	for _, resumed := range continued.GetStepsToResume() {
		id, stepType := resumed.GetStepExecutionId(), resumed.GetStep().GetStepType()
		if id == "" || stepType == "" {
			continue
		}
		c.executions[id] = execution{stepType: stepType, firstAt: from, waits: resumed.GetWaitingCondition() != nil}
		c.record.Steps[stepType]++
	}
}

// enter records a Step execution the first time one of its events appears and answers when it began.
func (c *compaction) enter(context *dexpb.StepMethodEventContext, at time.Time) time.Time {
	id := context.GetStepExecutionId()
	if id == "" {
		return at
	}
	if known, ok := c.executions[id]; ok {
		return known.firstAt
	}
	first := at
	if started := context.GetStartedTime(); started != nil && started.AsTime().Before(at) {
		first = started.AsTime()
	}
	c.executions[id] = execution{stepType: context.GetStepType(), firstAt: first}
	c.record.Steps[context.GetStepType()]++
	if known, ok := c.record.StepsFirstAt[context.GetStepType()]; !ok || first.Before(known) {
		c.record.StepsFirstAt[context.GetStepType()] = first.UTC()
	}
	c.record.Path = append(c.record.Path, runexport.PathEntry{Step: context.GetStepType(), At: first.UTC()})
	return first
}

// waits records a WaitFor event: the Step waits from its entry until its Execute begins.
func (c *compaction) waits(context *dexpb.StepMethodEventContext, at time.Time) {
	c.enter(context, at)
	if e, ok := c.executions[context.GetStepExecutionId()]; ok && !e.waited {
		e.waits = true
		c.executions[context.GetStepExecutionId()] = e
	}
}

// end marks a Step execution finished: its Execute completed or it failed into its route.
func (c *compaction) end(context *dexpb.StepMethodEventContext) {
	if e, ok := c.executions[context.GetStepExecutionId()]; ok {
		e.ended = true
		c.executions[context.GetStepExecutionId()] = e
	}
}

// executes records an Execute event; the first one ends the Step's wait.
func (c *compaction) executes(context *dexpb.StepMethodEventContext, at time.Time) {
	c.enter(context, at)
	id := context.GetStepExecutionId()
	e, ok := c.executions[id]
	if !ok || !e.waits || e.waited {
		return
	}
	began := at
	if started := context.GetStartedTime(); started != nil && started.AsTime().Before(at) {
		began = started.AsTime()
	}
	e.waited = true
	c.executions[id] = e
	c.record.WaitHours[e.stepType] += max(began.Sub(e.firstAt), 0).Hours()
}

// action counts an accepted Action and keeps the time it was first accepted.
func (c *compaction) action(name string, at time.Time) {
	c.record.Actions[name]++
	if _, known := c.record.ActionsFirstAt[name]; !known && len(c.record.ActionsFirstAt) < maxActionTimes {
		c.record.ActionsFirstAt[name] = at.UTC()
	}
}

// changed reports whether an RPC did anything: an Action, not a read such as a display or summary.
func changed(rpc *dexpb.RpcExecutionCompletedEvent) bool {
	decision := rpc.GetStepDecision()
	return len(rpc.GetUpsertAttributes()) > 0 || len(rpc.GetPublishToChannel()) > 0 || len(rpc.GetDeleteFromChannel()) > 0 ||
		len(rpc.GetRecordEvents()) > 0 || len(decision.GetNextSteps()) > 0 || decision.GetCloseDecision() != nil ||
		len(decision.GetCancelStepTypes()) > 0 || len(decision.GetCancelSiblingStepTypes()) > 0
}

func kvWrites(kvs []*dexpb.KV) []*dexpb.AttributeWrite {
	writes := make([]*dexpb.AttributeWrite, 0, len(kvs))
	for _, kv := range kvs {
		writes = append(writes, &dexpb.AttributeWrite{Key: kv.GetKey(), Value: kv.GetValue()})
	}
	return writes
}

// attributes applies writes made at at in order: the last write wins, a null
// deletes, and each distinct value of a field joins its timeline.
func (c *compaction) attributes(writes []*dexpb.AttributeWrite, at time.Time) {
	for _, write := range writes {
		key := write.GetKey()
		if !c.settings.Fields[key] && key != variantAttribute {
			continue
		}
		if index := write.GetIndexConfig(); index.GetEnable() {
			c.indexKeys[cmp.Or(index.GetIndexKey(), key)] = key
		}
		if _, null := write.GetValue().GetKind().(*dexpb.Value_NullValue); null || write.GetValue() == nil {
			c.set(key, nil)
			c.timeline(key, nil, at)
			continue
		}
		if value, ok := c.scalar(write.GetValue()); ok {
			c.set(key, value)
			c.timeline(key, value, at)
		}
	}
}

// set gives an allow-listed Attribute or the trial variant its current value; nil deletes it.
func (c *compaction) set(key string, value any) {
	if key == variantAttribute {
		c.record.Variant = nil
		if text, isText := value.(string); isText {
			c.record.Variant = &text
		}
		return
	}
	if value == nil {
		delete(c.record.Fields, key)
		return
	}
	c.record.Fields[key] = value
}

// timeline adds a field's value written at at when it differs from the last
// one; once the timeline holds maxFieldValues values, or the record's
// timelines take maxTimelineBytes, it is cut at the first value it leaves out.
func (c *compaction) timeline(key string, value any, at time.Time) {
	if key == variantAttribute {
		return
	}
	timeline, exists := c.record.FieldTimelines[key]
	n := len(timeline.Values)
	if timeline.CutAt != nil || (!exists && value == nil) || (n > 0 && timeline.Values[n-1].Value == value) {
		return
	}
	written := runexport.TimedValue{Value: value, At: at.UTC()}
	size := encodedSize(written)
	if n == maxFieldValues || c.timelineBytes+size > maxTimelineBytes {
		timeline.CutAt = &written.At
	} else {
		timeline.Values = append(timeline.Values, written)
		c.timelineBytes += size
	}
	c.record.FieldTimelines[key] = timeline
}

// encodedSize is the length of a timeline value as a record writes it.
func encodedSize(written runexport.TimedValue) int {
	encoded, err := json.Marshal(written)
	if err != nil {
		return maxTimelineBytes
	}
	return len(encoded)
}

// cut ends every allow-listed field's timeline, and the history, at the time
// from which a truncated history holds no event.
func (c *compaction) cut(at time.Time) {
	c.record.HistoryCutAt = &at
	for key := range c.settings.Fields {
		timeline := c.record.FieldTimelines[key]
		if timeline.CutAt == nil || at.Before(*timeline.CutAt) {
			timeline.CutAt = &at
		}
		c.record.FieldTimelines[key] = timeline
	}
}

// current replaces the fields and the variant with the run's current Attributes from its state.
func (c *compaction) current(state []*dexpb.KV) {
	c.record.Fields, c.record.Variant = map[string]any{}, nil
	c.record.FieldsFromState = true
	for _, kv := range state {
		if !c.settings.Fields[kv.GetKey()] && kv.GetKey() != variantAttribute {
			continue
		}
		if value, ok := c.scalar(kv.GetValue()); ok {
			c.set(kv.GetKey(), value)
		}
	}
}

// indexed keeps the search's values of allow-listed Attributes, by Attribute,
// and an open run's active Step types; the engine's other indexes are left
// out, since goals cannot list one of their names as a field.
func (c *compaction) indexed(run runIdentity) {
	for _, kv := range run.indexed {
		key := kv.GetKey()
		if key == string(engineindex.ActiveStepTypes) {
			if c.record.ClosedAt == nil {
				c.record.IndexedActiveSteps = stepTypes(kv.GetValue())
			}
			continue
		}
		attribute, known := c.indexKeys[key]
		if !known && c.settings.Fields[key] {
			attribute, known = key, true
		}
		if !known || attribute == variantAttribute {
			continue
		}
		if value, ok := c.scalar(kv.GetValue()); ok {
			if c.record.Indexed == nil {
				c.record.Indexed = map[string]any{}
			}
			c.record.Indexed[attribute] = value
		}
	}
}

// stepTypes reads the engine's list of active Step types, sorted.
func stepTypes(value *dexpb.Value) []string {
	decoded, _ := decode(value)
	var types []string
	switch v := decoded.(type) {
	case string:
		types = append(types, v)
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok {
				types = append(types, text)
			}
		}
	}
	sort.Strings(types)
	return types
}

// scalar keeps strings (cut and redacted), numbers and booleans; anything else is left out.
func (c *compaction) scalar(value *dexpb.Value) (any, bool) {
	decoded, ok := c.values.read(value)
	if !ok {
		return nil, false
	}
	switch v := decoded.(type) {
	case string:
		if c.settings.Redact != nil {
			v = c.settings.Redact(v)
		}
		return comm.TruncateUTF8(v, maxFieldBytes), true
	case bool, float64, int64, int:
		return v, true
	}
	if number, ok := decoded.(interface{ Float64() (float64, error) }); ok {
		f, err := number.Float64()
		return f, err == nil
	}
	return nil, false
}

// usage reads model usage from the results a Step passes on.
func (c *compaction) usage(stepType string, movements []*dexpb.StepMovement) {
	for _, movement := range movements {
		decoded, ok := c.values.read(movement.GetStepInput())
		if !ok {
			continue
		}
		result, isObject := decoded.(map[string]any)
		if !isObject {
			continue
		}
		if inner, wrapped := result["value"].(map[string]any); wrapped {
			// A connector query result carries its branch beside the value.
			result = inner
		}
		usage, hasUsage := result["usage"].(map[string]any)
		model, _ := result["servedModel"].(string)
		if model == "" {
			model, _ = result["requestedModel"].(string)
		}
		if !hasUsage || model == "" {
			continue
		}
		if id, _ := result["responseId"].(string); id != "" {
			if c.responses[id] {
				continue
			}
			c.responses[id] = true
		}
		in, _ := toInt(usage["inputTokens"])
		out, _ := toInt(usage["outputTokens"])
		c.record.AI.Calls++
		c.record.AI.InputTokens += in
		c.record.AI.OutputTokens += out
		if c.record.AI.Models == nil {
			c.record.AI.Models = map[string]int{}
		}
		c.record.AI.Models[model]++
		t := c.tokens[model]
		c.tokens[model] = [2]int64{t[0] + in, t[1] + out}
	}
}

func toInt(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case interface{ Int64() (int64, error) }:
		n, err := v.Int64()
		return n, err == nil
	}
	return 0, false
}

func (c *compaction) finish() {
	end := c.settings.At
	if c.record.ClosedAt != nil {
		end = *c.record.ClosedAt
	}
	if c.record.ClosedAt == nil {
		active := map[string]bool{}
		for _, e := range c.executions {
			if e.waits && !e.waited {
				c.record.WaitHours[e.stepType] += end.Sub(e.firstAt).Hours()
			}
			if !e.ended {
				active[e.stepType] = true
			}
		}
		for stepType := range active {
			c.record.ActiveSteps = append(c.record.ActiveSteps, stepType)
		}
		sort.Strings(c.record.ActiveSteps)
	}
	sort.SliceStable(c.record.Path, func(i, j int) bool { return c.record.Path[i].At.Before(c.record.Path[j].At) })
	if len(c.record.Path) > maxPathEntries {
		c.record.Path = c.record.Path[len(c.record.Path)-maxPathEntries:]
		c.record.PathTruncated = true
	}
	if c.record.AI.Calls > 0 {
		c.record.AI.USD = c.price()
	}
	c.place(end)
}

func (c *compaction) price() *float64 {
	total := 0.0
	for model, t := range c.tokens {
		p, ok := c.settings.Prices[model]
		if !ok {
			return nil
		}
		total += float64(t[0])/1e6*p.InputPerMTok + float64(t[1])/1e6*p.OutputPerMTok
	}
	return &total
}

// place sets the release Live at the run's start and the releases that became
// Live while it was open; a record whose start is unread has no release at its
// start and lists the releases since HistoryFrom.
func (c *compaction) place(end time.Time) {
	for _, release := range c.settings.Timeline {
		switch {
		case !release.LiveAt.After(c.record.StartedAt):
			if c.record.HistoryFrom == nil {
				c.record.ReleaseAtStart = release.Release
			}
		case !release.LiveAt.After(end):
			c.record.MovedAt = append(c.record.MovedAt, release.Release)
		}
	}
}

func statusName(status dexpb.FlowStatus) string {
	switch status {
	case dexpb.FlowStatus_FLOW_STATUS_SERVER_SIDE_TIMEOUT_INTERNAL_ONLY:
		return "timed_out"
	case dexpb.FlowStatus_FLOW_STATUS_CONTINUED_AS_NEW, dexpb.FlowStatus_FLOW_STATUS_UNSPECIFIED:
		return "running"
	}
	return strings.ToLower(strings.TrimPrefix(status.String(), "FLOW_STATUS_"))
}
