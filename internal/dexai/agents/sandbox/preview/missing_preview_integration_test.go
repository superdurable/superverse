// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
)

// The actual Dex/Temporal dependency resolves an absent RPC target. No Worker,
// Flow, provider substitute or resource is created by this read-only regression:
// a Project that never ran a Preview command has no PreviewFlow, and reading it
// starts none.
func TestMissingPreviewReadDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	flow := &preview.PreviewFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, flow)
	projectID := flowid.RandomID(16)
	id := preview.FlowID(projectID)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	dexflowtest.RequireAbsentForTest(ctx, t, client, id)
	var raw preview.Snapshot
	err := client.InvokeRPC(ctx, id, flow.GetPreview, nil, &raw)
	var inactive *dex.FlowNotActiveOrNotFoundError
	if !errors.As(err, &inactive) {
		t.Fatalf("pinned SDK missing RPC classification changed (%T)", err)
	}
	_, err = (&preview.PreviewFlowClient{Dex: client, Flow: flow, Execution: dexflowtest.ExecutionReaderForTest(t, address)}).Get(ctx, projectID)
	var missing *dex.FlowNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("an unstarted Preview was not reported as absent (%T)", err)
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, id)
	t.Logf("FlowID=%s; read-only missing RPC classified as absence; read created no execution", id)
}
