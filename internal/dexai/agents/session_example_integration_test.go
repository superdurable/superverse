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
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// exampleID is the embedded example the test creates its Project from.
const exampleID = "slack-thread-approval"

// TestProjectFromExampleDexIntegration creates a Project from the embedded
// slack-thread-approval example through the lane's merged business API, with
// the lane's Dex, Worker, Git host, PostgreSQL and MinIO all real:
//   - CreateProject with an unknown example is FAILED_PRECONDITION
//     EXAMPLE_NOT_OFFERED and starts nothing; a repeat of the creation returns
//     the same Project, and the same request_id without the example is
//     ALREADY_EXISTS;
//   - the READY Project names the example and the Git tree it was created
//     from, app-template/<id>@<tree> with this build's composed tree, and its
//     AppManifest declares the Slack connection;
//   - its Session recorded the example's card as version 1 with R1 to R3 and
//     its design built at the setup commit, greets with the example's marker
//     and made no model call;
//   - the Session deployed the setup commit's Preview once, as the greeting's
//     deployment, and while it awaits the person's first message a READY
//     Preview holds the Sandbox for the idle hold only;
//   - the example-change scenario's first input revises the card to version 2
//     (R1 to R4) and pushes the change through the commit gate.
func TestProjectFromExampleDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+mockTurnTimeout)
	defer cancel()
	owner := projecttest.RegisterAccountForTest(ctx, t)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner)
	connection, caller := projecttest.LoginForTest(ctx, t, owner)
	api := pb.NewProjectServiceClient(connection)
	request := &pb.CreateProjectRequest{Caller: caller, RequestId: flowid.RandomID(24), Name: t.Name(), TeamSpaceId: teamSpaceID,
		ManagedRepository: &pb.ManagedRepositorySource{Example: &pb.ExampleReference{ExampleId: exampleID}}}
	unknown := withExample(request, &pb.ExampleReference{ExampleId: "no-such-example"})
	unknown.RequestId = flowid.RandomID(24)
	if _, err := api.CreateProject(ctx, unknown); status.Code(err) != codes.FailedPrecondition || errorReason(err) != "EXAMPLE_NOT_OFFERED" {
		t.Fatalf("an unknown example answered %v, not FAILED_PRECONDITION EXAMPLE_NOT_OFFERED", err)
	}
	var revision string
	session := newSessionOf(t, ctx, conversation.ModelMock, "", func(client *projects.ProjectFlowClient) projects.Project {
		created, createErr := api.CreateProject(ctx, request)
		if createErr != nil {
			t.Fatalf("CreateProject from %s: %v", exampleID, createErr)
		}
		if again, againErr := api.CreateProject(ctx, request); againErr != nil || again.GetProjectId() != created.GetProjectId() {
			t.Fatalf("a repeated creation answered %v, %v instead of %s", again, againErr, created.GetProjectId())
		}
		if _, changedErr := api.CreateProject(ctx, withExample(request, nil)); status.Code(changedErr) != codes.AlreadyExists {
			t.Fatalf("the same request without the example answered %v, not ALREADY_EXISTS", changedErr)
		}
		project := projecttest.AwaitReadyForTest(ctx, t, client, created.GetProjectId())
		revision = project.TemplateRevision
		return project
	})
	snapshot, err := api.GetProject(ctx, &pb.GetProjectRequest{Caller: caller, ProjectId: session.identity.ProjectID})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	started, composed := snapshot.GetProject().GetExample(), comm.Must(apptemplate.ExampleTemplate(exampleID))
	if started.GetExampleId() != exampleID || started.GetTree() != composed.TreeID || revision != composed.Revision {
		t.Fatalf("the Project names the example %+v with revision %q, not %s", started, revision, composed.Revision)
	}
	if !connectsSlack(snapshot.GetProject().GetAppManifest()) {
		t.Fatalf("the setup commit's AppManifest declares no Slack connection: %+v", snapshot.GetProject().GetAppManifest())
	}

	// The Brief is version 1 with its design built at the setup commit, the
	// greeting opens the conversation, and no model was called.
	agent, messages := session.history(ctx)
	confirmed := agent.Description.ConfirmedRequirements
	if confirmed == nil || confirmed.Revision != 1 || len(confirmed.Spec.Features) != 3 || confirmed.Spec.Design == nil {
		t.Fatalf("the example's card is not version 1 with R1 to R3 and its design: %+v", confirmed)
	}
	if !agent.Description.ConfirmedDesignBuilt || agent.Description.ConfirmedDesignBuiltCommit != session.identity.HeadCommit {
		t.Fatalf("the example's design is not built at the setup commit %s: %+v", session.identity.HeadCommit, agent.Description)
	}
	if design := session.flowDesign(ctx, flowdesign.SlotConfirmed, confirmed.Spec.Design); len(design.Flows) != 1 || design.Flows[0].FlowType != "SlackThreadApprovalFlow" {
		t.Fatalf("the recorded design does not draw SlackThreadApprovalFlow: %+v", design)
	}
	if len(messages) == 0 || messages[0].Message.Role != conversation.MessageRoleSystem || !strings.Contains(messages[0].Message.Content, "example") {
		t.Fatalf("the conversation does not open with the example's greeting: %+v", messages)
	}
	if replies := ofRole(messages, conversation.MessageRoleAssistant); len(replies) != 0 {
		t.Fatalf("the Session called the model before the person's first message: %+v", replies)
	}

	// The setup commit's Preview deploys once, as the greeting's deployment;
	// until the person's first message a READY Preview holds the Sandbox for
	// the idle hold, not the Preview hold.
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool {
		return state.Deployment.Last.Commit == session.identity.HeadCommit
	})
	if last := deployed.Deployment.Last; last.Sequence != int64(messages[0].Sequence) || last.Status == preview.DeploymentFailed {
		t.Fatalf("the setup commit's deployment is %+v, not the greeting's sequence %d", last, messages[0].Sequence)
	}
	if deployed.Deployment.Last.Status == preview.DeploymentReady {
		ws := session.workspace(ctx)
		if limit := time.Now().Add(time.Hour + 5*time.Minute); ws.SandboxDeadline.IsZero() || ws.SandboxDeadline.After(limit) {
			t.Fatalf("an unattended example Preview holds the Sandbox until %s, beyond the idle hold", ws.SandboxDeadline)
		}
	}

	// The person's first change revises the card and is pushed.
	last := messages[len(messages)-1].Sequence
	session.send(ctx, mockInput(t, "example-change", 1).Content)
	changed := session.recordedRequirements(ctx, last, 2)
	if features := changed.Description.ConfirmedRequirements.Spec.Features; len(features) != 4 || features[3].ID != "R4" {
		t.Fatalf("the change did not add R4 to R1 to R3: %+v", features)
	}
	// Version 2 is recorded before the turn builds it; the push lands when the turn ends.
	built := session.finishedTurnAfter(ctx, last)
	requireNoFailure(t, built.History)
	if got := toolResult(t, built.History.Messages, conversation.ToolCommitAndPush, 0); got.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the change's commit_and_push ended as %q (%s): %s", got.ResultCode, got.Outcome, session.toolResultContent(ctx, built.History.Messages, conversation.ToolCommitAndPush, 0))
	}
}

// TestProjectFromPreviousExampleTreeDexIntegration creates, through
// ProjectFlow, a Project of the slack-thread-approval example at the previous
// tree this build keeps for it, as a server of the previous build accepts one
// during a rollout, with the lane's Dex, Worker, Git host, PostgreSQL and
// MinIO all real:
//   - Setup resolves the previous revision and the Project is READY with it,
//     and its first commit holds that tree, which the example-previous-tree
//     scenario's git command prints;
//   - the Session records no card, since this build's card describes another
//     tree, and its session-start segment says so instead of claiming version
//     1, which the scenario's first reply requires.
func TestProjectFromPreviousExampleTreeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+mockTurnTimeout)
	defer cancel()
	previous, isKept := apptemplate.PreviousTemplateForTest(exampleID)
	if !isKept {
		t.Fatalf("this build keeps no previous tree of %s", exampleID)
	}
	owner := projecttest.RegisterAccountForTest(ctx, t)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner)
	session := newSessionOf(t, ctx, conversation.ModelMock, "", func(client *projects.ProjectFlowClient) projects.Project {
		project := projecttest.CreateAtRevisionForTest(ctx, t, client, t.Name(), owner, teamSpaceID, previous.Revision)
		if project.TemplateRevision != previous.Revision {
			t.Fatalf("the Project records the revision %q, not %s", project.TemplateRevision, previous.Revision)
		}
		return project
	})
	started := session.waitForSnapshot(ctx, "the Session's start", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.StartingExample != nil
	})
	if example := started.Description.StartingExample; example.Tree != previous.TreeID || example.Choice != agentloop.ExampleChoicePicked || started.Description.ConfirmedRequirements != nil {
		t.Fatalf("the Session did not start from the previous tree %s without a card: %+v", previous.TreeID, started.Description)
	}
	_, messages := session.history(ctx)
	last := messages[len(messages)-1].Sequence
	session.send(ctx, mockInput(t, "example-previous-tree", 1).Content)
	agent := session.finishedTurnAfter(ctx, last)
	requireNoFailure(t, agent.History)
	_, messages = session.history(ctx)
	if final := finalReply(messages); !strings.HasPrefix(final, "This Project holds the Slack thread approval example as an earlier build composed it") {
		t.Fatalf("the turn did not reach the scenario's last reply, which follows the section check: %q", final)
	}
	if tree := session.toolResultContent(ctx, messages, conversation.ToolBash, 0); !strings.Contains(tree, previous.TreeID) {
		t.Fatalf("the first commit's tree is not the previous tree %s: %s", previous.TreeID, tree)
	}
}

// withExample is request for example, or for the template without one.
func withExample(request *pb.CreateProjectRequest, example *pb.ExampleReference) *pb.CreateProjectRequest {
	changed := proto.CloneOf(request)
	changed.ManagedRepository = &pb.ManagedRepositorySource{Example: example}
	return changed
}

// errorReason is the ErrorInfo reason of a status, or "".
func errorReason(err error) string {
	for _, detail := range status.Convert(err).Details() {
		if info, isInfo := detail.(*errdetails.ErrorInfo); isInfo {
			return info.GetReason()
		}
	}
	return ""
}

// connectsSlack reports whether the AppManifest declares a Slack connection.
func connectsSlack(manifest *pb.AppManifestSnapshot) bool {
	for _, connector := range manifest.GetConnectors() {
		if connector.GetConnectorId() == "slack" {
			return true
		}
	}
	return false
}
