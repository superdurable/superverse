// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflowtest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// maximumSearchPages bounds a child search; a Project has far fewer children.
const maximumSearchPages = 50

// ProjectChildIDsForTest lists, through Dex search, the IDs of the children of
// kind (for example release or deploy) that Flows of flowType left for the
// Project, closed ones included, sorted. Release and Deployment Flows index
// their ProjectID as keyword2.
func ProjectChildIDsForTest(ctx context.Context, t testing.TB, client *dex.Client, flowType, kind, projectID string) []string {
	t.Helper()
	query := "FlowType = '" + flowType + "' AND keyword2 = '" + projectID + "'"
	var ids []string
	token := ""
	for range maximumSearchPages {
		page, err := client.SearchFlows(ctx, query, 100, token)
		if err != nil {
			t.Fatalf("search the %s Flows of Project %s: %s", flowType, projectID, ErrorCategory(err))
		}
		for _, found := range page.Flows {
			if id, child := flowid.ParseProjectChild(kind, projectID, found.FlowID); child {
				ids = append(ids, id)
			}
		}
		if token = page.NextPageToken; token == "" {
			slices.Sort(ids)
			return slices.Compact(ids)
		}
	}
	t.Fatalf("the search of the %s Flows of Project %s did not end within %d pages", flowType, projectID, maximumSearchPages)
	return nil
}

// MapInstanceKeysForTest lists the instance keys of the AttributeMap named
// name in the state of flowID's latest run, sorted. Dex reports a map instance
// as the Attribute key name/instance.
func MapInstanceKeysForTest(ctx context.Context, t testing.TB, reader dexpb.FlowServiceClient, flowID, name string) []string {
	t.Helper()
	state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("read the state of %s: %s", flowID, ErrorCategory(err))
	}
	var keys []string
	for _, attribute := range state.GetAttributes() {
		if instance, isInstance := strings.CutPrefix(attribute.GetKey(), name+"/"); isInstance && instance != "" {
			keys = append(keys, instance)
		}
	}
	slices.Sort(keys)
	return keys
}
