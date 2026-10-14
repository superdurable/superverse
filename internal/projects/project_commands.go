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
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/platform/repohost"
)

// A Project runs one command at a time: Live removal or deletion. Admission
// makes it the active command; AwaitProjectCommands then runs its phase Steps
// in order, each committed by CommitProjectPhase.

// The phases of a command, as ProjectCommand.Phase records them: each phase
// Step, by its Step type, and the states before, after and instead of them.
const (
	phaseRemoveLiveServing                = "RemoveLiveServing"
	phaseRevokeDeploymentGrant            = "RevokeDeploymentGrant"
	phaseCommitLiveAbsent                 = "CommitLiveAbsent"
	phaseStopAndCleanChildren             = "StopAndCleanChildren"
	phaseDestroyLiveDataAndInfrastructure = "DestroyLiveDataAndInfrastructure"
	phaseReleaseConnectionUsages          = "ReleaseConnectionUsages"
	phaseReleaseRepositoryBinding         = "ReleaseRepositoryBinding"
	phaseArchiveManagedRepository         = "ArchiveManagedRepository"
	phaseCompleteDeletion                 = "CompleteDeletion"
	phaseQueued                           = "QUEUED"
	phaseFailed                           = "FAILED"
	phaseComplete                         = "COMPLETE"
)

// projectPhases lists the phase Steps of each command kind in order.
var projectPhases = map[string][]string{
	CommandRemoveLive: {phaseRemoveLiveServing, phaseRevokeDeploymentGrant, phaseCommitLiveAbsent},
	CommandDeleteProject: {phaseRemoveLiveServing, phaseRevokeDeploymentGrant, phaseCommitLiveAbsent, phaseStopAndCleanChildren,
		phaseDestroyLiveDataAndInfrastructure, phaseReleaseConnectionUsages, phaseArchiveManagedRepository, phaseReleaseRepositoryBinding, phaseCompleteDeletion},
}

const (
	// cleanupBudget is the wait limit of one cleanup phase that waits for its
	// effect to converge; the engine enforces it through PollingStepOptions.
	cleanupBudget = 30 * time.Minute
	// cleanupPollInterval spaces the rounds of a converging cleanup phase.
	cleanupPollInterval = 2 * time.Second
	// cleanupRoundTimeout bounds one round of a cleanup phase, which makes
	// several cloud, Kubernetes and child Flow calls.
	cleanupRoundTimeout = 40 * time.Second
	// cleanupBatch is how many children one StopAndCleanChildren round handles.
	cleanupBatch = 8
)

// projectPhaseInput is the Step input of every command phase. Failure carries
// a classified failure to RecordProjectFailure.
type projectPhaseInput struct {
	Command ProjectCommand
	Phase   int
	Failure *dexflow.Failure `json:",omitempty"`
}

// requestCommand admits one command at a time. A command's ID is the caller's
// request ID and its identity: a repeated ID answers with the current state,
// and a failed command restarts under its own ID. Admitted cleanup keeps its
// ID even when a client lost it.
func (f *ProjectFlow) requestCommand(ctx dex.Context, in ProjectCommand) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	continuingCleanup := in.Kind == CommandDeleteProject && p.ProjectStatus == StatusDeleting || in.Kind == CommandRemoveLive && p.ProjectStatus == StatusReady
	if continuingCleanup && p.ActiveCommand != nil && p.ActiveCommand.Kind == in.Kind {
		in.ID = p.ActiveCommand.ID
	}
	for _, previous := range []*ProjectCommand{p.ActiveCommand, p.LastCommand} {
		if previous == nil || previous.ID != in.ID {
			continue
		}
		if previous.Kind != in.Kind {
			return f.reject(ctx, dexflow.RejectionAlreadyExists, "command identity conflicts")
		}
		if previous != p.ActiveCommand || previous.Phase != phaseFailed {
			return f.result(ctx, nil)
		}
		if in.ExpectedRevision != p.Revision {
			return f.result(ctx, dexflow.RejectStale("failure revision changed"))
		}
		comm.MustDo(ProjectLastFailure.Set(ctx, nil))
		queueProjectCommand(ctx, in)
		result, err := f.result(ctx, nil)
		// RecordProjectFailure ended the command wait; restart it.
		result.NextSteps = []dex.StepMovement{dex.MovementOf(AwaitProjectCommands{flow: f}, nil)}
		return result, err
	}
	switch {
	case in.Kind == CommandRemoveLive && p.ActiveDeployment != nil:
		return f.reject(ctx, dexflow.RejectionAborted, "deployment is in progress")
	case !p.AdmitsChildren():
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "project admission is frozen")
	case p.Revision != in.ExpectedRevision:
		return f.result(ctx, dexflow.RejectStale("project revision changed"))
	case p.ActiveCommand != nil:
		return f.reject(ctx, dexflow.RejectionAborted, "project has an active command")
	}
	if in.Kind == CommandDeleteProject {
		comm.MustDo(ProjectStatus.Set(ctx, StatusDeleting))
	}
	queueProjectCommand(ctx, in)
	return f.result(ctx, nil)
}

// queueProjectCommand makes command the active command and hands it to
// AwaitProjectCommands.
func queueProjectCommand(ctx dex.Context, command ProjectCommand) {
	command.Phase = phaseQueued
	comm.MustDo(ProjectActiveCommand.Set(ctx, &command))
	comm.MustDo(ProjectCommands.Publish(ctx, command))
	touchProject(ctx)
}

// cleanupStepOptions are the options of a cleanup phase that waits for its
// effect to converge: one long Execute without locks, whose exhausted retries
// or expired wait limit move to onFailure.
func cleanupStepOptions(onFailure *dex.ExecuteFailure) *dex.StepOptions {
	return dexflow.PollingStepOptions(cleanupBudget, onFailure)
}

// cleanupPhases are the cleanup phases that wait for their effect to converge
// under cleanupStepOptions.
var cleanupPhases = []string{phaseRemoveLiveServing, phaseRevokeDeploymentGrant, phaseStopAndCleanChildren, phaseDestroyLiveDataAndInfrastructure, phaseArchiveManagedRepository}

// awaitRepositoryArchive reconciles uncertain repository host outcomes in the same
// polling attempt. Every call reads the owned repository before archiving it;
// a lost response can therefore converge without repeating a confirmed archive.
// Backoff is disposable and cancellation-aware, capped at eight seconds inside
// the 40-second round. The engine alone owns the overall cleanup wait limit.
func awaitRepositoryArchive(ctx dex.Context, effect func(context.Context) error) (*dexflow.Failure, error) {
	var backoff time.Duration
	return awaitPhase(ctx, "archive the managed repository", func(callCtx context.Context) error {
		if backoff > 0 {
			timer := time.NewTimer(backoff)
			defer timer.Stop()
			select {
			case <-callCtx.Done():
				return callCtx.Err()
			case <-timer.C:
			}
		}
		err := effect(callCtx)
		if callCtx.Err() != nil {
			return err
		}
		if errors.Is(err, repohost.ErrOutcomeUnknown) {
			backoff = min(8*time.Second, max(2*time.Second, backoff*2))
			return fmt.Errorf("%w: %w", ErrCleanupPending, err)
		}
		return err
	})
}

// awaitPhase runs a cleanup phase's effect until it converges and returns the
// classified Failure that ends the phase, if any. ErrCleanupPending and a call
// that went unanswered while the round goes on, such as a Kubernetes call or a
// deployment read past its own wait, are another round. An unclassified error
// ends the attempt with a stack, and the Step's retry policy starts the phase
// over; the engine ends a phase still pending at its wait limit, which
// RecordProjectFailure records as CLEANUP_TIMEOUT.
func awaitPhase(ctx dex.Context, operation string, effect func(context.Context) error) (*dexflow.Failure, error) {
	var failure *dexflow.Failure
	err := dexflow.PollUntil(ctx, cleanupPollInterval, cleanupRoundTimeout, func(callCtx context.Context) (bool, error) {
		err := effect(callCtx)
		if errors.Is(err, ErrCleanupPending) || unansweredCall(callCtx, err) {
			return false, nil
		}
		if err == nil {
			return true, nil
		}
		// PollUntil keeps a round its timeout cut off pending; classified, the
		// error of the call it cut off could end the phase instead.
		if callCtx.Err() == nil {
			if failure = classifyProjectFailure(err); failure != nil {
				return true, nil
			}
		}
		return false, fmt.Errorf("%s: %w", operation, err)
	})
	return failure, err
}

// classifyPhaseError turns the error of a command phase's effect into the
// classified Failure the phase records, or else into an error with a stack
// that the Step's retry policy handles.
func classifyPhaseError(operation string, err error) (*dexflow.Failure, error) {
	if err == nil {
		return nil, nil
	}
	if failure := classifyProjectFailure(err); failure != nil {
		return failure, nil
	}
	return nil, dex.ErrorWithStack(fmt.Errorf("%s: %w", operation, err))
}

// runsCommand reports whether command is still the Project's active one; a
// command replaced or completed meanwhile leaves its phase Steps idle.
func (p Project) runsCommand(command ProjectCommand) bool {
	return p.ActiveCommand != nil && p.ActiveCommand.ID == command.ID
}
