// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// TestAttachmentStagingQuotaDexIntegration fills a TeamSpace's staging quota
// of three files: with two staged, four creations race for the last slot and
// exactly one reserves it while the others end FAILED with
// attachment_quota_exceeded, their slots never held; a file beyond the byte
// cap and an uploader who is no member are refused alike. Once a staged
// attachment is deleted its slot is free again.
func TestAttachmentStagingQuotaDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	l := newLane(ctx, t, testSettings(), teamspaces.StagingLimits{Files: 3, Bytes: 1 << 20}, "")
	text := func(index int) []byte { return []byte(strings.Repeat("note ", 10) + string(rune('a'+index))) }
	var staged []Attachment
	for index := range 2 {
		a, _, err := l.declare(ctx, l.member, "note.md", "text/markdown", text(index))
		if err != nil || a.Status != StatusAwaitingUpload {
			t.Fatalf("declare staged file %d: %+v (%s)", index, a, dexflowtest.ErrorCategory(err))
		}
		staged = append(staged, a)
	}
	var wait sync.WaitGroup
	results := make(chan Attachment, 4)
	for index := range 4 {
		wait.Go(func() {
			a, _, err := l.declare(ctx, l.member, "race.md", "text/markdown", text(10+index))
			if err != nil {
				t.Errorf("a racing creation failed: %s", dexflowtest.ErrorCategory(err))
				return
			}
			results <- a
		})
	}
	wait.Wait()
	close(results)
	reserved, refused := 0, 0
	for a := range results {
		switch code, _, _ := a.PublicFailure(); {
		case a.Status == StatusAwaitingUpload:
			reserved++
		case code == failureQuotaExceeded.name:
			refused++
			if ended := l.awaitEnded(ctx, a.AttachmentID); ended.Status != StatusFailed {
				t.Errorf("a refused attachment ended %s", ended.Status)
			}
		default:
			t.Errorf("a racing creation answered %+v", a)
		}
	}
	if reserved != 1 || refused != 3 {
		t.Fatalf("%d racing creations reserved the last slot and %d were refused; want 1 and 3", reserved, refused)
	}

	l2 := newLane(ctx, t, testSettings(), teamspaces.StagingLimits{Files: 10, Bytes: 100}, "")
	tooLarge, _, err := l2.declare(ctx, l2.member, "large.md", "text/markdown", []byte(strings.Repeat("x", 101)))
	if code, _, _ := tooLarge.PublicFailure(); err != nil || code != failureQuotaExceeded.name {
		t.Fatalf("a file beyond the byte cap answered %+v (%s)", tooLarge, dexflowtest.ErrorCategory(err))
	}
	outsider, _, err := l2.declare(ctx, teamspaces.Actor{AccountID: "outsider-" + l2.teamSpaceID}, "note.md", "text/markdown", text(0))
	if code, _, _ := outsider.PublicFailure(); err != nil || code != failureNotMember.name {
		t.Fatalf("an outsider's file answered %+v (%s)", outsider, dexflowtest.ErrorCategory(err))
	}

	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, staged[0].AttachmentID, "free", l.member); err != nil {
		t.Fatal(err)
	}
	l.awaitEnded(ctx, staged[0].AttachmentID)
	freed, _, err := l.declare(ctx, l.member, "after.md", "text/markdown", text(20))
	if err != nil || freed.Status != StatusAwaitingUpload {
		t.Fatalf("the slot of the deleted attachment is not free: %+v (%s)", freed, dexflowtest.ErrorCategory(err))
	}
}

// TestAttachmentLapsedStagingDexIntegration fills a TeamSpace's staging
// quota of two files with the slot of an AttachmentFlow that ended without
// releasing it, as one whose release or deletion used up its retries leaves
// it: its hold lapsed and an object of it is left under its prefix; and with
// a slot whose hold has not lapsed. A new upload that finds the quota full
// reclaims the lapsed slot, its object deleted, and reserves; the next one
// finds nothing lapsed and is refused attachment_quota_exceeded, the held
// slot kept. A declared attachment holds its slot until its start plus the
// longest an attachment stays staged, and expires early enough to release it
// within that hold.
func TestAttachmentLapsedStagingDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	l := newLane(ctx, t, testSettings(), teamspaces.StagingLimits{Files: 2, Bytes: 1 << 20}, "")
	slot := func(heldUntil time.Time) string {
		t.Helper()
		id := flowid.RandomID(12)
		reservation := teamspaces.StagingReservation{AttachmentID: id, FlowID: flowid.AttachmentFlowID(l.teamSpaceID, id), Bytes: 10, Actor: l.member, HeldUntil: heldUntil}
		if err := l.teamSpaces.ReserveAttachmentStaging(ctx, l.teamSpaceID, reservation); err != nil {
			t.Fatalf("reserve the slot of %s: %v", id, err)
		}
		return id
	}
	lapsed, held := slot(time.Now().Add(-time.Minute)), slot(time.Now().Add(time.Hour))
	if _, err := l.objects.Put(ctx, stagingPrefix(l.teamSpaceID, lapsed)+"rendition/left.json", "application/json", []byte(`{"left":true}`)); err != nil {
		t.Fatal(err)
	}
	if listed, err := l.teamSpaces.LapsedAttachmentStaging(ctx, l.teamSpaceID); err != nil || len(listed) != 1 || listed[0] != lapsed {
		t.Fatalf("the TeamSpace lists the lapsed slots %v (%v), want only %s", listed, err, lapsed)
	}

	reclaiming, _, err := l.declare(ctx, l.member, "note.md", "text/markdown", []byte("A note that needs the lapsed slot."))
	if err != nil || reclaiming.Status != StatusAwaitingUpload {
		t.Fatalf("an upload into the full quota with a lapsed slot answered %s with %+v (%s)", reclaiming.Status, reclaiming.Failure, dexflowtest.ErrorCategory(err))
	}
	if want := reclaiming.CreatedAt.Add(testSettings().stagingHold()); !reclaiming.StagingHeldUntil.Equal(want) {
		t.Fatalf("the attachment holds its slot until %s, want %s", reclaiming.StagingHeldUntil, want)
	}
	if expiry := testSettings().expiry(reclaiming.CreatedAt, reclaiming.StagingHeldUntil); expiry.Add(testSettings().stagingTail()).After(reclaiming.StagingHeldUntil) {
		t.Fatalf("an attachment published at its start expires at %s, too late to release its slot by %s", expiry, reclaiming.StagingHeldUntil)
	}
	l.requireStagingGone(ctx, lapsed)
	if listed, err := l.teamSpaces.LapsedAttachmentStaging(ctx, l.teamSpaceID); err != nil || len(listed) != 0 {
		t.Fatalf("after the reclaim the TeamSpace lists the lapsed slots %v (%v)", listed, err)
	}

	// The held slot and the reclaiming upload's fill the quota, so the held one was kept.
	refused, _, err := l.declare(ctx, l.member, "other.md", "text/markdown", []byte("A note that finds no lapsed slot."))
	if code, _, _ := refused.PublicFailure(); err != nil || code != failureQuotaExceeded.name {
		t.Fatalf("an upload into the full quota without a lapsed slot (%s held) answered %+v (%s)", held, refused, dexflowtest.ErrorCategory(err))
	}
}

// TestAttachmentTimersDexIntegration runs both business timers with short
// settings: an attachment whose upload never arrives is checked once at the
// end of the upload wait and fails upload_missing, and a READY attachment
// expires at the end of its staging retention, with every staged version
// deleted and its slot released.
func TestAttachmentTimersDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	settings := testSettings()
	settings.UploadWait, settings.StagingRetention = 8*time.Second, 10*time.Second
	l := newLane(ctx, t, settings, teamspaces.StagingLimits{Files: 2, Bytes: 1 << 20}, "")
	missing, _, err := l.declare(ctx, l.member, "never.md", "text/markdown", []byte("never uploaded"))
	if err != nil || missing.Status != StatusAwaitingUpload {
		t.Fatalf("declare the missing upload: %+v (%s)", missing, dexflowtest.ErrorCategory(err))
	}
	ready := l.uploaded(ctx, "expires.md", "text/markdown", []byte("# Expires\n\nSoon.\n"))
	if ready.Status != StatusReady {
		t.Fatalf("the expiring attachment is %+v", ready)
	}
	failed := l.awaitEnded(ctx, missing.AttachmentID)
	if code, _, isFailed := failed.PublicFailure(); !isFailed || code != failureUploadMissing.name {
		t.Fatalf("the missing upload ended %+v", failed)
	}
	expired := l.awaitEnded(ctx, ready.AttachmentID)
	if expired.Status != StatusExpired || expired.Failure != nil {
		t.Fatalf("the READY attachment ended %+v", expired)
	}
	l.requireStagingGone(ctx, ready.AttachmentID)
	l.requireStagingGone(ctx, missing.AttachmentID)
	// Both slots are free again: two files reserve them.
	for index := range 2 {
		if a, _, err := l.declare(ctx, l.member, "again.md", "text/markdown", []byte{byte('a' + index)}); err != nil || a.Status != StatusAwaitingUpload {
			t.Fatalf("a slot of the ended attachments is not free: %+v (%s)", a, dexflowtest.ErrorCategory(err))
		}
	}
}

// TestAttachmentDeletionDexIntegration deletes attachments in every state
// before READY and when READY: while it awaits its upload, whose presigned
// PUT then lands after the deletion admitted and is deleted with the prefix;
// while the extractor child prepares it, which a slow extractor holds; and
// when READY. The same command ID answers again, another one while the first
// runs is refused, another account than the uploader is refused, and the
// platform admin deletes another's attachment. Every deletion ends DELETED
// with nothing left under its prefix.
func TestAttachmentDeletionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	l := newLane(ctx, t, testSettings(), teamspaces.StagingLimits{Files: 10, Bytes: 1 << 20}, slowExtractor(t, 15))
	data := []byte("# Late\n\nUploaded after the deletion.\n")
	awaiting, target, err := l.declare(ctx, l.member, "late.md", "text/markdown", data)
	if err != nil || target == nil {
		t.Fatalf("declare: %+v (%s)", awaiting, dexflowtest.ErrorCategory(err))
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, awaiting.AttachmentID, "delete", l.admin); err != nil {
		t.Fatalf("the platform admin's deletion of another's attachment: %s", dexflowtest.ErrorCategory(err))
	}
	if again, err := l.client.RequestDeletion(ctx, l.teamSpaceID, awaiting.AttachmentID, "delete", l.admin); err != nil || again.VisibleStatus() != StatusDeleting && again.Status != StatusDeleted {
		t.Fatalf("a repeat of the deletion answered %+v (%s)", again, dexflowtest.ErrorCategory(err))
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, awaiting.AttachmentID, "another", l.member); !rejectedWith(err, dexflow.RejectionFailedPrecondition) {
		t.Fatalf("another deletion while the first runs was not refused: %v", err)
	}
	if status := put(ctx, t, target, data); status != http.StatusOK {
		t.Fatalf("the PUT within the URL's validity answered %d", status)
	}

	preparing := l.uploadedUntil(ctx, "slow.md", "text/markdown", data, StatusPreparing)
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, preparing.AttachmentID, "delete", teamspaces.Actor{AccountID: "stranger-" + l.teamSpaceID}); !rejectedWith(err, dexflow.RejectionPermissionDenied) {
		t.Fatalf("a stranger's deletion was not refused: %v", err)
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, preparing.AttachmentID, "delete", l.member); err != nil {
		t.Fatal(err)
	}

	ready := l.uploaded(ctx, "ready.md", "text/markdown", []byte("# Ready\n\nThe attachment is ready.\n"))
	if ready.Status != StatusReady {
		t.Fatalf("the third attachment is %+v", ready)
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, ready.AttachmentID, "delete", l.member); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Attachment{awaiting, preparing, ready} {
		if ended := l.awaitEnded(ctx, a.AttachmentID); ended.Status != StatusDeleted || ended.Prepared != nil && a.AttachmentID == preparing.AttachmentID {
			t.Fatalf("attachment %s ended %+v", a.AttachmentID, ended)
		}
		l.requireStagingGone(ctx, a.AttachmentID)
	}
}

// uploadedUntil uploads data of mediaType and completes its upload once,
// then waits until the attachment reaches status.
func (l *lane) uploadedUntil(ctx context.Context, name, mediaType string, data []byte, status Status) Attachment {
	l.t.Helper()
	a, target, err := l.declare(ctx, l.member, name, mediaType, data)
	if err != nil || target == nil || put(ctx, l.t, target, data) != http.StatusOK {
		l.t.Fatalf("upload %s: %+v (%s)", name, a, dexflowtest.ErrorCategory(err))
	}
	if err = l.client.write(ctx, flowid.AttachmentFlowID(l.teamSpaceID, a.AttachmentID), l.client.flow.CompleteAttachmentUpload, UploadCompletion{AccountID: l.member.AccountID}, completionRefusal(l.member)); err != nil {
		l.t.Fatal(err)
	}
	reached, _, err := l.client.await(ctx, flowid.AttachmentFlowID(l.teamSpaceID, a.AttachmentID), time.Minute, func(a Attachment) bool { return a.Status == status })
	if err != nil || reached.Status != status {
		l.t.Fatalf("attachment %s did not reach %s: %+v (%s)", a.AttachmentID, status, reached, dexflowtest.ErrorCategory(err))
	}
	return reached
}

// TestAttachmentWorkerRestartDexIntegration replaces the Worker while the
// extractor child prepares an export. The preparation's attempt is lost with
// its Worker; the retry on the new Worker prepares again and the attachment
// becomes READY once, with one version of each object it wrote.
func TestAttachmentWorkerRestartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	objects := presigningStore(ctx, t)
	teamSpaceFlow := &teamspaces.TeamSpaceFlow{Connections: connections.NewUsages(connectiondao.New(postgrestest.OpenForTest(t)), objects), Objects: objects,
		Staging: teamspaces.StagingLimits{Files: 10, Bytes: 1 << 20}}
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Flow: teamSpaceFlow}
	extractor, runs := countedSlowExtractor(t, 30)
	bootstrap := NewAttachmentFlowBootstrap(objects, fileextract.NewExtractor(extractor), teamSpaceClient, testSettings())
	sdk, replace := dexflowtest.WorkerClientWithCrashForTest(ctx, t, address, host, teamSpaceFlow, bootstrap.Flow())
	teamSpaceClient.Dex = sdk
	l := &lane{t: t, objects: objects, client: bootstrap.Client(sdk), teamSpaces: teamSpaceClient, teamSpaceID: flowid.RandomID(8)}
	l.member, l.admin = teamspaces.Actor{AccountID: "member-" + l.teamSpaceID}, teamspaces.Actor{AccountID: "admin-" + l.teamSpaceID, PlatformAdmin: true}
	start := teamspaces.Start{Name: "Restart " + l.teamSpaceID, CreatorAccountID: l.member.AccountID}
	start.Identity = dexflow.StartIdentity{RequestID: flowid.Fingerprint(start), Fingerprint: flowid.Fingerprint(start), ActorAccountID: l.member.AccountID}
	if err := teamSpaceClient.Create(ctx, l.teamSpaceID, start); err != nil {
		t.Fatal(err)
	}
	// The fresh Worker's client closes before the cleanups registered ahead of it, so the lane closes on it first.
	closed := false
	t.Cleanup(func() {
		if !closed {
			l.close()
		}
	})

	data := []byte(leadIntake)
	preparing := l.uploadedUntil(ctx, "lead-intake.json", "application/json", data, StatusPreparing)
	// The Worker goes while the slow extractor holds the preparation's attempt.
	time.Sleep(3 * time.Second)
	fresh := replace()
	teamSpaceClient.Dex, l.client = fresh, bootstrap.Client(fresh)
	t.Cleanup(func() { closed = true; l.close() })
	ready, settled, err := l.client.await(ctx, flowid.AttachmentFlowID(l.teamSpaceID, preparing.AttachmentID), 5*time.Minute, func(a Attachment) bool { return a.Status == StatusReady })
	if err != nil || !settled || ready.Status != StatusReady || ready.Prepared.JSON.Redactions != 3 {
		t.Fatalf("the restarted preparation answered %+v (%s)", ready, dexflowtest.ErrorCategory(err))
	}
	if runs := extractorRuns(t, runs); runs < 2 {
		t.Fatalf("the extractor ran %d times; the replaced Worker's preparation was not lost and run again", runs)
	}
	for _, object := range []objectstore.ObjectRef{ready.Prepared.Rendition.Ref, ready.Prepared.Redacted.Ref} {
		if versions := versionCount(ctx, t, object.Key); versions != 1 {
			t.Fatalf("%s holds %d versions after the restarted preparation", object.Key, versions)
		}
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, ready.AttachmentID, "cleanup", l.member); err != nil {
		t.Fatal(err)
	}
	l.awaitEnded(ctx, ready.AttachmentID)
	t.Logf("attachment %s READY once after the Worker was replaced during its preparation", ready.AttachmentID)
}

// versionCount is the number of versions, delete markers included, that the
// lane's bucket holds of the full key.
func versionCount(ctx context.Context, t *testing.T, key string) int {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(config, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true })
	listed, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, Prefix: &key})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, version := range listed.Versions {
		if aws.ToString(version.Key) == key {
			count++
		}
	}
	for _, marker := range listed.DeleteMarkers {
		if aws.ToString(marker.Key) == key {
			count++
		}
	}
	return count
}

// TestAttachmentTeamSpaceDeletionDexIntegration deletes a TeamSpace while it
// has a READY attachment and one that awaits its upload, whose PUT already
// landed: the TeamSpace's deletion removes its whole staging prefix, and a
// new upload into the deleting TeamSpace is refused as teamspace_closed. The
// open attachments then end by themselves: the deletion of the READY one
// completes DELETED, its release counting the deleted TeamSpace as released.
func TestAttachmentTeamSpaceDeletionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	l := newLane(ctx, t, testSettings(), teamspaces.StagingLimits{Files: 10, Bytes: 1 << 20}, "")
	ready := l.uploaded(ctx, "ready.md", "text/markdown", []byte("# Ready\n\nThe attachment is ready.\n"))
	if ready.Status != StatusReady {
		t.Fatalf("the READY attachment is %+v with %+v", ready, ready.Failure)
	}
	data := []byte("# Open\n")
	open, target, err := l.declare(ctx, l.member, "open.md", "text/markdown", data)
	if err != nil || target == nil || put(ctx, t, target, data) != http.StatusOK {
		t.Fatalf("stage the open upload: %+v (%s)", open, dexflowtest.ErrorCategory(err))
	}
	if _, err = l.teamSpaces.RequestDeletion(ctx, l.teamSpaceID, teamspaces.DeletionRequest{Actor: l.admin, ID: "delete-teamspace"}); err != nil {
		t.Fatal(err)
	}
	late, _, err := l.declare(ctx, l.member, "late.md", "text/markdown", []byte("late"))
	if code, _, _ := late.PublicFailure(); err != nil || code != failureTeamSpaceClosed.name {
		t.Fatalf("an upload into the deleting TeamSpace answered %+v (%s)", late, dexflowtest.ErrorCategory(err))
	}
	if _, err = l.teamSpaces.Dex.WaitForFlow(ctx, flowid.TeamSpaceFlowID(l.teamSpaceID), dex.WaitForFlowOptions{}); err != nil {
		t.Fatalf("the TeamSpace did not complete its deletion: %v", err)
	}
	if done, err := l.objects.DeletePrefix(ctx, "attachment-staging/"+l.teamSpaceID+"/"); err != nil || !done {
		t.Fatalf("the deleted TeamSpace left staged objects (%v)", err)
	}
	if _, err = l.client.RequestDeletion(ctx, l.teamSpaceID, ready.AttachmentID, "after", l.member); err != nil {
		t.Fatal(err)
	}
	if ended := l.awaitEnded(ctx, ready.AttachmentID); ended.Status != StatusDeleted {
		t.Fatalf("the READY attachment of the deleted TeamSpace ended %+v", ended)
	}
	sum := sha256.Sum256(data)
	if open.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the open upload declared %s", open.SHA256)
	}
	t.Logf("TeamSpace %s deleted with attachments %s (READY) and %s (awaiting) open; its staging prefix is gone", l.teamSpaceID, ready.AttachmentID, open.AttachmentID)
}
