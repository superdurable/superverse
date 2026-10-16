// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestLiveNextPublishDexIntegration reads and writes Live's next-publish
// layer through projectconfig.Configuration as the API does, on a throwaway
// READY Project of the lane whose Preview source is prepared and which has
// nothing on Live and no applied Preview:
//   - the next publish read without a Release (GetProjectConfiguration with
//     next_publish and an empty release_id) is checked against the Preview's
//     source and lists every declared item as FOLLOWS_PREVIEW of a layer at
//     revision zero, with its gaps;
//   - before Live ever prepared the HEAD, the Preview tab's Next publish
//     column writes the layer checked against the Preview's source: a
//     connection of Live's own, validated READY, then "Same as Preview"
//     (follow_preview), and a connection name the source does not declare is
//     refused;
//   - once the HEAD's Release is prepared, its own read shows its frozen copy
//     stale before it was ever built, and DeriveLiveConfiguration is refused
//     when the layer revision or Preview's applied revision it names moved;
//   - the connections a person selects on the layer checked against the
//     Release are Live's own, the derived copy is current at its revision
//     until the layer moves, and "Same as Preview" makes a connection follow
//     again;
//   - following Preview is refused on the Preview scope.
//
// The read without a Release and the derive guard also go through the merged
// server's gRPC API (ProjectService and ReleaseService) as a signed-in
// caller. Dex, the Git host, the server, MinIO and PostgreSQL are real.
func TestLiveNextPublishDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+previewSetupTimeout+liveSourceTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	session.preparePreview(ctx)
	session.previews.Flow.Preview = preview.NewPreviewConfiguration(nil, nil, artifactStore(ctx, t), nil, nil, nil, nil)
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	project, account, configuration := session.identity.ProjectID, session.account, live.configuration
	read := func(target *pb.ProjectConfigurationTarget) (*pb.ProjectConfigurationSnapshot, []configitems.Item) {
		t.Helper()
		snapshot, items, err := configuration.Get(ctx, account, target)
		if err != nil {
			t.Fatalf("read the Live configuration %+v: %v", target, err)
		}
		return snapshot, items
	}
	next := &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, NextPublish: true}
	release := &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: head}

	// The read without a Release is checked against the Preview's source.
	snapshot, items := read(next)
	if snapshot.GetRevision() != 0 || snapshot.GetNextPublishRevision() != 0 || snapshot.GetPreviewConfigurationRevision() != 0 || snapshot.GetSourceCommit() != head ||
		snapshot.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_NEEDS_REVIEW || len(snapshot.GetRequirementGaps()) == 0 || len(items) == 0 {
		t.Fatalf("the next publish of a Project without a layer reads revision %d of %s, %s with %d gaps and %d items",
			snapshot.GetRevision(), snapshot.GetSourceCommit(), snapshot.GetStatus(), len(snapshot.GetRequirementGaps()), len(items))
	}
	for _, item := range items {
		if item.State != configitems.FollowsPreview || !item.FollowRefusal.IsZero() {
			t.Fatalf("with nothing on Live and no applied Preview, item %+v is %s", item.Key, item.State)
		}
	}

	// The same read through the merged server's gRPC API, as Studio's BFF sends it.
	connection, caller := projecttest.LoginForTest(ctx, t, account)
	projectAPI, releaseAPI := pb.NewProjectServiceClient(connection), pb.NewReleaseServiceClient(connection)
	served, err := projectAPI.GetProjectConfiguration(ctx, &pb.GetProjectConfigurationRequest{Target: next, Caller: caller})
	if err != nil || served.GetConfiguration().GetSourceCommit() != head || served.GetConfiguration().GetNextPublishRevision() != 0 ||
		len(served.GetConfiguration().GetItems()) != len(items) || served.GetConfiguration().GetItems()[0].GetState() != pb.ConfigurationItem_STATE_FOLLOWS_PREVIEW {
		t.Fatalf("GetProjectConfiguration of the next publish without a Release answered %v", err)
	}
	if _, err = projectAPI.GetProjectConfiguration(ctx, &pb.GetProjectConfigurationRequest{
		Target: &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW, NextPublish: true}, Caller: caller,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a next-publish target on Preview answered %v", err)
	}

	// Live never prepared the HEAD, so no Release of it has a configuration; the Preview tab's Next
	// publish column still writes the layer, checked against the Preview's source.
	if _, _, err = configuration.Get(ctx, account, release); err == nil {
		t.Fatalf("the HEAD %s has a Live configuration before Live prepared it", head)
	}
	previewNext := &pb.FlowApiTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, NextPublish: true}
	session.configureConnections(ctx, configuration, previewNext)
	_, items = read(next)
	previewKey := items[slices.IndexFunc(items, func(item configitems.Item) bool { return item.Kind == configitems.KindConnection })].Key
	if itemState(t, items, previewKey) != configitems.Own {
		t.Fatalf("the connection %s selected against the Preview's source is not Live's own", previewKey.ConnectionName)
	}
	list, revision := session.configurationRevision(ctx, configuration, previewNext)
	form := &pb.FlowApiContext{Target: previewNext, DefinitionRevision: list.DefinitionRevision, AppManifestRevision: list.AppManifestRevision, ConfigurationRevision: revision}
	var followed struct {
		ConfigurationRevision string `json:"configurationRevision"`
	}
	previewRoute := "/api/v2/connector-selections/" + url.PathEscape(previewKey.ConnectorID) + "/" + url.PathEscape(previewKey.ConnectionName)
	session.invokeConfiguration(ctx, configuration, form, projectconfig.WriteAccess, http.MethodPut, previewRoute, map[string]bool{"followPreview": true}, &followed)
	if _, after := read(next); itemState(t, after, previewKey) != configitems.FollowsPreview {
		t.Fatalf("the connection %s does not follow Preview after Same as Preview against the Preview's source", previewKey.ConnectionName)
	}
	// A connection name the Preview's source does not declare is refused.
	form.ConfigurationRevision++
	undeclared, _ := json.Marshal(map[string]string{"teamSpaceConnectionId": "cundeclared01"})
	document, err := configuration.InvokeForAccount(ctx, account, form, projectconfig.WriteAccess, http.MethodPut,
		"/api/v2/connector-selections/"+url.PathEscape(previewKey.ConnectorID)+"/not-declared", undeclared)
	if err != nil || document.StatusCode != http.StatusNotFound {
		t.Fatalf("a selection the Preview's source does not declare answered %d (%v)", document.StatusCode, err)
	}

	// Live prepares the HEAD's Release: its copy was never built.
	if prepared := live.prepare(ctx, head, 1); prepared.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the Release of %s is %s", head, prepared.PreparedSource.Status)
	}
	shown, _ := read(next)
	if copied, _ := read(release); !copied.GetIsStale() || copied.GetNextPublishRevision() != shown.GetRevision() {
		t.Fatalf("the never-built copy of %s reads stale=%t at layer revision %d of %d", head, copied.GetIsStale(), copied.GetNextPublishRevision(), shown.GetRevision())
	}

	// The derive guard: the layer and Preview's applied revision must be the ones shown, through gRPC
	// (a moved revision is the API's REVISION_CHANGED, an Aborted status) and through the configuration.
	if _, err = releaseAPI.DeriveLiveConfiguration(ctx, &pb.DeriveLiveConfigurationRequest{ProjectId: project, ReleaseId: head, ExpectedRevision: shown.GetRevision() + 1, Caller: caller}); status.Code(err) != codes.Aborted {
		t.Fatalf("DeriveLiveConfiguration over a moved layer revision answered %v", err)
	}
	if _, err = releaseAPI.DeriveLiveConfiguration(ctx, &pb.DeriveLiveConfigurationRequest{ProjectId: project, ReleaseId: head, ExpectedRevision: shown.GetRevision(), ExpectedPreviewConfigurationRevision: 5, Caller: caller}); status.Code(err) != codes.Aborted {
		t.Fatalf("DeriveLiveConfiguration over a moved Preview revision answered %v", err)
	}
	if _, err := configuration.DeriveLive(ctx, account, project, head, shown.GetRevision()+1, 0); !errors.Is(err, projectconfig.ErrConflict) {
		t.Fatalf("a copy over a layer revision that is not current answered %v", err)
	}
	if _, err := configuration.DeriveLive(ctx, account, project, head, shown.GetRevision(), 5); !errors.Is(err, projectconfig.ErrConflict) {
		t.Fatalf("a copy over a Preview revision that is not applied answered %v", err)
	}

	// A person selects the connections on the layer; they become Live's own.
	session.configureConnections(ctx, configuration, live.target(head))
	snapshot, items = read(next)
	layer := snapshot.GetRevision()
	connectionKeys := []configitems.Key{}
	for _, item := range items {
		if item.Kind == configitems.KindConnection {
			if item.State != configitems.Own || !item.DiffersFromLive {
				t.Fatalf("the selected connection %s is %s", item.ConnectionName, item.State)
			}
			connectionKeys = append(connectionKeys, item.Key)
		}
	}
	if layer == 0 || len(connectionKeys) == 0 {
		t.Fatalf("the layer is at revision %d with %d connection items", layer, len(connectionKeys))
	}
	derived, err := releaseAPI.DeriveLiveConfiguration(ctx, &pb.DeriveLiveConfigurationRequest{ProjectId: project, ReleaseId: head, ExpectedRevision: layer, Caller: caller})
	if err != nil || derived.GetConfiguration().GetIsStale() || derived.GetConfiguration().GetNextPublishRevision() != layer {
		t.Fatalf("DeriveLiveConfiguration at the layer revision shown answered %v", err)
	}
	copiedRevision := derived.GetConfiguration().GetRevision()
	if copied, _ := read(release); copied.GetIsStale() || copied.GetRevision() != copiedRevision || copied.GetNextPublishRevision() != layer ||
		copied.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_READY {
		t.Fatalf("the copy just built reads stale=%t at revision %d of %d, layer %d, %s", copied.GetIsStale(), copied.GetRevision(), copiedRevision, copied.GetNextPublishRevision(), copied.GetStatus())
	}

	// "Same as Preview": the connection follows again, and the copy is stale.
	key := connectionKeys[0]
	list, revision = session.configurationRevision(ctx, configuration, live.target(head))
	form = &pb.FlowApiContext{Target: live.target(head), DefinitionRevision: list.DefinitionRevision, AppManifestRevision: list.AppManifestRevision, ConfigurationRevision: revision}
	route := "/api/v2/connector-selections/" + url.PathEscape(key.ConnectorID) + "/" + url.PathEscape(key.ConnectionName)
	session.invokeConfiguration(ctx, configuration, form, projectconfig.WriteAccess, http.MethodPut, route, map[string]bool{"followPreview": true}, &followed)
	if _, after := read(next); itemState(t, after, key) != configitems.FollowsPreview {
		t.Fatalf("the connection %s does not follow Preview after Same as Preview", key.ConnectionName)
	}
	if copied, _ := read(release); !copied.GetIsStale() {
		t.Fatal("the copy is not stale after a layer write")
	}

	// Following Preview is refused on Preview itself.
	previewTarget := &pb.FlowApiTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW}
	previewList, previewRevision := session.configurationRevision(ctx, configuration, previewTarget)
	previewForm := &pb.FlowApiContext{Target: previewTarget, DefinitionRevision: previewList.DefinitionRevision, AppManifestRevision: previewList.AppManifestRevision, ConfigurationRevision: previewRevision}
	body, _ := json.Marshal(map[string]bool{"followPreview": true})
	document, err = configuration.InvokeForAccount(ctx, account, previewForm, projectconfig.WriteAccess, http.MethodPut, route, body)
	var coded struct {
		Code string `json:"code"`
	}
	if err != nil || document.StatusCode != http.StatusConflict || json.Unmarshal(document.JSON, &coded) != nil || coded.Code != confighost.ItemCannotFollowCodeForTest {
		t.Fatalf("following Preview on the Preview scope answered %d %s (%v)", document.StatusCode, coded.Code, err)
	}
	t.Logf("FlowID=%s Release %s of Project %s: %d items follow Preview with no layer; %s owned and followed again against the Preview's source before Live prepared it; "+
		"layer revision %d owns %d connections; copy %d; Same as Preview followed %s", session.id, head, project, len(items), previewKey.ConnectionName, layer, len(connectionKeys), copiedRevision, key.ConnectionName)
}

// itemState is the state of key's item among items.
func itemState(t *testing.T, items []configitems.Item, key configitems.Key) configitems.State {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			return item.State
		}
	}
	t.Fatalf("no item %+v", key)
	return ""
}
