// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// A connector_develop subagent works in a worktree of the connectors library,
// which one helper process per operation creates and finishes
// (connector_contribution.py, fixed program text that no model input
// selects). The Sandbox keeps one clone of the library inside the workspace's
// Git directory, which the application's Git commands and the main agent's
// file tools never reach:
//
//	/workspace/.git/superverse/connectors/library                the clone
//	/workspace/.git/superverse/connectors/worktrees/<agent ID>   one worktree per subagent
//	refs/superverse/contributions/<agent ID>/base and /result    in the clone
//	/workspace/.git/superverse/connectors/contributions/<agent ID>.json   the finish's receipt
//
// The clone fetches the library's main before every new branch, from the
// remote the ConnectorLibrary names, and the finish pushes the branch there.

//go:embed connector_contribution.py
var connectorContributionProgram string

// maximumContributionAnswerBytes bounds one helper answer.
const maximumContributionAnswerBytes = 64 << 10

// The fields of every request to the contribution helper: the connector,
// which it checks before the operation, and the library's clone URL.
const (
	slugKey = "slug"
	urlKey  = "url"
)

// ConnectorLibrary is where the contributions of the Session's develop
// subagents go: the branch of a connector, the remote a Sandbox fetches the
// library from and pushes a branch to, with a token minted for that one
// operation, and the identity that commits.
type ConnectorLibrary interface {
	Branch(projectID, slug string) string
	FetchRemote(ctx context.Context) (connectorlibrary.Remote, error)
	PushRemote(ctx context.Context) (connectorlibrary.Remote, error)
	Committer(ctx context.Context) (connectorlibrary.Identity, error)
	PublishLocalReleaseCandidate(ctx context.Context, tag string, assets map[string][]byte) error
}

// ContributionWorktreeRoot is the agent root of the develop subagent agentID:
// its worktree of the connectors library.
func ContributionWorktreeRoot(agentID string) string {
	return path.Join(sandboxd.WorkspaceRoot, ".git", "superverse", "connectors", "worktrees", agentID)
}

// ContributionTarget is the contribution of one develop subagent: the
// subagent, its Session's Project, the connector's slug and the kind.
type ContributionTarget struct {
	AgentID   string                        `json:"agentId"`
	ProjectID string                        `json:"projectId"`
	Slug      string                        `json:"slug"`
	Kind      conversation.ContributionKind `json:"kind"`
}

// ContributionWorktree is a develop subagent's created worktree: the branch it
// is on, the commit it started from and its root.
type ContributionWorktree struct {
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Root   string `json:"worktree"`
}

// ContributionResult is what the finish of a develop subagent's worktree
// did. Commit is the contribution, committed once by the library's committer
// on top of Base as the branch's head, with the connector's Module and its
// diffstat, and Location is where the branch was pushed. Saved is the commit
// that keeps the work of a subagent that did not contribute, off the branch.
// Rejected says why a change that is not one contribution of the connector was
// refused, Unchanged marks a worktree that held no change, and Missing one that
// no longer exists.
type ContributionResult struct {
	Branch      string
	base        string
	Commit      string
	saved       string
	Module      string
	Files       int
	insertions  int
	deletions   int
	Location    connectorlibrary.Location
	Rejected    string
	CheckFailed string
	Unchanged   bool
	Missing     bool
	// ReleaseCandidate is the candidate of the pushed head, or CandidateRefused
	// says why the library's rules refuse one.
	ReleaseCandidate *connectorlibrary.ReleaseCandidate
	CandidateRefused string
}

// CreateContributionWorktree fetches the library's main, and the connector's
// branch when the remote has it, into the Sandbox's clone, and checks the
// branch out as the subagent's worktree: from the remote branch when an
// earlier contribution pushed it, otherwise from main. A recorded base is
// reused, so a repeated creation creates nothing. It is refused
// (workspace_disk_low) when the volume has less than 256 MiB free.
func (operations *GitOperations) CreateContributionWorktree(ctx context.Context, client *sandboxd.SandboxClient, target ContributionTarget) (ContributionWorktree, error) {
	remote, err := operations.Library.FetchRemote(ctx)
	if err != nil {
		return ContributionWorktree{}, sandboxd.MarkTransient(fmt.Errorf("prepare the connectors library: %w", err))
	}
	branch := operations.Library.Branch(target.ProjectID, target.Slug)
	request := map[string]any{"branch": branch, slugKey: target.Slug, urlKey: remote.CloneURL, "minimumFreeBytes": minimumFreeBytes}
	var answer ContributionWorktree
	if err = runContribution(ctx, client, "create", target.AgentID, remote, request, &answer); err != nil {
		return ContributionWorktree{}, err
	}
	if answer.Branch != branch || !flowid.IsCommitSHA(answer.Base) || answer.Root != ContributionWorktreeRoot(target.AgentID) {
		return ContributionWorktree{}, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable,
			fmt.Errorf("the contribution helper created %q on %q at %q, not a commit on %s at %s", answer.Base, answer.Branch, answer.Root, branch, ContributionWorktreeRoot(target.AgentID)))
	}
	return answer, nil
}

// contributionAnswer is the helper's answer to a finish.
type contributionAnswer struct {
	Base        string `json:"base"`
	Commit      string `json:"commit"`
	Module      string `json:"module"`
	Files       int    `json:"files"`
	Insertions  int    `json:"insertions"`
	Deletions   int    `json:"deletions"`
	Pushed      bool   `json:"pushed"`
	PushRefused string `json:"pushRefused"`
	Saved       string `json:"saved"`
	Rejected    string `json:"rejected"`
	CheckFailed string `json:"checkFailed"`
	Unchanged   bool   `json:"unchanged"`
	Missing     bool   `json:"missing"`
}

// FinishContribution verifies the change of the develop subagent's worktree:
// it must touch exactly the connector's module, whose connector.yaml declares
// metadata.name the slug, and for a new connector also add its catalog.yaml
// entry, and nothing else. It commits the change once as the library's
// committer with the subject connector(<slug>): <summary> and no trailer,
// moves the branch to it, pushes the branch without force where the library
// takes pushes and removes the worktree. The helper's receipt makes every step
// happen once: a retry reuses the commit and reads the remote branch before it
// pushes, so a lost answer is reconciled. A change that is not one
// contribution of the connector is kept in the result ref, pushed nowhere and
// reported in Rejected. A subagent that did not complete (contributes false)
// only saves its work, which stays in the result ref, verified and pushed
// nowhere.
func (operations *GitOperations) FinishContribution(ctx context.Context, client *sandboxd.SandboxClient, target ContributionTarget, summary string, contributes bool) (ContributionResult, error) {
	committer, err := operations.Library.Committer(ctx)
	if err != nil {
		return ContributionResult{}, sandboxd.MarkTransient(fmt.Errorf("read the committer of the connectors library: %w", err))
	}
	remote, err := operations.Library.PushRemote(ctx)
	if err != nil {
		return ContributionResult{}, sandboxd.MarkTransient(fmt.Errorf("prepare the push to the connectors library: %w", err))
	}
	branch := operations.Library.Branch(target.ProjectID, target.Slug)
	request := map[string]any{"branch": branch, slugKey: target.Slug, "contribution": target.Kind.String(), "subject": ContributionSubject(target.Slug, summary),
		"name": committer.Name, "email": committer.Email, "push": contributes && remote.Location != connectorlibrary.LocationSandbox, urlKey: remote.CloneURL,
		"publicURL": connectorlibrary.PublicCloneURL, "contribute": contributes}
	var answer contributionAnswer
	if err = runContribution(ctx, client, "finish", target.AgentID, remote, request, &answer); err != nil {
		return ContributionResult{}, err
	}
	for _, secret := range sandboxd.GitTokenForms(remote.Token()) {
		answer.CheckFailed = strings.ReplaceAll(answer.CheckFailed, secret, "[redacted]")
	}
	result := ContributionResult{Branch: branch, base: answer.Base, Commit: answer.Commit, saved: answer.Saved, Files: answer.Files, insertions: answer.Insertions, deletions: answer.Deletions,
		Location: connectorlibrary.LocationSandbox, Rejected: answer.Rejected, CheckFailed: answer.CheckFailed, Unchanged: answer.Unchanged, Missing: answer.Missing}
	switch {
	case answer.Missing || answer.Unchanged || answer.Rejected != "" || answer.CheckFailed != "" || !contributes:
		return result, nil
	case !flowid.IsCommitSHA(answer.Commit) || !flowid.IsCommitSHA(answer.Base) || answer.Module == "":
		return ContributionResult{}, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the contribution helper finished with %q on %q, which is no contribution", answer.Commit, answer.Base))
	case answer.PushRefused != "":
		result.Rejected = "its branch could not be pushed: " + answer.PushRefused
		return result, nil
	}
	result.Module = connectorlibrary.ModulePath + "/" + answer.Module
	if !answer.Pushed {
		return result, nil
	}
	result.Location = remote.Location
	result.ReleaseCandidate, result.CandidateRefused, err = operations.prepareReleaseCandidate(ctx, client, target, remote, answer)
	return result, err
}

// candidateAnswer is the helper's answer to a candidate: the candidate and,
// built for the local Git host, its assets by name, workspace-relative paths.
type candidateAnswer struct {
	connectorlibrary.ReleaseCandidate
	Assets map[string]string `json:"assets"`
}

// maximumCandidateAssetBytes bounds one asset, a release's metadata or Studio UI archive.
const maximumCandidateAssetBytes = 16 << 20

// prepareReleaseCandidate chooses the release candidate of the pushed head and,
// for the local Git host's copy, builds it in the Sandbox with the library's
// own tooling at main, pushes its tag and stores its assets; the upstream
// library's workflow publishes the other candidates. A candidate the library's
// rules refuse, such as a version that is not after the latest release, is
// refused, not failed.
func (operations *GitOperations) prepareReleaseCandidate(ctx context.Context, client *sandboxd.SandboxClient, target ContributionTarget,
	remote connectorlibrary.Remote, finished contributionAnswer) (*connectorlibrary.ReleaseCandidate, string, error) {
	local := remote.Location == connectorlibrary.LocationLocalGitHost
	request := map[string]any{slugKey: target.Slug, "commit": finished.Commit, "module": finished.Module, urlKey: remote.CloneURL, "local": local,
		"publicURL": connectorlibrary.PublicCloneURL}
	var answer candidateAnswer
	err := runContribution(ctx, client, "candidate", target.AgentID, remote, request, &answer)
	var refused *sandboxd.ToolFailure
	if errors.As(err, &refused) && refused.Code() == toolfailure.ReleaseCandidateRefused {
		return nil, refused.Error(), nil
	}
	if err != nil {
		return nil, "", err
	}
	candidate := answer.ReleaseCandidate
	if candidate.Commit != finished.Commit || candidate.Directory != finished.Module || candidate.Tag != candidate.Directory+"/"+candidate.Version {
		return nil, "", sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the contribution helper chose the candidate %s of %s, not one of %s", candidate.Tag, candidate.Commit, finished.Commit))
	}
	if !local {
		return &candidate, "", nil
	}
	assets := make(map[string][]byte, len(answer.Assets))
	for name, path := range answer.Assets {
		read, readErr := client.ReadFile(ctx, sandboxd.ReadFileRequest{Path: sandboxd.WorkspacePath(path), Limit: maximumCandidateAssetBytes})
		if readErr != nil {
			return nil, "", fmt.Errorf("read the release candidate asset %s: %w", name, readErr)
		}
		if !read.EOF {
			return nil, "", sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the release candidate asset %s exceeds %d bytes", name, maximumCandidateAssetBytes))
		}
		assets[name] = read.Data
	}
	if _, built := assets[connectorlibrary.ReleaseMetadataAsset]; !built {
		return nil, "", sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the contribution helper built no %s for %s", connectorlibrary.ReleaseMetadataAsset, candidate.Tag))
	}
	if err = operations.Library.PublishLocalReleaseCandidate(ctx, candidate.Tag, assets); err != nil {
		return nil, "", sandboxd.MarkTransient(fmt.Errorf("store the release candidate %s on the local Git host: %w", candidate.Tag, err))
	}
	return &candidate, "", nil
}

// ContributionSubject is the subject of a contribution's commit and the title
// of its pull request, the form the library requires.
func ContributionSubject(slug, summary string) string {
	return "connector(" + slug + "): " + summary
}

// contributionRefusals are the codes of the helper's refusals.
var contributionRefusals = map[string]toolfailure.Code{"workspace_disk_low": toolfailure.WorkspaceDiskLow, "release_candidate_refused": toolfailure.ReleaseCandidateRefused}

// runContribution runs one helper operation for agentID against remote with
// request on its stdin and decodes its answer: a refusal is the tool failure
// of its code, a failure in reaching the remote is transient, any other
// helper failure a platform tool_error, and an answer outside the contract a
// platform fault.
func runContribution(ctx context.Context, client *sandboxd.SandboxClient, operation, agentID string, remote connectorlibrary.Remote, request, answer any) error {
	if !flowid.IsAgentID(agentID) {
		panic("a contribution worktree needs an agent ID that flowid.AgentTaskID derived, not " + agentID)
	}
	output, err := client.RunRemoteHelperJSON(ctx, []string{"python3", "-c", connectorContributionProgram, operation, sandboxd.WorkspaceRoot, agentID},
		remote.CloneURL, remote.Token(), comm.Must(json.Marshal(request)), maximumContributionAnswerBytes)
	if err != nil {
		return err
	}
	var refusal worktreeRefusal
	if json.Unmarshal(output, &refusal) == nil && refusal.Refusal != "" {
		code, isKnown := contributionRefusals[refusal.Refusal]
		if !isKnown {
			return sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the contribution helper refused %s with the unknown reason %q", operation, refusal.Refusal))
		}
		return sandboxd.NewToolFailure(code, fmt.Errorf("subagent %s: %s", agentID, refusal.Detail))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(answer); err != nil {
		return sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the contribution helper answered %s with an unreadable answer: %w", operation, err))
	}
	return nil
}
