// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"slices"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
)

// ReadPreviewTarget returns the AppManifest of the source the Project's
// Preview prepared for the Session's HEAD. It reads stored state alone, the
// HEAD from the Session's retained summary and the preparation from storage,
// so it never starts a Sandbox and Session liveness does not gate it. A HEAD
// without a preparation is preview.ErrSourceNotPrepared. The configuration
// host authorized the Project before this read.
func (s *SessionFlowService) ReadPreviewTarget(ctx context.Context, project string) (*pb.AppManifestSnapshot, error) {
	summary, err := s.Agent.GetSessionSummary(ctx, agentloop.SessionFlowID(project))
	if err != nil {
		return nil, err
	}
	head := summary.Identity.HeadCommit
	if head == "" {
		return nil, preview.ErrSourceNotPrepared
	}
	return s.Previews.ReadPreparedSource(ctx, project, head)
}

// ReadAppliedPreviewConfiguration returns the configuration object the
// Project's Preview last applied, or the zero ref before the first Apply or
// for a Project that never ran a Preview command. The caller authorized the
// Project before this read.
func (s *SessionFlowService) ReadAppliedPreviewConfiguration(ctx context.Context, project string) (objectstore.ObjectRef, error) {
	state, err := s.readPreview(ctx, project)
	if err != nil || state == nil {
		return objectstore.ObjectRef{}, err
	}
	return state.Applied.Object(), nil
}

// ReadPreviewValidation returns the VALIDATED record of the newest READY
// Preview start of commit, a start or an automatic deployment, or nil when the
// Preview's RecentStarts holds none, for instance once it was dropped. The
// Release client authorized the Project before this read.
func (s *SessionFlowService) ReadPreviewValidation(ctx context.Context, project, commit string) (*releases.PreviewValidation, error) {
	state, err := s.readPreview(ctx, project)
	if err != nil || state == nil {
		return nil, err
	}
	for _, start := range slices.Backward(state.RecentStarts) {
		if start.Commit == commit && start.Outcome == preview.StartReady {
			return &releases.PreviewValidation{Kind: releases.PreviewValidated, StartCommandID: start.CommandID, StartedAt: start.EndedAt}, nil
		}
	}
	return nil, nil
}

// ReadRecentPreviewStarts returns the starts the Project's Preview recorded,
// oldest first and bounded, or none for a Project that never ran a Preview
// command. The caller authorized the Project before this read.
func (s *SessionFlowService) ReadRecentPreviewStarts(ctx context.Context, project string) ([]preview.StartRecord, error) {
	state, err := s.readPreview(ctx, project)
	if err != nil || state == nil {
		return nil, err
	}
	return state.RecentStarts, nil
}
