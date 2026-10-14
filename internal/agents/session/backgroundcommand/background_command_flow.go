// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package backgroundcommand

import (
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// The watched task's ID, installed with the start for the Dex Web views.
var taskIDAttribute = dex.DefineAttribute[string]("TaskID")

// BackgroundCommandFlow follows one background command of a Session in the Sandbox's command ledger
// until it ends, as a SubFlow of the Session's AwaitBackgroundCommand Step, which records the end.
type BackgroundCommandFlow struct {
	dex.FlowDefaults
	sandbox *sandboxtools.Runner
}

// SubFlow is the condition that starts the Flow for watch and is satisfied when it ends.
func SubFlow(watch BackgroundCommandWatch) dex.Condition {
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, taskIDAttribute, watch.TaskID)
	return dex.SubFlow(&BackgroundCommandFlow{}, watch, dex.SubFlowOptions{Attributes: attributes.Values()})
}

// GetSteps registers the watch and its failure route; both complete the Flow with the command's end.
func (flow *BackgroundCommandFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(WatchBackgroundCommand{flow: flow}), dex.DefineStep(RecordBackgroundCommandLost{})}
}

// GetRPCs registers the Dex Web views only: the Session never calls the Flow.
func (flow *BackgroundCommandFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
	}
}

const readRPCTimeout = 5 * time.Second

// GetPersistenceSchema registers the task ID; the input and the completion output carry the watch.
func (*BackgroundCommandFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskIDAttribute}}
}

// GetDexSummary is the row of the Dex Web Flow list, whose FlowID names the Session.
//
// dex:field attribute-key:TaskID value-type:string editable:false description:"Watched background command"
func (*BackgroundCommandFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"TaskID": comm.Must(taskIDAttribute.Get(ctx))}}, nil
}

// GetDexDisplay is the header of the Dex Web Flow view.
//
// dex:field attribute-key:TaskID value-type:string editable:false description:"Watched background command"
func (*BackgroundCommandFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"TaskID": comm.Must(taskIDAttribute.Get(ctx))}}, nil
}

// dex:group group-id:watch group-label:"Watch"
// dex:explanation text:"Follow the background command in the Sandbox's command ledger until it ends or its watch limit passes."
type WatchBackgroundCommand struct {
	dex.StepDefaultsNoWaitFor[BackgroundCommandWatch]
	flow *BackgroundCommandFlow
}

// GetStepOptions: the watch outlasts the command's lifetime by watchSlack, so it sees the ledger end
// the command; past it, or after exhausted retries, the command is lost.
func (WatchBackgroundCommand) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(sandboxtools.BackgroundCommandLifetime+watchSlack, dex.ProceedToOnExecuteFailure(RecordBackgroundCommandLost{}, lostStepOptions))
}

func (step WatchBackgroundCommand) Execute(ctx dex.Context, watch BackgroundCommandWatch) (*dex.StepDecision, error) {
	end, err := step.flow.watchCommand(ctx, watch)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(end), nil
}

// dex:group group-id:watch group-label:"Watch"
// dex:explanation text:"Complete with a lost command after the watch passed its limit or exhausted its retries."
type RecordBackgroundCommandLost struct {
	dex.StepDefaultsNoWaitFor[BackgroundCommandWatch]
}

var lostStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second}

func (RecordBackgroundCommandLost) GetStepOptions() *dex.StepOptions { return lostStepOptions }

func (RecordBackgroundCommandLost) Execute(_ dex.Context, _ BackgroundCommandWatch) (*dex.StepDecision, error) {
	return dex.GracefulComplete(agenttasks.BackgroundCommandEnd{EndReason: agenttasks.BackgroundCommandEndLost, EndedAt: time.Now().UTC()}), nil
}
