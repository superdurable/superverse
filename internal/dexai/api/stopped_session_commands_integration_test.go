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
	"errors"
	"log/slog"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This creates a throwaway READY Project with its actual Worker, lets the
// Project's Session initialize and terminates its engine execution, as an
// operator stop would. The retained status stays ACTIVE, yet nothing treats the Session as
// running: its authorization and a command that reaches the stopped execution
// answer FAILED_PRECONDITION SESSION_EXECUTION_TERMINATED, while the snapshot
// stays readable and reports FAILED with the conversation preserved. The
// Preview configuration read reads stored state alone, so the stopped
// execution does not gate it: it answers that the Session's HEAD, which no
// preparation holds, needs one. The test then deletes the Project: the
// deletion discharges the terminated Session by its exact membership and
// resource absence and ends DELETED.
func TestStoppedSessionCommandsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	// The Preview configuration read finds the HEAD's prepared source in the
	// platform's artifact bucket, as the server's Preview configuration does.
	previewFlow := &preview.PreviewFlow{Preview: preview.NewPreviewConfiguration(nil, nil, artifactStore(t), nil, nil, nil, nil)}
	projectFlow, flow, workspaceFlow := &projects.ProjectFlow{}, &sessionagent.SessionAgentFlow{}, &sessionworkspace.SessionWorkspaceFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, previewFlow, flow, workspaceFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	// This client registry supplies definitions only. Real handlers and every
	// application dependency belong to the existing Worker selected above.
	client := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, "")
	service := &agents.SessionFlowService{
		Projects: &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"},
		Previews: &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader}, Agent: client,
		Workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader),
		Logger:     slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, service.Projects, t.Name())
	projectID, account := project.ProjectID, project.CreatorAccountID
	id := agentloop.SessionFlowID(projectID)
	t.Logf("stopped-engine probe FlowID=%s", id)
	await := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s did not converge; reconcile %s", what, id)
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	await("Session initialization", func() bool {
		snapshot, readErr := client.GetSnapshot(ctx, id)
		return readErr == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	if _, err := service.AuthorizeSession(ctx, accounts.AccountSummary{AccountID: account}, projectID); err != nil {
		t.Fatalf("a running Session was not authorized (%T)", err)
	}
	if err := sdk.StopFlow(ctx, string(id), dex.StopOptions{Type: dex.TerminateFlow, Reason: "stopped-engine detection test"}); err != nil {
		t.Fatalf("terminate failed (%T)", err)
	}
	await("engine termination", func() bool {
		summary, readErr := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(id)})
		return readErr == nil && summary.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_TERMINATED
	})

	// The live read reports the stopped execution with the retained summary.
	_, err := client.GetLiveSessionSummary(ctx, id)
	stopped, isStopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err)
	if !isStopped || stopped.Reason != agentloop.ExecutionTerminated || stopped.SummaryForTest().Status != agentloop.SessionStatusActive {
		t.Fatalf("the live summary of a terminated Session is not a stopped execution (%v)", err)
	}
	// Authorization, which every command and the archive read share, as an
	// Agent command and the archive read answer it.
	_, err = service.AuthorizeSession(ctx, accounts.AccountSummary{AccountID: account}, projectID)
	requireStoppedStatus(t, "command authorization", notAdmittedCommand(err))
	requireStoppedStatus(t, "archive read", boundary.AgentRPCStatus(err))
	// A command that skips authorization and meets the stopped execution.
	err = client.SendPendingMessage(ctx, id, conversation.PendingUserMessage{MessageID: "stopped-probe", Value: conversation.UserMessage{Content: "hello"}})
	requireStoppedStatus(t, "command", notAdmittedCommand(err))
	// The Preview configuration read asks the stopped Session for nothing.
	_, err = service.ReadPreviewTarget(ctx, projectID)
	if public := status.Convert(boundary.ConfigurationRPCStatus(err)); !errors.Is(err, preview.ErrSourceNotPrepared) ||
		public.Code() != codes.FailedPrecondition || public.Message() != "Prepare the current source" {
		t.Fatalf("the Preview configuration read of the terminated Session answered %s %q, not the unprepared HEAD (%v)", public.Code(), public.Message(), err)
	}
	// The snapshot stays readable and presents the Session as FAILED.
	snapshot, err := service.Get(ctx, account, projectID)
	if err != nil || snapshot.ExecutionFailure != agentloop.ExecutionTerminated || snapshot.Agent == nil {
		t.Fatalf("the snapshot of a terminated Session is unreadable or not FAILED (%T)", err)
	}
	if presented := sessionSnapshot(snapshot); presented.Status != pb.SessionStatus_SESSION_STATUS_FAILED || presented.Failure.GetCode() != agentloop.ExecutionTerminated.String() {
		t.Fatalf("the terminated Session is presented as %s", presented.Status)
	}
	t.Logf("FlowID=%s terminated: authorization and command answer FAILED_PRECONDITION SESSION_EXECUTION_TERMINATED; the Preview configuration read answers the unprepared HEAD; snapshot FAILED", id)

	// Deleting the Project discharges the terminated Session.
	project, err = service.Projects.Read(ctx, projectID)
	if err != nil {
		t.Fatalf("Project read failed (%T)", err)
	}
	if _, err = service.Projects.RequestDeletion(ctx, projectID, "delete-stopped", project.Revision); err != nil {
		t.Fatalf("the deletion of Project %s was not admitted (%T)", projectID, err)
	}
	await("the deletion of the Project", func() bool {
		current, readErr := service.Projects.Read(ctx, projectID)
		if readErr == nil && current.ActiveCommand != nil && current.ActiveCommand.Phase == "FAILED" && current.LastFailure != nil {
			t.Fatalf("the deletion of Project %s failed in %s with %s: %s", projectID, current.LastFailure.Phase, current.LastFailure.Code, current.LastFailure.Message)
		}
		return readErr == nil && current.ProjectStatus == projects.StatusDeleted
	})
	// The terminated Session never provisioned an instance or ran a Preview.
	_, err = service.Workspaces.Get(ctx, projectID)
	var missing *dex.FlowNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("the terminated Session %s has a workspace Flow (%T)", id, err)
	}
	_, err = service.Previews.Get(ctx, projectID)
	if !errors.As(err, &missing) {
		t.Fatalf("the Project of the terminated Session %s has a Preview (%T)", id, err)
	}
	t.Logf("Project %s DELETED with the terminated Session %s discharged and no Sandbox resources left", projectID, id)
}

// requireStoppedStatus requires the public status of a stopped Session: the
// single ErrorInfo with the canonical contact-operator remedy.
func requireStoppedStatus(t *testing.T, operation string, err error) {
	t.Helper()
	public := status.Convert(err)
	if public.Code() != codes.FailedPrecondition {
		t.Fatalf("%s answered %s, want FAILED_PRECONDITION", operation, public.Code())
	}
	details := public.Details()
	if len(details) != 1 {
		t.Fatalf("%s carried %d details, want one ErrorInfo", operation, len(details))
	}
	if info, isInfo := details[0].(*errdetails.ErrorInfo); !isInfo || info.GetDomain() != "superverse.v2" ||
		info.GetReason() != agentloop.ExecutionTerminated.String() || len(info.GetMetadata()) != 1 ||
		info.GetMetadata()[boundary.RemedyMetadataKeyForTest] != pb.FailureRemedy_FAILURE_REMEDY_CONTACT_OPERATOR.String() {
		t.Fatalf("%s did not carry the single ErrorInfo SESSION_EXECUTION_TERMINATED: %v", operation, details)
	}
}

// artifactStore is the platform's versioned artifact bucket: SUPERVERSE_S3_ENDPOINT
// and SUPERVERSE_ARTIFACT_BUCKET of the local lane, with the platform MinIO's
// credentials in the AWS environment, and the server's key prefix.
func artifactStore(t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	config, err := awsconfig.LoadDefaultConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store, err := objectstore.NewStore(s3.NewFromConfig(config, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
