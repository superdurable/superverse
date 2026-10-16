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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

const (
	gitCommandTimeout     = 5 * time.Minute
	maximumGitOutputBytes = 1 << 20
	maximumGitDetailBytes = 4096
)

// committerFlags give every platform commit and rebase one process-scoped
// identity, independent of any Git configuration.
var committerFlags = []string{"-c", "user.name=SuperVerse Agent", "-c", "user.email=agent@superdurable.dev"}

// Repository is the Project repository a Session workspace checks out, as the
// accepted Lease records it. CloneURL is where the Sandbox clones it from, and
// the origin of that URL is the only host the repository token is sent to.
type Repository struct {
	ProjectID       string `json:"projectId"`
	InstallationID  int64  `json:"installationId"`
	RepositoryID    int64  `json:"repositoryId"`
	Owner           string `json:"owner"`
	Name            string `json:"name"`
	CloneURL        string `json:"cloneUrl"`
	DefaultBranch   string `json:"defaultBranch"`
	ExpectedHeadSHA string `json:"expectedHeadSha,omitempty"`
}

// GitOperations is the platform's repository protocol in a workspace. It mints
// tokens through credentials and never reads a Flow. CheckDefinitions is the
// commit gate's Flow definition stage, and Library the connectors library that
// develop subagents contribute to.
type GitOperations struct {
	Credentials      *HostCredentials
	CheckDefinitions WorktreeDefinitionCheck
	Library          ConnectorLibrary
}

// repositoryGit binds the Git runner of one workspace to the repository token.
func (operations *GitOperations) repositoryGit(ctx context.Context, client *sandboxd.SandboxClient, repository Repository) (workspaceGit, error) {
	credential, err := operations.Credentials.repositoryCredential(ctx, tokenRequest{installationID: repository.InstallationID, repositoryID: repository.RepositoryID})
	if err != nil {
		return workspaceGit{}, fmt.Errorf("mint repository token: %w", err)
	}
	return workspaceGit{client: client, token: credential.token, scope: sandboxd.CredentialScope(repository.CloneURL)}, nil
}

func validateRepository(repository Repository) error {
	if repository.ProjectID == "" || repository.InstallationID <= 0 || repository.RepositoryID <= 0 {
		return errors.New("repository identity is incomplete")
	}
	for name, value := range map[string]string{
		"repository owner": repository.Owner,
		"repository name":  repository.Name,
		"default branch":   repository.DefaultBranch,
	} {
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	if repository.ExpectedHeadSHA != "" && !flowid.IsCommitSHA(repository.ExpectedHeadSHA) {
		return errors.New("expected repository HEAD is invalid")
	}
	if cloneURL, err := url.Parse(repository.CloneURL); err != nil || (cloneURL.Scheme != "http" && cloneURL.Scheme != "https") || cloneURL.Host == "" ||
		cloneURL.User != nil || cloneURL.RawQuery != "" || cloneURL.Fragment != "" {
		return errors.New("repository clone URL is invalid")
	}
	return nil
}

// workspaceGit runs the platform's own commands in one workspace. Every Git
// command runs in the trusted Git environment; only the commands that talk to
// the repository host carry the repository token, scoped to the host's origin.
type workspaceGit struct {
	client *sandboxd.SandboxClient
	token  string
	scope  string
}

// git runs one local Git command.
func (g workspaceGit) git(ctx context.Context, arguments ...string) (processResult, error) {
	return g.run(ctx, sandboxd.GitProcessEnvironment(), gitCommandTimeout, append([]string{"git"}, arguments...)...)
}

// gitRemote runs one Git command that talks to GitHub with the token, which
// its diagnostics never show. A failure of such a command, whether the process
// exited nonzero or the transport failed, is transient: the network, GitHub or
// the token endpoint may recover, and each caller queries before it repeats an
// effect. A decision it makes from the result, such as a rejected push, is typed
// by the caller and stays typed.
func (g workspaceGit) gitRemote(ctx context.Context, arguments ...string) (processResult, error) {
	result, err := g.run(ctx, sandboxd.GitRemoteEnvironment(g.scope, g.token), gitCommandTimeout, append([]string{"git"}, arguments...)...)
	result.secrets = sandboxd.GitTokenForms(g.token)
	if err != nil {
		err = sandboxd.MarkTransient(err)
	}
	return result, err
}

// step runs one local Git command whose output is not needed and reports a
// failure as action.
func (g workspaceGit) step(ctx context.Context, action string, arguments ...string) error {
	result, err := g.git(ctx, arguments...)
	if err != nil {
		return commandError(action, result, err)
	}
	return nil
}

// remoteStep is step for a command that talks to GitHub.
func (g workspaceGit) remoteStep(ctx context.Context, action string, arguments ...string) error {
	result, err := g.gitRemote(ctx, arguments...)
	if err != nil {
		return commandError(action, result, err)
	}
	return nil
}

// processResult is the output of one platform command. secrets are the token
// forms its environment carried, which every report of its output redacts.
type processResult struct {
	stdout, stderr string
	exitCode       int
	secrets        []string
}

// redact replaces every secret in output. Reports redact before truncating: a
// size boundary must not leave a token prefix that no longer matches.
func (result processResult) redact(output string) string {
	for _, secret := range result.secrets {
		output = strings.ReplaceAll(output, secret, "[REDACTED]")
	}
	return output
}

// run executes one trusted command to its exit. A nonzero exit is an error
// that keeps the exit code and output in the result.
func (g workspaceGit) run(ctx context.Context, environment []sandboxd.EnvironmentVariable, timeout time.Duration, arguments ...string) (processResult, error) {
	output, err := g.client.RunToExit(ctx, sandboxd.StartRequest{
		ExecutionID:        sandboxd.PlatformExecutionID("direct-git"),
		Arguments:          arguments,
		Environment:        environment,
		Timeout:            timeout,
		MaximumOutputBytes: maximumGitOutputBytes,
	})
	result := processResult{stdout: output.Stdout, stderr: output.Stderr, exitCode: output.ExitCode}
	if err == nil && output.ExitCode != 0 {
		err = fmt.Errorf("git exited with code %d", output.ExitCode)
	}
	return result, err
}

// remoteHead reads the GitHub head of branch; found is false when the branch
// does not exist.
func (g workspaceGit) remoteHead(ctx context.Context, branch string) (string, bool, error) {
	result, err := g.gitRemote(ctx, "ls-remote", "--exit-code", "origin", "refs/heads/"+branch)
	if err != nil {
		if result.exitCode == 2 {
			return "", false, nil
		}
		return "", false, commandError("inspect remote branch", result, err)
	}
	fields := strings.Fields(result.stdout)
	if len(fields) != 2 || !flowid.IsCommitSHA(fields[0]) {
		return "", false, errors.New("remote branch response is invalid")
	}
	return fields[0], true, nil
}

func (g workspaceGit) localHead(ctx context.Context) (string, error) {
	result, err := g.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", commandError("resolve local HEAD", result, err)
	}
	sha := strings.TrimSpace(result.stdout)
	if !flowid.IsCommitSHA(sha) {
		return "", errors.New("git returned an invalid local HEAD")
	}
	return sha, nil
}

// isAncestor keeps Git's negative answer apart from a failed command.
func (g workspaceGit) isAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	result, err := g.git(ctx, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if result.exitCode == 1 {
		return false, nil
	}
	return false, commandError("inspect Git ancestry", result, err)
}

// commandFailureError is a failed Git command, shown as the redacted head of
// its output. Its cause stays reachable for classification and is never shown.
type commandFailureError struct {
	message string
	cause   error
}

func (failure *commandFailureError) Error() string { return failure.message }
func (failure *commandFailureError) Unwrap() error { return failure.cause }

// commandError reports a failed Git command with the redacted head of its
// output: Git states its error first.
func commandError(action string, result processResult, err error) error {
	detail := strings.TrimSpace(result.stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.stdout)
	}
	if detail == "" {
		detail = err.Error()
	}
	return &commandFailureError{message: fmt.Sprintf("%s: %s", action, comm.TruncateUTF8(result.redact(detail), maximumGitDetailBytes)), cause: err}
}

// commandTail reports a failed build command by the redacted ends of its
// output, within about limit bytes: a build explains its failure last. make
// echoes each command to stdout while compilers write to stderr, so both ends
// are kept, and stdout keeps at least a third of the budget.
func commandTail(result processResult, limit int) string {
	stdout := result.redact(strings.TrimSpace(result.stdout))
	stderr := result.redact(strings.TrimSpace(result.stderr))
	stdout = comm.TailUTF8(stdout, max(limit/3, limit-len(stderr)))
	stderr = comm.TailUTF8(stderr, limit-len(stdout))
	var sections []string
	if stdout != "" {
		sections = append(sections, "stdout (end):\n"+stdout)
	}
	if stderr != "" {
		sections = append(sections, "stderr (end):\n"+stderr)
	}
	return strings.Join(sections, "\n")
}
