// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package git

import (
	"context"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
)

// VerifyApplicationSource exposes the commit gate to its live scenario, which
// injects the workspace package's real Flow definition check and so lives in
// the external test package.
func (operations *GitOperations) VerifyApplicationSource(ctx context.Context, client *sandboxd.SandboxClient, repository Repository, request CommitRequest, progress func(string)) (SourceGate, error) {
	return operations.verifyApplicationSource(ctx, client, repository, request, progress)
}

// The tests name these constants.
const (
	SourceGatePassedForTest  = sourceGatePassed
	SourceGateSkippedForTest = sourceGateSkipped
)
