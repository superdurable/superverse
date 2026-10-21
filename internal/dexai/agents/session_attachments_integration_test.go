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
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// These scenarios send attachments the way a person does: the test signs in
// as the Project's owner and uploads each fixture through the lane's
// AttachmentService, with the real presigned PUT into the platform's artifact
// bucket, and completes it until its AttachmentFlow prepared it READY; then
// it sends the message, or creates the Project with it as the first message,
// through the lane's business API, which admits the attachments and reserves
// them in the Project. The Session's model calls bind each attachment to the
// Project on first use and show it; Dex, the storage, the extractor and the
// Session's tools are real, only the model is the mock.

// attachmentUploader acts for one person of a TeamSpace through the lane's
// business API.
type attachmentUploader struct {
	t           *testing.T
	connection  *grpc.ClientConn
	caller      *pb.CallerContext
	teamSpaceID string
}

// newAttachmentUploader signs in the account, which projecttest registered,
// once the read model shows it in the TeamSpace, whose members upload.
func newAttachmentUploader(ctx context.Context, t *testing.T, accountID, teamSpaceID string) *attachmentUploader {
	t.Helper()
	connection, caller := projecttest.LoginForTest(ctx, t, accountID)
	members := memberdao.New(postgrestest.OpenForTest(t))
	for {
		role, err := members.TeamSpaceRole(ctx, teamSpaceID, accountID, teamspaces.StatusDeleted)
		if err == nil && role != "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the read model never showed %s in TeamSpace %s", accountID, teamSpaceID)
		case <-time.After(time.Second):
		}
	}
	return &attachmentUploader{t: t, connection: connection, caller: caller, teamSpaceID: teamSpaceID}
}

// fixtureMediaTypes are the media types of the scenarios' fixture files, by extension.
var fixtureMediaTypes = map[string]string{".png": "image/png", ".md": "text/markdown", ".json": "application/json"}

// upload uploads the scenario fixture name and returns its READY attachment,
// which is deleted when the test ends instead of staying staged for a day.
func (u *attachmentUploader) upload(ctx context.Context, name string) *pb.Attachment {
	u.t.Helper()
	data, err := mockmodel.AttachmentFixtureForTest(name)
	if err != nil {
		u.t.Fatal(err)
	}
	api := pb.NewAttachmentServiceClient(u.connection)
	sum := sha256.Sum256(data)
	declared, err := api.CreateAttachmentUpload(ctx, &pb.CreateAttachmentUploadRequest{TeamSpaceId: u.teamSpaceID, RequestId: "fixture-" + flowid.RandomID(12), FileName: name,
		MediaType: fixtureMediaTypes[path.Ext(name)], ByteSize: int64(len(data)), Sha256: hex.EncodeToString(sum[:]), Caller: u.caller})
	if err != nil || declared.GetUpload() == nil {
		u.t.Fatalf("the upload of %s answered %v (%v)", name, declared, err)
	}
	id := declared.GetAttachment().GetAttachmentId()
	u.t.Cleanup(func() {
		deletion := &pb.DeleteAttachmentRequest{TeamSpaceId: u.teamSpaceID, AttachmentId: id, RequestId: "cleanup-" + id, Caller: u.caller}
		if _, err := api.DeleteAttachment(context.WithoutCancel(ctx), deletion); err != nil {
			u.t.Errorf("the deletion of attachment %s: %v", id, err)
		}
	})
	target := declared.GetUpload()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target.GetUrl(), bytes.NewReader(data))
	if err != nil {
		u.t.Fatal(err)
	}
	for _, header := range target.GetHeaders() {
		request.Header.Set(header.GetName(), header.GetValue())
	}
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		u.t.Fatalf("the presigned PUT of %s: %v", name, err)
	}
	_ = answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		u.t.Fatalf("the presigned PUT of %s answered %d", name, answer.StatusCode)
	}
	for {
		completed, err := api.CompleteAttachmentUpload(ctx, &pb.CompleteAttachmentUploadRequest{TeamSpaceId: u.teamSpaceID, AttachmentId: id, Caller: u.caller})
		switch attachment := completed.GetAttachment(); {
		case err != nil, attachment.GetStatus() == pb.AttachmentStatus_ATTACHMENT_STATUS_FAILED:
			u.t.Fatalf("the completion of %s answered %v (%v)", name, attachment, err)
		case attachment.GetStatus() == pb.AttachmentStatus_ATTACHMENT_STATUS_READY:
			u.t.Logf("attachment %s (%s) READY", id, name)
			return attachment
		}
		select {
		case <-ctx.Done():
			u.t.Fatalf("attachment %s (%s) never became READY", id, name)
		case <-time.After(time.Second):
		}
	}
}

// attachmentIDs are the IDs of attachments, in order.
func attachmentIDs(attached []*pb.Attachment) []string {
	ids := make([]string, 0, len(attached))
	for _, attachment := range attached {
		ids = append(ids, attachment.GetAttachmentId())
	}
	return ids
}

// createProject creates a Project of the uploader's TeamSpace through the
// lane's CreateProject with content and attached as its first message, on
// the mock model, and returns it once READY; it is deleted when the test ends.
func (u *attachmentUploader) createProject(ctx context.Context, client *projects.ProjectFlowClient, content string, attached ...*pb.Attachment) projects.Project {
	u.t.Helper()
	name := u.t.Name()
	request := &pb.CreateProjectRequest{Caller: u.caller, RequestId: flowid.RandomID(24), Name: name[:min(len(name), 120)], ManagedRepository: &pb.ManagedRepositorySource{},
		TeamSpaceId: u.teamSpaceID, InitialMessage: content, MessageId: "first-" + flowid.RandomID(12), Model: pb.AgentModel_AGENT_MODEL_MOCK, InitialAttachmentIds: attachmentIDs(attached)}
	created, err := pb.NewProjectServiceClient(u.connection).CreateProject(ctx, request)
	if err != nil {
		u.t.Fatalf("CreateProject with %d attachments: %v", len(attached), err)
	}
	u.t.Cleanup(func() { projecttest.DeleteForTest(context.WithoutCancel(ctx), u.t, client, created.GetProjectId()) })
	return projecttest.AwaitReadyForTest(ctx, u.t, client, created.GetProjectId())
}

// send sends content with attached to the Project's Session through the
// lane's SendMessage.
func (u *attachmentUploader) send(ctx context.Context, projectID, content string, attached ...*pb.Attachment) {
	u.t.Helper()
	_, err := pb.NewAgentServiceClient(u.connection).SendMessage(ctx, &pb.SendMessageRequest{Caller: u.caller, ProjectId: projectID,
		MessageId: "mock-" + time.Now().UTC().Format("150405.000000000"), Text: content, AttachmentIds: attachmentIDs(attached)})
	if err != nil {
		u.t.Fatalf("sending a message with %d attachments to Project %s: %v", len(attached), projectID, err)
	}
}

// The attachment-image scenario's first message carries a flowchart and
// creates the Project through CreateProject. The Session's first segment opens
// after the message, so the model sees the image in the segment_start, after
// its text, which the mock model requires with the envelope of the quoted
// request listing it; the first reply's usage forces a compaction, and the
// next segment shows the image again. The first render bound the attachment:
// the Project holds the exact rendition under its prefix with the manifest,
// and the public view summarizes it. The second message compacts once more,
// and that segment lists the flowchart among the earlier attachments.
func TestSessionMockAttachmentImageDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	first := mockInput(t, "attachment-image", 1)
	if len(first.AttachmentsForTest()) != 1 {
		t.Fatalf("attachment-image input 1 attaches %v", first.AttachmentsForTest())
	}
	owner := projecttest.RegisterAccountForTest(ctx, t)
	uploader := newAttachmentUploader(ctx, t, owner, projecttest.TeamSpaceForTest(ctx, t, owner))
	flowchart := uploader.upload(ctx, first.AttachmentsForTest()[0])
	session := newSessionOf(t, ctx, "", "", func(client *projects.ProjectFlowClient) projects.Project {
		return uploader.createProject(ctx, client, first.Content, flowchart)
	})
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if final := assistant[len(assistant)-1].Message.Content; !strings.Contains(final, "still shows the attached flowchart") {
		t.Fatalf("the image was not shown again after the compaction; the last reply: %q", final)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "attachment-image", Input: 1, NextInput: 2})

	_, messages := session.history(ctx)
	var summarized bool
	for _, message := range ofRole(messages, conversation.MessageRoleUser) {
		for _, attachment := range message.Message.Attachments {
			summarized = summarized || attachment.ID == flowchart.GetAttachmentId() && attachment.Name == flowchart.GetFileName() && attachment.Kind == conversation.AttachmentKindImage &&
				attachment.Width == int(flowchart.GetSummary().GetWidth())
		}
	}
	if !summarized {
		t.Fatal("the public view of the user message does not summarize its attachment")
	}
	store := artifactStore(ctx, t)
	var manifest struct {
		Rendition objectstore.ObjectRef `json:"rendition"`
	}
	bound := session.identity.ProjectID + "/attachments/" + flowchart.GetAttachmentId() + "/"
	if _, err := store.GetCurrentJSON(ctx, bound+"manifest.json", &manifest); err != nil || !strings.HasPrefix(manifest.Rendition.Key, store.FullKey(bound+"rendition/")) {
		t.Fatalf("the first render did not bind the attachment to the Project: %+v (%v)", manifest.Rendition, err)
	}
	if _, err := store.Get(ctx, manifest.Rendition); err != nil {
		t.Fatalf("the Project holds no exact copy of the rendition: %v", err)
	}

	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "attachment-image", 2).Content)
	agent = session.finishedTurnAfter(ctx, before)
	requireNoFailure(t, agent.History)
	assistant = replies(agent.History)
	if final := assistant[len(assistant)-1].Message.Content; !strings.Contains(final, "lists the earlier flowchart") {
		t.Fatalf("the segment after the second compaction does not list the flowchart; the last reply: %q", final)
	}
}

// The attachment-document scenario's message carries a document and an image,
// which SendMessage admits. The document's rendition shows its outline and an
// excerpt, so the model reads the refunds section with read_attachment, a
// platform tool that reads the Project's bound copy of the sections bundle
// without the workspace; reading the image as text is refused with
// invalid_arguments.
func TestSessionMockAttachmentDocumentDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	input := mockInput(t, "attachment-document", 1)
	if names := input.AttachmentsForTest(); len(names) != 2 {
		t.Fatalf("attachment-document input 1 attaches %v", names)
	}
	session := newMockSession(t, ctx, "")
	uploader := newAttachmentUploader(ctx, t, session.account, session.teamSpaceID)
	document, flowchart := uploader.upload(ctx, input.AttachmentsForTest()[0]), uploader.upload(ctx, input.AttachmentsForTest()[1])
	if summary := document.GetSummary(); summary.GetSections() != 10 {
		t.Fatalf("the document was prepared as %v", summary)
	}
	uploader.send(ctx, session.identity.ProjectID, input.Content+" "+mockmodel.ArgumentMarkerForTest("document", document.GetAttachmentId())+" "+
		mockmodel.ArgumentMarkerForTest("image", flowchart.GetAttachmentId()), document, flowchart)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	if read := toolResult(t, messages, conversation.ToolReadAttachment, 0); read.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("read_attachment of the refunds section did not succeed: %+v", read)
	}
	if refused := toolResult(t, messages, conversation.ToolReadAttachment, 1); refused.Outcome == conversation.ToolOutcomeSucceeded || refused.ResultCode != "invalid_arguments" {
		t.Fatalf("read_attachment of the image was not refused as invalid_arguments: %+v", refused)
	}
	assistant := replies(agent.History)
	if final := assistant[len(assistant)-1].Message.Content; !strings.Contains(final, "finance lead within two working days") {
		t.Fatalf("the scenario did not reach its last reply: %q", final)
	}
}

// The attachment-workflow-import scenario's first message carries an n8n
// export with three secret values and creates the Project through
// CreateProject, which starts it from the default template: an automation
// export is the behavior authority. Preparation replaced the three values
// with the redaction marker. The segment_start names the workflow import;
// place_attachment writes the Project's bound redacted copy into the
// workspace, the ported inventory in the Sandbox reports the three markers as
// literal-secret findings, and once .gitignore no longer ignores /.local/,
// commit_and_push refuses the placed file with attachment_in_source and
// commits nothing.
func TestSessionMockAttachmentWorkflowImportDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	input := mockInput(t, "attachment-workflow-import", 1)
	if names := input.AttachmentsForTest(); len(names) != 1 {
		t.Fatalf("attachment-workflow-import input 1 attaches %v", names)
	}
	owner := projecttest.RegisterAccountForTest(ctx, t)
	uploader := newAttachmentUploader(ctx, t, owner, projecttest.TeamSpaceForTest(ctx, t, owner))
	export := uploader.upload(ctx, input.AttachmentsForTest()[0])
	if summary := export.GetSummary(); summary.GetJsonFormat() != string(conversation.AttachmentJSONFormatN8n) || summary.GetRedactions() != 3 {
		t.Fatalf("the export was prepared as %v", summary)
	}
	var created projects.Project
	session := newSessionOf(t, ctx, "", "", func(client *projects.ProjectFlowClient) projects.Project {
		created = uploader.createProject(ctx, client, input.Content+" "+mockmodel.ArgumentMarkerForTest("attachment", export.GetAttachmentId()), export)
		return created
	})
	if created.TemplateRevision != apptemplate.Current().Revision {
		t.Fatalf("the Project of the export starts from %q, not the default template", created.TemplateRevision)
	}
	head := session.identity.HeadCommit
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	for _, tool := range []conversation.ToolName{conversation.ToolPlaceAttachment, conversation.ToolBash, conversation.ToolEdit} {
		if result := toolResult(t, messages, tool, 0); result.Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("%s did not succeed: %+v", tool, result)
		}
	}
	if refused := toolResult(t, messages, conversation.ToolCommitAndPush, 0); refused.Outcome == conversation.ToolOutcomeSucceeded || refused.ResultCode != "attachment_in_source" {
		t.Fatalf("commit_and_push did not refuse the placed attachment: %+v", refused)
	}
	// The scenario ends with the workspace dirty, so the platform's automatic turns follow its last reply.
	assistant := replies(agent.History)
	if !slices.ContainsFunc(assistant, func(reply conversation.SequencedMessage) bool {
		return strings.Contains(reply.Message.Content, "the commit was refused")
	}) {
		for _, message := range agent.History.Messages {
			t.Logf("%d %s %q", message.Sequence, message.Message.Role, message.Message.Content)
		}
		t.Fatal("the scenario did not reach its last reply")
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Session.Identity.HeadCommit != head {
		t.Fatalf("the refused commit moved the Session HEAD from %s (%v)", head, err)
	}
}

// While a local agent executes the Session, the lane's SendMessage refuses a
// message with an attachment as not admitted, FAILED_PRECONDITION, since the
// remote delivery carries text only; the Session's definite refusal drops the
// holds of that send's admission. A message the platform agent took before
// keeps its attachment counted when the person sends it again under the same
// message ID and the local agent refuses that send, and so does a new message
// that carries it again, whose other, new attachment the Project does not
// count.
func TestSessionRemoteAttachmentRefusalDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, "")
	uploader := newAttachmentUploader(ctx, t, session.account, session.teamSpaceID)
	flowchart, other := uploader.upload(ctx, "order-process.png"), uploader.upload(ctx, "order-process.png")
	agentAPI := pb.NewAgentServiceClient(uploader.connection)
	taken := &pb.SendMessageRequest{Caller: uploader.caller, ProjectId: session.identity.ProjectID, MessageId: "taken-" + flowid.RandomID(12),
		Text: "What does the flowchart show?", AttachmentIds: []string{flowchart.GetAttachmentId()}}
	if _, err := agentAPI.SendMessage(ctx, taken); err != nil {
		t.Fatalf("the platform agent did not take the message with the flowchart: %v", err)
	}
	session.finishedTurn(ctx)
	counted, err := session.projects.Read(ctx, session.identity.ProjectID)
	if err != nil || counted.AttachmentTotals.Count != 1 {
		t.Fatalf("after the platform agent took the message the Project counts %+v (%v), not the flowchart", counted.AttachmentTotals, err)
	}
	pair, err := pb.NewRemoteAgentServiceClient(uploader.connection).CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: session.identity.ProjectID, Caller: uploader.caller})
	if err != nil {
		t.Fatal(err)
	}
	remote := remoteHTTPClient{t: t, endpoint: pair.GetEndpoint(), client: &http.Client{Timeout: 30 * time.Second}}
	var paired bridgeapi.PairResponse
	remote.post(ctx, bridgeapi.PathPair, bridgeapi.PairRequest{Code: pair.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: "darwin", Arch: "arm64"}}, &paired, http.StatusOK)
	remote.token = paired.Token
	var active bridgeapi.ActivateResponse
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "attachment-refusal", ProviderThreadID: "attachment-refusal", HostLabel: "integration host"}}
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	session.waitForSnapshot(ctx, "the local agent's executor", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Executor.Mode == remoteexec.ModeRemote && agent.Description.Executor.Epoch == active.Epoch
	})

	refused := func(request *pb.SendMessageRequest, what string) {
		t.Helper()
		_, err := agentAPI.SendMessage(ctx, request)
		failure := status.Convert(err)
		notAdmitted := slices.ContainsFunc(failure.Details(), func(detail any) bool {
			info, isInfo := detail.(*errdetails.ErrorInfo)
			return isInfo && info.GetReason() == "AGENT_COMMAND_NOT_ADMITTED"
		})
		if failure.Code() != codes.FailedPrecondition || !notAdmitted || !strings.Contains(failure.Message(), "Superverse agent") {
			t.Fatalf("%s to the local agent answered %v", what, err)
		}
		project, err := session.projects.Read(ctx, session.identity.ProjectID)
		if err != nil || project.AttachmentTotals != counted.AttachmentTotals {
			t.Fatalf("after the refusal of %s the Project counts %+v (%v), not %+v", what, project.AttachmentTotals, err, counted.AttachmentTotals)
		}
	}
	refused(taken, "the taken message sent again")
	refused(&pb.SendMessageRequest{Caller: uploader.caller, ProjectId: session.identity.ProjectID, MessageId: "remote-" + flowid.RandomID(12),
		Text: "Compare the two flowcharts.", AttachmentIds: []string{flowchart.GetAttachmentId(), other.GetAttachmentId()}}, "a new message with the flowchart and another image")
}
