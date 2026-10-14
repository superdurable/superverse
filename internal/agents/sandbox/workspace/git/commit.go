// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

var turnBaseRefPattern = regexp.MustCompile(`^refs/superverse/turns/[0-9a-f]{64}$`)

// CommitResult is the remote default-branch HEAD verified to equal the local
// HEAD, and the source gate that admitted it, empty for a blocked handoff. The
// caller records CommitSHA; no Flow is told from inside the workspace.
type CommitResult struct {
	CommitSHA       string
	previousHeadSHA string
	created         bool
	SourceGate      SourceGate
}

// CommitAndPush commits every workspace change of the call request.CallID and
// publishes the local HEAD to the default branch without force. Unless the
// call hands off a blocked checkpoint, the source gate runs first and reports
// each stage to progress.
//
// Git ancestry, not callID, decides what is published: the workspace may hold
// commits of earlier calls. Local behind remote is a checked fast-forward,
// local ahead is a push, and divergence rebases and then asks the Agent to
// verify again before anything is pushed. Remote reads before and after the
// push reconcile concurrent writes and lost responses.
func (operations *GitOperations) CommitAndPush(ctx context.Context, client *sandboxd.SandboxClient, repository Repository, request CommitRequest, progress func(string)) (CommitResult, error) {
	if err := validateRepository(repository); err != nil {
		return CommitResult{}, err
	}
	var gate SourceGate
	if !request.Blocked {
		var err error
		if gate, err = operations.verifyApplicationSource(ctx, client, repository, request, progress); err != nil {
			return CommitResult{}, err
		}
	}
	g, err := operations.repositoryGit(ctx, client, repository)
	if err != nil {
		return CommitResult{}, err
	}
	result, err := g.commitAndPush(ctx, repository.DefaultBranch, request.CallID, request.Summary)
	result.SourceGate = gate
	return result, err
}

func (g workspaceGit) commitAndPush(ctx context.Context, branch, callID, summary string) (CommitResult, error) {
	g.clearStaleTurnBases(ctx, callID)
	rebaseActive, err := g.isRebaseActive(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	if rebaseActive {
		return CommitResult{}, g.continueMainRebase(ctx)
	}
	current, err := g.git(ctx, "branch", "--show-current")
	if err != nil || strings.TrimSpace(current.stdout) != branch {
		return CommitResult{}, errors.New("workspace is not on the default branch")
	}
	if err = g.step(ctx, "stage workspace changes", "add", "--all"); err != nil {
		return CommitResult{}, err
	}
	quiet, quietErr := g.git(ctx, "diff", "--cached", "--quiet")
	created := quietErr != nil && quiet.exitCode == 1
	if quietErr != nil && !created {
		return CommitResult{}, commandError("inspect staged changes", quiet, quietErr)
	}
	if created {
		if err = g.step(ctx, "commit workspace changes", append(committerFlags, "commit", "-m", commitMessage(summary, callID))...); err != nil {
			return CommitResult{}, err
		}
	}
	previousHead, createdByTurn, err := g.turnBase(ctx, callID, created)
	if err != nil {
		return CommitResult{}, err
	}
	if err = g.remoteStep(ctx, "fetch default branch", "fetch", "--prune", "origin"); err != nil {
		return CommitResult{}, err
	}
	remote, err := g.existingRemoteHead(ctx, branch)
	if err != nil {
		return CommitResult{}, fmt.Errorf("inspect remote default branch: %w", err)
	}
	local, err := g.localHead(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	if remote == local {
		return CommitResult{CommitSHA: local, previousHeadSHA: previousHead, created: createdByTurn}, nil
	}
	localBehind, err := g.isAncestor(ctx, local, remote)
	if err != nil {
		return CommitResult{}, err
	}
	if localBehind {
		return g.fastForward(ctx, local, remote)
	}
	remoteBehind, err := g.isAncestor(ctx, remote, local)
	if err != nil {
		return CommitResult{}, err
	}
	if !remoteBehind {
		if base, mergeErr := g.git(ctx, "merge-base", local, remote); mergeErr != nil {
			if base.exitCode == 1 {
				return CommitResult{}, sandboxd.NewToolFailure(toolfailure.GitRemoteConflict, errors.New("workspace and remote have unrelated histories")).WithEffect(toolfailure.EffectPartial)
			}
			return CommitResult{}, commandError("inspect common Git ancestor", base, mergeErr)
		}
		return CommitResult{}, g.rebaseOntoMain(ctx, remote)
	}
	if err = g.push(ctx, branch, local, remote); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{CommitSHA: local, previousHeadSHA: previousHead, created: createdByTurn}, nil
}

// turnBase returns the HEAD before the commit of callID and whether callID
// authored the current HEAD, also when it did so in an earlier attempt.
func (g workspaceGit) turnBase(ctx context.Context, callID string, created bool) (string, bool, error) {
	createdByTurn := created
	if !createdByTurn {
		message, err := g.git(ctx, "log", "-1", "--format=%B")
		if err != nil {
			return "", false, commandError("inspect current commit", message, err)
		}
		createdByTurn = hasTurnMarker(message.stdout, callID)
	}
	if !createdByTurn {
		head, err := g.localHead(ctx)
		return head, false, err
	}
	base, err := g.resolveTurnBase(ctx, callID)
	return base, true, err
}

// fastForward moves a local branch that is behind to the observed remote commit.
func (g workspaceGit) fastForward(ctx context.Context, local, remote string) (CommitResult, error) {
	if err := g.step(ctx, "fast-forward default branch", "merge", "--ff-only", remote); err != nil {
		return CommitResult{}, err
	}
	merged, err := g.localHead(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	if merged != remote {
		return CommitResult{}, errors.New("fast-forward did not reach the observed remote commit")
	}
	return CommitResult{CommitSHA: remote, previousHeadSHA: local}, nil
}

// push publishes local over remote, its ancestor, and confirms the result.
func (g workspaceGit) push(ctx context.Context, branch, local, remote string) error {
	confirmed, err := g.existingRemoteHead(ctx, branch)
	if err != nil {
		return fmt.Errorf("reverify remote default branch: %w", err)
	}
	if confirmed != remote {
		if err = g.remoteStep(ctx, "refresh default branch", "fetch", "--prune", "origin"); err != nil {
			return err
		}
		return g.rebaseOntoMain(ctx, confirmed)
	}
	if pushed, pushErr := g.gitRemote(ctx, "push", "--porcelain", "origin", "HEAD:refs/heads/"+branch); pushErr != nil {
		if err = g.reconcileFailedPush(ctx, branch, local, remote, commandError("push default branch", pushed, pushErr), pushRejected(pushed, branch)); err != nil {
			return err
		}
	}
	confirmed, found, err := g.remoteHead(ctx, branch)
	if err != nil {
		return fmt.Errorf("confirm pushed branch: %w", err)
	}
	if !found || confirmed != local {
		return errors.New("remote default branch did not converge to the pushed commit")
	}
	return nil
}

// reconcileFailedPush reads the branch after a push that reported failure. It
// returns nil only when the push landed after all. An unchanged branch is not a
// rebase condition: only Git's machine-readable reject flag proves a terminal
// rejection, and transport ambiguity stays an uncertain error to reconcile.
func (g workspaceGit) reconcileFailedPush(ctx context.Context, branch, local, remote string, failure error, rejected bool) error {
	after, found, err := g.remoteHead(ctx, branch)
	switch {
	case err != nil:
		return errors.Join(failure, err)
	case !found:
		return failure
	case after == local:
		return nil
	case after == remote && rejected:
		return sandboxd.NewToolFailure(toolfailure.GitPushRejected, failure).WithEffect(toolfailure.EffectPartial)
	case after == remote:
		return failure
	}
	if err = g.remoteStep(ctx, "refresh default branch", "fetch", "--prune", "origin"); err != nil {
		return errors.Join(failure, err)
	}
	return g.rebaseOntoMain(ctx, after)
}

// pushRejected reads git push --porcelain status, never localized error text.
func pushRejected(result processResult, branch string) bool {
	if result.exitCode == 0 {
		return false
	}
	for _, line := range strings.Split(result.stdout, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || fields[0] != "!" {
			continue
		}
		if _, target, ok := strings.Cut(fields[1], ":"); ok && target == "refs/heads/"+branch {
			return true
		}
	}
	return false
}

// clearStaleTurnBases deletes the recovery refs of earlier turns. The current
// turn keeps its ref, so a retried call still finds its base; a later call
// removes it, which is after the caller durably recorded this result. Failures
// are ignored: resolveTurnBase falls back to HEAD^ and no push depends on it.
func (g workspaceGit) clearStaleTurnBases(ctx context.Context, callID string) {
	listed, err := g.git(ctx, "for-each-ref", "--format=%(refname)", "refs/superverse/turns/")
	if err != nil {
		return
	}
	current := turnBaseRef(callID)
	for _, ref := range strings.Fields(listed.stdout) {
		if ref != current && turnBaseRefPattern.MatchString(ref) {
			_, _ = g.git(ctx, "update-ref", "-d", ref)
		}
	}
}

// resolveTurnBase reads the recorded base of callID's commit, recording HEAD^
// the first time.
func (g workspaceGit) resolveTurnBase(ctx context.Context, callID string) (string, error) {
	ref := turnBaseRef(callID)
	stored, storedErr := g.git(ctx, "rev-parse", "--verify", ref)
	if storedErr == nil {
		sha := strings.TrimSpace(stored.stdout)
		if !flowid.IsCommitSHA(sha) {
			return "", errors.New("stored turn base is invalid")
		}
		return sha, nil
	}
	if stored.exitCode != 1 && stored.exitCode != 128 {
		return "", commandError("inspect turn base", stored, storedErr)
	}
	parent, err := g.git(ctx, "rev-parse", "HEAD^")
	if err != nil {
		return "", commandError("resolve commit parent", parent, err)
	}
	sha := strings.TrimSpace(parent.stdout)
	if !flowid.IsCommitSHA(sha) {
		return "", errors.New("commit parent is invalid")
	}
	if err = g.step(ctx, "record turn base", "update-ref", ref, sha); err != nil {
		return "", err
	}
	return sha, nil
}

func turnBaseRef(callID string) string {
	return "refs/superverse/turns/" + turnMarker(callID)
}

func (g workspaceGit) isRebaseActive(ctx context.Context) (bool, error) {
	result, err := g.git(ctx, "rev-parse", "--verify", "REBASE_HEAD")
	if err == nil {
		return true, nil
	}
	if result.exitCode == 1 || result.exitCode == 128 {
		return false, nil
	}
	return false, commandError("inspect rebase state", result, err)
}

// continueMainRebase completes a rebase the Agent resolved. Like every rebase,
// it ends in main_reverification_required or the remaining conflicts: nothing
// is pushed before the Agent verified the rebased source.
func (g workspaceGit) continueMainRebase(ctx context.Context) error {
	if err := g.step(ctx, "stage resolved rebase changes", "add", "--all"); err != nil {
		return err
	}
	return g.rebase(ctx, "continue main rebase", "--continue")
}

// rebaseOntoMain rebases the local commits onto the remote head.
func (g workspaceGit) rebaseOntoMain(ctx context.Context, remote string) error {
	return g.rebase(ctx, "rebase onto main", remote)
}

func (g workspaceGit) rebase(ctx context.Context, action, argument string) error {
	result, err := g.git(ctx, append(committerFlags, "rebase", argument)...)
	if err == nil {
		return mainReverificationRequired()
	}
	if conflict := g.rebaseConflict(ctx); conflict != nil {
		return conflict
	}
	return commandError(action, result, err)
}

func mainReverificationRequired() error {
	return sandboxd.NewToolFailure(toolfailure.MainReverificationRequired, errors.New("main changed; rerun the applicable checks, then call commit_and_push again")).WithEffect(toolfailure.EffectPartial)
}

func (g workspaceGit) rebaseConflict(ctx context.Context) error {
	paths, err := g.git(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return commandError("inspect rebase conflicts", paths, err)
	}
	conflicts := boundedLines(paths.stdout, 64, func(line string) bool { return line != "" && !strings.ContainsRune(line, '\x00') })
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return sandboxd.NewToolFailure(toolfailure.MainRebaseConflict, fmt.Errorf("main rebase has unresolved conflicts: %s", strings.Join(conflicts, ", "))).WithEffect(toolfailure.EffectPartial)
}

// commitMessage keeps the summary's first line within 72 bytes, without
// splitting a UTF-8 character, and appends the turn marker of callID.
func commitMessage(summary, callID string) string {
	line := strings.TrimSpace(strings.SplitN(summary, "\n", 2)[0])
	if line == "" {
		line = "Update project"
	}
	return comm.TruncateUTF8(line, 72) + "\n\nSuperVerse-Turn: " + turnMarker(callID)
}

func hasTurnMarker(message, callID string) bool {
	marker := "SuperVerse-Turn: " + turnMarker(callID)
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		if line == marker {
			return true
		}
	}
	return false
}

func turnMarker(callID string) string {
	digest := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(digest[:])
}
