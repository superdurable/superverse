// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"context"

	"github.com/superdurable/superversev2/internal/comm/platform/githubissues"
)

// PlatformIssueFlowDependency is the issue repository PlatformIssueFlow files
// in: a githubissues.Repository, or githubissues.NotConfigured where the
// server names no issue repository. EnsureIssue queries the label before it
// writes, so a repeated attempt finds the labeled issue an earlier one opened;
// LabelIssue attaches the label to an issue GitHub opened without it, and a
// repeat changes nothing.
type PlatformIssueFlowDependency interface {
	EnsureIssue(ctx context.Context, issue githubissues.Issue) (githubissues.Filed, error)
	LabelIssue(ctx context.Context, number int64, label string) error
}
