// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// WorkspaceRoot is the workspace mount and the image WORKDIR. Kubernetes may
// mount it with an owner other than sandboxd's process UID, so Git must be told
// that it is a safe directory.
const WorkspaceRoot = "/workspace"

// ToolProcessEnvironment returns a fresh environment overlay for a process
// that runs repository code in root, the agent root: the model's commands, the
// commit gate, Preview definition generation and the Preview supervisor.
//
// The repository is a durable PVC rooted at /workspace, which is also the
// image's HOME. HOME must therefore point at the sandbox's ephemeral /tmp:
// otherwise HOME-relative tool state becomes untracked project files that the
// next commit_and_push commits. The image ENV already moves the Go, XDG and npm
// caches under /tmp and owns them. Pagers print to their output instead of
// waiting for a terminal that a command never has.
//
// Agent Sandbox v1.0.1 merges these values over sandboxd's environment for each
// ProcessService Start request, so the overlay needs no new Sandbox API surface.
func ToolProcessEnvironment(root string, redirects ...ModuleRedirect) []EnvironmentVariable {
	// The command-scope safe.directory is protected Git configuration, but
	// unlike a global one it neither changes the workspace nor outlives the process.
	configuration := [][2]string{{"safe.directory", root}}
	environment := []EnvironmentVariable{{Name: "HOME", Value: "/tmp"}, {Name: "PAGER", Value: "cat"}, {Name: "GIT_PAGER", Value: "cat"}}
	var unchecked []string
	for _, redirect := range redirects {
		configuration = append(configuration, [2]string{"url." + redirect.CloneURL + ".insteadOf", "https://" + redirect.ModulePath})
		unchecked = append(unchecked, redirect.ModulePath)
	}
	if len(unchecked) > 0 {
		environment = append(environment, EnvironmentVariable{Name: "GONOSUMDB", Value: strings.Join(unchecked, ",")})
	}
	return append(environment, gitConfiguration(configuration...)...)
}

// ModuleRedirect sends the Git fetches of a Go module's repository to another
// clone URL in the tool environment: in the local lanes the connectors
// library's module goes to the local Git host's copy, which serves it without
// a credential, so that go get of a branch that only the copy holds resolves.
// The module also skips the checksum database, which knows no such branch;
// go.sum still verifies what a module records. Released versions still come
// from the module proxy, which the redirect does not touch.
type ModuleRedirect struct {
	ModulePath string
	CloneURL   string
}

// GitProcessEnvironment is the environment of every Git command the platform
// itself runs in the workspace. The model can write any workspace file, including
// /workspace/.gitconfig (the image HOME) and .git/hooks, so a trusted run reads
// no global or system configuration, runs no hook, asks no credential helper and
// never prompts.
func GitProcessEnvironment() []EnvironmentVariable {
	return gitEnvironment(nil)
}

// GitRemoteEnvironment is GitProcessEnvironment for a command that talks to the
// repository host: it also sends the repository token as an authorization header
// to the URLs below scope, the origin of the clone URL with a trailing slash. The
// token stays inside this one Git subprocess and goes to no other host.
func GitRemoteEnvironment(scope, token string) []EnvironmentVariable {
	return gitEnvironment([][2]string{{"http." + scope + ".extraheader", gitAuthorizationHeader(token)}})
}

// CredentialScope is the origin of a clone URL with a trailing slash, the scope
// the authorization header of GitRemoteEnvironment applies to.
func CredentialScope(cloneURL string) string {
	parsed := comm.Must(url.Parse(cloneURL))
	return parsed.Scheme + "://" + parsed.Host + "/"
}

func gitEnvironment(extra [][2]string) []EnvironmentVariable {
	configuration := append([][2]string{
		{"safe.directory", WorkspaceRoot},
		{"core.hooksPath", "/dev/null"},
		{"credential.helper", ""},
	}, extra...)
	return append([]EnvironmentVariable{
		{Name: "HOME", Value: "/tmp"},
		{Name: "GIT_EDITOR", Value: "true"},
	}, gitConfiguration(configuration...)...)
}

// gitConfiguration isolates Git from system and global configuration, disables
// prompts and supplies the given command-scope configuration entries.
func gitConfiguration(entries ...[2]string) []EnvironmentVariable {
	environment := []EnvironmentVariable{
		{Name: "GIT_CONFIG_NOSYSTEM", Value: "1"},
		{Name: "GIT_CONFIG_GLOBAL", Value: "/dev/null"},
		{Name: "GIT_TERMINAL_PROMPT", Value: "0"},
		{Name: "GIT_ASKPASS", Value: "/bin/false"},
		{Name: "SSH_ASKPASS", Value: "/bin/false"},
		{Name: "GIT_CONFIG_COUNT", Value: strconv.Itoa(len(entries))},
	}
	for index, entry := range entries {
		suffix := strconv.Itoa(index)
		environment = append(environment,
			EnvironmentVariable{Name: "GIT_CONFIG_KEY_" + suffix, Value: entry[0]},
			EnvironmentVariable{Name: "GIT_CONFIG_VALUE_" + suffix, Value: entry[1]},
		)
	}
	return environment
}

// gitAuthorizationHeader is the HTTP header that carries token to GitHub.
func gitAuthorizationHeader(token string) string {
	return "Authorization: Basic " + gitBasicCredential(token)
}

func gitBasicCredential(token string) string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
}

// GitTokenForms are the forms of token that a Git diagnostic could echo: the
// header, its base64 credential and the token itself. Without a token there
// is nothing to redact.
func GitTokenForms(token string) []string {
	if token == "" {
		return nil
	}
	return []string{gitAuthorizationHeader(token), gitBasicCredential(token), token}
}
