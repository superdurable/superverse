// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// TestProjectAttachmentsDexIntegration drives the lifetime attachment quota
// of a throwaway READY Project on the lane's server Worker, whose
// configuration sets the default caps (1,000 attachments and 2 GiB). Each
// admission of a message holds its attachments, which count once: a retry of
// the message under a new admission, and a later message that carries one of
// them again, count only the new ones. Releasing an admission drops only its
// holds, so the release of a refused retry, or of one of two messages that
// carry the same file, frees nothing another admission holds, and a repeat
// finds nothing; an attachment held by more admissions than an instance lists
// stays counted. A message past either cap is RESOURCE_EXHAUSTED and changes
// nothing; and once the Project's deletion is admitted it reserves nothing
// more.
func TestProjectAttachmentsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow := &projects.ProjectFlow{}
	// This client registry supplies the definition only; the existing Worker runs the Flow with its server's caps.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow)
	client := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	projectID := projecttest.CreateForTest(ctx, t, client, t.Name()).ProjectID
	file := func(id string, bytes int64) projects.AttachedFile {
		return projects.AttachedFile{ID: id, Kind: "image", MediaType: "image/png", FileName: id + ".png", SHA256: fmt.Sprintf("%064x", bytes), Bytes: bytes}
	}
	totals := func(want projects.AttachmentTotals, after string) {
		t.Helper()
		project, err := client.Read(ctx, projectID)
		if err != nil || project.AttachmentTotals != want {
			t.Fatalf("after %s the Project counts %+v (%v), want %+v", after, project.AttachmentTotals, err, want)
		}
	}
	reserve := func(messageID, admissionID string, files ...projects.AttachedFile) error {
		return client.ReserveAttachments(ctx, projectID, projects.AttachmentReservation{MessageID: messageID, AdmissionID: admissionID, Files: files})
	}
	release := func(admissionID string, ids ...string) {
		t.Helper()
		if err := client.ReleaseAttachments(ctx, projectID, admissionID, ids); err != nil {
			t.Fatalf("the release of admission %s: %v", admissionID, err)
		}
	}
	requireRejected := func(err error, code dexflow.RejectionCode, what string) {
		t.Helper()
		if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); !isRejected || !rejected.HasCode(code) {
			t.Fatalf("%s answered %v, not the rejection %v", what, err, code)
		}
	}
	totals(projects.AttachmentTotals{}, "the creation")

	a, b, c, d := file("aaaaaaaaaaaa", 100), file("bbbbbbbbbbbb", 200), file("cccccccccccc", 50), file("dddddddddddd", 25)
	for range 2 {
		if err := reserve("message-1", "admission-1", a, b); err != nil {
			t.Fatalf("the first message's reservation: %v", err)
		}
	}
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "a message and a replay of its reservation")
	if err := reserve("message-1", "admission-1-retry", a, b); err != nil {
		t.Fatalf("the reservation of the first message's retry: %v", err)
	}
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "a retry of the message under a new admission")
	// The Session refuses the retry, say while a question waits; the first send still delivers the files.
	release("admission-1-retry", a.ID, b.ID)
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "the release of the refused retry")
	if err := reserve("message-2", "admission-2", b, c); err != nil {
		t.Fatalf("the second message's reservation: %v", err)
	}
	totals(projects.AttachmentTotals{Count: 3, Bytes: 350}, "a message that carries an earlier attachment again")
	release("admission-2", b.ID, c.ID)
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "the release of the second message, which leaves the first's")
	release("admission-2", b.ID, c.ID)
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "a repeated release")

	// Two messages carry the same new file; the refusal of the one that reserved it first frees nothing.
	for _, admission := range []string{"admission-x", "admission-y"} {
		if err := reserve("message-"+admission, admission, d); err != nil {
			t.Fatalf("the reservation of %s: %v", admission, err)
		}
	}
	release("admission-x", d.ID)
	totals(projects.AttachmentTotals{Count: 3, Bytes: 325}, "the release of one of two messages that carry the same file")
	release("admission-y", d.ID)
	totals(projects.AttachmentTotals{Count: 2, Bytes: 300}, "the release of the other one")

	// An attachment held by more admissions than an instance lists is counted for good.
	kept := file("kkkkkkkkkkkk", 10)
	for index := range 9 {
		if err := reserve(fmt.Sprintf("held-%d", index), fmt.Sprintf("held-%d", index), kept); err != nil {
			t.Fatalf("hold %d: %v", index, err)
		}
	}
	for index := range 9 {
		release(fmt.Sprintf("held-%d", index), kept.ID)
	}
	totals(projects.AttachmentTotals{Count: 3, Bytes: 310}, "the release of every admission of a kept attachment")

	requireRejected(reserve("message-3", "admission-3", file("eeeeeeeeeeee", 2<<30)), dexflow.RejectionResourceExhausted, "a message past the byte cap")
	totals(projects.AttachmentTotals{Count: 3, Bytes: 310}, "a refused message")
	for batch := range 100 {
		var files []projects.AttachedFile
		for index := range 10 {
			if number := batch*10 + index; number < 997 {
				files = append(files, file(fmt.Sprintf("f%011d", number), 1))
			}
		}
		if len(files) > 0 {
			if err := reserve(fmt.Sprintf("bulk-%d", batch), fmt.Sprintf("bulk-%d", batch), files...); err != nil {
				t.Fatalf("the reservation of bulk message %d: %v", batch, err)
			}
		}
	}
	totals(projects.AttachmentTotals{Count: 1000, Bytes: 310 + 997}, "the messages up to the count cap")
	requireRejected(reserve("message-4", "admission-4", file("gggggggggggg", 1)), dexflow.RejectionResourceExhausted, "a message past the count cap")
	if err := reserve("message-1", "admission-1-again", a); err != nil {
		t.Fatalf("a retry at the cap of an already reserved attachment: %v", err)
	}

	project, err := client.Read(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.RequestDeletion(ctx, projectID, "delete-"+projectID, project.Revision); err != nil {
		t.Fatalf("the deletion of Project %s was not admitted: %v", projectID, err)
	}
	release("bulk-0", "f00000000000")
	requireRejected(reserve("message-5", "admission-5", file("ffffffffffff", 1)), dexflow.RejectionFailedPrecondition, "a reservation while deleting")
	t.Logf("Project %s counted its attachments, refused past both caps and while deleting", projectID)
}
