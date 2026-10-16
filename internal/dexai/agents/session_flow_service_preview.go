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
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

func (s *SessionFlowService) GetPreview(ctx context.Context, account, project string) (preview.Snapshot, error) {
	summary, err := s.readAuthorizedSession(ctx, account, project)
	if err != nil {
		return preview.Snapshot{}, err
	}
	state, err := s.readPreview(ctx, project)
	if err != nil {
		return preview.Snapshot{}, err
	}
	return previewView(summary, state), nil
}

// ReadPreviewServing reads what a Preview Runs request needs of the Project's
// Session: its retained summary, its instance and the Project's Preview. It
// reads no conversation and no connector contribution, so Agent,
// Contributions and ExecutionFailure of the result stay empty. The caller
// authorized the account on the Project, and the Session's liveness does not
// gate the read, as the stopped Session's snapshot is readable too.
func (s *SessionFlowService) ReadPreviewServing(ctx context.Context, project string) (Snapshot, error) {
	started := time.Now()
	summary, err := s.Agent.GetSessionSummary(ctx, agentloop.SessionFlowID(project))
	if err != nil {
		s.logSessionReadFailure(ctx, "read_session_summary", started, err)
		return Snapshot{}, err
	}
	started = time.Now()
	workspaceState, err := s.readWorkspace(ctx, project)
	if err != nil {
		s.logSessionReadFailure(ctx, "read_session_workspace", started, err)
		return Snapshot{}, err
	}
	started = time.Now()
	previewState, err := s.readPreview(ctx, project)
	if err != nil {
		s.logSessionReadFailure(ctx, "read_preview", started, err)
		return Snapshot{}, err
	}
	return Snapshot{Session: summary, Workspace: workspaceState, PreviewState: previewState}, nil
}

// previewView is the Preview one Session read reports: the Project's
// PreviewFlow's state, or NOT_STARTED in the first-demand form, generation,
// revision and instance all empty, while the Project never ran a Preview
// command.
func previewView(session agentloop.SessionSummary, state *preview.Snapshot) preview.Snapshot {
	if state != nil {
		return *state
	}
	return preview.Snapshot{
		Identity:       sandbox.Identity{ProjectID: session.Identity.ProjectID},
		Preview:        preview.PreviewState{Status: preview.PreviewNotStarted},
		PreparedSource: preview.PreparedPreviewSource{Status: preview.PreviewSourceNotPrepared},
	}
}

// isFirstDemand reports whether command observed no Preview: the form that
// starts the Project's Preview with its first command.
func isFirstDemand(command preview.Command) bool {
	return command.WorkspaceRequestID == "" && command.Generation == 0 && command.ExpectedRevision == 0
}

// PreviewCommand sends a start-preview or stop-preview command to the
// Project's Preview, which acquires the Session's workspace for a start.
func (s *SessionFlowService) PreviewCommand(ctx context.Context, account, project string, command preview.Command) (preview.Snapshot, error) {
	summary, err := s.readAuthorizedSession(ctx, account, project)
	if err != nil {
		return preview.Snapshot{}, err
	}
	switch command.Kind {
	case preview.CommandStartPreview:
		return s.sendAcquiringCommand(ctx, summary, command)
	case preview.CommandStopPreview:
		// A Project without a Preview has nothing to stop, and the read's
		// FlowNotFoundError says so.
		if _, err = s.Previews.Get(ctx, project); err != nil {
			return preview.Snapshot{}, err
		}
		return s.Previews.Command(ctx, project, command)
	default:
		panic("unsupported Preview command " + string(command.Kind))
	}
}

func (s *SessionFlowService) PreparePreviewSource(ctx context.Context, account, project string, command preview.Command) error {
	summary, err := s.readAuthorizedSession(ctx, account, project)
	if err != nil {
		return err
	}
	_, err = s.sendAcquiringCommand(ctx, summary, command)
	return err
}

// sendAcquiringCommand sends a command that runs on the Session's current
// instance, which the Preview acquires from the ACTIVE Session. The first
// command, in the first-demand form, starts the Preview; a retry of it
// repeats that form, which the Preview recognizes by its ID. Any other
// command names the Preview it observed, with no instance while the Preview
// never acquired one, as after a refused first command, and the Preview
// compares it.
func (s *SessionFlowService) sendAcquiringCommand(ctx context.Context, summary agentloop.SessionSummary, command preview.Command) (preview.Snapshot, error) {
	if summary.Status != agentloop.SessionStatusActive {
		return preview.Snapshot{}, ErrSessionNotActive
	}
	project := summary.Identity.ProjectID
	if isFirstDemand(command) {
		state, err := s.readPreview(ctx, project)
		if err != nil {
			return preview.Snapshot{}, err
		}
		if state != nil && state.ActiveCommand.ID != command.ID && state.LastCommand.ID != command.ID {
			// The Preview exists, so the caller's observation is stale.
			return *state, preview.ErrConflict
		}
		if state == nil {
			s.logColdPath(ctx, "StartPreview", project)
			if err = s.Previews.Start(ctx, sandbox.Identity{ProjectID: project}); err != nil {
				return preview.Snapshot{}, err
			}
		}
		command.Generation = sandbox.InstanceGeneration
	}
	return s.Previews.Command(ctx, project, command)
}

func (s *SessionFlowService) GetPreviewDefinitions(ctx context.Context, account, project string) (preview.Snapshot, []appmanifest.FlowDefinition, error) {
	if _, err := s.readAuthorizedSession(ctx, account, project); err != nil {
		return preview.Snapshot{}, nil, err
	}
	return s.Previews.ReadDefinitions(ctx, project)
}
