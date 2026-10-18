// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import "fmt"

// attachmentIDLength is the length of every attachment ID the server assigns.
const attachmentIDLength = 12

// AttachmentCreationID assigns the ID of one uploaded file reproducibly from
// its uploader's idempotency key, as TeamSpaceCreationID does for a
// TeamSpace: twelve lowercase letters and digits, such as a7f2c9xq4m1p. A
// replay addresses the same AttachmentFlow; its start fingerprint rejects a
// replay that names another file.
func AttachmentCreationID(accountID, requestID string) string {
	return creationID("superverse-v2-attachment-creation-v1", "attachment", accountID, requestID, attachmentIDLength)
}

// AttachmentFlowID addresses the AttachmentFlow of one uploaded file of a
// TeamSpace, for example attachment-k3v9q2ma-a7f2c9xq4m1p. Neither ID holds
// the separator.
func AttachmentFlowID(teamSpaceID, attachmentID string) string {
	return "attachment-" + teamSpaceID + "-" + attachmentID
}

// ValidateAttachmentID checks a caller-supplied attachment ID.
func ValidateAttachmentID(attachmentID string) error {
	if len(attachmentID) != attachmentIDLength || !IsLowerAlphanumeric(attachmentID) {
		return fmt.Errorf("%w: attachment ID must contain %d lowercase letters or digits", ErrInvalidIdentity, attachmentIDLength)
	}
	return nil
}
