// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
)

// TestAttachmentServiceDexIntegration drives AttachmentService through the
// actual gRPC API, with TeamSpaceFlow, ProjectFlow and AttachmentFlow on the
// lane's server Worker, the read model of the lane's PostgreSQL and the
// platform bucket of the lane's MinIO, which the test reaches with presigned
// requests as a browser does. A TeamSpace's member uploads an image: an
// outsider's upload is refused by the read model, a file of an unaccepted
// type is INVALID_ARGUMENT, the presigned PUT stores the bytes, and only the
// uploader completes the upload, which answers READY with the image's size.
// Once the member's Project holds its copy of the attachment, as its
// Session's first render makes it, a reader of the Project downloads it
// inline through a presigned GET, an outsider is refused and an attachment
// the Project holds no copy of has no download. Only the uploader deletes it.
//
// The member's Project's Setup creates its repository, so the test requires
// SUPERVERSE_TEST_GITHUB_EFFECTS=true. The Project and then the TeamSpace are
// deleted when it ends.
func TestAttachmentServiceDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	key := []byte(strings.Repeat("a", 32))
	teamSpaceFlow, projectFlow := &teamspaces.TeamSpaceFlow{}, &projects.ProjectFlow{}
	objects := attachmentStore(ctx, t)
	// The client's Flow supplies the definition and the declared files' bounds; the server's Worker runs it.
	flows := attachments.NewAttachmentFlowBootstrap(objects, fileextract.NewExtractor("superverse-server"), nil, attachments.Settings{
		ImageMaxBytes: 1 << 20, DocumentMaxBytes: 20 << 20, JSONMaxBytes: 4 << 20, UploadURLValidity: attachments.DefaultUploadURLValidity,
	})
	sdk := dexflowtest.ServerWorkerClient(t, teamSpaceFlow, projectFlow, flows.Flow())
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}
	attachmentClient, renditions := flows.Client(sdk), attachments.NewRenditionStore(objects, 1<<20)
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, ProjectIDLength: projectClient.ProjectIDLength, Projects: projectClient, TeamSpaces: teamSpaceClient,
		TeamSpaceReads: teamspacedao.New(db), Members: memberdao.New(db), Attachments: attachmentClient, Renditions: renditions})
	teamSpaceAPI, memberAPI, accountAPI := pb.NewTeamSpaceServiceClient(connection), pb.NewTeamSpaceMemberServiceClient(connection), pb.NewAccountServiceClient(connection)
	attachmentAPI := pb.NewAttachmentServiceClient(connection)

	id := flowid.RandomID(10)
	alice, _ := registerNamedCaller(ctx, t, accountAPI, "alice-"+id)
	bob, bobEmail := registerNamedCaller(ctx, t, accountAPI, "bob-"+id)
	carol, _ := registerNamedCaller(ctx, t, accountAPI, "carol-"+id)
	created, err := teamSpaceAPI.CreateTeamSpace(ctx, &pb.CreateTeamSpaceRequest{RequestId: "attach-" + id, Name: "Attach " + id, Caller: alice})
	if err != nil {
		t.Fatalf("create the TeamSpace: %v", err)
	}
	teamSpaceID := created.GetTeamSpaceId()
	t.Cleanup(func() { deleteTeamSpace(ctx, t, teamSpaceClient, teamSpaceID) })
	awaitTrue(ctx, t, "the TeamSpace's row", func() bool {
		_, err := teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: alice})
		return err == nil
	})
	if _, err = memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: bobEmail, Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: alice}); err != nil {
		t.Fatalf("alice adds bob: %v", err)
	}
	awaitTrue(ctx, t, "bob's membership in the read model", func() bool {
		role, err := memberdao.New(db).TeamSpaceRole(ctx, teamSpaceID, bob.GetAccountId(), teamspaces.StatusDeleted)
		return err == nil && role == teamspaces.RoleMember
	})

	data := attachmentImage(t)
	sum := sha256.Sum256(data)
	upload := &pb.CreateAttachmentUploadRequest{TeamSpaceId: teamSpaceID, RequestId: "upload-" + id, FileName: "order-process.png", MediaType: "image/png",
		ByteSize: int64(len(data)), Sha256: hex.EncodeToString(sum[:]), Caller: carol}
	_, err = attachmentAPI.CreateAttachmentUpload(ctx, upload)
	requireCode(t, err, codes.PermissionDenied, "an outsider's upload")
	upload.Caller, upload.FileName = bob, "order-process.gif"
	_, err = attachmentAPI.CreateAttachmentUpload(ctx, upload)
	requireCode(t, err, codes.InvalidArgument, "a PNG named as a GIF")
	upload.FileName = "order-process.png"
	declared, err := attachmentAPI.CreateAttachmentUpload(ctx, upload)
	if err != nil || declared.GetAttachment().GetStatus() != pb.AttachmentStatus_ATTACHMENT_STATUS_AWAITING_UPLOAD || declared.GetUpload() == nil ||
		declared.GetAttachment().GetKind() != pb.AttachmentKind_ATTACHMENT_KIND_IMAGE {
		t.Fatalf("bob's upload answered %v (%v)", declared, err)
	}
	attachmentID := declared.GetAttachment().GetAttachmentId()
	if status := putUpload(ctx, t, declared.GetUpload(), data); status != http.StatusOK {
		t.Fatalf("the presigned PUT answered %d", status)
	}
	complete := &pb.CompleteAttachmentUploadRequest{TeamSpaceId: teamSpaceID, AttachmentId: attachmentID, Caller: alice}
	_, err = attachmentAPI.CompleteAttachmentUpload(ctx, complete)
	requireCode(t, err, codes.PermissionDenied, "a completion by another member than the uploader")
	complete.Caller = bob
	var ready *pb.Attachment
	awaitTrue(ctx, t, "the uploaded image READY", func() bool {
		completed, err := attachmentAPI.CompleteAttachmentUpload(ctx, complete)
		if err != nil {
			t.Fatalf("bob completes the upload: %v", err)
		}
		ready = completed.GetAttachment()
		return ready.GetStatus() == pb.AttachmentStatus_ATTACHMENT_STATUS_READY
	})
	if ready.GetSummary().GetWidth() != 320 || ready.GetSummary().GetHeight() != 180 || ready.GetExpiresAt() == "" || ready.GetFailure() != nil {
		t.Fatalf("the READY image is %v", ready)
	}

	project := projecttest.CreateInTeamSpaceForTest(ctx, t, projectClient, "Attach "+id, bob.GetAccountId(), teamSpaceID)
	prepared, err := attachmentClient.GetForTest(ctx, teamSpaceID, attachmentID)
	if err != nil {
		t.Fatal(err)
	}
	reference := conversation.AttachmentReference{Attachment: prepared.ReferenceForTest(), Variant: conversation.AttachmentVariantFull}
	if _, err = renditions.Resolve(ctx, project.ProjectID, []conversation.AttachmentReference{reference}); err != nil {
		t.Fatalf("the first render of the attachment in Project %s: %v", project.ProjectID, err)
	}
	downloads := &pb.GetAttachmentDownloadsRequest{ProjectId: project.ProjectID, AttachmentIds: []string{attachmentID, flowid.RandomID(12)},
		Disposition: pb.AttachmentDisposition_ATTACHMENT_DISPOSITION_INLINE, Caller: carol}
	_, err = attachmentAPI.GetAttachmentDownloads(ctx, downloads)
	requireCode(t, err, codes.PermissionDenied, "an outsider's downloads")
	downloads.Caller = bob
	answered, err := attachmentAPI.GetAttachmentDownloads(ctx, downloads)
	if err != nil || len(answered.GetDownloads()) != 1 || answered.GetDownloads()[0].GetAttachmentId() != attachmentID {
		t.Fatalf("bob's downloads answered %v (%v)", answered, err)
	}
	if got := download(ctx, t, answered.GetDownloads()[0].GetUrl()); !bytes.Equal(got, data) {
		t.Fatalf("the download answered %d bytes, not the upload", len(got))
	}

	deletion := &pb.DeleteAttachmentRequest{TeamSpaceId: teamSpaceID, AttachmentId: attachmentID, RequestId: "delete-" + id, Caller: alice}
	_, err = attachmentAPI.DeleteAttachment(ctx, deletion)
	requireCode(t, err, codes.PermissionDenied, "a deletion by another member than the uploader")
	deletion.Caller = bob
	deleted, err := attachmentAPI.DeleteAttachment(ctx, deletion)
	if err != nil || deleted.GetAttachment().GetStatus() != pb.AttachmentStatus_ATTACHMENT_STATUS_DELETING && deleted.GetAttachment().GetStatus() != pb.AttachmentStatus_ATTACHMENT_STATUS_DELETED {
		t.Fatalf("bob's deletion answered %v (%v)", deleted, err)
	}
	t.Logf("attachment %s of TeamSpace %s: READY, copied into Project %s, downloaded, deleting", attachmentID, teamSpaceID, project.ProjectID)
}

// attachmentStore is the platform bucket the lane's server writes, presigning
// for the same loopback MinIO, which a browser on the host reaches.
func attachmentStore(ctx context.Context, t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pathStyle := func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }
	store, err := objectstore.NewStore(s3.NewFromConfig(config, pathStyle), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	return store.WithPresigner(s3.NewPresignClient(s3.NewFromConfig(config, pathStyle)))
}

// attachmentImage is a PNG of 320 by 180 pixels.
func attachmentImage(t *testing.T) []byte {
	t.Helper()
	picture := image.NewPaletted(image.Rect(0, 0, 320, 180), []color.Color{color.White, color.Black})
	for x := range 320 {
		picture.SetColorIndex(x, 90, 1)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// putUpload sends the browser's PUT of data with exactly the headers the
// upload target names.
func putUpload(ctx context.Context, t *testing.T, target *pb.AttachmentUploadTarget, data []byte) int {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target.GetUrl(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range target.GetHeaders() {
		request.Header.Set(header.GetName(), header.GetValue())
	}
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = answer.Body.Close() }()
	_, _ = io.Copy(io.Discard, answer.Body)
	return answer.StatusCode
}

// download reads a presigned GET.
func download(ctx context.Context, t *testing.T, url string) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = answer.Body.Close() }()
	body, err := io.ReadAll(answer.Body)
	if err != nil || answer.StatusCode != http.StatusOK {
		t.Fatalf("the presigned GET answered %d (%v)", answer.StatusCode, err)
	}
	return body
}
