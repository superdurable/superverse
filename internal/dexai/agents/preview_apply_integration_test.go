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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// These scenarios run Apply to Preview on the Session of a throwaway READY
// Project, on the lane's real Dex, MinIO, PostgreSQL, local Git host and
// Sandbox; only the model is the mock. A start that applies freezes exactly
// the configuration revision its sender saw; every other start, the automatic
// deployment of a push and a menu Start, revalidates the version the last
// Apply froze, which a stop keeps. Each start is recorded in RecentStarts.

// summaryModelConnection is the template's one declared connection.
const summaryModelConnection = "summary-model"

// appliedPreviewSession is a mock Session whose Preview prepared the Session's
// HEAD and whose configuration selects summaryConnection, a TeamSpace
// connection the test created, so the test can change its settings.
type appliedPreviewSession struct {
	*mockSession
	configuration     *projectconfig.Configuration
	summaryConnection string
}

// newAppliedPreviewSession starts a mock Session as newConfiguredPreviewSession
// does, but seeds the summary model's TeamSpace connection itself.
func newAppliedPreviewSession(t *testing.T, ctx context.Context) (*appliedPreviewSession, agentloop.AgentSnapshot) {
	t.Helper()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	session.preparePreview(ctx)
	session.previews.Flow.Preview = preview.NewPreviewConfiguration(nil, nil, artifactStore(ctx, t), nil, nil, nil, nil)
	configuration := &projectconfig.Configuration{Projects: session.projects, Storage: laneStorage(t), Preview: session.service(), ReleaseCandidates: laneReleaseCandidates(t),
		Shared: laneTeamSpaceConnections(ctx, t, session.projects)}
	t.Cleanup(configuration.Close)
	target := &pb.FlowApiTarget{ProjectId: session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW}
	list, _ := session.configurationRevision(ctx, configuration, target)
	var declared *struct {
		ConnectorID    string `json:"connectorId"`
		ConnectionName string `json:"connectionName"`
		ModulePath     string `json:"modulePath"`
		ModuleVersion  string `json:"moduleVersion"`
		AuthMethodID   string `json:"authMethodId"`
	}
	for i := range list.Connections {
		if list.Connections[i].ConnectionName == summaryModelConnection {
			declared = &list.Connections[i]
		}
	}
	if declared == nil {
		t.Fatalf("the template declares no %s connection", summaryModelConnection)
	}
	settings := map[string]json.RawMessage{"provider": comm.Must(json.Marshal(openAIProvider)), "model": comm.Must(json.Marshal(openAIPreviewModel))}
	credentials := map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(openAIKey(t)))}
	connectionID := session.createConnection(ctx, configuration, declared.ConnectionName, declared.ConnectorID, declared.ModulePath, declared.ModuleVersion, declared.AuthMethodID, settings, credentials)
	session.seeded = map[string]string{summaryModelConnection: connectionID}
	session.configureConnections(ctx, configuration, target)
	return &appliedPreviewSession{mockSession: session, configuration: configuration, summaryConnection: connectionID}, agent
}

// projectStorage is the Project's Live storage, which holds its Preview
// configuration too.
func (s *appliedPreviewSession) projectStorage(ctx context.Context) infra.StorageRefs {
	s.t.Helper()
	project, err := s.projects.Read(ctx, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Project was not read (%T)", err)
	}
	return project.Storage
}

// headRevision is the revision of the Preview's configuration head.
func (s *appliedPreviewSession) headRevision(ctx context.Context) uint64 {
	s.t.Helper()
	_, revision := s.configurationRevision(ctx, s.configuration, &pb.FlowApiTarget{ProjectId: s.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW})
	return revision
}

// previewItems is the Preview's item diff as GetProjectConfiguration reports it.
func (s *appliedPreviewSession) previewItems(ctx context.Context) confighost.PreviewItemsReport {
	s.t.Helper()
	report, err := s.configuration.PreviewItems(ctx, s.account, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Preview items were not read (%T)", err)
	}
	return report
}

// changeModel edits the settings of the summary model's TeamSpace connection,
// as the TeamSpace's Connections page does; the Preview's head selects it, so
// an Apply would freeze the new settings.
func (s *appliedPreviewSession) changeModel(ctx context.Context, model string) {
	s.t.Helper()
	shared := s.configuration.Shared.Connections
	row, _, _, err := shared.Connection(ctx, s.teamSpaceID, s.summaryConnection)
	if err != nil {
		s.t.Fatalf("the summary model's connection was not read (%T)", err)
	}
	settings := map[string]json.RawMessage{"provider": comm.Must(json.Marshal(openAIProvider)), "model": comm.Must(json.Marshal(model))}
	if _, err = shared.Update(ctx, connections.ConnectionUpdate{TeamSpaceID: s.teamSpaceID, ConnectionID: s.summaryConnection, ActorAccountID: s.account,
		ExpectedRevision: row.Revision, Settings: settings}); err != nil {
		s.t.Fatalf("the summary model's settings were not changed (%T)", err)
	}
}

// sendStart sends a start-preview through the Session Service, as Studio's
// Apply (apply, at revision) or Start does, on the Preview it reads now.
func (s *mockSession) sendStart(ctx context.Context, apply bool, revision uint64) (preview.Command, error) {
	s.t.Helper()
	current, err := s.previews.Get(ctx, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Preview read failed (%T)", err)
	}
	start := preview.Command{ID: "start-" + uuid.NewString(), Kind: preview.CommandStartPreview, ActorAccountID: s.account, ApplyConfiguration: apply, ConfigurationRevision: revision,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	_, err = s.service().PreviewCommand(ctx, s.account, s.identity.ProjectID, start)
	return start, err
}

// start sends a start and waits for its end.
func (s *mockSession) start(ctx context.Context, apply bool, revision uint64) (preview.Command, preview.Snapshot) {
	s.t.Helper()
	start, err := s.sendStart(ctx, apply, revision)
	if err != nil {
		// The engine's answer names no configuration value.
		s.t.Fatalf("the Preview start was not admitted (%T): %v", err, err)
	}
	return start, s.awaitPreview(ctx, start.ID)
}

// startRecord is the record of commandID, or fails the test.
func startRecord(t *testing.T, state preview.Snapshot, commandID string) preview.StartRecord {
	t.Helper()
	var found []preview.StartRecord
	for _, record := range state.RecentStarts {
		if record.CommandID == commandID {
			found = append(found, record)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Preview records %d starts of %s, want one", len(found), commandID)
	}
	return found[0]
}

// TestPreviewApplyDexIntegration applies the Preview's configuration and
// checks what a later start runs with:
//   - before the first Apply every head item is ADDED; an Apply records the
//     applied version, its sender and the root commit's version V0, after which
//     nothing is unapplied, and the same command ID with another flag is
//     refused as a conflict;
//   - a TeamSpace settings change counts as one unapplied change, and an Apply
//     of a revision older than the head is refused PREVIEW_CONFIGURATION_CHANGED;
//   - the automatic deployment of a push runs the applied version, V1, not the
//     head;
//   - the next Apply freezes the settings refresh as a new head revision, and
//     the same freeze again, the retry of a lost answer, recognizes that head;
//   - the applied version survives a stop, and a menu Start runs it;
//   - RecentStarts keeps the newest 50 starts.
func TestPreviewApplyDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout+6*previewCommandTimeout)
	defer cancel()
	session, first := newAppliedPreviewSession(t, ctx)
	project := session.identity.ProjectID
	root := session.summary(ctx).Identity.HeadCommit

	if items := session.previewItems(ctx); items.UnappliedChangeCount == 0 || slices.ContainsFunc(items.Items, func(item configitems.Item) bool { return item.State != configitems.Added }) {
		t.Fatalf("before the first Apply every item must be ADDED: %+v", items)
	}
	applied := session.headRevision(ctx)
	apply, state := session.start(ctx, true, applied)
	record := startRecord(t, state, apply.ID)
	switch {
	case state.Preview.Status != preview.PreviewReady:
		t.Fatalf("the Apply ended %s (%s)", state.Preview.Status, failureCode(state))
	case state.Applied.CommandID != apply.ID || state.Applied.ActorAccountID != session.account || state.Applied.SourceCommit != root || state.Applied.ConfigurationRevision != applied:
		t.Fatalf("the Apply recorded %+v, want %s by the owner of %s at revision %d", state.Applied, apply.ID, root, applied)
	case state.Configuration.ObjectVersion != state.Applied.ObjectVersion || state.Preview.Version.Number != 0 || state.Preview.Version.Subject == "":
		t.Fatalf("the Preview runs %s, not the applied version, as %+v", state.Configuration.ObjectVersion, state.Preview.Version)
	case record.Outcome != preview.StartReady || !record.Applied || record.ActorAccountID != session.account || record.Commit != root || record.Version.Number != 0 || len(record.Timings) == 0 || record.EndedAt.IsZero():
		t.Fatalf("the Apply's record is %+v", record)
	}
	if items := session.previewItems(ctx); items.UnappliedChangeCount != 0 {
		t.Fatalf("after the Apply %d changes are unapplied", items.UnappliedChangeCount)
	}
	conflicting := apply
	conflicting.ApplyConfiguration, conflicting.ConfigurationRevision = false, 0
	conflicting.ExpectedRevision, conflicting.WorkspaceRequestID, conflicting.Generation = state.Revision, state.WorkspaceRequestID, state.Generation
	if _, err := session.service().PreviewCommand(ctx, session.account, project, conflicting); !errors.Is(err, preview.ErrConflict) {
		t.Fatalf("the same command ID without Apply must be a conflict (%T)", err)
	}

	session.changeModel(ctx, "gpt-5-nano")
	items := session.previewItems(ctx)
	changed := slices.IndexFunc(items.Items, func(item configitems.Item) bool { return item.State == configitems.Changed })
	if items.UnappliedChangeCount != 1 || changed < 0 || items.Items[changed].Kind != configitems.KindConnection || items.Items[changed].ConnectionName != summaryModelConnection {
		t.Fatalf("the settings change must be the one unapplied change: %+v", items)
	}
	stale, refused := session.start(ctx, true, applied-1)
	if refusal := startRecord(t, refused, stale.ID); refusal.Outcome != preview.StartRefused || refusal.FailureCode != "PREVIEW_CONFIGURATION_CHANGED" ||
		refused.Failure == nil || refused.Failure.Code != "PREVIEW_CONFIGURATION_CHANGED" || refused.Applied != state.Applied {
		t.Fatalf("the Apply of an older revision was not refused PREVIEW_CONFIGURATION_CHANGED: %+v (%s)", refusal, failureCode(refused))
	}
	// Refused before it pinned anything, the Apply left the serving Preview as it was.
	if refused.LastCommand.ID != stale.ID || refused.Preview.URL != state.Preview.URL || refused.Configuration.ObjectVersion != state.Configuration.ObjectVersion {
		t.Fatalf("the refused Apply changed the serving Preview: %s after %s", refused.Preview.URL, refused.LastCommand.ID)
	}

	note := "apply-" + time.Now().UTC().Format("20060102150405")
	session.send(ctx, mockInput(t, "preview-autodeploy", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	agent := session.finishedTurnAfter(ctx, first.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the note was not pushed: %+v", pushed)
	}
	head := session.summary(ctx).Identity.HeadCommit
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	deployment := startRecord(t, deployed, deployed.Deployment.Last.CommandID)
	switch {
	case deployed.Deployment.Last.Status != preview.DeploymentReady || deployed.Preview.SourceCommit != head:
		t.Fatalf("the push %s did not deploy: %s (%s)", head, deployed.Deployment.Last.Status, failureCode(deployed))
	case deployed.Configuration.ObjectVersion != state.Applied.ObjectVersion || deployed.Applied != state.Applied:
		t.Fatalf("the deployment runs %s, not the applied version %s", deployed.Configuration.ObjectVersion, state.Applied.ObjectVersion)
	case deployed.Preview.Version.Number != 1 || deployment.Version.Number != 1 || deployment.Applied || deployment.ActorAccountID != "" || deployment.Outcome != preview.StartReady:
		t.Fatalf("the deployment of the first push must run V1 without applying: %+v, record %+v", deployed.Preview.Version, deployment)
	}
	if items = session.previewItems(ctx); items.UnappliedChangeCount != 1 {
		t.Fatalf("the deployment applied the settings change: %d unapplied", items.UnappliedChangeCount)
	}

	reapply, reapplied := session.start(ctx, true, applied)
	if reapplied.Preview.Status != preview.PreviewReady || reapplied.Applied.CommandID != reapply.ID || reapplied.Applied.ConfigurationRevision != applied+1 {
		t.Fatalf("the Apply of the settings change must freeze the refreshed head %d: %+v (%s)", applied+1, reapplied.Applied, failureCode(reapplied))
	}
	if items = session.previewItems(ctx); items.UnappliedChangeCount != 0 {
		t.Fatalf("after the second Apply %d changes are unapplied", items.UnappliedChangeCount)
	}
	// The same freeze again, as an attempt retried after its answer was
	// lost, finds the refresh it recorded and freezes that head; an older
	// revision does not.
	scope := infra.ScopeRef{ProjectID: project, Scope: infra.PreviewKeysOf(project).Scope, Storage: session.projectStorage(ctx)}
	metadata, err := session.configuration.ReadMetadata(ctx, scope, head)
	if err != nil {
		t.Fatalf("the prepared source's metadata was not read (%T)", err)
	}
	metadata.ConfigurationRevision = applied
	if retried, retryErr := session.configuration.ValidatePreparedPreview(ctx, scope, reapplied.AppManifest, metadata); retryErr != nil || retried.SnapshotConfigurationRevision != applied+1 {
		t.Fatalf("the retried freeze of revision %d did not recognize its refresh: %d (%v)", applied, retried.SnapshotConfigurationRevision, retryErr)
	}
	metadata.ConfigurationRevision = applied - 1
	if _, retryErr := session.configuration.ValidatePreparedPreview(ctx, scope, reapplied.AppManifest, metadata); !errors.Is(retryErr, projectconfig.ErrRevisionChanged) {
		t.Fatalf("a freeze of an older revision must be ErrRevisionChanged: %v", retryErr)
	}

	// Live follows Preview's applied values: the next publish of the pushed
	// commit follows the TeamSpace connection the Preview applied.
	live := newLiveDelivery(session.mockSession)
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the Release of %s was not prepared: %s", head, release.PreparedSource.Status)
	}
	shown, liveItems, err := live.configuration.Get(ctx, session.account, &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: head})
	if err != nil {
		t.Fatalf("the Live items were not read (%T)", err)
	}
	followed := slices.IndexFunc(liveItems, func(item configitems.Item) bool {
		return item.Kind == configitems.KindConnection && item.ConnectionName == summaryModelConnection
	})
	var value struct {
		TeamSpaceConnectionID string `json:"teamSpaceConnectionId"`
	}
	if followed < 0 || liveItems[followed].State != configitems.FollowsPreview || json.Unmarshal(liveItems[followed].PreviewValueJSON, &value) != nil ||
		value.TeamSpaceConnectionID != session.summaryConnection || shown.GetPreviewConfigurationRevision() != reapplied.Applied.ConfigurationRevision {
		t.Fatalf("Live does not follow the applied connection at Preview revision %d: %+v", shown.GetPreviewConfigurationRevision(), liveItems)
	}
	// The applied document's credential broker, its scope-bound token, never
	// crosses into Live: the copy a publish builds selects the connection again
	// with Live's own broker.
	session.requireNoPreviewSecretsInLive(ctx, live.configuration, head, shown, reapplied.Applied)

	session.stopPreview(ctx)
	stopped, err := session.previews.Get(ctx, project)
	if err != nil || stopped.Applied != reapplied.Applied || stopped.Configuration.ObjectKey != "" {
		t.Fatalf("the stop did not keep the applied version: %+v (%v)", stopped.Applied, err)
	}
	menu, started := session.start(ctx, false, 0)
	if started.Preview.Status != preview.PreviewReady || started.Configuration.ObjectVersion != reapplied.Applied.ObjectVersion || startRecord(t, started, menu.ID).Applied {
		t.Fatalf("the menu Start after the stop did not run the applied version: %s %s (%s)", started.Preview.Status, started.Configuration.ObjectVersion, failureCode(started))
	}

	// The timeline's data source lists the starts oldest first, the Apply and the menu Start with
	// their own outcome, actor and version.
	starts, err := session.service().ReadRecentPreviewStarts(ctx, project)
	if err != nil {
		t.Fatalf("the recent Preview starts were not read (%T)", err)
	}
	var applyAt, menuAt = -1, -1
	for index, record := range starts {
		if index > 0 && record.AdmittedAt.Before(starts[index-1].AdmittedAt) {
			t.Fatalf("the recent starts are not oldest first at %d: %v before %v", index, record.AdmittedAt, starts[index-1].AdmittedAt)
		}
		switch record.CommandID {
		case apply.ID:
			applyAt = index
		case menu.ID:
			menuAt = index
		}
	}
	if applyAt < 0 || menuAt <= applyAt {
		t.Fatalf("the Apply is at %d and the menu Start at %d, want the Apply first", applyAt, menuAt)
	}
	if a, m := starts[applyAt], starts[menuAt]; !a.Applied || m.Applied || a.Outcome != preview.StartReady || m.Outcome != preview.StartReady ||
		a.ActorAccountID != session.account || m.ActorAccountID != session.account || a.Version.Subject == "" || m.Version.Subject == "" || m.Version.Number < a.Version.Number {
		t.Fatalf("the recent starts record the Apply %+v and the menu Start %+v", a, m)
	}

	// Refused starts are recorded as cheaply as any: 50 more drop the oldest.
	oldest := started.RecentStarts[0].CommandID
	var last preview.Snapshot
	for range 50 {
		_, last = session.start(ctx, true, applied-1)
	}
	if len(last.RecentStarts) != 50 || slices.ContainsFunc(last.RecentStarts, func(record preview.StartRecord) bool { return record.CommandID == oldest }) {
		t.Fatalf("RecentStarts holds %d records and still %s", len(last.RecentStarts), oldest)
	}
	session.requireDeletedConnectionReported(ctx)
	session.stopPreview(ctx)
	t.Logf("FlowID=%s applied revision %d, deployed %s on it, applied the refreshed revision %d and kept it across a stop", session.id, applied, head, applied+1)
}

// TestPreviewApplyWorkerRestartDexIntegration replaces the lane's server while
// an Apply runs. The start completes once after the restart, with exactly one
// record. Run it alone: the restart replaces the Worker every other test uses.
func TestPreviewApplyWorkerRestartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+3*previewCommandTimeout)
	defer cancel()
	session, _ := newAppliedPreviewSession(t, ctx)
	project := session.identity.ProjectID
	apply, err := session.sendStart(ctx, true, session.headRevision(ctx))
	if err != nil {
		t.Fatalf("the Apply was not admitted (%T)", err)
	}
	session.await(ctx, "the Apply's configuration", func() bool {
		state, readErr := session.previews.Get(ctx, project)
		return readErr == nil && (state.Applied.CommandID == apply.ID || state.ActiveCommand.ID != apply.ID)
	})
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, readErr := session.previews.Get(ctx, project)
		return readErr
	})
	state := session.awaitPreview(ctx, apply.ID)
	if record := startRecord(t, state, apply.ID); state.Preview.Status != preview.PreviewReady || record.Outcome != preview.StartReady || state.Applied.CommandID != apply.ID {
		t.Fatalf("the Apply across the restart ended %s with %+v (%s)", state.Preview.Status, record, failureCode(state))
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s: %s completed once across the server restart", session.id, apply.ID)
}

// requireNoPreviewSecretsInLive builds the frozen copy of head from Live's
// next-publish layer at the revisions shown, as publishing does, then reads
// the Preview's applied document and the copy from the Project's storage and
// requires that the copy selects the followed connection with Live's own
// credential broker, never the applied document's, and holds no secret
// environment reference of Preview.
func (s *appliedPreviewSession) requireNoPreviewSecretsInLive(ctx context.Context, configuration *projectconfig.Configuration, head string, shown *pb.ProjectConfigurationSnapshot, applied preview.AppliedConfiguration) {
	s.t.Helper()
	project, storage := s.identity.ProjectID, s.projectStorage(ctx)
	previewScope := infra.ScopeRef{ProjectID: project, Scope: infra.PreviewKeysOf(project).Scope, Storage: storage}
	data, err := configuration.Storage.ReadConfiguration(ctx, previewScope, applied.Object())
	if err != nil {
		s.t.Fatalf("the applied Preview document was not read (%T)", err)
	}
	var document connectorconfig.Configuration
	if err = json.Unmarshal(data, &document); err != nil || document.CredentialBroker == nil {
		s.t.Fatalf("the applied Preview document holds no credential broker to keep out of Live (%v)", err)
	}
	token := document.CredentialBroker.TokenSecretRef
	if _, err = configuration.DeriveLive(ctx, s.account, project, head, shown.GetNextPublishRevision(), shown.GetPreviewConfigurationRevision()); err != nil {
		s.t.Fatalf("the copy was not built (%T)", err)
	}
	liveScope := infra.ScopeRef{ProjectID: project, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: storage}
	objects, err := configuration.Storage.ConfigurationObjects(ctx, liveScope)
	if err != nil {
		s.t.Fatalf("the Live configuration objects were not opened (%T)", err)
	}
	prefix := comm.Must((connectorconfig.Scope{ProjectID: project, Kind: infra.ScopeLive.String()}).Prefix())
	object, err := objects.ReadObject(ctx, prefix+"/release-drafts/"+head+"/configuration/head", "")
	if err != nil {
		s.t.Fatalf("the Live copy was not read (%T)", err)
	}
	var copied connectorconfig.Configuration
	if err = json.Unmarshal(object.Contents, &copied); err != nil || len(copied.Connections) == 0 {
		s.t.Fatalf("the copy selects no followed connection (%v)", err)
	}
	if copied.CredentialBroker == nil || copied.CredentialBroker.TokenSecretRef == token || strings.Contains(string(object.Contents), token.Key) {
		s.t.Fatal("the Live copy does not have its own credential broker")
	}
	for name, value := range copied.Environment {
		if value.SecretRef != nil {
			s.t.Fatalf("the Live copy holds the secret reference of %s", name)
		}
	}
}

// requireDeletedConnectionReported deletes the TeamSpace connection the
// Preview's head selects and requires the Preview diff to report it CHANGED
// with CONNECTION_DELETED, and readiness to report it as a gap, so Apply stays
// disabled. Deletion refuses a connection a scope uses, and a selection
// records a usage, so the test first removes the Project's usage row in the
// lane's PostgreSQL: the race it stands for is a deletion that passed its usage
// check just before the selection recorded its usage.
func (s *appliedPreviewSession) requireDeletedConnectionReported(ctx context.Context) {
	s.t.Helper()
	if _, err := postgrestest.OpenForTest(s.t).ExecContext(ctx, `DELETE FROM team_space_connection_usages WHERE team_space_id = $1 AND connection_id = $2`,
		s.teamSpaceID, s.summaryConnection); err != nil {
		s.t.Fatalf("the usage row was not removed (%T)", err)
	}
	shared := s.configuration.Shared.Connections
	row, _, _, err := shared.Connection(ctx, s.teamSpaceID, s.summaryConnection)
	if err != nil {
		s.t.Fatalf("the summary model's connection was not read (%T)", err)
	}
	if err = shared.Delete(ctx, s.teamSpaceID, s.summaryConnection, row.Revision, s.account, string(projects.StatusDeleted)); err != nil {
		s.t.Fatalf("the summary model's connection was not deleted (%T)", err)
	}
	items := s.previewItems(ctx)
	deleted := slices.IndexFunc(items.Items, func(item configitems.Item) bool {
		return item.Kind == configitems.KindConnection && item.ConnectionName == summaryModelConnection
	})
	if deleted < 0 || items.Items[deleted].State != configitems.Changed || !slices.Equal(items.Items[deleted].Warnings, []configitems.Warning{configitems.ConnectionDeleted}) ||
		items.UnappliedChangeCount == 0 {
		s.t.Fatalf("the deleted connection is not reported: %+v", items)
	}
	snapshot, _, err := s.configuration.Get(ctx, s.account, &pb.ProjectConfigurationTarget{ProjectId: s.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW})
	if err != nil {
		s.t.Fatalf("the Preview configuration was not read (%T)", err)
	}
	if snapshot.GetStatus() == pb.ConfigurationStatus_CONFIGURATION_STATUS_READY || !slices.ContainsFunc(snapshot.GetRequirementGaps(), func(gap *pb.ConfigurationRequirementGap) bool {
		return gap.GetConnectionName() == summaryModelConnection
	}) {
		s.t.Fatalf("readiness does not report the deleted connection as a gap: %s %v", snapshot.GetStatus(), snapshot.GetRequirementGaps())
	}
}
