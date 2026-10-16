// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// RequestedTimer is the durable wait a start_timer call asks for.
func RequestedTimer(call conversation.ToolCall, startedAt time.Time) (PendingTimer, error) {
	input, err := toolinput.StartTimer.Decode(call.Arguments.String())
	if err != nil {
		return PendingTimer{}, invalidToolArguments(call, err)
	}
	return PendingTimer{CallID: call.ID, DurationSeconds: int64(input.DurationSeconds), Reason: strings.TrimSpace(input.Reason), StartedAt: &startedAt}, nil
}

// RequestedSubagent is the subagent a start_agent call asks for.
func RequestedSubagent(call conversation.ToolCall) (agenttasks.SubagentCall, error) {
	input, err := toolinput.StartAgent.Decode(call.Arguments.String())
	if err != nil {
		return agenttasks.SubagentCall{}, invalidToolArguments(call, err)
	}
	kind, isKind := agenttasks.ParseSubagentKind(string(input.Kind))
	if !isKind {
		panic(fmt.Sprintf("the start_agent schema admits the unknown kind %q", input.Kind))
	}
	requested := agenttasks.SubagentCall{Kind: kind, Description: strings.TrimSpace(input.Description), Prompt: input.Prompt}
	switch {
	case kind.ContributesConnector() && (!input.Slug.Set || !input.Contribution.Set):
		return agenttasks.SubagentCall{}, invalidToolArguments(call, fmt.Errorf("kind %s needs slug and contribution", kind))
	case !kind.ContributesConnector() && (input.Slug.Set || input.Contribution.Set):
		return agenttasks.SubagentCall{}, invalidToolArguments(call, fmt.Errorf("slug and contribution belong to kind %s only", agenttasks.SubagentConnectorDevelop))
	case kind.ContributesConnector():
		var contribution conversation.ContributionKind
		comm.MustDo(contribution.UnmarshalText([]byte(input.Contribution.Value)))
		requested.Connector = &agenttasks.ConnectorTarget{Slug: input.Slug.Value, Contribution: contribution}
	}
	return requested, nil
}

func invalidToolArguments(call conversation.ToolCall, err error) error {
	return fmt.Errorf("tool %q has invalid arguments: %w", call.Name, err)
}

// WorkspaceAccess is immutable admission metadata. It describes the
// capability required by an accepted tool call, never model intent.
type WorkspaceAccess string

const (
	WorkspaceAccessNone     WorkspaceAccess = "none"
	WorkspaceAccessReadOnly WorkspaceAccess = "read_only"
	WorkspaceAccessMutating WorkspaceAccess = "mutating"
)

// A tool that may be repeated is retried by Dex only after a transient fault
// (sandboxd.IsTransient): four attempts, two seconds apart at first, doubling
// up to thirty seconds, within the attempt timeout plus three minutes. Any
// other failure is a result the model reads, and a command is attempted once.
const (
	toolRepeatableAttempts      = 4
	ToolRetryInitialInterval    = 2 * time.Second
	ToolRetryBackoffCoefficient = 2
	ToolRetryMaximumInterval    = 30 * time.Second
	toolRetryGrace              = 3 * time.Minute
)

// ToolDefinition binds the provider-visible spec of a tool to its immutable
// execution policy.
type ToolDefinition struct {
	conversation.ToolSpec
	// Run is nil for a built-in handled by a SessionAgentFlow Step and for a
	// platform tool.
	Run sandboxtools.Tool
	// Platform marks a read-only tool that the server runs for the Session's
	// own Project through the Session's ProjectDiagnosticsReader, without the
	// workspace.
	Platform bool
	// RemoteOnly offers this tool to the connected executor, not the platform model.
	RemoteOnly bool
	// strict has the provider guarantee that the model's input follows the
	// input schema. The provider takes a subset of JSON Schema for a strict
	// tool, so the bounds a schema declares stay with the tool's decoder, which
	// validates the input after the tool_use completes. Platform tools are never
	// strict; the catalog marks two more tools loose (see loose).
	strict bool
	// eagerInputStreaming streams a long input as the model writes it, so that
	// the content of write and edit does not wait for the whole tool_use and
	// Studio shows a requirements card's draft while the model writes it.
	eagerInputStreaming bool
	WorkspaceAccess     WorkspaceAccess
	// RequiresApproval routes the call through AwaitToolApproval. No current
	// catalog tool sets it; the path stays because tool approval is a retained
	// Agent capability.
	RequiresApproval bool
	// LongRunning makes the call's Execute SYNC instead of inheriting the
	// Session's ASYNC default, so it starts as a regular execution without an
	// ASYNC local phase.
	LongRunning bool
	// uninterruptible keeps a running call when the user presses Stop: its
	// effect must complete or be reconciled by the tool itself, so the turn
	// ends at the next boundary instead.
	uninterruptible           bool
	AttemptTimeout            time.Duration
	MaximumAttempts           int
	RetryTotalDuration        time.Duration
	supportsParallelExecution bool
}

// UsesWorkspace reports whether the tool is dispatched on the Session lease.
func (definition ToolDefinition) UsesWorkspace() bool {
	return definition.WorkspaceAccess != WorkspaceAccessNone
}

// IsBuiltIn reports whether a SessionAgentFlow Step runs the tool itself.
func (definition ToolDefinition) IsBuiltIn() bool {
	return definition.Run == nil && !definition.Platform
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// validate rejects a catalog entry the provider or the Dex retry policy
// cannot accept.
func (definition ToolDefinition) validate() error {
	var schema struct {
		Type                 string `json:"type"`
		AdditionalProperties *bool  `json:"additionalProperties"`
	}
	switch {
	case !toolNamePattern.MatchString(string(definition.Name)):
		return errors.New("tool name must be 1-64 letters, digits or underscores")
	case strings.TrimSpace(definition.Description) == "":
		return errors.New("tool description must not be empty")
	case json.Unmarshal([]byte(definition.InputSchema), &schema) != nil || schema.Type != "object":
		return errors.New("tool input schema must describe an object")
	case schema.AdditionalProperties == nil || *schema.AdditionalProperties:
		return errors.New("tool input schema must reject additional properties")
	case definition.WorkspaceAccess != WorkspaceAccessNone && definition.WorkspaceAccess != WorkspaceAccessReadOnly && definition.WorkspaceAccess != WorkspaceAccessMutating:
		return fmt.Errorf("invalid workspace access %q", definition.WorkspaceAccess)
	case definition.Platform && (definition.Run != nil || definition.UsesWorkspace()):
		return errors.New("a platform tool runs on the server without the workspace")
	case definition.Platform && definition.strict:
		return errors.New("a platform tool is not strict")
	case definition.MaximumAttempts < 1 || definition.MaximumAttempts > math.MaxInt32:
		return errors.New("maximum attempts must be between 1 and the Dex int32 limit")
	case definition.AttemptTimeout < 0 || definition.RetryTotalDuration < 0:
		return errors.New("attempt timeout and retry duration must not be negative")
	default:
		return nil
	}
}

var toolCatalog = []ToolDefinition{
	// Built-in tools are handled by Steps of SessionAgentFlow.
	builtIn(conversation.ToolWriteTodos, "Replace your progress list with a complete ordered todo list, written in business language the user understands. Keep exactly one item in_progress while you work and update statuses as items finish. An empty list clears it. It records progress only; it never asks for approval.", toolinput.WriteTodos.JSONSchema()),
	builtIn(conversation.ToolStartTimer, "Wait durably before continuing. A steered user message interrupts the wait.", toolinput.StartTimer.JSONSchema()),
	builtIn(conversation.ToolSetEffort, "Raise the effort of your next replies until the user's next message: high, or xhigh for the hardest work. Use it before writing a new connector, for a hard bug, or after the same problem failed twice; say why.", toolinput.SetEffort.JSONSchema()),
	builtIn(conversation.ToolRequestUserInput, "Pause durably only for a specific missing user-owned requirement, information or access required to continue and absent from the retained conversation: a business fact, account or access that only the user has, or a choice among several connections or providers that could meet one need. Explain the missing fact and blocker. Choices the request leaves open, such as names, storage primitives, limits, formats and wording, are yours: if you would recommend an option, take it and continue. Never ask about technical implementation: Go types, SDK and Dex APIs, Flow design, cross-Flow access, storage and build fixes are resolved from the reference, skills, module source and compiler. Never ask whether to proceed or to confirm the requirements: confirm_requirements shows them without waiting, and a question the user answered is never asked again. Keep dependent todo items pending until the user answers.", toolinput.RequestUserInput.JSONSchema()),
	streamed(loose(builtIn(conversation.ToolConfirmRequirements, "Show the user what you will build before you write code for a new application or a large change. Make a small change directly without it. Write the card in business language: a title and summary; the features, each one behavior a test can prove, with IDs R1, R2, ... in increasing order; the connections to external systems, existing or new, with the connector's operations and what the user must set up; what the user must prepare; and how acceptance works. When several connections could meet a need, ask with request_user_input first and put the chosen one in the card. The card does not wait: it is recorded as the next version, the result names that version, and you keep building in the same turn. The user changes it by sending a message; when a message changes the requirements, call it again with the revised card and build only the difference. A card that revises recorded requirements keeps every recorded feature's ID and numbers a new feature above every number used, never reusing a removed feature's number; the card of a completely different request after a recorded version sets replaces_requirements and numbers from R1 again. Build exactly the features of the current version. A card that adds, removes or changes Flows also carries their design, which Studio draws until the build is complete: each such Flow in full, named with the Go types you will implement; set is_failure_route only for breakage, never for a business rejection.", toolinput.ConfirmRequirements.JSONSchema()))),
	// Sandbox tools execute over the Session's accepted workspace lease.
	commandTool(conversation.ToolBash, "Run a shell command with bash -c in the workspace root (/workspace) and return its output when it ends. "+
		"Commands are non-interactive: stdin is empty and pagers print directly. The result is stdout and stderr merged in output order, at most "+
		"30,000 bytes: the first 8,000 and the last 22,000, cut at line boundaries; a cut result names the file that holds the full output. "+
		"A nonzero exit adds an exit code line. Say in description what the command does, for the user. A command that runs past timeout_seconds "+
		"(at most 600) continues in the background as a task: the result shows its output so far and its task ID. With run_in_background the command "+
		"starts as a background task at once, for a server, a long build or a test run you keep working beside; at most four run at a time. "+
		"You are notified when a task ends; read its output with read_task_output and stop it with stop_task. A command that prints more than "+
		"16 MiB is stopped with every process it started."),
	fileTool(conversation.ToolReadTaskOutput, "Read the state of a background task and its output from a byte offset: at most 30,000 bytes, and the offset to read on from. "+
		"The state is running, exited with its code, stopped or lost.", toolinput.ReadTaskOutput.JSONSchema(), WorkspaceAccessReadOnly, sandboxtools.ReadTaskOutput),
	fileTool(conversation.ToolStopTask, "Stop a task. A background command (cmd-...) gets SIGTERM to its process group and, after 10 seconds, SIGKILL, and sends no notification. "+
		"A subagent (agent-...) saves its changes and reports as stopped. Stopping a task that already ended answers its state.", toolinput.StopTask.JSONSchema(), WorkspaceAccessMutating, sandboxtools.StopTask),
	fileTool(conversation.ToolRead, "Read a text file with line numbers, as cat -n prints them: the line number, a tab, the line. A call returns at most 2,000 lines and 64 KiB; a line over 2,000 characters is cut and marked, and when more of the file remains the last line gives the offset to read on from. Use offset and limit to read a range, and read a file before you edit it. "+
		"The workspace is readable, and so are these read-only roots: the installed Dex skills (/opt/superverse/dex-skills), Go module sources (find a module's directory with go list -m -f '{{.Dir}}' <module>, after go get or go mod download when it is not restored), the Go standard library (/usr/local/go/src), the Preview logs (/var/lib/superverse/preview/logs) and the full output files that bash names. A directory or a binary file is refused.",
		toolinput.Read.JSONSchema(), WorkspaceAccessReadOnly, sandboxtools.Read),
	streamed(fileTool(conversation.ToolEdit, "Replace text in one existing text file of the workspace. old_string must match the file exactly once, including whitespace and indentation and without the line numbers that read prints; include surrounding lines to make it unique, or set replace_all to replace every occurrence, for example to rename a symbol. old_string and new_string must differ. A failed call changes nothing: read the file again and correct old_string. A repeated call, such as a retry, never edits twice. Files over 8 MiB, binary files, read-only roots and .git are refused.",
		toolinput.Edit.JSONSchema(), WorkspaceAccessMutating, sandboxtools.Edit)),
	streamed(fileTool(conversation.ToolWrite, "Create a file of the workspace, or replace it completely, with content of at most 4 MiB; missing directories are created. Use edit for a change to an existing file and write for a new file or a complete rewrite. Writing the same content again changes nothing. Read-only roots and .git are refused.",
		toolinput.Write.JSONSchema(), WorkspaceAccessMutating, sandboxtools.Write)),
	fileTool(conversation.ToolGrep, "Search file contents with ripgrep regular expressions in the workspace, or in the path you give, which may be a read-only root such as a Go module directory. path is a literal existing file or directory; wildcards in path are never expanded. Resolve a Go module directory with go list -m -f '{{.Dir}}' <module>, then pass that exact path. .gitignore is honored and .git is skipped. output_mode files_with_matches (the default) lists matching files; use content when you need path:line:text matches, with context from 0 to 20 lines, or count for matching lines per file. Narrow with path and glob, and page a long result with head_limit and offset. A result is at most 32 KiB.",
		toolinput.Grep.JSONSchema(), WorkspaceAccessReadOnly, sandboxtools.Grep),
	fileTool(conversation.ToolGlob, "Find files by a glob such as *.go or **/*.test.ts, most recently modified first: at most 200 paths and 16 KiB. It searches the workspace or a literal path you give, which may be a read-only root; wildcards in path are never expanded. A pattern with a slash is matched against the whole path, so start it with **/ to match below that path. .gitignore is honored and .git is skipped. Use it to find the actual name of a file or to list a directory.",
		toolinput.Glob.JSONSchema(), WorkspaceAccessReadOnly, sandboxtools.Glob),
	catalogTool(conversation.ToolConnectorCatalog, "Search the official published Connector catalog by provider or capability. Returns only the matching release identities and operations, with each connector's exact Go module and the install_command that adds the release to go.mod. Run install_command, find the module's directory with go list -m -f '{{.Dir}}' <module>, then read connector.yaml and the generated factory there with read and grep; catalog descriptions do not define Go signatures.", toolinput.ConnectorCatalog.JSONSchema()),
	remoteOnly(uninterruptible(longRunning(sandboxTool(conversation.ToolStartConnectorContribution,
		"Start a connector contribution in a separate worktree of the connectors library. It returns the worktree path, base commit and contributor skill path. Work in that path with the file tools or bash. At most one open contribution per connector and 16 distinct connectors per Session.",
		toolinput.StartConnectorContribution.JSONSchema(), WorkspaceAccessMutating, sandboxtools.CreateContributionWorktree, 2*time.Minute)))),
	remoteOnly(uninterruptible(longRunning(sandboxTool(conversation.ToolSubmitConnectorContribution,
		"Verify, commit and push the open connector worktree as the GitHub App, then submit it for a draft pull request and release candidate. The result confirms acceptance; connector notifications report publication or failure later.",
		toolinput.SubmitConnectorContribution.JSONSchema(), WorkspaceAccessMutating, sandboxtools.FinishContribution, 15*time.Minute)))),
	remoteOnly(uninterruptible(longRunning(sandboxTool(conversation.ToolOpenTaskWorktree,
		"Open one isolated worktree of the application for a Codex subagent. It snapshots the current workspace, including uncommitted files, and returns its path and base commit. Give the subagent that path and keep its work there. A repeated open of an active name returns it; opening after merge or discard starts a new attempt. At most three checkouts are open.",
		toolinput.OpenTaskWorktree.JSONSchema(), WorkspaceAccessMutating, sandboxtools.CreateAgentWorktree, 2*time.Minute)))),
	remoteOnly(uninterruptible(longRunning(sandboxTool(conversation.ToolMergeTaskWorktree,
		"Finish a Codex subagent's worktree and merge its changes into the workspace, or discard them. paths applies selected files and leaves the rest mergeable. The receipt lists applied, conflicting, unchanged and unknown paths; resolve conflicts in the workspace before continuing. A repeated merge returns its recorded receipt.",
		toolinput.MergeTaskWorktree.JSONSchema(), WorkspaceAccessMutating, sandboxtools.MergeAgentChanges, 3*time.Minute)))),
	// The delegation tools. The Session runs start_agent itself: it creates the
	// subagent's worktree and starts its SubagentFlow. Both may run for a while
	// and must finish their effects, so Stop never cancels them.
	uninterruptible(loose(longRunning(sandboxTool(conversation.ToolStartAgent, "Start a subagent in the background: an agent with its own context and its own git worktree of the workspace, "+
		"your uncommitted files included, that works on one bounded task and reports when it finishes. kind investigate answers a question about the code or its dependencies; "+
		"kind change makes a bounded change, in files that do not depend on your own work, and checks it. "+
		"kind connector_select chooses connectors for the requirements from the published catalog and reports each choice, its alternatives and the gaps; it edits nothing. "+
		"kind connector_develop builds one missing connector or operation, or fixes a defect of a connector, in its own worktree of the connectors library: "+
		"name the connector's slug and the contribution (new, operation or fix); when it completes, the platform commits its change as the GitHub App and pushes its branch for a draft pull request. "+
		"Brief it completely in prompt: it sees nothing of the conversation. "+
		"It answers at once with the task ID, such as agent-k3n8q5r1, and the snapshot commit; the report arrives as a task notification, so never wait or poll for it. "+
		"At most three subagents run at once, and at most six start before the user writes again. stop_task stops one; merge_agent_changes merges its changes.",
		toolinput.StartAgent.JSONSchema(), WorkspaceAccessMutating, sandboxtools.CreateAgentWorktree, 2*time.Minute)))),
	uninterruptible(loose(longRunning(sandboxTool(conversation.ToolMergeAgentChanges, "Merge the changes of a finished subagent into the workspace as uncommitted changes, or discard them. "+
		"apply merges each file three ways, from the snapshot the subagent started from, the workspace now and the subagent's result: a file only one side changed takes that side; "+
		"a text file both changed is merged and holds conflict markers where the changes overlap; a binary file, a deletion against a change and a file both added keep "+
		"the workspace's version. paths restricts apply to those files or directories and leaves the other changes mergeable; apply without paths, or discard, "+
		"closes the subagent's changes. The result lists the applied, conflicting (with the conflict kind) and unchanged files and the paths that name no changed file; "+
		"a repeated call answers the recorded result. Then run the checks and commit as for your own changes.",
		toolinput.MergeAgentChanges.JSONSchema(), WorkspaceAccessMutating, sandboxtools.MergeAgentChanges, 3*time.Minute)))),
	// Its source gate can run for minutes; see longRunning.
	uninterruptible(loose(longRunning(sandboxTool(conversation.ToolCommitAndPush, "Verify changed application source with the whole source gate, then commit and push: the template's static checks, the platform Flow-definition check and the application's API tests (make test-app) with their Flow and branch coverage and the requirement trace. "+
		"Call it when the requested application, its API tests and its trace are complete; unfinished work is not a blocker, so finish it first. After a requirements confirmation, pass requirements_revision and one requirements item per confirmed feature ID (R1, R2, ...) with the files or symbols that implement it and the tests that prove it, "+
		"or an untested_reason when no test can; a small change with nothing confirmed passes none. Fix the reported stage, rerun its make test-app RUN=... and retry. Set blocked=true only when a concrete blocker that you cannot resolve prevents completion, such as missing user-owned information or access or a check you cannot repair; "+
		"a blocked call skips the gate but still commits and pushes. Complete source awaiting Studio credentials uses blocked=false; report real execution as pending. The result names the commit, the stages and the test counts.",
		toolinput.CommitAndPush.JSONSchema(), WorkspaceAccessMutating, sandboxtools.CommitAndPush, commitAttemptTimeout)))),
	// Platform tools read the Session's own Project on the server.
	platformTool(conversation.ToolReadDeliveryStatus, "Read this Project's delivery status on the server, also while the Sandbox is stopped: Project and Setup status and failures, the Session HEAD, the Preview (status, commit, failure, engine) and the Live pointer, the newest three Releases with their source and build status and failures, including the log tail of a failed build, and the newest three Live deployments with their phase and failure. Every resource names its FlowID for read_platform_flow. Output is redacted.", toolinput.ReadDeliveryStatus.JSONSchema()),
	platformTool(conversation.ToolReadPlatformFlow, "Read one platform Flow of this Project on the server: its summary, its business status Attributes or a page of its history. Only this Project's project-, setup-, release- and deploy- FlowIDs and this Session's own preview-, session-workspace- and sandbox-provision- FlowIDs are readable; any other is refused with flow_not_in_project. Output is redacted and never names a run.", toolinput.ReadPlatformFlow.JSONSchema()),
	platformTool(conversation.ToolReadLiveAppRuns, "Read the runs of the application deployed to Live on the server: search one Flow type, or one run's summary, state or history page, or the end of the Live application container's log. Payloads stored as blobs are omitted, and output is redacted. For the Preview, run dexcli flow search, summary, state or history with --no-hydrate in the Sandbox instead, and read /var/lib/superverse/preview/logs.", toolinput.ReadLiveAppRuns.JSONSchema()),
}

// platformTool is a read-only server-side tool: it joins parallel waves and
// starts as a regular execution. It changes nothing, so it may be repeated.
func platformTool(name conversation.ToolName, description, schema string) ToolDefinition {
	return ToolDefinition{ToolSpec: toolSpec(name, description, schema), Platform: true,
		WorkspaceAccess: WorkspaceAccessNone, supportsParallelExecution: true, LongRunning: true,
		AttemptTimeout: 45 * time.Second, MaximumAttempts: toolRepeatableAttempts, RetryTotalDuration: 45*time.Second + toolRetryGrace}
}

func builtIn(name conversation.ToolName, description, schema string) ToolDefinition {
	return ToolDefinition{ToolSpec: toolSpec(name, description, schema), strict: true, WorkspaceAccess: WorkspaceAccessNone, MaximumAttempts: 1}
}

// sandboxTool retries a Sandbox call that may be repeated, such as Git with
// query-before-mutate recovery, within attemptTimeout per attempt.
func sandboxTool(name conversation.ToolName, description, schema string, access WorkspaceAccess, run sandboxtools.Tool, attemptTimeout time.Duration) ToolDefinition {
	return ToolDefinition{ToolSpec: toolSpec(name, description, schema), Run: run, strict: true,
		WorkspaceAccess: access, supportsParallelExecution: access == WorkspaceAccessReadOnly,
		AttemptTimeout: attemptTimeout, MaximumAttempts: toolRepeatableAttempts, RetryTotalDuration: attemptTimeout + toolRetryGrace}
}

// loose leaves a tool non-strict. confirm_requirements and commit_and_push are
// the only non-platform tools that are not strict, and the reason is the
// provider, not the tools. The provider compiles the schemas of all the strict
// tools of a request into one grammar and rejects a request whose grammar is
// too large (HTTP 400, "The compiled grammar is too large"), a limit that the
// per-request counts (20 strict tools, 24 optional parameters) do not show. The
// card of confirm_requirements is the largest schema: with it strict the rest
// of the tools is rejected, and without it only one more tool of the size of read
// fits. The requirement trace of commit_and_push is the next: with both loose,
// four more tools of that size still fit (measured on Opus 5.5 on 2026-10-07).
// read_task_output, stop_task and bash's run_in_background then joined the strict
// set, which the live test accepted on every supported model (2026-10-07).
// Their decoders still validate every input after the tool_use completes, and a
// failure is a result the model corrects.
//
// Whoever adds a strict tool or grows the schema of a strict one (the
// application test trace of commit_and_push is due) must run make
// test-claude-live, whose TestLiveClaudeStrictToolSet sends the frozen tool set
// to every supported model: the startup check below cannot see the grammar
// limit. A tool that no longer fits is made loose here, with its reason, rather
// than weakening that test. start_agent and merge_agent_changes joined loose,
// so they leave the measured strict set unchanged; whether they fit strict is
// for the next live measurement to decide.
func loose(definition ToolDefinition) ToolDefinition {
	definition.strict = false
	return definition
}

// toolShortAttempt is the attempt timeout of the file tools and the connector
// catalog.
const toolShortAttempt = time.Minute

// commitAttemptTimeout holds the gate's stage limits (19 minutes) and Git.
const commitAttemptTimeout = 25 * time.Minute

// fileTool is a file call, repeatable by identity: a read, a search, an edit
// that its intent record keeps from applying twice, or a write of the same
// content.
func fileTool(name conversation.ToolName, description, schema string, access WorkspaceAccess, run sandboxtools.Tool) ToolDefinition {
	return sandboxTool(name, description, schema, access, run, toolShortAttempt)
}

// catalogTool is the read of the published connector catalog.
func catalogTool(name conversation.ToolName, description, schema string) ToolDefinition {
	return sandboxTool(name, description, schema, WorkspaceAccessReadOnly, sandboxtools.ConnectorCatalog, toolShortAttempt)
}

// streamed makes the provider stream the tool's long input as the model
// writes it: a file's content, and a requirements card, whose draft Studio
// shows while it streams. Eager streaming is independent of strictness: the
// decoder still validates a loose tool's input once its block is complete.
// Changing it changes the frozen tool set and so the prompt digest, which
// rolls each open Session's context over once at its next turn.
func streamed(definition ToolDefinition) ToolDefinition {
	definition.eagerInputStreaming = true
	return definition
}

// uninterruptible keeps a running call when the user presses Stop:
// commit_and_push commits, rebases and pushes in stages whose interruption
// would leave the branch for the model to reconcile.
func uninterruptible(definition ToolDefinition) ToolDefinition {
	definition.uninterruptible = true
	return definition
}

// longRunning marks a call that can outlast the ASYNC local phase (about seven
// seconds). In that phase the Session answers no RPC, snapshot reads included,
// and a longer call is cut off and repeated as a regular execution.
func longRunning(definition ToolDefinition) ToolDefinition {
	definition.LongRunning = true
	return definition
}

func remoteOnly(definition ToolDefinition) ToolDefinition {
	definition.RemoteOnly = true
	return definition
}

// commandTool runs a command through the Sandbox's command ledger, which
// starts it once per call identity, so a retried attempt finds the command
// instead of starting it again. The Session gives each call the polling
// options of its own timeout (commandStepOptions), not these fields, and
// settles a command after a timeout or exhausted retries from the ledger.
func commandTool(name conversation.ToolName, description string) ToolDefinition {
	return ToolDefinition{ToolSpec: toolSpec(name, description, toolinput.Bash.JSONSchema()),
		Run: sandboxtools.BeginCommand, strict: true, WorkspaceAccess: WorkspaceAccessMutating, LongRunning: true, MaximumAttempts: 1}
}

func toolSpec(name conversation.ToolName, description, schema string) conversation.ToolSpec {
	return conversation.ToolSpec{Name: name, Description: description, InputSchema: conversation.MustJSONObject(schema)}
}

// The catalog is static, so it is checked once here and a defect stops the
// process at startup; scheduling relies on these bounds without checking again.
func init() {
	seen := make(map[conversation.ToolName]bool, len(toolCatalog))
	for _, definition := range toolCatalog {
		if seen[definition.Name] {
			panic(fmt.Errorf("duplicate catalog tool %q", definition.Name))
		}
		seen[definition.Name] = true
		if err := definition.validate(); err != nil {
			panic(fmt.Errorf("tool %q: %w", definition.Name, err))
		}
	}
	if err := checkStrictLimits(toolCatalog); err != nil {
		panic(err)
	}
}

// The provider's limits on the strict tools of one request: at most 20 of
// them, and at most 24 optional parameters across all their schemas.
const (
	maximumStrictTools              = 20
	maximumStrictOptionalParameters = 24
)

// checkStrictLimits requires the strict tools of the catalog to stay within
// the provider's per-request counts. It cannot see the provider's limit on the
// size of the compiled grammar (see loose): make test-claude-live shows that the
// API takes the frozen tool set. An optional parameter is a property that no required list names, nested
// properties and the properties of array items included.
func checkStrictLimits(definitions []ToolDefinition) error {
	tools, optional := 0, 0
	for _, definition := range definitions {
		if !definition.strict {
			continue
		}
		var schema schemaNode
		if err := json.Unmarshal([]byte(definition.InputSchema), &schema); err != nil {
			return fmt.Errorf("tool %q: read the input schema: %w", definition.Name, err)
		}
		tools++
		optional += schema.optionalParameters()
	}
	switch {
	case tools > maximumStrictTools:
		return fmt.Errorf("the catalog has %d strict tools; the provider takes at most %d", tools, maximumStrictTools)
	case optional > maximumStrictOptionalParameters:
		return fmt.Errorf("the strict tools have %d optional parameters; the provider takes at most %d", optional, maximumStrictOptionalParameters)
	default:
		return nil
	}
}

// schemaNode is the part of a JSON Schema that counts parameters.
type schemaNode struct {
	Required   []string              `json:"required"`
	Properties map[string]schemaNode `json:"properties"`
	Items      *schemaNode           `json:"items"`
}

func (node schemaNode) optionalParameters() int {
	count := 0
	for name, property := range node.Properties {
		if !slices.Contains(node.Required, name) {
			count++
		}
		count += property.optionalParameters()
	}
	if node.Items != nil {
		count += node.Items.optionalParameters()
	}
	return count
}

// SessionTools is the frozen tool set of a Session segment on model, in name
// order: catalog tools except RemoteOnly, without set_effort for a model that
// takes no effort. A segment keeps it whatever the turn's mode.
func SessionTools(model conversation.Model) []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(toolCatalog))
	for _, definition := range toolCatalog {
		if definition.RemoteOnly {
			continue
		}
		if definition.Name != conversation.ToolSetEffort || modelprovider.ProfileOf(model).Effort {
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

// platformOnlyTools are the catalog tools a connected executor lacks: platform
// delegation and controls of the platform model.
var platformOnlyTools = []conversation.ToolName{conversation.ToolStartAgent, conversation.ToolMergeAgentChanges,
	conversation.ToolSetEffort, conversation.ToolStartTimer}

// RemoteTools is the server-owned catalog for a connected executor: the
// catalog without platformOnlyTools.
func RemoteTools() []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(toolCatalog))
	for _, definition := range toolCatalog {
		if !definition.RemoteOnly && slices.Contains(platformOnlyTools, definition.Name) {
			continue
		}
		switch definition.Name {
		case conversation.ToolStopTask:
			definition.Description = "Stop a background command (cmd-...). It gets SIGTERM and, after 10 seconds, SIGKILL, and sends no notification. Stopping a command that already ended answers its state."
			var schema map[string]json.RawMessage
			comm.MustDo(json.Unmarshal([]byte(definition.InputSchema), &schema))
			var properties map[string]json.RawMessage
			comm.MustDo(json.Unmarshal(schema["properties"], &properties))
			properties["task_id"] = comm.Must(json.Marshal(map[string]any{"type": "string", "minLength": 5, "maxLength": 64, "pattern": "^cmd-[a-z0-9]+$", "description": "The background command ID, such as cmd-qjo5x7m2k4ab."}))
			schema["properties"] = comm.Must(json.Marshal(properties))
			definition.InputSchema = conversation.MustJSONObject(string(comm.Must(json.Marshal(schema))))
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

// RemoteToolDigest changes whenever the server catalog's names, descriptions
// or strict schemas change; a Bridge then refreshes its MCP catalog.
func RemoteToolDigest() string {
	definitions := RemoteTools()
	specs := make([]conversation.ToolSpec, 0, len(definitions))
	for _, definition := range definitions {
		specs = append(specs, definition.ToolSpec)
	}
	digest := sha256.Sum256(comm.Must(json.Marshal(specs)))
	return hex.EncodeToString(digest[:])
}

// RemoteToolNames is the allow-list used for serial and parallel admission.
func RemoteToolNames() []conversation.ToolName {
	definitions := RemoteTools()
	names := make([]conversation.ToolName, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

// subagentToolNames are the catalog tools of every subagent: commands in its
// root, the read-only file tools and the connector catalog.
var subagentToolNames = []conversation.ToolName{conversation.ToolBash, conversation.ToolRead, conversation.ToolGrep, conversation.ToolGlob, conversation.ToolConnectorCatalog}

// SubagentTools is the frozen tool set of a subagent of kind on model, in name
// order: every subagent tool, edit and write for a kind that edits files, the
// platform readers for a kind that reads the platform, and set_effort for a
// kind that raises its effort on a model that takes effort. Its bash is
// subagentBash.
func SubagentTools(kind agenttasks.SubagentKind, model conversation.Model) []ToolDefinition {
	names := slices.Clone(subagentToolNames)
	if kind.EditsFiles() {
		names = append(names, conversation.ToolEdit, conversation.ToolWrite)
	}
	if kind.ReadsPlatform() {
		names = append(names, conversation.ToolReadDeliveryStatus, conversation.ToolReadPlatformFlow, conversation.ToolReadLiveAppRuns)
	}
	if kind.RaisesEffort() && modelprovider.ProfileOf(model).Effort {
		names = append(names, conversation.ToolSetEffort)
	}
	definitions := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		if name == conversation.ToolBash {
			definitions = append(definitions, subagentBash)
		} else {
			definitions = append(definitions, CatalogTool(name))
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

// subagentBash is bash as a subagent sees it: without run_in_background,
// because a subagent's command runs in the foreground, and stopped at its
// timeout instead of moving to the background. The input still decodes with
// the bash schema; the Sandbox refuses run_in_background of a subagent.
var subagentBash = func() ToolDefinition {
	definition := CatalogTool(conversation.ToolBash)
	var schema struct {
		Type                 string                     `json:"type"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	comm.MustDo(json.Unmarshal([]byte(definition.InputSchema), &schema))
	delete(schema.Properties, "run_in_background")
	schema.Properties["timeout_seconds"] = comm.Must(json.Marshal(map[string]any{"type": "integer", "minimum": 1, "maximum": 600, "default": 120,
		"description": "Seconds the command may run; default 120, at most 600. A command still running then is stopped with every process it started."}))
	definition.InputSchema = conversation.MustJSONObject(string(comm.Must(json.Marshal(schema))))
	definition.Description = "Run a shell command with bash -c in your root, your worktree, and return its output when it ends. Commands are non-interactive: " +
		"stdin is empty and pagers print directly. The result is stdout and stderr merged in output order, at most 30,000 bytes: the first 8,000 and the last 22,000, " +
		"cut at line boundaries; a cut result names the file that holds the full output. A nonzero exit adds an exit code line. Say in description what the command " +
		"does. A command that runs past timeout_seconds (at most 600) is stopped with every process it started. Commands run one at a time, at a lower priority, " +
		"and never in the background; start no server."
	return definition
}()

// SegmentTool resolves a call's tool from the segment's frozen tool names and
// the binary's catalog; false for a tool the segment was not offered or this
// binary no longer has.
func SegmentTool(toolNames []conversation.ToolName, name conversation.ToolName) (ToolDefinition, bool) {
	if !slices.Contains(toolNames, name) {
		return ToolDefinition{}, false
	}
	for _, definition := range toolCatalog {
		if definition.Name == name {
			return definition, true
		}
	}
	return ToolDefinition{}, false
}

// CatalogTool is the definition of a catalog tool. The catalog is static, so a
// definition is a function of the name; RouteTool admitted the name of every
// call that is dispatched, waits or recovers.
func CatalogTool(name conversation.ToolName) ToolDefinition {
	for _, definition := range toolCatalog {
		if definition.Name == name {
			return definition
		}
	}
	panic(fmt.Sprintf("tool %q is not in the catalog", name))
}

// RequestedEffort is the effort a set_effort call asks for.
func RequestedEffort(call conversation.ToolCall) (conversation.Effort, string, error) {
	input, err := toolinput.SetEffort.Decode(call.Arguments.String())
	if err != nil {
		return "", "", invalidToolArguments(call, err)
	}
	return conversation.Effort(input.Level), strings.TrimSpace(input.Reason), nil
}
