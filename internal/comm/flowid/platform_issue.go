// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

// PlatformIssueSignature identifies the kind of a platform fault across
// Sessions: the hex SHA-256 of its owner, tool, code and error type, never of
// a message or a payload, so every Session that meets the same fault has the
// same signature.
func PlatformIssueSignature(owner, tool, code, errorType string) string {
	return Fingerprint(struct{ Owner, Tool, Code, ErrorType string }{owner, tool, code, errorType})
}

// platformIssueSignatureLength is how many hex characters of a signature name
// its PlatformIssueFlow and its GitHub label.
const platformIssueSignatureLength = 16

// PlatformIssueFlowID addresses the PlatformIssueFlow of one signature in one
// environment, for example platform-issue-test-3f9a1c2b7d4e5f60: the first 16
// hex characters of the signature, as the incident's GitHub label carries
// them. It belongs to no Project, so every Session's report of the signature
// reaches the same Flow.
func PlatformIssueFlowID(environment, signature string) string {
	if !IsSHA256Hex(signature) {
		panic("platform issue signature is not a SHA-256 sum: " + signature)
	}
	return "platform-issue-" + environment + "-" + signature[:platformIssueSignatureLength]
}
