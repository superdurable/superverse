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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

const (
	templateManifestPath  = ".superverse/template.json"
	platformSkillManifest = "/opt/superverse/dex-skills/dex-app-builder/SKILL.md"
	maximumAgentContext   = 8 << 10
	// The template's AGENTS.md is the file map; keep room for the header lines.
	maximumAgentInstructions = 6 << 10
)

var (
	// makeCommandPattern is the only command shape a template may declare.
	makeCommandPattern   = regexp.MustCompile(`^make [a-z][a-z0-9-]*$`)
	topLevelEntryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

// templateManifest is the committed .superverse/template.json of a template
// repository: its build profile, file map and declared make targets.
type templateManifest struct {
	SchemaVersion     int               `json:"schemaVersion"`
	BuildProfile      string            `json:"buildProfile"`
	OpenAPISpec       string            `json:"openapiSpec"`
	AgentInstructions string            `json:"agentInstructions"`
	Commands          map[string]string `json:"commands"`
}

// errTemplateManifestUnreadable marks a template manifest that could not be
// read at HEAD, as opposed to one that was read but does not decode.
var errTemplateManifestUnreadable = errors.New("committed template manifest is unreadable")

// readTemplateManifest reads and decodes the template manifest at HEAD. Each
// caller validates the fields it relies on.
func (g workspaceGit) readTemplateManifest(ctx context.Context) (templateManifest, error) {
	result, err := g.git(ctx, "show", "HEAD:"+templateManifestPath)
	if err != nil {
		return templateManifest{}, fmt.Errorf("%w: %w", errTemplateManifestUnreadable, commandError("read committed template manifest", result, err))
	}
	var manifest templateManifest
	if err := json.Unmarshal([]byte(result.stdout), &manifest); err != nil {
		return templateManifest{}, fmt.Errorf("decode committed template manifest: %w", err)
	}
	return manifest, nil
}

// bootstrapCommand is the command of .superverse/template.json that restores
// the dependencies, which the platform starts when it checks a workspace out.
const bootstrapCommand = "bootstrap"

// DeclaredBootstrap reads the bootstrap command of HEAD's template manifest:
// its arguments, or none when HEAD has no manifest or the manifest declares no
// bootstrap command. refusal says why a declared command cannot be started.
func (operations *GitOperations) DeclaredBootstrap(ctx context.Context, client *sandboxd.SandboxClient) (arguments []string, refusal string, err error) {
	shown, err := workspaceGit{client: client}.git(ctx, "show", "HEAD:"+templateManifestPath)
	switch {
	case err != nil && shown.exitCode != 0:
		return nil, "", nil
	case err != nil:
		return nil, "", commandError("read committed template manifest", shown, err)
	}
	var manifest templateManifest
	if err = json.Unmarshal([]byte(shown.stdout), &manifest); err != nil {
		return nil, "the committed template manifest does not decode", nil
	}
	command, declared := manifest.Commands[bootstrapCommand]
	switch {
	case !declared:
		return nil, "", nil
	case !makeCommandPattern.MatchString(command):
		return nil, fmt.Sprintf("the template manifest declares the unsupported bootstrap command %q", comm.TruncateUTF8(command, 128)), nil
	}
	return strings.Fields(command), "", nil
}

// RepositoryContext derives bounded, trusted prompt context from the checked-out
// HEAD: the top-level entries, the template's declared paths and commands, and
// its AGENTS.md file map.
func (operations *GitOperations) RepositoryContext(ctx context.Context, client *sandboxd.SandboxClient) (string, error) {
	g := workspaceGit{client: client}
	head, err := g.localHead(ctx)
	if err != nil {
		return "", err
	}
	listed, err := g.git(ctx, "ls-tree", "--name-only", "HEAD")
	if err != nil {
		return "", commandError("inspect repository structure", listed, err)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Trusted repository context generated from the current checkout.\nHEAD: %s\n", head)
	if entries := boundedLines(listed.stdout, 64, topLevelEntryPattern.MatchString); len(entries) > 0 {
		fmt.Fprintf(&text, "Top-level entries: %s\n", strings.Join(entries, ", "))
	}
	manifest, manifestErr := g.readTemplateManifest(ctx)
	if errors.Is(manifestErr, errTemplateManifestUnreadable) {
		fmt.Fprintln(&text, "Template manifest: not present.")
		return comm.TruncateUTF8(text.String(), maximumAgentContext), nil
	}
	if manifestErr != nil || manifest.SchemaVersion != 1 {
		return "", errors.New("template manifest at current HEAD is invalid")
	}
	if manifest.AgentInstructions != "AGENTS.md" || manifest.OpenAPISpec != "openapi/openapi.yaml" || manifest.BuildProfile != "go-react-v1" {
		return "", errors.New("template manifest contains unsupported context paths")
	}
	commands := make([]string, 0, len(manifest.Commands))
	for _, command := range manifest.Commands {
		if !makeCommandPattern.MatchString(command) {
			return "", errors.New("template manifest contains an unsupported command")
		}
		commands = append(commands, command)
	}
	sort.Strings(commands)
	fmt.Fprintf(&text, "Template: profile=%s.\nOpenAPI: %s. Agent instructions: %s (included below).\nCommands: %s.\n",
		manifest.BuildProfile, manifest.OpenAPISpec, manifest.AgentInstructions, strings.Join(commands, ", "))
	fmt.Fprintf(&text, "Dex skill: platform-managed at %s; before using a feature the repository reference does not show, read the skill section AGENTS.md maps to it with the read tool. Generate clients after API changes. commit_and_push runs the final source gate: checkStatic, the platform Flow-definition check and the testApplication API tests with the requirement trace; use focused checks beforehand rather than repeating that full gate. Real provider/deployment acceptance is separate.\n",
		platformSkillManifest)
	// Inline the repository's own file map so the Agent starts from the exact
	// files to change. It is repository data at HEAD, bounded like any read.
	if instructions, err := g.git(ctx, "show", "HEAD:"+manifest.AgentInstructions); err == nil {
		fileMap := instructions.stdout
		if len(fileMap) > maximumAgentInstructions {
			fileMap = comm.TruncateUTF8(fileMap, maximumAgentInstructions) + "\n[AGENTS.md truncated; read the rest with read]\n"
		}
		fmt.Fprintf(&text, "\n--- %s at HEAD ---\n%s", manifest.AgentInstructions, fileMap)
	}
	return comm.TruncateUTF8(text.String(), maximumAgentContext), nil
}

// boundedLines returns at most limit trimmed lines of value that keep accepts.
func boundedLines(value string, limit int, keep func(string) bool) []string {
	var result []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); keep(line) {
			result = append(result, line)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}
