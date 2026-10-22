// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"context"
	"encoding/json"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"testing"
	"time"
)

// PersistLiveCredentialForTest admits the developer key through the business API and
// exact-reads its hard-delete envelope; no live test obtains a platform Session key.
func PersistLiveCredentialForTest(t *testing.T, key string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	flow := &projects.ProjectFlow{}
	sdk := dexflowtest.ServerWorkerClient(t, flow)
	client := &projects.ProjectFlowClient{Dex: sdk, Flow: flow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	project := projecttest.CreateForTest(ctx, t, client, t.Name())
	connection, caller := projecttest.LoginForTest(ctx, t, project.CreatorAccountID)
	credentials := pb.NewModelCredentialServiceClient(connection)
	revision := uint64(0)
	_, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: key, Caller: caller})
	if err != nil {
		t.Fatalf("set live Project credential (%T)", err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		revision := uint64(1)
		if _, err := credentials.DeleteModelCredential(cleanup, &pb.DeleteModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, Caller: caller}); err != nil {
			t.Errorf("delete live Project credential (%T)", err)
		}
	})
	reader := dexflowtest.ExecutionReaderForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"))
	response, err := reader.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: flowid.SessionFlowID(project.ProjectID), Keys: []string{"ModelKeys/anthropic"}})
	if err != nil || len(response.GetAttributes()) != 1 {
		t.Fatal("exact-read live Project credential")
	}
	var envelope modelcredential.Envelope
	if json.Unmarshal(response.GetAttributes()[0].GetValue().GetObjValue().GetPayload(), &envelope) != nil || envelope.Revision != 1 || envelope.Key == "" {
		t.Fatal("live credential envelope is unavailable")
	}
	return envelope.Key
}
