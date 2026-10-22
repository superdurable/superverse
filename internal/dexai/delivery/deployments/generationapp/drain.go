// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/dex/sdk-go/dex"
)

type drainState struct {
	By            string
	Attempt       int32
	EffectVersion string
}

var drainProgress = dex.DefineAttribute[drainState]("DrainProgress")

type DrainProbeFlow struct{ dex.FlowDefaults }

func (DrainProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(DrainStart{}), dex.DefineStep(DrainLong{})}
}
func (f DrainProbeFlow) GetRPCs() []dex.RPCDef { return []dex.RPCDef{dex.DefineRPC(f.ReadDrain, nil)} }
func (DrainProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{drainProgress}}
}
func (DrainProbeFlow) ReadDrain(ctx dex.Context, _ dex.None) (*dex.RPCResult[drainState], error) {
	s, e := drainProgress.Get(ctx)
	return &dex.RPCResult[drainState]{Output: s}, e
}

type DrainStart struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (DrainStart) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if e := drainProgress.Set(ctx, drainState{}); e != nil {
		return nil, e
	}
	return dex.GoTo(DrainLong{}, nil), nil
}

type DrainLong struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (DrainLong) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 5 * time.Minute, HeartbeatTimeout: 15 * time.Second, ExecuteRetry: &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: time.Second, TotalDuration: 5 * time.Minute}}
}
func drainEffect(ctx context.Context, id string) (string, error) {
	client := s3.New(s3.Options{Region: os.Getenv("AWS_REGION"), BaseEndpoint: aws.String(os.Getenv("DEX_PROJECT_STORAGE_ENDPOINT")), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), "")})
	bucket := os.Getenv("DEX_PROJECT_STORAGE_BUCKET")
	key := os.Getenv("DEX_PROJECT_STORAGE_PREFIX") + "projects/" + os.Getenv("DEX_PROJECT_ID") + "/live/trigger-inboxes/" + id + "/head"
	existing, e := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if e == nil {
		return aws.ToString(existing.VersionId), nil
	}
	var missing *types.NotFound
	if !errors.As(e, &missing) {
		return "", e
	}
	written, e := client.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &key, Body: strings.NewReader(id), IfNoneMatch: aws.String("*")})
	if e != nil {
		return "", e
	}
	return aws.ToString(written.VersionId), nil
}
func (DrainLong) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	effect, e := drainEffect(ctx, ctx.FlowID())
	if e != nil {
		return nil, e
	}
	if version == "drain-1" {
		log.Printf("DRAIN_EFFECT_READY flow=%s version=%s", ctx.FlowID(), effect)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(3 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Printf("DRAIN_CANCELLED flow=%s error=%v", ctx.FlowID(), ctx.Err())
				return nil, ctx.Err()
			case <-ticker.C:
				if e := ctx.RecordHeartbeat("drain"); e != nil {
					return nil, e
				}
			case <-timer.C:
				return nil, fmt.Errorf("old handler unexpectedly outlived drain")
			}
		}
	}
	if e := drainProgress.Set(ctx, drainState{By: version, Attempt: ctx.Attempt(), EffectVersion: effect}); e != nil {
		return nil, e
	}
	return dex.GracefulComplete(nil), nil
}
func drainHandlers(mux *http.ServeMux, client *dex.Client) {
	mux.HandleFunc("POST /drain/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_, e := client.StartFlow(r.Context(), DrainProbeFlow{}, id, nil, dex.StartFlowOptions{IDReusePolicy: dex.IDReuseDisallow, RequestID: &id})
		respond(w, nil, e)
	})
	mux.HandleFunc("GET /drain/{id}", func(w http.ResponseWriter, r *http.Request) {
		var s drainState
		e := client.InvokeRPC(r.Context(), r.PathValue("id"), DrainProbeFlow{}.ReadDrain, nil, &s)
		respond(w, s, e)
	})
}
