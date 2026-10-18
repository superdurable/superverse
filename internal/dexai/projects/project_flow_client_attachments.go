// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// ReserveAttachments counts the attachments of the message in.MessageID
// against the Project's lifetime quota under the admission in.AdmissionID,
// with exactly their Attachments instances loaded. An attachment is counted
// once: a retried or duplicated message, or a later message that carries it
// again, only holds it too. A refusal is a *dexflow.Rejection:
// RESOURCE_EXHAUSTED past the quota, FAILED_PRECONDITION for a Project that is
// not READY, such as one being deleted.
func (c *ProjectFlowClient) ReserveAttachments(ctx context.Context, projectID string, in AttachmentReservation) error {
	ids := make([]string, 0, len(in.Files))
	for _, file := range in.Files {
		ids = append(ids, file.ID)
	}
	return c.invokeAttachments(ctx, projectID, c.Flow.ReserveProjectAttachments, in, ids)
}

// ReleaseAttachments drops the holds of the admission admissionID, after a
// definite refusal of that admission's send; an attachment that another
// admission holds, which another message or an earlier send of the same
// message may have delivered, stays counted.
func (c *ProjectFlowClient) ReleaseAttachments(ctx context.Context, projectID, admissionID string, attachmentIDs []string) error {
	return c.invokeAttachments(ctx, projectID, c.Flow.ReleaseProjectAttachments, attachmentRelease{AdmissionID: admissionID, AttachmentIDs: attachmentIDs}, attachmentIDs)
}

// invokeAttachments calls an attachment RPC under the Project's write lock
// with the Attachments instances of ids loaded, and returns its rejection as
// the error.
func (c *ProjectFlowClient) invokeAttachments(ctx context.Context, projectID string, rpc, in any, ids []string) error {
	loads := make([]dex.AttributeMapLoad, 0, len(ids))
	for _, id := range ids {
		loads = append(loads, ProjectAttachments.Load(id))
	}
	result, err := dexflow.InvokeLockedWith[attachmentResult](ctx, c.Dex, memberLockWait, flowid.ProjectFlowID(projectID), rpc, in,
		dex.RPCInvokeOptions{LoadAttributeMapInstances: loads})
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}
