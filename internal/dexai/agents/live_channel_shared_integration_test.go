// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// TestLiveChannelSharedDexIntegration checks CHANNEL_SHARED on Live's next
// publish of a version, on Projects whose Sessions push the slack-relay-trigger
// application to main. In TeamSpace A the viewer's Project drafts a binding of
// the shared platform Slack connection to a channel, and another Project of A
// publishes the same binding to Live. In TeamSpace B a Project publishes the
// same channel to Live first, which never warns A's draft. A usage row of a
// Project that cannot be read is skipped and logged, and never fails the read.
// Once A's other Project is on Live, the binding is CHANNEL_SHARED: its owner
// reads that Project's name, and a member of the viewer's Project who is not a
// member of the other one reads the warning without a name. Dex, the Sandbox,
// the Git host, the local build service, the Live runtime, MinIO and
// PostgreSQL are real; only the model is the mock.
func TestLiveChannelSharedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+3*liveSourceTimeout+2*liveBuildTimeout+2*liveDeploymentTimeout)
	defer cancel()
	content := mockInput(t, "slack-relay-trigger", 1).Content
	viewer := newMockSession(t, ctx, content)
	publisherName := "CHANNEL_SHARED publisher " + flowid.RandomID(6)
	publisher := newMockSessionIn(t, ctx, viewer.account, viewer.teamSpaceID, publisherName, content)
	other := newMockSession(t, ctx, content)
	for _, session := range []*mockSession{viewer, publisher, other} {
		requireSlackPushed(ctx, t, session)
	}
	db := postgrestest.OpenForTest(t)
	dao, objects := connectiondao.New(db), artifactStore(ctx, t)
	team, channel := "T"+strings.ToUpper(flowid.RandomID(9)), "C"+strings.ToUpper(flowid.RandomID(9))
	sharedA := seedPlatformSlackConnection(ctx, t, dao, objects, viewer.teamSpaceID, viewer.account, connectiondao.ProviderAccount{AccountID: team})
	sharedB := seedPlatformSlackConnection(ctx, t, dao, objects, other.teamSpaceID, other.account, connectiondao.ProviderAccount{AccountID: team})
	binding := triggerBindingConfiguration{connectorID: connections.SlackConnectorID, connectionName: SlackRelayConnectionForTest, triggerName: slackRelayTrigger,
		bindingName: slackRelayBinding, configuration: map[string]any{"channelId": channel}}

	// TeamSpace B's Project listens to the channel on Live.
	publishSlackLive(ctx, other, sharedB.ConnectionID, binding)

	// An unreadable Project uses TeamSpace A's connection on Live.
	unreadable := flowid.RandomID(projecttest.IDLengthForTest(t))
	if _, err := db.ExecContext(ctx, `INSERT INTO team_space_connection_usages (team_space_id, connection_id, project_id, scope, added_at) VALUES ($1, $2, $3, 'live', now())`,
		viewer.teamSpaceID, sharedA.ConnectionID, unreadable); err != nil {
		t.Fatalf("the usage of the unreadable Project was not written (%T)", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM team_space_connection_usages WHERE project_id = $1`, unreadable); err != nil {
			t.Errorf("the usage of the unreadable Project %s was not deleted (%T)", unreadable, err)
		}
	})

	// The viewer's draft binds TeamSpace A's connection to the channel.
	live := newLiveDelivery(viewer)
	var logged bytes.Buffer
	live.configuration.Logger = slog.New(slog.NewTextHandler(&logged, nil))
	head := viewer.summary(ctx).Identity.HeadCommit
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the viewer's Release of %s is %s", head, release.PreparedSource.Status)
	}
	viewer.seeded = map[string]string{SlackRelayConnectionForTest: sharedA.ConnectionID}
	viewer.bindings = []triggerBindingConfiguration{binding}
	viewer.configureConnections(ctx, live.configuration, live.target(head))
	if item := live.channelBinding(ctx, viewer.account, head); slices.Contains(item.Warnings, configitems.ChannelShared) {
		t.Fatalf("a channel only another TeamSpace's Live listens to warned the draft: %+v", item)
	}
	if !strings.Contains(logged.String(), "CHANNEL_SHARED skipped") || !strings.Contains(logged.String(), unreadable) {
		t.Fatalf("the unreadable Project %s was not logged as skipped", unreadable)
	}

	// TeamSpace A's other Project publishes the channel to Live.
	publishSlackLive(ctx, publisher, sharedA.ConnectionID, binding)
	if item := live.channelBinding(ctx, viewer.account, head); !slices.Contains(item.Warnings, configitems.ChannelShared) || item.WarningProjectName != publisherName {
		t.Fatalf("the owner of both Projects reads %+v, not CHANNEL_SHARED naming %q", item, publisherName)
	}

	// A member of the viewer's Project only reads the warning without a name.
	member := projecttest.RegisterAccountForTest(ctx, t)
	addProjectMember(ctx, t, viewer, member)
	if item := live.channelBinding(ctx, member, head); !slices.Contains(item.Warnings, configitems.ChannelShared) || item.WarningProjectName != "" {
		t.Fatalf("a member of only the viewer's Project reads %+v, not CHANNEL_SHARED without a name", item)
	}
	t.Logf("Projects %s (viewer), %s (publisher, %s) and %s (TeamSpace %s); channel %s on connections %s and %s; skipped %s",
		viewer.identity.ProjectID, publisher.identity.ProjectID, publisherName, other.identity.ProjectID, other.teamSpaceID, channel, sharedA.ConnectionID, sharedB.ConnectionID, unreadable)
}

// requireSlackPushed waits for the slack-relay-trigger turn and checks that it
// pushed its checkpoint to main.
func requireSlackPushed(ctx context.Context, t *testing.T, session *mockSession) {
	t.Helper()
	requireNoFailure(t, session.finishedTurn(ctx).History)
	_, messages := session.history(ctx)
	if pushed := lastToolResult(t, conversation.HistoryPage{Messages: messages}, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded ||
		pushed.ResultCode != agentloop.CommitBlockedForTest {
		t.Fatalf("the Slack trigger of %s was not pushed: %+v", session.id, pushed)
	}
}

// publishSlackLive publishes the Session's HEAD to Live with the Slack
// connection selected and binding saved: it prepares the Release, configures
// its Live draft, builds it with a skip reason and deploys it.
func publishSlackLive(ctx context.Context, session *mockSession, connectionID string, binding triggerBindingConfiguration) {
	session.t.Helper()
	live := newLiveDelivery(session)
	head := session.summary(ctx).Identity.HeadCommit
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		session.t.Fatalf("the source of the Release of %s is %s", head, release.PreparedSource.Status)
	}
	session.seeded = map[string]string{SlackRelayConnectionForTest: connectionID}
	session.bindings = []triggerBindingConfiguration{binding}
	session.configureConnections(ctx, live.configuration, live.target(head))
	built := live.awaitBuild(ctx, head, live.mustAdmit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), PreviewSkipReason: skipPreview}).BuildRevisionID)
	deployment := live.deployLive(ctx, head, built)
	session.t.Logf("Project %s serves %s on Live as deployment %s", session.identity.ProjectID, head, deployment.DeploymentID)
}

// channelBinding reads Live's next publish of the Release of commit for actor
// and returns the Slack trigger binding's item.
func (l *liveDelivery) channelBinding(ctx context.Context, actor, commit string) configitems.Item {
	l.session.t.Helper()
	target := &pb.ProjectConfigurationTarget{ProjectId: l.session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: commit}
	_, items, err := l.configuration.Get(ctx, actor, target)
	if err != nil {
		l.session.t.Fatalf("the Live items of %s were not read for %s: %v", commit, actor, err)
	}
	index := slices.IndexFunc(items, func(item configitems.Item) bool {
		return item.Kind == configitems.KindTriggerBinding && item.BindingName == slackRelayBinding
	})
	if index < 0 {
		l.session.t.Fatalf("Live's next publish of %s has no %s binding item", commit, slackRelayBinding)
	}
	return items[index]
}

// addProjectMember makes account a member of the Session's TeamSpace and a
// builder of its Project, as their owners do.
func addProjectMember(ctx context.Context, t *testing.T, session *mockSession, account string) {
	t.Helper()
	flow := &teamspaces.TeamSpaceFlow{}
	teamSpaces := &teamspaces.TeamSpaceFlowClient{Dex: dexflowtest.ServerWorkerClient(t, flow), Flow: flow}
	if _, err := teamSpaces.AddMember(ctx, session.teamSpaceID, teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: session.account}, AccountID: account, Role: teamspaces.RoleMember}); err != nil {
		t.Fatalf("%s did not join TeamSpace %s: %v", account, session.teamSpaceID, err)
	}
	if _, err := session.projects.AddMember(ctx, session.identity.ProjectID, projects.MemberChange{ActorAccountID: session.account, AccountID: account, Role: projects.RoleBuilder}); err != nil {
		t.Fatalf("%s did not join Project %s: %v", account, session.identity.ProjectID, err)
	}
	// The read model authorizes reads; wait until it holds both roles.
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := session.projects.Authorize(ctx, account, session.identity.ProjectID, projects.CapabilityBuild); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the read model does not authorize %s on Project %s", account, session.identity.ProjectID)
		}
		time.Sleep(time.Second)
	}
}
