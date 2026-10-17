// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// readConnectorChecks fixes the Project from the Session identity, looks up
// only that Project's contribution and reads the current head from GitHub.
func (d *ProjectDiagnostics) readConnectorChecks(ctx context.Context, identity agentloop.SessionIdentity, arguments string) ([]byte, error) {
	input, err := decodeArguments(toolinput.ReadConnectorChecks.Decode, arguments)
	if err != nil {
		return nil, err
	}
	snapshot, err := d.Contributions.Snapshot(ctx, contribution.Identity{ProjectID: identity.ProjectID, Slug: input.Slug})
	if err != nil {
		return nil, notFound(err, "connector contribution")
	}
	redaction := newRedactor(identity.ProjectID, d.Redactor)
	if snapshot.Record.HeadCommit == "" || snapshot.Record.Location == connectorlibrary.LocationSandbox {
		return redaction.encode(map[string]any{"slug": input.Slug, "available": false, "reason": "the contribution has no published head"}), nil
	}
	checks, err := d.ConnectorChecks.ReadHeadChecks(ctx, snapshot.Record.HeadCommit)
	if errors.Is(err, connectorlibrary.ErrNotConfigured) {
		return redaction.encode(map[string]any{"slug": input.Slug, "head": snapshot.Record.HeadCommit,
			"available": false, "reason": "this environment has no upstream GitHub connector library"}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the connector's current CI status: %w", err)
	}
	if checks.State == connectorlibrary.HeadChecksFailed {
		checks, err = d.ConnectorChecks.ReadFailedHeadJobs(ctx, checks)
		if err != nil {
			return nil, fmt.Errorf("read the connector's failed CI jobs: %w", err)
		}
	}
	output := map[string]any{"slug": input.Slug, "available": true, "checks": checks}
	if candidate := snapshot.Record.ReleaseCandidate; candidate != nil && snapshot.Record.Location == connectorlibrary.LocationUpstream {
		run, runErr := d.ConnectorChecks.ReadReleaseCandidateRun(ctx, candidate.ReleaseCandidate, snapshot.Record.Location, snapshot.Record.HeadRecordedAt)
		if runErr != nil {
			return nil, fmt.Errorf("read the connector's release-candidate run: %w", runErr)
		}
		output["release_candidate_run"] = run
	}
	return redaction.encode(output), nil
}
