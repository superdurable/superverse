// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	_ "embed"
	"strings"
)

// The model-facing instructions are compiled into the binary; an empty one is
// a build defect. connector_contributor_skill.md is a copy of the skill
// dex-skills/dex-connector-contributor/SKILL.md, which the Coding image ships;
// make generate-go refreshes it and make check-generated finds a stale copy.
//
//go:generate cp ../../../../dex-skills/dex-connector-contributor/SKILL.md prompts/v1/connector_contributor_skill.md
var (
	//go:embed prompts/v1/session.md
	sessionPromptFile string
	//go:embed prompts/v1/source_handoff.md
	sourceHandoffPromptFile string
	//go:embed prompts/v1/platform_event.md
	platformEventPromptFile string
	//go:embed prompts/v1/compaction.md
	compactionPromptFile string
	//go:embed prompts/v1/unfinished_source_handoff.md
	unfinishedSourceHandoffPromptFile string
	//go:embed prompts/v1/output_limit_continuation.md
	outputLimitContinuationPromptFile string
	//go:embed prompts/v1/unsummarized_context.md
	unsummarizedContextPromptFile string
	//go:embed prompts/v1/subagent.md
	subagentPromptFile string
	//go:embed prompts/v1/subagent_application_root.md
	subagentApplicationRootPromptFile string
	//go:embed prompts/v1/subagent_rules.md
	subagentRulesPromptFile string
	//go:embed prompts/v1/subagent_investigate.md
	subagentInvestigatePromptFile string
	//go:embed prompts/v1/subagent_change.md
	subagentChangePromptFile string
	//go:embed prompts/v1/subagent_connector_select.md
	subagentConnectorSelectPromptFile string
	//go:embed prompts/v1/subagent_connector_develop.md
	subagentConnectorDevelopPromptFile string
	//go:embed prompts/v1/connector_contributor_skill.md
	connectorContributorSkillFile string
	//go:embed prompts/v1/remote_agent.md
	remoteAgentPromptFile string
)

var (
	// SessionSystemPrompt is the Session profile plus the source handoff
	// guidance and how to act on delivery notifications.
	SessionSystemPrompt = prompt(sessionPromptFile) + "\n\n" + prompt(sourceHandoffPromptFile) + "\n\n" + prompt(platformEventPromptFile)
	// CompactionInstruction is the final system instruction of a compaction
	// call: the summary inside <summary> tags and no tool call.
	CompactionInstruction = prompt(compactionPromptFile)
	// UnfinishedSourceHandoffInstruction is appended on each continuation of a
	// turn that stopped with changed source and no unblocked commit.
	UnfinishedSourceHandoffInstruction = prompt(unfinishedSourceHandoffPromptFile)
	// OutputLimitContinuation continues a reply that stopped at the output
	// limit without a tool call.
	OutputLimitContinuation = prompt(outputLimitContinuationPromptFile)
	// UnsummarizedContext is the summary of a segment whose earlier context
	// could not be summarized.
	UnsummarizedContext = prompt(unsummarizedContextPromptFile)
	// The system prompts of the subagent kinds: what a subagent is, its root,
	// the rules every subagent follows and the task of its kind. A
	// connector_develop subagent's root is a worktree of the connectors
	// library, and its prompt ends with the connector contributor skill.
	SubagentInvestigatePrompt      = subagentPrompt(prompt(subagentApplicationRootPromptFile), prompt(subagentInvestigatePromptFile))
	SubagentChangePrompt           = subagentPrompt(prompt(subagentApplicationRootPromptFile), prompt(subagentChangePromptFile))
	SubagentConnectorSelectPrompt  = subagentPrompt(prompt(subagentApplicationRootPromptFile), prompt(subagentConnectorSelectPromptFile))
	SubagentConnectorDevelopPrompt = subagentPrompt(prompt(subagentConnectorDevelopPromptFile), "# Connector contributor guide\n\n"+skillBody(connectorContributorSkillFile))
	// RemoteAgentPrompt opens a remote agent's context: how it works through
	// the superverse tools, its messages and the Dex skills.
	RemoteAgentPrompt = strings.ReplaceAll(prompt(remoteAgentPromptFile), "{{sender_envelope_prefix}}", SenderEnvelopePrefix)
)

// subagentPrompt joins what every subagent is, its root, the rules and the
// task of its kind.
func subagentPrompt(root, task string) string {
	return strings.Join([]string{prompt(subagentPromptFile), root, prompt(subagentRulesPromptFile), task}, "\n\n")
}

// skillBody is a skill without its front matter and its title, which the
// prompt replaces with a heading of its own.
func skillBody(file string) string {
	text := prompt(file)
	if rest, hasFrontMatter := strings.CutPrefix(text, "---\n"); hasFrontMatter {
		_, text, _ = strings.Cut(rest, "\n---\n")
	}
	if rest, hasTitle := strings.CutPrefix(strings.TrimSpace(text), "# "); hasTitle {
		_, text, _ = strings.Cut(rest, "\n")
	}
	return prompt(text)
}

func prompt(file string) string {
	text := strings.TrimSpace(file)
	if text == "" {
		panic("an embedded Agent prompt is empty")
	}
	return text
}
