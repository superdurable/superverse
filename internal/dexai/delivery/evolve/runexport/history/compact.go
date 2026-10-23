// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package history

import (
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// Bounds of a record: a string field's value, and its path, of which the newest entries are kept.
const (
	maxFieldBytes  = 256
	maxPathEntries = 200
)

// variantAttribute is the Attribute a trial sets once at Flow start; the export always reads it.
const variantAttribute = "evolve-variant"

// runIdentity is a run's identity and times as the engine's search reports them.
type runIdentity struct {
	flowType  string
	FlowID    string
	status    dexpb.FlowStatus
	StartedAt time.Time
	closedAt  *time.Time
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

// compact turns a run's history, in event order, into its record; values
// decodes a Dex value, loading a blob within its budget, and answers false for
// a value it will not read.
func compact(run runIdentity, events []*dexpb.FlowHistoryEvent, values *engineValues, s Settings) runexport.Record {
	c := compaction{
		settings: s, values: values,
		record: runexport.Record{
			FlowType: run.flowType, FlowID: run.FlowID, Status: statusName(run.status),
			StartedAt: run.StartedAt.UTC(), ClosedAt: run.closedAt,
			Fields: map[string]any{}, Steps: map[string]int{}, Actions: map[string]int{}, WaitHours: map[string]float64{},
		},
		executions: map[string]execution{}, responses: map[string]bool{}, tokens: map[string][2]int64{},
	}
	for _, event := range events {
		c.event(event)
	}
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
	settings   Settings
	values     *engineValues
	record     runexport.Record
	executions map[string]execution
	responses  map[string]bool
	tokens     map[string][2]int64
}

func (c *compaction) event(event *dexpb.FlowHistoryEvent) {
	at := event.GetEventTime().AsTime()
	switch payload := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_FlowStartedOrContinued:
		c.attributes(kvWrites(payload.FlowStartedOrContinued.GetInitialStart().GetInitialAttributes()))
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		c.waits(payload.StepWaitForPending.GetContext(), at)
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		c.waits(payload.StepWaitForCompleted.GetContext(), at)
		c.attributes(payload.StepWaitForCompleted.GetOutput().GetUpsertAttributes())
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		c.waits(payload.StepWaitForFailed.GetContext(), at)
		c.end(payload.StepWaitForFailed.GetContext())
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		c.executes(payload.StepExecutePending.GetContext(), at)
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		e := payload.StepExecuteCompleted
		c.executes(e.GetContext(), at)
		c.end(e.GetContext())
		c.attributes(e.GetOutput().GetUpsertAttributes())
		c.usage(e.GetContext().GetStepType(), e.GetOutput().GetStepDecision().GetNextSteps())
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		c.executes(payload.StepExecuteFailed.GetContext(), at)
		c.end(payload.StepExecuteFailed.GetContext())
	case *dexpb.FlowHistoryEvent_RpcExecutionCompleted:
		rpc := payload.RpcExecutionCompleted
		if changed(rpc) {
			c.record.Actions[rpc.GetRpcName()]++
		}
		c.attributes(rpc.GetUpsertAttributes())
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

// attributes applies writes in order: the last write wins and a null deletes.
func (c *compaction) attributes(writes []*dexpb.AttributeWrite) {
	for _, write := range writes {
		key := write.GetKey()
		if !c.settings.Fields[key] && key != variantAttribute {
			continue
		}
		if _, null := write.GetValue().GetKind().(*dexpb.Value_NullValue); null || write.GetValue() == nil {
			delete(c.record.Fields, key)
			if key == variantAttribute {
				c.record.Variant = nil
			}
			continue
		}
		value, ok := c.scalar(write.GetValue())
		if !ok {
			continue
		}
		if key == variantAttribute {
			if text, isText := value.(string); isText {
				c.record.Variant = &text
			}
			continue
		}
		c.record.Fields[key] = value
	}
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

// place sets the release Live at the run's start and the releases that became Live while it was open.
func (c *compaction) place(end time.Time) {
	for _, release := range c.settings.Timeline {
		switch {
		case !release.LiveAt.After(c.record.StartedAt):
			c.record.ReleaseAtStart = release.Release
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
