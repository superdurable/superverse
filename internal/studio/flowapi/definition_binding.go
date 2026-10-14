// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"strings"

	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// An execution is bound to the definitions it started under through its Start
// RequestID, which Dex stores in the request memo. Summary, Display, edit and
// Action interpret only executions bound to the current definitions; others
// keep engine diagnostics.
const (
	studioStartRequestPrefix = "sv2-start-v1."
	definitionCurrent        = "CURRENT"
	definitionDifferent      = "DIFFERENT"
	definitionUnavailable    = "UNAVAILABLE"
)

// boundStartRequestID binds a Start's logical fingerprint to its definition
// revision. The pinned SDK accepts any non-empty RequestID; this one is exactly
// 142 ASCII bytes: the prefix, the definition SHA-256, a dot and the
// fingerprint SHA-256, both lowercase hex.
func boundStartRequestID(definitionRevision, logicalDigest string) string {
	return studioStartRequestPrefix + strings.TrimPrefix(definitionRevision, "sha256:") + "." + logicalDigest
}

// startRequestBinding reports how an execution's RequestID binds it to the
// current definition revision, and the revision it names.
func startRequestBinding(requestID, currentRevision string) (binding, revision string) {
	if !strings.HasPrefix(requestID, studioStartRequestPrefix) {
		return definitionUnavailable, ""
	}
	parts := strings.Split(strings.TrimPrefix(requestID, studioStartRequestPrefix), ".")
	if len(parts) != 2 || !flowid.IsSHA256Hex(parts[0]) || !flowid.IsSHA256Hex(parts[1]) {
		return definitionUnavailable, ""
	}
	revision = "sha256:" + parts[0]
	if revision != currentRevision {
		return definitionDifferent, revision
	}
	return definitionCurrent, revision
}

type executionDefinitionError struct{}

func (executionDefinitionError) Error() string {
	return "This execution's Flow Definition is unavailable; use engine diagnostics"
}

func requireCurrentDefinition(requestID, currentRevision string) error {
	if binding, _ := startRequestBinding(requestID, currentRevision); binding != definitionCurrent {
		return executionDefinitionError{}
	}
	return nil
}
