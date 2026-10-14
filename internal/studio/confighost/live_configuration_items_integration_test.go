// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package confighost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

const (
	slackModule      = "github.com/superdurable/dex-connectors-library/connectors/slack"
	slackConnection  = "slack-workspace"
	slackStartKey    = "slack-thread-approval-start"
	slackReplyKey    = "slack-thread-approval-reply"
	stripeModule     = "github.com/superdurable/dex-connectors-library/connectors/stripe"
	stripeConnection = "payments"
	nextPublishPath  = "/api/v2/project-configuration/live-items"
	copiesPath       = "/api/v2/project-configuration/live-derivations"
)

// nextPublishFixture is one Project's Live and Preview scopes in a real
// versioned MinIO bucket, each served by the production configuration host,
// which confines its object store to its own scope's prefix, with the lane's
// PostgreSQL for TeamSpace connections, the official Slack v0.22.1 and Stripe
// v0.21.1 releases and the FDG the pinned dexcli generated from Slack's
// thread-approval example.
type nextPublishFixture struct {
	t             *testing.T
	ctx           context.Context
	live, preview *ConfigurationHost
	// objects is the bucket's store under the test's prefix, which every host confines to its own scope.
	objects             connectorconfig.ObjectStore
	client              *s3.Client
	bucket, prefix      string
	teamSpaceID         string
	graph               []byte
	definitionRevisions map[string]string
	digests             map[string]string
}

// TestLiveNextPublishLayer walks Live's next-publish layer on the production
// host and the prefix-confined store: a Project without a layer reads the
// seed computed from what runs, every secret, the address-bound connection
// and each item that differs from Preview's applied value being Live's own;
// following Preview is refused on Preview, for a secret and for an
// address-bound connection; concurrent first writes store the seed once with
// one winner; each item kind is made Live's own and follows Preview again; an
// item Preview's applied version lacks follows current Live; a version's copy
// is refused when the layer or Preview's applied revision moved, keeps only
// what the version declares with Live's own secrets and credential broker,
// freezes for a build and stays frozen after a layer write; and the copy is
// stale after a layer write, a Preview apply and a new Live. Unpublished
// per-version draft edits are dropped. Neither host reads the other's prefix.
// Writes checked against the Preview's prepared source, which Live never
// prepared, own and follow a connection and a Step setting, and one the source
// does not declare is refused.
func TestLiveNextPublishLayer(t *testing.T) {
	f := newNextPublishFixture(t)
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	account := flowid.RandomID(32)
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, "admin@superverse.test").Register(f.ctx, accountdao.Account{
		AccountID: account, CanonicalEmail: "next-publish-" + account + "@example.com", Status: accountdao.StatusActive,
		PasswordRecord: json.RawMessage(`{}`), CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: account, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("register the account: %v", err)
	}
	slackConnectionRow := func(name string) connectiondao.Connection {
		row, inserted, err := dao.Create(f.ctx, connectiondao.Connection{
			TeamSpaceID: f.teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: name, Name: name,
			ConnectorID: connections.SlackConnectorID, ModulePath: slackModule, VersionLine: "v0", ManifestVersion: "v0.22.1",
			ClientSource: connectiondao.ClientUser, Settings: json.RawMessage(`{}`), Revision: 1, CredentialStatus: connectiondao.CredentialReady,
			ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: account, UpdatedByAccountID: account, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil || !inserted {
			t.Fatalf("create the TeamSpace Slack connection %s: inserted=%t: %v", name, inserted, err)
		}
		return row
	}
	first, second := slackConnectionRow("next-publish-slack-a"), slackConnectionRow("next-publish-slack-b")
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM team_space_connection_usages WHERE team_space_id = $1`, `DELETE FROM team_space_connections WHERE team_space_id = $1`,
		} {
			if _, cleanupErr := db.ExecContext(cleanup, statement, f.teamSpaceID); cleanupErr != nil {
				t.Errorf("clean up: %v", cleanupErr)
			}
		}
		if _, cleanupErr := db.ExecContext(cleanup, `DELETE FROM scope_credential_tokens WHERE project_id = $1`, f.live.project.scope.ProjectID); cleanupErr != nil {
			t.Errorf("clean up the scope token: %v", cleanupErr)
		}
		if _, cleanupErr := db.ExecContext(cleanup, `DELETE FROM accounts WHERE account_id = $1`, account); cleanupErr != nil {
			t.Errorf("clean up the account: %v", cleanupErr)
		}
	})
	providers := connections.NewProviderHTTPClient(true)
	artifacts, err := objectstore.NewStore(f.client, f.bucket, "", f.prefix, true)
	storageRequire(t, err, "construct the artifact store")
	shared := SharedConnections{
		Providers: providers, Usages: connections.NewUsages(dao, nil), ReadURL: "http://127.0.0.1:1/internal/connection-credentials/read",
		Connections:      connections.NewTeamSpaceConnections(dao, artifacts, f.live.connectorReleases, nil, connections.PlatformClients{}, providers, nil, nil, true),
		ProjectTeamSpace: func(context.Context, string) (string, error) { return f.teamSpaceID, nil },
	}
	f.live.shared, f.preview.shared = shared, shared

	// Neither host reaches the other's prefix, whatever its store holds.
	if _, err = f.live.project.objects.ReadObject(f.ctx, f.preview.project.prefix+"/configuration/head", ""); !errors.Is(err, infra.ErrAccessDenied) {
		t.Fatalf("the Live host read the Preview's prefix: %v", err)
	}

	// Live runs version 1. Preview applied its own values; the secret and the broker never reach Live.
	withStripe, withoutStripe := strings.Repeat("b", 40), strings.Repeat("c", 40)
	liveSecret := "live-secret-" + uuid.NewString()
	live1 := f.publish(func(document *connectorconfig.Configuration) {
		document.Environment["APP_KEPT"], document.Environment["APP_OTHER"] = value("one"), value("same")
		document.Environment["APP_DROPPED"], document.Environment["APP_LIVE_ONLY"] = value("x"), value("live-only")
		document.Environment["SIGNING_SECRET"] = f.liveSecret("SIGNING_SECRET", liveSecret)
		document.Connections = []connectorconfig.ConnectionConfiguration{slackEntry(first.ConnectionID)}
		document.TriggerBindings = []connectorconfig.TriggerConfiguration{binding(slackStartKey, "C1")}
		document.CredentialBroker = f.liveBroker()
	})
	preview := previewApplied(first.ConnectionID, 7)
	next := &NextPublish{Live: live1.ref("dep-1"), Preview: &preview}
	f.prepare(withStripe, true)
	f.prepare(withoutStripe, false)
	// An unpublished per-version draft edit of the old model is dropped.
	stale, _, err := f.live.project.commitDraft(withStripe).ReadConfiguration(f.ctx)
	storageRequire(t, err, "read the version's initialized copy")
	stale.Environment = map[string]connectorconfig.EnvironmentValue{"APP_KEPT": value("old-draft-edit")}
	_, _, err = f.live.project.commitDraft(withStripe).WriteConfiguration(f.ctx, stale.Revision, stale)
	storageRequire(t, err, "write an old per-version draft edit")

	// Without a layer every read resolves the seed and writes nothing.
	report := f.report(withStripe, next)
	if report.LayerRevision != 0 || report.PreviewRevision != 7 || report.Copy == nil || !report.Copy.Stale {
		t.Fatalf("the first read of the next publish is %+v", report)
	}
	f.requireItems(report, map[configitems.Key]itemWant{
		environmentKey("APP_KEPT"):       {state: configitems.Own, differsFromPreview: true},
		environmentKey("APP_OTHER"):      {state: configitems.FollowsPreview},
		environmentKey("APP_LIVE_ONLY"):  {state: configitems.Own, differsFromPreview: true},
		environmentKey("APP_NEW"):        {state: configitems.FollowsPreview, differsFromLive: true},
		environmentKey("SIGNING_SECRET"): {state: configitems.Own, refusal: appmanifest.FollowRefusedSecret, differsFromPreview: true},
		environmentKey("APP_DROPPED"):    {state: configitems.NoLongerUsed, differsFromLive: true},
		connectionKey():                  {state: configitems.FollowsPreview},
		stripeKey():                      {state: configitems.Own, refusal: appmanifest.FollowRefusedEndpointBound, differsFromPreview: true},
		bindingKey(slackStartKey):        {state: configitems.Own, differsFromPreview: true},
		bindingKey(slackReplyKey):        {state: configitems.FollowsPreview, differsFromLive: true},
		stepKey():                        {state: configitems.FollowsPreview},
	})
	if item := findItem(t, report.Items, environmentKey("APP_NEW")); string(item.PreviewValueJSON) != `{"value":"from-preview"}` || item.LiveValueJSON != nil {
		t.Fatalf("the new variable shows Preview %s and Live %s", item.PreviewValueJSON, item.LiveValueJSON)
	}
	if item := findItem(t, report.Items, environmentKey("SIGNING_SECRET")); item.PreviewValueJSON != nil || item.LiveValueJSON != nil {
		t.Fatal("a secret item shows a value")
	}
	if !slices.ContainsFunc(report.Gaps, func(gap appmanifest.RequirementGap) bool {
		return gap.Kind == appmanifest.GapOperationConfigurationRequired && gap.StepName == "PostSlackCompletion"
	}) {
		t.Fatalf("the Step setting that nothing configures is no gap: %+v", report.Gaps)
	}
	// The address-bound connection Preview selects is a gap on Live that says why.
	f.requireRefusedGap(report, appmanifest.FollowRefusedEndpointBound, func(gap appmanifest.RequirementGap) bool {
		return gap.Kind == appmanifest.GapConnectionRequired && gap.ConnectionName == stripeConnection
	})
	if _, err = f.objects.ReadObject(f.ctx, f.layerKey(), ""); !errors.Is(err, connectorconfig.ErrObjectNotFound) {
		t.Fatalf("a read stored the layer: %v", err)
	}

	// Following Preview is refused on Preview, for a secret and for an address-bound connection.
	previewSource := strings.Repeat("d", 40)
	f.preparePreview(previewSource)
	previewAdmission := ConfigurationAdmission{ActorID: "next-publish-builder", TeamSpaceID: f.teamSpaceID, SourceCommit: previewSource, DefinitionRevision: f.definitionRevisions[previewSource], AppManifestRevision: 1, ConfigurationRevision: 1}
	f.requireCode(configurationCall(t, f.ctx, f.preview, previewAdmission, http.MethodPut, selectionPath(connections.SlackConnectorID, slackConnection), map[string]any{"followPreview": true}, 409), itemCannotFollowCode)
	f.requireCode(f.actor(withStripe, next, 0, http.MethodPut, selectionPath("stripe", stripeConnection), map[string]any{"followPreview": true}, 409), itemCannotFollowCode)
	f.requireCode(f.actor(withStripe, next, 0, http.MethodPut, "/api/v2/application-environment", map[string]any{"followPreview": []string{"SIGNING_SECRET"}}, 409), itemCannotFollowCode)

	// Four concurrent first writes over the absent layer: one seed is stored, and one change wins over it.
	type raced struct {
		status int
		body   string
		err    error
	}
	results := make(chan raced, 4)
	start := make(chan struct{})
	for index := range cap(results) {
		go func() {
			<-start
			body := comm.Must(json.Marshal(map[string]any{"values": map[string]string{"APP_KEPT": fmt.Sprintf("race-%d", index)}}))
			document, invokeErr := f.live.Invoke(f.ctx, f.actorAdmission(withStripe, next, 0), http.MethodPut, "/api/v2/application-environment", body)
			results <- raced{document.StatusCode, string(document.JSON), invokeErr}
		}()
	}
	close(start)
	winners := 0
	for range cap(results) {
		result := <-results
		storageRequire(t, result.err, "a concurrent first write")
		switch result.status {
		case http.StatusOK:
			winners++
		case http.StatusConflict:
		default:
			t.Fatalf("a concurrent first write answered %d: %s", result.status, result.body)
		}
	}
	// The seed is the layer's first revision, and the winner's change the second.
	layer := f.layer()
	if winners != 1 || layer.Revision != 2 || !strings.HasPrefix(*layer.Environment["APP_KEPT"].Value, "race-") {
		t.Fatalf("concurrent first writes had %d winners and stored revision %d", winners, layer.Revision)
	}
	if layer.Environment["SIGNING_SECRET"].SecretRef == nil || *layer.Environment["SIGNING_SECRET"].SecretRef != *live1.document.Environment["SIGNING_SECRET"].SecretRef ||
		!configitems.Holds(layer, bindingKey(slackStartKey)) || configitems.Holds(layer, connectionKey()) || configitems.Holds(layer, stripeKey()) {
		t.Fatal("the stored seed does not hold Live's secret and changed binding as its own, or holds what follows or what Live lacks")
	}

	// Each kind becomes Live's own and follows Preview again.
	revision := layer.Revision
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, selectionPath(connections.SlackConnectorID, slackConnection), map[string]any{"teamSpaceConnectionId": second.ConnectionID}, 200))
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, bindingPath(slackReplyKey), map[string]any{"configuration": replyConfiguration("C2")}, 200))
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, stepPath(), map[string]any{"configuration": map[string]any{"text": "Approved"}}, 200))
	f.actor(withStripe, next, revision, http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_NEW": "live-own"}}, 200)
	revision++
	report = f.report(withStripe, next)
	f.requireItems(report, map[configitems.Key]itemWant{
		connectionKey():               {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		bindingKey(slackReplyKey):     {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		stepKey():                     {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		environmentKey("APP_NEW"):     {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		environmentKey("APP_KEPT"):    {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		environmentKey("APP_OTHER"):   {state: configitems.FollowsPreview},
		environmentKey("APP_DROPPED"): {state: configitems.NoLongerUsed, differsFromLive: true},
	})
	var list struct {
		ConfigurationRevision string `json:"configurationRevision"`
		Connections           []struct {
			ConnectionName     string `json:"connectionName"`
			SharedConnectionID string `json:"sharedConnectionId"`
		} `json:"connections"`
	}
	storageRequire(t, json.Unmarshal(f.actor(withStripe, next, revision, http.MethodGet, "/api/v2/connector-connections", nil, 200), &list), "decode the layer's connections")
	if list.ConfigurationRevision != strconv.FormatUint(revision, 10) || !slices.ContainsFunc(list.Connections, func(view struct {
		ConnectionName     string `json:"connectionName"`
		SharedConnectionID string `json:"sharedConnectionId"`
	}) bool {
		return view.ConnectionName == slackConnection && view.SharedConnectionID == second.ConnectionID
	}) {
		t.Fatalf("the layer's connections show revision %s and %+v", list.ConfigurationRevision, list.Connections)
	}
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, selectionPath(connections.SlackConnectorID, slackConnection), map[string]any{"followPreview": true}, 200))
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, bindingPath(slackReplyKey), map[string]any{"followPreview": true}, 200))
	revision = f.saved(f.actor(withStripe, next, revision, http.MethodPut, stepPath(), map[string]any{"followPreview": true}, 200))
	f.actor(withStripe, next, revision, http.MethodPut, "/api/v2/application-environment", map[string]any{"followPreview": []string{"APP_NEW", "APP_LIVE_ONLY"}}, 200)
	revision++
	report = f.report(withStripe, next)
	f.requireItems(report, map[configitems.Key]itemWant{
		connectionKey():           {state: configitems.FollowsPreview},
		bindingKey(slackReplyKey): {state: configitems.FollowsPreview, differsFromLive: true},
		stepKey():                 {state: configitems.FollowsPreview},
		environmentKey("APP_NEW"): {state: configitems.FollowsPreview, differsFromLive: true},
		// Preview's applied version lacks it, so it follows current Live's value.
		environmentKey("APP_LIVE_ONLY"): {state: configitems.FollowsPreview, differsFromPreview: true},
	})
	// The Preview tab's Next publish column: the layer checked against the Preview's prepared source, which
	// the platform reads through the Preview's host and hands over, of a commit Live never prepared.
	named, err := f.preview.PreparedSource(f.ctx, previewSource)
	storageRequire(t, err, "read the Preview's prepared source through its host")
	if _, err = f.objects.ReadObject(f.ctx, f.live.project.preparedSourceKey(previewSource), ""); !errors.Is(err, connectorconfig.ErrObjectNotFound) {
		t.Fatalf("Live prepared the Preview's commit: %v", err)
	}
	viaPreview := &NextPublish{Live: next.Live, Preview: next.Preview, Source: named}
	f.call(withStripe, viaPreview, nextPublishPath, NextPublishRequest{ManifestDigest: f.digests[previewSource]}, 400)
	revision = f.saved(f.actor(previewSource, viaPreview, revision, http.MethodPut, selectionPath(connections.SlackConnectorID, slackConnection), map[string]any{"teamSpaceConnectionId": second.ConnectionID}, 200))
	revision = f.saved(f.actor(previewSource, viaPreview, revision, http.MethodPut, stepPath(), map[string]any{"configuration": map[string]any{"text": "From the Preview tab"}}, 200))
	viewed := f.report(previewSource, viaPreview)
	if viewed.Copy != nil || viewed.LayerRevision != revision {
		t.Fatalf("the next publish checked against the Preview's source reads %+v at layer revision %d of %d", viewed.Copy, viewed.LayerRevision, revision)
	}
	f.requireItems(viewed, map[configitems.Key]itemWant{
		connectionKey(): {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
		stepKey():       {state: configitems.Own, differsFromLive: true, differsFromPreview: true},
	})
	revision = f.saved(f.actor(previewSource, viaPreview, revision, http.MethodPut, selectionPath(connections.SlackConnectorID, slackConnection), map[string]any{"followPreview": true}, 200))
	revision = f.saved(f.actor(previewSource, viaPreview, revision, http.MethodPut, stepPath(), map[string]any{"followPreview": true}, 200))
	f.requireItems(f.report(previewSource, viaPreview), map[configitems.Key]itemWant{
		connectionKey(): {state: configitems.FollowsPreview},
		stepKey():       {state: configitems.FollowsPreview},
	})
	// The Preview's source declares no Stripe connection: neither its selection nor following is admitted.
	f.actor(previewSource, viaPreview, revision, http.MethodPut, selectionPath("stripe", stripeConnection), map[string]any{"teamSpaceConnectionId": second.ConnectionID}, 404)
	f.actor(previewSource, viaPreview, revision, http.MethodPut, selectionPath("stripe", stripeConnection), map[string]any{"followPreview": true}, 404)
	if f.layer().Revision != revision {
		t.Fatal("a refused write against the Preview's source wrote the layer")
	}

	// A secret Live has none of is a gap that says why.
	f.actor(withStripe, next, revision, http.MethodPut, "/api/v2/application-environment", map[string]any{"remove": []string{"SIGNING_SECRET"}}, 200)
	revision++
	f.requireRefusedGap(f.report(withStripe, next), appmanifest.FollowRefusedSecret, func(gap appmanifest.RequirementGap) bool { return gap.EnvironmentName == "SIGNING_SECRET" })
	// A secret entered on the layer lives in Live's own secret area.
	newSecret := "new-live-secret-" + uuid.NewString()
	f.actor(withStripe, next, revision, http.MethodPut, "/api/v2/application-environment", map[string]any{"secrets": map[string]string{"SIGNING_SECRET": newSecret}}, 200)
	revision++
	if reference := f.layer().Environment["SIGNING_SECRET"].SecretRef; reference == nil || !strings.HasPrefix(reference.Key, f.live.project.prefix+"/") {
		t.Fatalf("the layer's secret is %+v, not in Live's own area", reference)
	}

	// Publishing the version without Stripe: the copy is refused when what was shown moved.
	f.call(withoutStripe, next, copiesPath, CopyRequest{ManifestDigest: f.digests[withoutStripe], ExpectedLayerRevision: revision - 1, ExpectedPreviewRevision: 7}, 409)
	f.call(withoutStripe, next, copiesPath, CopyRequest{ManifestDigest: f.digests[withoutStripe], ExpectedLayerRevision: revision, ExpectedPreviewRevision: 6}, 409)
	f.actor(withoutStripe, next, revision, http.MethodPut, stepPath(), map[string]any{"configuration": map[string]any{"text": "Approved"}}, 200)
	revision++
	copyReport := f.copy(withoutStripe, next, revision, 7)
	copied := f.copied(withoutStripe)
	if copyReport.LayerRevision != revision || copied.Revision != copyReport.ConfigurationRevision || configitems.Holds(copied, stripeKey()) || configitems.Holds(copied, environmentKey("APP_DROPPED")) {
		t.Fatalf("the copy %+v holds what the version does not declare", copyReport)
	}
	if copied.CredentialBroker == nil || copied.CredentialBroker.TokenSecretRef == preview.CredentialBroker.TokenSecretRef || !strings.HasPrefix(copied.CredentialBroker.TokenSecretRef.Key, f.live.project.prefix+"/") ||
		copied.Environment["SIGNING_SECRET"].SecretRef == nil || !strings.HasPrefix(copied.Environment["SIGNING_SECRET"].SecretRef.Key, f.live.project.prefix+"/") {
		t.Fatal("the copy does not hold Live's own credential broker and secret")
	}
	if entry := connectionEntry(copied, slackConnection); entry == nil || entry.SharedConnectionID != first.ConnectionID || entry.Provider != "slack" ||
		*copied.Environment["APP_LIVE_ONLY"].Value != "live-only" || *copied.Environment["APP_OTHER"].Value != "same" {
		t.Fatalf("the copy does not select Preview's connection for Live or resolve the followed values: %+v", entry)
	}
	if state := f.report(withoutStripe, next); state.Copy.Stale || state.Copy.Revision != copied.Revision || len(state.Gaps) != 0 {
		t.Fatalf("the copy just built reads %+v with gaps %v", state.Copy, appmanifest.RequirementGapNames(state.Gaps))
	}

	// A build freezes the copy, and a layer write leaves the frozen version alone.
	buildID := "build-" + uuid.NewString()
	var frozen struct {
		Snapshot connectorconfig.SnapshotRef `json:"snapshot"`
	}
	storageRequire(t, json.Unmarshal(configurationCall(t, f.ctx, f.live, ConfigurationAdmission{Internal: true, SourceCommit: withoutStripe}, http.MethodPost, "/api/v2/project-configuration/validate",
		ValidationRequest{RequestID: buildID, AppManifestRevision: 1, ManifestDigest: f.digests[withoutStripe], ConfigurationRevision: copied.Revision}, 200), &frozen), "decode the build's freeze")
	f.actor(withoutStripe, next, revision, http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_KEPT": "after-publish"}}, 200)
	revision++
	snapshot, err := f.live.project.canonical.ReadSnapshot(f.ctx, frozen.Snapshot)
	storageRequire(t, err, "read the frozen version")
	if *snapshot.Environment["APP_KEPT"].Value == "after-publish" || f.copied(withoutStripe).Revision != copied.Revision {
		t.Fatal("a layer write changed the frozen version or the copy")
	}

	// Stale after the layer write, after a Preview apply and after a new Live.
	if !f.report(withoutStripe, next).Copy.Stale {
		t.Fatal("the copy is not stale after a layer write")
	}
	f.copy(withoutStripe, next, revision, 7)
	reapplied := previewApplied(first.ConnectionID, 8)
	if !f.report(withoutStripe, &NextPublish{Live: next.Live, Preview: &reapplied}).Copy.Stale {
		t.Fatal("the copy is not stale after a Preview apply")
	}
	if !f.report(withoutStripe, &NextPublish{Live: LiveConfiguration{DeploymentID: "dep-2", Snapshot: &frozen.Snapshot}, Preview: &preview}).Copy.Stale {
		t.Fatal("the copy is not stale after a new Live")
	}
	if f.report(withoutStripe, next).Copy.Stale {
		t.Fatal("the copy is stale against what it was built from")
	}

	// Another Project whose Live runs the address-bound connection Preview selects too: its seed owns it.
	other := f.otherLiveHost()
	otherLive := liveVersion{}
	otherToken, err := other.project.canonical.CreateCredentialBrokerToken(f.ctx, "next-publish-"+uuid.NewString())
	storageRequire(t, err, "store the other Project's broker token")
	otherLive.document, _, err = other.project.canonical.WriteConfiguration(f.ctx, 0, connectorconfig.Configuration{
		Connections: []connectorconfig.ConnectionConfiguration{slackEntry(first.ConnectionID), stripeEntry("cstripelive")}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
		OperationConfigurations: []connectorconfig.OperationConfiguration{}, Environment: map[string]connectorconfig.EnvironmentValue{},
		CredentialBroker: &connectorconfig.CredentialBrokerConfiguration{Endpoint: "http://127.0.0.1:1/internal/connection-credentials/read", TokenSecretRef: otherToken},
	})
	storageRequire(t, err, "write the other Project's Live version")
	otherLive.snapshot, err = other.project.canonical.FreezeConfiguration(f.ctx, otherLive.document.Revision)
	storageRequire(t, err, "freeze the other Project's Live version")
	f.prepareOn(other, withStripe, true)
	var otherReport NextPublishReport
	storageRequire(t, json.Unmarshal(configurationCall(t, f.ctx, other, ConfigurationAdmission{Internal: true, TeamSpaceID: f.teamSpaceID, SourceCommit: withStripe,
		NextPublish: &NextPublish{Live: otherLive.ref("dep-other"), Preview: &preview}}, http.MethodPost, nextPublishPath, NextPublishRequest{ManifestDigest: f.digests[withStripe]}, 200), &otherReport), "decode the other Project's next publish")
	f.requireItems(otherReport, map[configitems.Key]itemWant{
		stripeKey():     {state: configitems.Own, refusal: appmanifest.FollowRefusedEndpointBound},
		connectionKey(): {state: configitems.FollowsPreview},
	})
	t.Logf("real MinIO + PostgreSQL + Slack v0.22.1 + Stripe v0.21.1: project=%s; seed, refusals, one of 4 first writes, own and follow per kind, Live fallback, writes against the Preview's source, copy guard and freeze, staleness",
		f.live.project.scope.ProjectID)
}

func newNextPublishFixture(t *testing.T) *nextPublishFixture {
	t.Helper()
	endpoint, bucket := os.Getenv("SUPERVERSE_STUDIO_TEST_S3_ENDPOINT"), os.Getenv("SUPERVERSE_STUDIO_TEST_S3_BUCKET")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || bucket == "" {
		t.Fatal("requires explicit real loopback MinIO endpoint and existing versioned bucket in private test environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	graph, err := os.ReadFile(filepath.Join("testdata", "slack-thread-approval-fdg.json"))
	storageRequire(t, err, "read the generated Slack example FDG")
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	storageRequire(t, err, "load private AWS configuration")
	client := s3.NewFromConfig(cfg, func(options *s3.Options) { options.BaseEndpoint = aws.String(endpoint); options.UsePathStyle = true })
	id := uuid.NewString()
	prefix := "studio-next-publish-integration/" + id
	objects, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, AllowUnencrypted: true})
	storageRequire(t, err, "verify real bucket versioning")
	t.Cleanup(func() { cleanupConfigurationStorage(t, client, bucket, prefix+"/") })
	hosts := map[string]*ConfigurationHost{}
	for _, kind := range []string{"live", "preview"} {
		host, hostErr := NewConfigurationHost(objects, connectorconfig.Scope{ProjectID: id, Kind: kind}, connectorlibrary.OfficialReleaseDownloadURL, SharedConnections{Providers: connections.NewProviderHTTPClient(true)})
		storageRequire(t, hostErr, "construct a production configuration host")
		t.Cleanup(host.Close)
		hosts[kind] = host
	}
	return &nextPublishFixture{
		t: t, ctx: ctx, live: hosts["live"], preview: hosts["preview"], objects: objects, client: client, bucket: bucket, prefix: prefix, teamSpaceID: "ts" + flowid.RandomID(10), graph: graph,
		definitionRevisions: map[string]string{}, digests: map[string]string{},
	}
}

// manifest is the version's AppManifest: the Slack connection with both
// bindings, with or without the Stripe connection, and five variables, one a
// secret.
func (f *nextPublishFixture) manifest(withStripe bool) []byte {
	empty := ""
	environment := []any{
		map[string]any{"name": "APP_KEPT"}, map[string]any{"name": "APP_OTHER"}, map[string]any{"name": "APP_NEW"}, map[string]any{"name": "APP_LIVE_ONLY"},
		map[string]any{"name": "SIGNING_SECRET", "secret": true, "required": true},
	}
	connectors := []any{map[string]any{
		"connectionName": slackConnection, "connectorId": connections.SlackConnectorID, "authMethodId": &empty, "modulePath": slackModule, "version": "v0.22.1",
		"operations": []string{"listThreadMessages", "postThreadReply"},
		"triggerBindings": []any{
			map[string]string{"triggerName": "channelThreadCreated", "bindingName": slackStartKey},
			map[string]string{"triggerName": "threadReplyCreated", "bindingName": slackReplyKey},
		},
	}}
	if withStripe {
		connectors = append(connectors, map[string]any{
			"connectionName": stripeConnection, "connectorId": "stripe", "authMethodId": &empty, "modulePath": stripeModule, "version": "v0.21.1",
			"operations": []string{"createACHCheckoutSession"},
		})
	}
	return comm.Must(json.Marshal(map[string]any{
		"schemaVersion": "superverse.dev/dex-app/v1", "application": map[string]any{"port": 8080, "healthPath": "/healthz", "environment": environment},
		"flowDefinitions": []any{map[string]any{"sourcePath": "flow/workflow.go"}}, "connectors": connectors,
	}))
}

func (f *nextPublishFixture) prepareOn(host *ConfigurationHost, commit string, withStripe bool) {
	f.t.Helper()
	manifest := f.manifest(withStripe)
	f.digests[commit] = objectstore.Digest(manifest)
	registration := map[string]any{"expectedRevision": 0, "sourceCommit": commit, "manifestDigest": f.digests[commit], "manifest": string(manifest), "definitions": []json.RawMessage{f.graph}}
	var prepared struct {
		DefinitionRevision    string `json:"definitionRevision"`
		ConfigurationRevision uint64 `json:"configurationRevision"`
	}
	storageRequire(f.t, json.Unmarshal(configurationCall(f.t, f.ctx, host, ConfigurationAdmission{Internal: true}, http.MethodPut, "/api/v2/project-configuration/sources", registration, 200), &prepared), "decode the prepared source")
	if prepared.ConfigurationRevision != 1 {
		f.t.Fatalf("the preparation initialized revision %d of the configuration it edits", prepared.ConfigurationRevision)
	}
	f.definitionRevisions[commit] = prepared.DefinitionRevision
}

func (f *nextPublishFixture) prepare(commit string, withStripe bool) {
	f.prepareOn(f.live, commit, withStripe)
}

// otherLiveHost is the production Live host of another Project in the same
// bucket, with the fixture's TeamSpace connections.
func (f *nextPublishFixture) otherLiveHost() *ConfigurationHost {
	f.t.Helper()
	host, err := NewConfigurationHost(f.objects, connectorconfig.Scope{ProjectID: uuid.NewString(), Kind: "live"}, connectorlibrary.OfficialReleaseDownloadURL, f.live.shared)
	storageRequire(f.t, err, "construct another Project's Live host")
	f.t.Cleanup(host.Close)
	return host
}

func (f *nextPublishFixture) preparePreview(commit string) { f.prepareOn(f.preview, commit, false) }

// liveVersion is a version frozen in the Live scope's canonical head.
type liveVersion struct {
	snapshot connectorconfig.SnapshotRef
	document connectorconfig.Configuration
}

func (version liveVersion) ref(deploymentID string) LiveConfiguration {
	return LiveConfiguration{DeploymentID: deploymentID, Snapshot: &version.snapshot}
}

// publish freezes a Live version in the canonical head through the Connector
// SDK store, as a build's freeze writes it.
func (f *nextPublishFixture) publish(fill func(*connectorconfig.Configuration)) liveVersion {
	f.t.Helper()
	current, _, err := f.live.project.canonical.ReadConfiguration(f.ctx)
	if err != nil && !errors.Is(err, connectorconfig.ErrObjectNotFound) {
		storageRequire(f.t, err, "read the canonical head")
	}
	document := connectorconfig.Configuration{
		Connections: []connectorconfig.ConnectionConfiguration{}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
		OperationConfigurations: []connectorconfig.OperationConfiguration{}, Environment: map[string]connectorconfig.EnvironmentValue{},
	}
	fill(&document)
	written, _, err := f.live.project.canonical.WriteConfiguration(f.ctx, current.Revision, document)
	storageRequire(f.t, err, "write the Live version")
	snapshot, err := f.live.project.canonical.FreezeConfiguration(f.ctx, written.Revision)
	storageRequire(f.t, err, "freeze the Live version")
	return liveVersion{snapshot: snapshot, document: written}
}

func (f *nextPublishFixture) liveSecret(name, secret string) connectorconfig.EnvironmentValue {
	f.t.Helper()
	reference, err := f.live.project.canonical.CreateApplicationSecret(f.ctx, name, secret)
	storageRequire(f.t, err, "store the Live secret")
	return connectorconfig.EnvironmentValue{SecretRef: &reference}
}

func (f *nextPublishFixture) liveBroker() *connectorconfig.CredentialBrokerConfiguration {
	f.t.Helper()
	token, err := f.live.project.canonical.CreateCredentialBrokerToken(f.ctx, "next-publish-"+uuid.NewString())
	storageRequire(f.t, err, "store Live's broker token")
	return &connectorconfig.CredentialBrokerConfiguration{Endpoint: "http://127.0.0.1:1/internal/connection-credentials/read", TokenSecretRef: token}
}

// previewApplied is the version the Preview applied at revision, as the
// platform hands it to the Live host: without its secrets and its broker.
func previewApplied(connectionID string, revision uint64) connectorconfig.Configuration {
	return connectorconfig.Configuration{
		Revision: revision, Connections: []connectorconfig.ConnectionConfiguration{slackEntry(connectionID), stripeEntry("cstripelive")},
		TriggerBindings:         []connectorconfig.TriggerConfiguration{binding(slackStartKey, "C9"), {ConnectorID: connections.SlackConnectorID, ConnectionName: slackConnection, TriggerName: "threadReplyCreated", BindingName: slackReplyKey, Configuration: comm.Must(json.Marshal(replyConfiguration("C9")))}},
		OperationConfigurations: []connectorconfig.OperationConfiguration{},
		Environment:             map[string]connectorconfig.EnvironmentValue{"APP_KEPT": value("preview"), "APP_OTHER": value("same"), "APP_NEW": value("from-preview")},
		CredentialBroker:        &connectorconfig.CredentialBrokerConfiguration{Endpoint: "http://127.0.0.1:2/preview", TokenSecretRef: connectorconfig.ApplicationSecretRef{Key: "app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/preview", Version: "v1", Digest: "sha256:" + strings.Repeat("1", 64)}},
	}
}

func value(text string) connectorconfig.EnvironmentValue {
	return connectorconfig.EnvironmentValue{Value: &text}
}

func slackEntry(connectionID string) connectorconfig.ConnectionConfiguration {
	return connectorconfig.ConnectionConfiguration{ConnectorID: connections.SlackConnectorID, ConnectionName: slackConnection, ModulePath: slackModule, Provider: "slack",
		SharedConnectionID: connectionID, Configuration: json.RawMessage(`{}`)}
}

func stripeEntry(connectionID string) connectorconfig.ConnectionConfiguration {
	return connectorconfig.ConnectionConfiguration{ConnectorID: "stripe", ConnectionName: stripeConnection, ModulePath: stripeModule, Provider: "stripe",
		SharedConnectionID: connectionID, Configuration: json.RawMessage(`{}`)}
}

func binding(name, channel string) connectorconfig.TriggerConfiguration {
	return connectorconfig.TriggerConfiguration{ConnectorID: connections.SlackConnectorID, ConnectionName: slackConnection, TriggerName: "channelThreadCreated", BindingName: name,
		Configuration: comm.Must(json.Marshal(map[string]any{"channelId": channel, "threadTriggerMatcher": map[string]any{"messageContains": "request approval"}}))}
}

func replyConfiguration(channel string) map[string]any {
	return map[string]any{"channelId": channel, "threadReplyMatcher": map[string]any{"messageContains": "approve", "posterUserIds": []string{"U2"}}}
}

func (f *nextPublishFixture) call(commit string, next *NextPublish, path string, input any, want int) []byte {
	f.t.Helper()
	return configurationCall(f.t, f.ctx, f.live, ConfigurationAdmission{Internal: true, TeamSpaceID: f.teamSpaceID, SourceCommit: commit, NextPublish: next}, http.MethodPost, path, input, want)
}

func (f *nextPublishFixture) report(commit string, next *NextPublish) NextPublishReport {
	f.t.Helper()
	var report NextPublishReport
	storageRequire(f.t, json.Unmarshal(f.call(commit, next, nextPublishPath, NextPublishRequest{ManifestDigest: f.digests[commit]}, 200), &report), "decode the next publish")
	return report
}

func (f *nextPublishFixture) copy(commit string, next *NextPublish, layerRevision, previewRevision uint64) CopyReport {
	f.t.Helper()
	var report CopyReport
	storageRequire(f.t, json.Unmarshal(f.call(commit, next, copiesPath, CopyRequest{ManifestDigest: f.digests[commit], ExpectedLayerRevision: layerRevision, ExpectedPreviewRevision: previewRevision}, 200), &report), "decode the copy")
	return report
}

func (f *nextPublishFixture) actorAdmission(commit string, next *NextPublish, revision uint64) ConfigurationAdmission {
	return ConfigurationAdmission{ActorID: "next-publish-builder", TeamSpaceID: f.teamSpaceID, SourceCommit: commit, DefinitionRevision: f.definitionRevisions[commit],
		AppManifestRevision: 1, ConfigurationRevision: revision, NextPublish: next}
}

// actor sends a person's Setup request to the layer, checked against
// commit's source, at the layer revision the form was read at.
func (f *nextPublishFixture) actor(commit string, next *NextPublish, revision uint64, method, path string, input any, want int) []byte {
	f.t.Helper()
	return configurationCall(f.t, f.ctx, f.live, f.actorAdmission(commit, next, revision), method, path, input, want)
}

// saved is the layer revision a connector save answered.
func (f *nextPublishFixture) saved(answer []byte) uint64 {
	f.t.Helper()
	var saved struct {
		ConfigurationRevision string `json:"configurationRevision"`
	}
	storageRequire(f.t, json.Unmarshal(answer, &saved), "decode the save")
	revision, err := strconv.ParseUint(saved.ConfigurationRevision, 10, 64)
	storageRequire(f.t, err, "parse the saved revision")
	return revision
}

func (f *nextPublishFixture) requireCode(answer []byte, want string) {
	f.t.Helper()
	var coded struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(answer, &coded) != nil || coded.Code != want {
		f.t.Fatalf("the refusal has code %q, want %s", coded.Code, want)
	}
}

func (f *nextPublishFixture) layerKey() string {
	return f.live.project.prefix + "/next-publish/configuration/head"
}

func (f *nextPublishFixture) layer() connectorconfig.Configuration {
	f.t.Helper()
	document, _, err := f.live.project.nextPublishLayer().ReadConfiguration(f.ctx)
	storageRequire(f.t, err, "read the layer")
	return document
}

func (f *nextPublishFixture) copied(commit string) connectorconfig.Configuration {
	f.t.Helper()
	document, _, err := f.live.project.commitDraft(commit).ReadConfiguration(f.ctx)
	storageRequire(f.t, err, "read the copy")
	return document
}

// itemWant is what an item must read.
type itemWant struct {
	state                               configitems.State
	refusal                             appmanifest.FollowRefusal
	differsFromLive, differsFromPreview bool
}

func (f *nextPublishFixture) requireItems(report NextPublishReport, want map[configitems.Key]itemWant) {
	f.t.Helper()
	for key, expected := range want {
		item := findItem(f.t, report.Items, key)
		if item.State != expected.state || item.FollowRefusal != expected.refusal || item.DiffersFromLive != expected.differsFromLive || item.DiffersFromPreview != expected.differsFromPreview {
			f.t.Fatalf("item %+v is %s refused %q differsFromLive=%t differsFromPreview=%t, want %+v", key, item.State, item.FollowRefusal, item.DiffersFromLive, item.DiffersFromPreview, expected)
		}
	}
}

func (f *nextPublishFixture) requireRefusedGap(report NextPublishReport, refusal appmanifest.FollowRefusal, matches func(appmanifest.RequirementGap) bool) {
	f.t.Helper()
	if !slices.ContainsFunc(report.Gaps, func(gap appmanifest.RequirementGap) bool { return matches(gap) && gap.FollowRefusal == refusal }) {
		f.t.Fatalf("no gap carries the refusal %s: %+v", refusal, report.Gaps)
	}
}

func connectionEntry(document connectorconfig.Configuration, name string) *connectorconfig.ConnectionConfiguration {
	for index := range document.Connections {
		if document.Connections[index].ConnectionName == name {
			return &document.Connections[index]
		}
	}
	return nil
}

func findItem(t *testing.T, items []configitems.Item, key configitems.Key) configitems.Item {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("no item %+v", key)
	return configitems.Item{}
}

func selectionPath(connectorID, connectionName string) string {
	return "/api/v2/connector-selections/" + connectorID + "/" + connectionName
}

func bindingPath(name string) string {
	trigger := "channelThreadCreated"
	if name == slackReplyKey {
		trigger = "threadReplyCreated"
	}
	return "/api/v2/connector-trigger-bindings/slack/" + slackConnection + "/" + trigger + "/" + name
}

func stepPath() string {
	return "/api/v2/connector-use-configurations/slack/" + slackConnection + "/postThreadReply/Flow/PostSlackCompletion"
}

func environmentKey(name string) configitems.Key {
	return configitems.Key{Kind: configitems.KindEnvironment, EnvironmentName: name}
}

func connectionKey() configitems.Key {
	return configitems.ConnectionKey(connections.SlackConnectorID, slackConnection)
}

func stripeKey() configitems.Key { return configitems.ConnectionKey("stripe", stripeConnection) }

func stepKey() configitems.Key {
	return configitems.Key{Kind: configitems.KindStepSetting, ConnectorID: connections.SlackConnectorID, ConnectionName: slackConnection, OperationID: "postThreadReply", FlowType: "Flow", StepType: "PostSlackCompletion"}
}

func bindingKey(name string) configitems.Key {
	trigger := "channelThreadCreated"
	if name == slackReplyKey {
		trigger = "threadReplyCreated"
	}
	return configitems.Key{Kind: configitems.KindTriggerBinding, ConnectorID: connections.SlackConnectorID, ConnectionName: slackConnection, TriggerName: trigger, BindingName: name}
}
