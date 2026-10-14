// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"context"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
)

// Authorization is what Studio Runs resolved for one operation: the
// authenticated actor and the serving target the operation addresses.
type Authorization struct {
	ActorID string
	// TargetRevision identifies the serving target and its definitions; a Start
	// request must name it.
	TargetRevision string
	// WorkerTarget is the host:port of the serving Worker that new Flows run on.
	WorkerTarget string
	// WorkerHeadless makes Dex resolve WorkerTarget itself, as the Live Worker
	// endpoint requires, so the Flows follow it to each new generation.
	WorkerHeadless bool
}

// FlowAPI serves the Studio Runs operations of one authorized serving target
// in process. Studio Runs builds one per request after it authorizes the
// account, selects the target and admits the caller's definition revision;
// nothing is cached across requests.
type FlowAPI struct {
	client        dexpb.FlowServiceClient
	authorization Authorization
	bundle        *fdg.Bundle
	checkWorker   func(context.Context) error
}

// New returns the Flow API of a serving target. bundle holds the target's
// serving definitions, which every operation trusts as ParseBundle validated
// them. checkWorker verifies that the target's Worker is ready to run a Flow.
func New(client dexpb.FlowServiceClient, authorization Authorization, bundle *fdg.Bundle, checkWorker func(context.Context) error) *FlowAPI {
	return &FlowAPI{client: client, authorization: authorization, bundle: bundle, checkWorker: checkWorker}
}
