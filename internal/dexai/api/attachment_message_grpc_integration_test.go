// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAttachmentMessageAdmissionDexIntegration drives the admission of
// message attachments through the actual gRPC API, with TeamSpaceFlow,
// ProjectFlow, AttachmentFlow and SessionAgentFlow on the lane's server
// Worker, its PostgreSQL and MinIO, whose presigned PUTs the test sends as a
// browser does. A TeamSpace member uploads an n8n export and an image;
// another member uploads an image; the member also declares a file it never
// uploads.
//   - CreateProject refuses, before any Flow starts, another person's
//     attachment, one not READY, one the TeamSpace does not hold, attachments
//     without a message ID, a repeated attachment and more than ten.
//   - A first message whose text matches an example but which carries the
//     export starts from the default template; its Project counts the export
//     once, and a retry returns the same Project while one that changes the
//     attachments is ALREADY_EXISTS. The same text with the image keeps the
//     match. The Session's history shows each first message's attachment.
//   - SendMessage refuses another person's attachment and one not READY as
//     not admitted, and a blank message without attachments; a message
//     without attachments is queued as before; a blank message with the image
//     is queued, and the Project counts the image once also when the message
//     is sent again.
//
// Each Project's Setup creates its repository, so the test requires
// SUPERVERSE_TEST_GITHUB_EFFECTS=true. The Projects and then the TeamSpace are
// deleted when it ends.
func TestAttachmentMessageAdmissionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	key := []byte(strings.Repeat("b", 32))
	teamSpaceFlow, projectFlow, sessionFlow := &teamspaces.TeamSpaceFlow{}, &projects.ProjectFlow{}, &sessionagent.SessionAgentFlow{}
	previewFlow, workspaceFlow := &preview.PreviewFlow{}, &sessionworkspace.SessionWorkspaceFlow{}
	objects := attachmentStore(ctx, t)
	// The client's Flow supplies the definition and the declared files' bounds; the server's Worker runs it.
	flows := attachments.NewAttachmentFlowBootstrap(objects, fileextract.NewExtractor("superverse-server"), nil, attachments.Settings{
		ImageMaxBytes: 1 << 20, DocumentMaxBytes: 20 << 20, JSONMaxBytes: 4 << 20, UploadURLValidity: attachments.DefaultUploadURLValidity,
	})
	sdk := dexflowtest.ServerWorkerClient(t, teamSpaceFlow, projectFlow, sessionFlow, previewFlow, workspaceFlow, flows.Flow())
	reader := dexflowtest.ExecutionReaderForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"))
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	sessionClient := sessionagent.NewSessionAgentFlowClientForTest(sdk, sessionFlow, reader, "")
	sessions := &agents.SessionFlowService{Projects: projectClient, Agent: sessionClient, Logger: slog.Default(), Previews: &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader},
		Workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader)}
	attachmentClient := flows.Client(sdk)
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, []string{"admin@superverse.test"}), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, ProjectIDLength: projectClient.ProjectIDLength, Projects: projectClient, TeamSpaces: teamSpaceClient,
		TeamSpaceReads: teamspacedao.New(db), Members: memberdao.New(db), Sessions: sessions, SessionAgents: sessionClient,
		SessionModels: SessionModels{Mock: true, Local: true}, ClosestExamplePick: true, Attachments: attachmentClient, Renditions: attachments.NewRenditionStore(objects, 1<<20),
		AttachmentAdmission: attachments.NewAttachmentFlowService(attachmentClient, projectClient, 10)})
	accountAPI, attachmentAPI := pb.NewAccountServiceClient(connection), pb.NewAttachmentServiceClient(connection)
	projectAPI, agentAPI, sessionAPI := pb.NewProjectServiceClient(connection), pb.NewAgentServiceClient(connection), pb.NewSessionServiceClient(connection)

	id := flowid.RandomID(10)
	bob, _ := registerNamedCaller(ctx, t, accountAPI, "admit-bob-"+id)
	alice, _ := registerNamedCaller(ctx, t, accountAPI, "admit-alice-"+id)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, bob.GetAccountId())
	if _, err := teamSpaceClient.AddMember(ctx, teamSpaceID, teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: bob.GetAccountId()}, AccountID: alice.GetAccountId(), Role: teamspaces.RoleMember}); err != nil {
		t.Fatalf("bob adds alice: %v", err)
	}
	awaitTrue(ctx, t, "the memberships in the read model", func() bool {
		owner, ownerErr := memberdao.New(db).TeamSpaceRole(ctx, teamSpaceID, bob.GetAccountId(), teamspaces.StatusDeleted)
		member, err := memberdao.New(db).TeamSpaceRole(ctx, teamSpaceID, alice.GetAccountId(), teamspaces.StatusDeleted)
		return ownerErr == nil && owner != "" && err == nil && member == teamspaces.RoleMember
	})
	upload := func(caller *pb.CallerContext, name, mediaType string, data []byte, put bool) *pb.Attachment {
		t.Helper()
		return uploadAttachment(ctx, t, attachmentAPI, caller, teamSpaceID, name, mediaType, data, put)
	}
	export := upload(bob, "lead-intake.json", "application/json", comm.Must(mockmodel.AttachmentFixtureForTest("lead-intake.json")), true)
	image := upload(bob, "order-process.png", "image/png", attachmentImage(t), true)
	others := upload(alice, "other.png", "image/png", attachmentImage(t), true)
	pending := upload(bob, "pending.png", "image/png", attachmentImage(t), false)
	if export.GetSummary().GetJsonFormat() != "n8n" || export.GetSummary().GetRedactions() != 3 {
		t.Fatalf("the export is %v", export)
	}

	text := mockmodel.WithoutMarkers(mockInput(t, "example-matched", 1).Content)
	matched, isMatch := apptemplate.ClosestExample(text)
	if !isMatch {
		t.Fatalf("this build matches no example to %q", text)
	}
	create := func(initial string, ids ...string) *pb.CreateProjectRequest {
		return &pb.CreateProjectRequest{Caller: bob, RequestId: flowid.RandomID(24), Name: t.Name(), ManagedRepository: &pb.ManagedRepositorySource{}, TeamSpaceId: teamSpaceID,
			InitialMessage: initial, MessageId: "first-" + flowid.RandomID(12), Model: pb.AgentModel_AGENT_MODEL_MOCK, InitialAttachmentIds: ids}
	}
	eleven := make([]string, 11)
	for index := range eleven {
		eleven[index] = flowid.RandomID(12)
	}
	withoutMessage := create(text, image.GetAttachmentId())
	withoutMessage.MessageId = ""
	for _, refusal := range []struct {
		what    string
		request *pb.CreateProjectRequest
		code    codes.Code
	}{
		{"another person's attachment", create(text, others.GetAttachmentId()), codes.PermissionDenied},
		{"an attachment awaiting its upload", create(text, pending.GetAttachmentId()), codes.FailedPrecondition},
		{"an attachment the TeamSpace does not hold", create(text, flowid.RandomID(12)), codes.NotFound},
		{"attachments without a message ID", withoutMessage, codes.InvalidArgument},
		{"a repeated attachment", create(text, image.GetAttachmentId(), image.GetAttachmentId()), codes.InvalidArgument},
		{"eleven attachments", create(text, eleven...), codes.InvalidArgument},
	} {
		_, err := projectAPI.CreateProject(ctx, refusal.request)
		requireCode(t, err, refusal.code, "a creation with "+refusal.what)
		projectID, _ := flowid.ProjectCreationIDs(bob.GetAccountId(), refusal.request.GetRequestId(), projectClient.ProjectIDLength)
		if _, err = projectAPI.GetProject(ctx, &pb.GetProjectRequest{Caller: bob, ProjectId: projectID}); status.Code(err) != codes.NotFound {
			t.Fatalf("a creation with %s started Project %s: %v", refusal.what, projectID, err)
		}
	}

	// The export is the behavior authority, so its Project starts from the default template.
	exported := create(text, export.GetAttachmentId())
	exportProject := createWithFirstTurn(ctx, t, projectAPI, exported, projectClient)
	for _, retry := range []struct {
		request *pb.CreateProjectRequest
		code    codes.Code
	}{
		{exported, codes.OK},
		{withChange(exported, func(r *pb.CreateProjectRequest) { r.InitialAttachmentIds = []string{image.GetAttachmentId()} }), codes.AlreadyExists},
		{withChange(exported, func(r *pb.CreateProjectRequest) { r.InitialAttachmentIds = nil }), codes.AlreadyExists},
	} {
		retried, err := projectAPI.CreateProject(ctx, retry.request)
		requireCode(t, err, retry.code, "a retry of the export's creation")
		if retry.code == codes.OK && retried.GetProjectId() != exportProject {
			t.Fatalf("the same request answered Project %s, not %s", retried.GetProjectId(), exportProject)
		}
	}
	imaged := create(text, image.GetAttachmentId())
	imageProject := createWithFirstTurn(ctx, t, projectAPI, imaged, projectClient)
	example := comm.Must(apptemplate.ExampleTemplate(matched.ExampleID))
	prepared, err := attachmentClient.GetForTest(ctx, teamSpaceID, export.GetAttachmentId())
	if err != nil {
		t.Fatal(err)
	}
	for _, started := range []struct {
		projectID, revision string
		attachment          *pb.Attachment
		totals              projects.AttachmentTotals
	}{
		{exportProject, apptemplate.Current().Revision, export, projects.AttachmentTotals{Count: 1, Bytes: prepared.Prepared.ObjectBytes}},
		{imageProject, example.Revision, image, projects.AttachmentTotals{Count: 1}},
	} {
		project := projecttest.AwaitReadyForTest(ctx, t, projectClient, started.projectID)
		if project.TemplateRevision != started.revision || project.AttachmentTotals.Count != started.totals.Count || started.totals.Bytes != 0 && project.AttachmentTotals != started.totals {
			t.Fatalf("Project %s starts from %q and counts %+v, want %q and %+v", started.projectID, project.TemplateRevision, project.AttachmentTotals, started.revision, started.totals)
		}
		firstMessage := awaitUserAttachments(ctx, t, sessionAPI, bob, started.projectID, 1)
		if shown := firstMessage[0]; shown.GetAttachmentId() != started.attachment.GetAttachmentId() || shown.GetFileName() != started.attachment.GetFileName() ||
			shown.GetKind() != started.attachment.GetKind() || shown.GetByteSize() != started.attachment.GetByteSize() {
			t.Fatalf("the first message of Project %s shows %v, not %v", started.projectID, shown, started.attachment)
		}
	}

	send := func(messageID, content string, ids ...string) error {
		_, err := agentAPI.SendMessage(ctx, &pb.SendMessageRequest{Caller: bob, ProjectId: exportProject, MessageId: messageID, Text: content, AttachmentIds: ids})
		return err
	}
	requireNotAdmitted(t, send("another-"+id, "Use this.", others.GetAttachmentId()), codes.PermissionDenied, "a message with another person's attachment")
	requireNotAdmitted(t, send("pending-"+id, "Use this.", pending.GetAttachmentId()), codes.FailedPrecondition, "a message with an attachment awaiting its upload")
	requireCode(t, send("blank-"+id, " "), codes.InvalidArgument, "a blank message without attachments")
	if err := send("plain-"+id, "A message without attachments."); err != nil {
		t.Fatalf("a message without attachments: %v", err)
	}
	for range 2 {
		if err = send("image-"+id, "", image.GetAttachmentId()); err != nil {
			t.Fatalf("a blank message with the image: %v", err)
		}
	}
	project, err := projectClient.Read(ctx, exportProject)
	if err != nil || project.AttachmentTotals.Count != 2 {
		t.Fatalf("after the image's message Project %s counts %+v (%v), want 2 attachments", exportProject, project.AttachmentTotals, err)
	}
	shown := awaitUserAttachments(ctx, t, sessionAPI, bob, exportProject, 2)
	if last := shown[len(shown)-1]; last.GetAttachmentId() != image.GetAttachmentId() || last.GetWidth() != 320 || last.GetHeight() != 180 {
		t.Fatalf("the image's message shows %v", last)
	}
	t.Logf("Projects %s (export, default template) and %s (image, %s) admitted their attachments", exportProject, imageProject, matched.ExampleID)
}

// uploadAttachment declares a file of the caller in the TeamSpace and, when
// put, sends its presigned PUT and completes it until it is READY.
func uploadAttachment(ctx context.Context, t *testing.T, api pb.AttachmentServiceClient, caller *pb.CallerContext, teamSpaceID, name, mediaType string, data []byte, put bool) *pb.Attachment {
	t.Helper()
	sum := sha256.Sum256(data)
	declared, err := api.CreateAttachmentUpload(ctx, &pb.CreateAttachmentUploadRequest{TeamSpaceId: teamSpaceID, RequestId: "upload-" + flowid.RandomID(12), FileName: name,
		MediaType: mediaType, ByteSize: int64(len(data)), Sha256: hex.EncodeToString(sum[:]), Caller: caller})
	if err != nil || declared.GetUpload() == nil {
		t.Fatalf("the upload of %s answered %v (%v)", name, declared, err)
	}
	// The attachment ends with the test instead of staying staged for a day.
	t.Cleanup(func() {
		_, err := api.DeleteAttachment(context.WithoutCancel(ctx), &pb.DeleteAttachmentRequest{TeamSpaceId: teamSpaceID, AttachmentId: declared.GetAttachment().GetAttachmentId(),
			RequestId: "cleanup-" + declared.GetAttachment().GetAttachmentId(), Caller: caller})
		if err != nil {
			t.Errorf("the deletion of attachment %s: %v", declared.GetAttachment().GetAttachmentId(), err)
		}
	})
	if !put {
		return declared.GetAttachment()
	}
	if code := putUpload(ctx, t, declared.GetUpload(), data); code != http.StatusOK {
		t.Fatalf("the presigned PUT of %s answered %d", name, code)
	}
	var ready *pb.Attachment
	awaitTrue(ctx, t, name+" READY", func() bool {
		completed, err := api.CompleteAttachmentUpload(ctx, &pb.CompleteAttachmentUploadRequest{TeamSpaceId: teamSpaceID, AttachmentId: declared.GetAttachment().GetAttachmentId(), Caller: caller})
		if err != nil || completed.GetAttachment().GetStatus() == pb.AttachmentStatus_ATTACHMENT_STATUS_FAILED {
			t.Fatalf("the completion of %s answered %v (%v)", name, completed, err)
		}
		ready = completed.GetAttachment()
		return ready.GetStatus() == pb.AttachmentStatus_ATTACHMENT_STATUS_READY
	})
	return ready
}

// awaitUserAttachments waits until the Session's history, as GetSession
// answers it, holds count user messages with attachments, and returns the
// attachments in order.
func awaitUserAttachments(ctx context.Context, t *testing.T, api pb.SessionServiceClient, caller *pb.CallerContext, projectID string, count int) []*pb.AgentAttachment {
	t.Helper()
	var shown []*pb.AgentAttachment
	awaitTrue(ctx, t, "the attachments of Project "+projectID+"'s user messages", func() bool {
		read, err := api.GetSession(ctx, &pb.GetSessionRequest{Caller: caller, ProjectId: projectID})
		shown = nil
		messages := 0
		for _, message := range read.GetSession().GetAgent().GetHistory().GetMessages() {
			if attached := message.GetMessage().GetAttachments(); message.GetMessage().GetRole() == pb.AgentMessage_ROLE_USER && len(attached) > 0 {
				shown, messages = append(shown, attached...), messages+1
			}
		}
		return err == nil && messages >= count
	})
	return shown
}

// requireNotAdmitted requires an Agent command refused with code and the
// ErrorInfo AGENT_COMMAND_NOT_ADMITTED, which tells Studio it had no effect.
func requireNotAdmitted(t *testing.T, err error, code codes.Code, what string) {
	t.Helper()
	failure := status.Convert(err)
	notAdmitted := slices.ContainsFunc(failure.Details(), func(detail any) bool {
		info, isInfo := detail.(*errdetails.ErrorInfo)
		return isInfo && info.GetReason() == "AGENT_COMMAND_NOT_ADMITTED"
	})
	if failure.Code() != code || !notAdmitted {
		t.Fatalf("%s answered %v, want %s not admitted", what, err, code)
	}
}
