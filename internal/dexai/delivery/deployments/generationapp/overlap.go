// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// The old Order shape stays unchanged. Shipping is additive state in its own Attribute.
type oldOrder struct{ Name string }
type shipping struct{ Address string }
type overlapState struct {
	Order         oldOrder
	Shipping      shipping
	StartedBy     string
	LongBy        string
	AddedBy       string
	SharedFirstBy string
	SharedBy      string
	NewRPCBy      string
	AddedAttempt  int32
	RPCAttempt    int32
}

var (
	orderState      = dex.DefineAttribute[oldOrder]("Order")
	shippingState   = dex.DefineAttribute[shipping]("OrderShipping")
	overlapProgress = dex.DefineAttribute[overlapState]("Progress")
	longWorker      = dex.DefineAttribute[string]("LongWorker")
)

type OverlapProbeFlow struct{ dex.FlowDefaults }

func (OverlapProbeFlow) GetSteps() []dex.StepDef {
	steps := []dex.StepDef{dex.DefineStartStep(StartOverlap{}), dex.DefineStep(LongOverlap{}), dex.DefineStep(SharedOverlap{})}
	if version != "1" {
		steps = append(steps, dex.DefineStep(AddedOverlap{}))
	}
	return steps
}
func (f OverlapProbeFlow) GetRPCs() []dex.RPCDef {
	rpcs := []dex.RPCDef{dex.DefineRPC(f.GetOverlap, nil)}
	if version != "1" {
		rpcs = append(rpcs, dex.DefineRPC(f.AddShipping, nil))
	}
	return rpcs
}
func (OverlapProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderState, shippingState, overlapProgress, longWorker}}
}
func (OverlapProbeFlow) GetOverlap(ctx dex.Context, _ dex.None) (*dex.RPCResult[overlapState], error) {
	state, err := overlapProgress.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Order, err = orderState.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.LongBy, err = longWorker.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Shipping, err = shippingState.Get(ctx)
	return &dex.RPCResult[overlapState]{Output: state}, err
}
func (OverlapProbeFlow) AddShipping(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	// Only the new registry contains this RPC; an old sticky Worker must reject it without consuming an attempt.
	state, err := overlapProgress.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.NewRPCBy, state.RPCAttempt = version, ctx.Attempt()
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(AddedOverlap{}, nil)}}, errors.Join(overlapProgress.Set(ctx, state), shippingState.Set(ctx, shipping{Address: "new-address"}))
}

type StartOverlap struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (StartOverlap) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := errors.Join(orderState.Set(ctx, oldOrder{Name: "before"}), shippingState.Set(ctx, shipping{}), overlapProgress.Set(ctx, overlapState{StartedBy: version}), longWorker.Set(ctx, "")); err != nil {
		return nil, err
	}
	return dex.GoTo(SharedOverlap{}, 1), nil
}

type LongOverlap struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (LongOverlap) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 5 * time.Minute, HeartbeatTimeout: 15 * time.Second}
}
func (LongOverlap) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	// Retain a decoded old Order while N+1 writes OrderShipping. Rewriting this old shape cannot erase the separate shipping Attribute.
	order, err := orderState.Get(ctx)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	heartbeat := time.NewTicker(3 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-heartbeat.C:
			if err := ctx.RecordHeartbeat("long-step-" + version); err != nil {
				return nil, err
			}
		case <-timer.C:
			order.Name = "old-worker-finished"
			// A separate fact avoids overwriting the concurrently committed RPC's Progress.
			if err := errors.Join(orderState.Set(ctx, order), longWorker.Set(ctx, version)); err != nil {
				return nil, err
			}
			return dex.GoTo(SharedOverlap{}, 3), nil
		}
	}
}

type AddedOverlap struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (AddedOverlap) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state, err := overlapProgress.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.AddedBy, state.AddedAttempt = version, ctx.Attempt()
	if err := overlapProgress.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(SharedOverlap{}, 2), nil
}

type SharedOverlap struct {
	dex.StepDefaultsNoWaitFor[int]
}

func (SharedOverlap) Execute(ctx dex.Context, stage int) (*dex.StepDecision, error) {
	state, err := overlapProgress.Get(ctx)
	if err != nil {
		return nil, err
	}
	if stage == 1 {
		state.SharedFirstBy = version
		if err := overlapProgress.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(LongOverlap{}, nil), nil
	}
	if stage == 3 {
		return dex.GracefulComplete(nil), nil
	}
	state.SharedBy = version
	if err := overlapProgress.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

func overlapHandlers(mux *http.ServeMux, client *dex.Client) {
	mux.HandleFunc("POST /overlap/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_, err := client.StartFlow(r.Context(), OverlapProbeFlow{}, id, nil, dex.StartFlowOptions{IDReusePolicy: dex.IDReuseDisallow, RequestID: &id})
		respond(w, nil, err)
	})
	mux.HandleFunc("POST /overlap/{id}/shipping", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, client.InvokeRPC(r.Context(), r.PathValue("id"), OverlapProbeFlow{}.AddShipping, nil, nil))
	})
	mux.HandleFunc("GET /overlap/{id}", func(w http.ResponseWriter, r *http.Request) {
		var state overlapState
		err := client.InvokeRPC(r.Context(), r.PathValue("id"), OverlapProbeFlow{}.GetOverlap, nil, &state)
		respond(w, state, err)
	})
}
