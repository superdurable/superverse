// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package workspace

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

// previewDefinitionsProgram generates strict FDG 2.0 of the declared Flows from
// an isolated archive: of the clean HEAD for a manual Preview preparation, of a
// pinned commit and its exported source tree for an automatic deployment, or
// of the tree the next commit would hold for the commit gate. It is fixed
// program text; no model input selects what it runs.
//
//go:embed preview_definitions.py
var previewDefinitionsProgram string

const (
	previewDefinitionsTimeout  = 4 * time.Minute
	worktreeDefinitionsTimeout = 2 * time.Minute
	// platformFailureExitCode is the generator's exit code for a failure of the
	// platform's own preparation, such as a source tree that is not the
	// commit's export, which asking again may repair. Every other nonzero exit
	// rejects the source.
	platformFailureExitCode = 2
)

// ErrPreviewDefinitionsRejected means the generator rejected the source: it
// changed, or it declares a Flow without a valid strict definition. The error
// carries the generator's reason; generating the same commit again fails the
// same way.
var ErrPreviewDefinitionsRejected = errors.New("preview source generation failed")

type PreviewDefinition struct {
	SourcePath string          `json:"sourcePath"`
	Graph      json.RawMessage `json:"graph"`
}

// PreviewDefinitions is the generator's answer: the definitions and, for a
// pinned commit, the digest of the dex-app.yaml of its exported source tree.
type PreviewDefinitions struct {
	Definitions    []PreviewDefinition `json:"definitions"`
	ManifestDigest string              `json:"manifestDigest"`
}

// PreparePreviewDefinitions generates the Flow Definitions of commit, which
// must still be the clean HEAD of the workspace, for the preparation requestID.
// A rejected source is ErrPreviewDefinitionsRejected; any other error leaves
// the outcome unknown and the generation may run again.
func (c *Capabilities) PreparePreviewDefinitions(ctx context.Context, binding Binding, requestID, commit string) ([]PreviewDefinition, error) {
	answer, err := c.generateDefinitions(ctx, binding, requestID, commit, commit)
	return answer.Definitions, err
}

// PreparePinnedPreviewDefinitions generates the Flow Definitions of commit,
// which need not be HEAD, for the deployment requestID, and returns them with
// the digest of the dex-app.yaml of tree, the commit's exported source tree
// that superverse-previewctl source ensure answered. It reads no working tree.
// A rejected source is ErrPreviewDefinitionsRejected; a tree that is not the
// commit's export, and any other error, may be asked again.
func (c *Capabilities) PreparePinnedPreviewDefinitions(ctx context.Context, binding Binding, requestID, tree, commit string) (PreviewDefinitions, error) {
	if tree == "" {
		return PreviewDefinitions{}, errors.New("a pinned preparation requires the commit's source tree")
	}
	return c.generateDefinitions(ctx, binding, requestID, commit, "--pinned", tree, commit)
}

// generateDefinitions runs the generator for the preparation requestID of
// commit with the mode's arguments and reads its answer.
func (c *Capabilities) generateDefinitions(ctx context.Context, binding Binding, requestID, commit string, mode ...string) (PreviewDefinitions, error) {
	if !flowid.IsCommitSHA(commit) || requestID == "" {
		return PreviewDefinitions{}, errors.New("exact Preview source identity is required")
	}
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewDefinitions, error) {
		executionID := sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: binding.FlowID, CallID: "preview-definitions/" + requestID + "/" + commit})
		output, err := runDefinitionsProgram(ctx, client, executionID, previewDefinitionsTimeout, mode...)
		if err != nil {
			return PreviewDefinitions{}, err
		}
		// The generator prints its reason last; the source is the user's own.
		reason := comm.TailUTF8(strings.TrimSpace(output.Stderr), maximumDiagnosticTailBytes)
		switch output.ExitCode {
		case 0:
		case platformFailureExitCode:
			return PreviewDefinitions{}, fmt.Errorf("the Flow definition generator could not prepare %s: %s", commit, reason)
		default:
			return PreviewDefinitions{}, fmt.Errorf("%w: exit code %d: %s", ErrPreviewDefinitionsRejected, output.ExitCode, reason)
		}
		var answer PreviewDefinitions
		if err := json.Unmarshal([]byte(output.Stdout), &answer); err != nil {
			return PreviewDefinitions{}, fmt.Errorf("%w: unreadable generator output: %w", ErrPreviewDefinitionsRejected, err)
		}
		if len(answer.Definitions) == 0 {
			return PreviewDefinitions{}, fmt.Errorf("%w: the source declares no Flow Definitions", ErrPreviewDefinitionsRejected)
		}
		return answer, nil
	})
}

// checkWorktreeDefinitions is the commit gate's git.WorktreeDefinitionCheck:
// the same generator renders the tree that committing the working tree would
// create, without staging anything, and returns the tree and its Flows. A
// rejection wraps git.ErrFlowDefinitionsRejected with the generator's complete
// reason, which the gate bounds.
func checkWorktreeDefinitions(ctx context.Context, client *sandboxd.SandboxClient) (git.RenderedSource, error) {
	output, err := runDefinitionsProgram(ctx, client, sandboxd.PlatformExecutionID("worktree-definitions"), worktreeDefinitionsTimeout, "--worktree")
	if err != nil {
		return git.RenderedSource{}, err
	}
	if output.ExitCode != 0 {
		return git.RenderedSource{}, fmt.Errorf("%w: exit code %d: %s", git.ErrFlowDefinitionsRejected, output.ExitCode, strings.TrimSpace(output.Stderr))
	}
	var rendered git.RenderedSource
	if err = json.Unmarshal([]byte(output.Stdout), &rendered); err != nil {
		return git.RenderedSource{}, fmt.Errorf("read the rendered Flows: %w", err)
	}
	if !flowid.IsCommitSHA(rendered.Tree) { // a tree ID has the form of a commit ID
		return git.RenderedSource{}, fmt.Errorf("the Flow definition generator named the invalid tree %q", rendered.Tree)
	}
	return rendered, nil
}

// runDefinitionsProgram runs the generator in one mode. It runs repository
// tooling, so it gets the tool environment. A nonzero exit is the generator's
// answer, not an error.
func runDefinitionsProgram(ctx context.Context, client *sandboxd.SandboxClient, executionID sandboxd.ExecutionID, timeout time.Duration, mode ...string) (sandboxd.ProcessOutput, error) {
	output, err := client.RunToExit(ctx, sandboxd.StartRequest{
		ExecutionID: executionID,
		Arguments:   append([]string{"python3", "-c", previewDefinitionsProgram}, mode...),
		Environment: sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot),
		Timeout:     timeout,
	})
	if err != nil {
		return output, fmt.Errorf("run the Flow definition generator: %w", err)
	}
	return output, nil
}

// CheckWorktreeDefinitionsForTest is checkWorktreeDefinitions for the tests.
func CheckWorktreeDefinitionsForTest(ctx context.Context, client *sandboxd.SandboxClient) (git.RenderedSource, error) {
	return checkWorktreeDefinitions(ctx, client)
}
