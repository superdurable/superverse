// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package toolfailure

// Owner says who has to act on a failed tool call. Its hint, if any, closes
// the hint of every failure it owns.
type Owner struct{ name, hint string }

var (
	// OwnerCall: the call itself was wrong or exceeded a limit; the model
	// corrects it.
	OwnerCall = Owner{name: "call"}
	// OwnerApplication: the application source failed a check; the model
	// repairs the source.
	OwnerApplication = Owner{name: "application"}
	// OwnerPlatform: the platform failed in a way no call can cause; the model
	// works around it or tells the user.
	OwnerPlatform = Owner{name: "platform", hint: "This is a platform problem that your call did not cause, and repeating it unchanged will not help. " +
		"Use an alternative tool if one can do the same work; " +
		"otherwise tell the user in business language what is blocked and end the turn."}
)

// String is the spelling the model reads.
func (owner Owner) String() string { return owner.name }

// HintForTest is the advice that closes the hint of the owner's failures, if any.
func (owner Owner) HintForTest() string { return owner.hint }

// Effect says what a failed call left behind. An effect that may have left
// work done has advice that follows the hint of the code.
type Effect struct{ name, hint string }

var (
	// EffectNone: nothing changed.
	EffectNone = Effect{name: "none"}
	// EffectCompleted: the intended change was made.
	EffectCompleted = Effect{name: "completed"}
	// EffectPartial: part of the change was made, such as a commit that was not
	// pushed or a command that was killed after it ran.
	EffectPartial = Effect{name: "partial", hint: "Part of the work may be done: inspect the current state before repeating the call."}
	// EffectUnknown: whether anything changed could not be observed.
	EffectUnknown = Effect{name: "unknown", hint: "Whether the call took effect is unknown: inspect the current state before repeating it."}
)

// String is the spelling the model reads.
func (effect Effect) String() string { return effect.name }

// Hint is the advice that follows the hint of the code, if any.
func (effect Effect) Hint() string { return effect.hint }

// Code is a tool failure code: the stable name the model reads and a stored
// tool result keeps, declared once below with its owner and its hint. A zero
// Code is no code; reading it panics.
type Code struct{ definition *definition }

type definition struct {
	name  string
	owner Owner
	hint  string
	// sourceGate marks a failure of the source gate, whose message holds the
	// end of a check's output.
	sourceGate bool
	// ownHint marks a code whose hint takes precedence over its owner's.
	ownHint bool
}

// String is the spelling the model reads and a stored tool result keeps.
func (code Code) String() string { return code.definition.name }

// Owner is who has to act on the failure.
func (code Code) Owner() Owner { return code.definition.owner }

// Hint is the advice of the code.
func (code Code) Hint() string { return code.definition.hint }

// OwnerHint is the owner's advice that closes the code's hint: none for a code
// whose own hint takes precedence.
func (code Code) OwnerHint() string {
	if code.definition.ownHint {
		return ""
	}
	return code.definition.owner.hint
}

// IsSourceGate reports whether the failure is a source gate's, whose message
// holds the end of a check's output.
func (code Code) IsSourceGate() bool { return code.definition.sourceGate }

// Parse returns the code spelled name, such as one a Sandbox helper answered.
func Parse(name string) (Code, bool) {
	code, isKnown := codes[name]
	return code, isKnown
}

// codes are the declared codes by their spelling.
var codes = map[string]Code{}

func define(name string, owner Owner, sourceGate bool, hint string) Code {
	if hint == "" {
		panic("tool failure code " + name + " has no hint")
	}
	if _, isDeclared := codes[name]; isDeclared {
		panic("tool failure code " + name + " is declared twice")
	}
	code := Code{definition: &definition{name: name, owner: owner, hint: hint, sourceGate: sourceGate}}
	codes[name] = code
	return code
}

func callCode(name, hint string) Code        { return define(name, OwnerCall, false, hint) }
func applicationCode(name, hint string) Code { return define(name, OwnerApplication, false, hint) }
func sourceGateCode(name, hint string) Code  { return define(name, OwnerApplication, true, hint) }
func platformCode(name, hint string) Code    { return define(name, OwnerPlatform, false, hint) }

// platformCodeWithOwnHint is a platform failure whose own advice, such as one
// retry, replaces the platform owner's advice not to repeat the call.
func platformCodeWithOwnHint(name, hint string) Code {
	code := define(name, OwnerPlatform, false, hint)
	code.definition.ownHint = true
	return code
}

// The call is wrong; the model corrects it.
var (
	InvalidArguments       = callCode("invalid_arguments", "The arguments do not match the tool's input schema. Correct them and call again.")
	PathIsDirectory        = callCode("path_is_directory", "The path is a directory. Use glob to list its files, or name a file.")
	SandboxRequestRejected = callCode("sandbox_request_rejected", "The Sandbox refused the request as malformed. Check the path and the numeric arguments.")
	PathNotFound           = callCode("path_not_found", "The path does not exist. Find the actual name with glob.")
	PathOutsideRoots       = callCode("path_outside_roots", "The path is outside the workspace and the read-only roots, is a write into a read-only root, or passes through .git. "+
		"Use a path in the workspace; only reads may name a read-only root, and Git changes go through commit_and_push.")
	PathPermissionDenied = callCode("path_permission_denied", "The permissions of the file, or of a directory on its path, deny the file tools access; they share the UID of your commands. "+
		"Inspect them with ls -ld in bash and restore them with chmod, then call again.")
	BinaryFile                 = callCode("binary_file", "The file is not valid UTF-8 text, and read and edit handle text files only. Inspect it with a command in bash.")
	FileTooLarge               = callCode("file_too_large", "The file is larger than edit handles (8 MiB). Change it with a command in bash, or split it into smaller files.")
	EditNoMatch                = callCode("edit_no_match", "Read the file again and copy old_string exactly, including whitespace and indentation.")
	EditAmbiguous              = callCode("edit_ambiguous", "old_string occurs more than once. Add surrounding lines to make it unique, or set replace_all to replace every occurrence.")
	EditNoop                   = callCode("edit_noop", "old_string and new_string are equal, so nothing would change. Check what you meant to change, or skip the edit.")
	InvalidPattern             = callCode("invalid_pattern", "The regular expression or glob does not parse. Fix its syntax (ripgrep regular expressions for grep, a glob such as **/*.go for glob) and call again.")
	SandboxConflict            = callCode("sandbox_conflict", "The file changed or conflicts with the request. Read its current state, then call again.")
	SandboxPayloadTooLarge     = callCode("sandbox_payload_too_large", "The content exceeds the Sandbox's size limit. Work in smaller pieces.")
	CatalogResultTooLarge      = callCode("catalog_result_too_large", "Too many operations matched. Name one exact operation to narrow the result.")
	CommandNotStarted          = callCode("command_not_started", "Nothing ran. Check that the program and the working directory exist.")
	CommandOutputLimitExceeded = callCode("command_output_limit_exceeded", "The command printed more than the output limit and was killed. "+
		"Redirect large output to a file and read only the part you need.")
	CommandTimedOut = callCode("command_timed_out", "The command exceeded its timeout and was killed with its process group. "+
		"Use a longer timeout_seconds or a smaller step.")
	BackgroundLimitReached = callCode("background_limit_reached", "Four background commands already run in this Sandbox. Wait for one to finish or stop one first.")
	TaskNotFound           = callCode("task_not_found", "No task has that ID. Use the task ID that a background command (such as cmd-qjo5x7m2k4ab) or start_agent (such as agent-k3n8q5r1) returned.")
)

// Delegation to subagents that the call asks for too often or too early.
var (
	SubagentLimitReached  = callCode("subagent_limit_reached", "Three subagents already run. Wait for a task notification before you start another, or stop one with stop_task.")
	SubagentBudgetReached = callCode("subagent_budget_reached", "Six subagents were started since the user's last message. Continue with the subagents you have, or ask the user before you start more.")
	SnapshotTooLarge      = callCode("snapshot_too_large", "The uncommitted and untracked files of the workspace exceed 100 MiB, which a subagent's worktree would copy. Commit, delete or ignore the large files first.")
	AgentNotFinished      = callCode("agent_not_finished", "The subagent is still running. Wait for its task notification before you merge or discard its changes.")
	// A connector_develop subagent works on the branch of its connector, which
	// one subagent at a time may change.
	ConnectorContributionRunning = callCode("connector_contribution_running", "A connector_develop subagent already works on this connector. "+
		"Wait for its task notification, then start another for the same slug if more work is needed.")
	ConnectorContributionLimitReached = callCode("connector_contribution_limit_reached", "This Session already contributes to 16 connectors, the most it may. "+
		"Continue with the connectors it contributes to, or tell the user what cannot be built.")
	// The library's rules refuse a release candidate of the contributed head,
	// such as a version that is not after the connector's latest release.
	ReleaseCandidateRefused = callCode("release_candidate_refused", "The connectors library refused a release candidate of this contribution for the reason shown. "+
		"Start another connector_develop for the same connector that fixes it, such as a metadata.version after the latest release.")
)

// The application source is wrong; the model repairs it.
var (
	FlowDefinitionInvalid      = sourceGateCode("flow_definition_invalid", "Preview and Release render every Flow of the pushed commit the same way. Fix every reported problem in the source, then call commit_and_push again.")
	SourceCheckFailed          = sourceGateCode("source_check_failed", "A source check failed. Fix the cause shown in the message, then call commit_and_push again.")
	SourceGateWeakened         = applicationCode("source_gate_weakened", "The working tree changed a gate command that HEAD declares (checkStatic or testApplication). Restore it, or call commit_and_push with blocked=true only when the user asked to change the source gate.")
	ConnectorMockInProduction  = sourceGateCode("connector_mock_in_production", "A production package imports a connector mock. Keep mocks in internal/apptest behind the apptest build tag, then call commit_and_push again.")
	ApplicationTestsFailed     = sourceGateCode("application_tests_failed", "Fix the failing tests and engine problems above in the source or the tests, rerun them with the make test-app command shown, then call commit_and_push again.")
	ApplicationTestsIncomplete = sourceGateCode("application_tests_incomplete", "The API tests do not exercise what the source declares. Add tests through apptest.Start for every Flow and every wired branch of a mocked connector named above, then call commit_and_push again.")
	RequirementsUntested       = sourceGateCode("requirements_untested", "Trace every confirmed requirement to tests that pass, or give an untested_reason when no test can prove it; add the missing tests, then call commit_and_push again.")
	GitRemoteConflict          = applicationCode("git_remote_conflict", "The workspace and the remote branch cannot be combined. The commit exists locally and was not pushed. Inspect the repository before calling commit_and_push again.")
	GitPushRejected            = applicationCode("git_push_rejected", "GitHub rejected the push and the remote branch is unchanged. The commit exists locally and was not pushed. Fix the cause named in the message before calling commit_and_push again.")
	MainReverificationRequired = applicationCode("main_reverification_required", "Main changed, and the commit was rebased onto it locally but not pushed. "+
		"Rerun the applicable checks on the rebased source, then call commit_and_push again.")
	MainRebaseConflict = applicationCode("main_rebase_conflict", "Rebasing onto main stopped on conflicts and nothing was pushed. Resolve every conflicted file, then call commit_and_push again to continue.")
)

// The platform failed; the model works around it or tells the user.
var (
	SandboxAccessDenied        = platformCode("sandbox_access_denied", "The Sandbox denied access to a path.")
	ProcessOutputLimitExceeded = platformCode("process_output_limit_exceeded", "A platform helper produced more output than allowed.")
	CatalogUnavailable         = platformCode("catalog_unavailable", "The connector catalog could not be fetched from the Sandbox.")
	CatalogInvalid             = platformCode("catalog_invalid", "The published connector catalog is invalid.")
	SourceCheckUnavailable     = platformCode("source_check_unavailable", "The source checks of the committed template are unavailable.")
	PlatformCommandUnavailable = platformCode("platform_command_unavailable", "A program the platform needs is missing from the Sandbox image.")
	ToolIdentityUnavailable    = platformCode("tool_identity_unavailable", "The original durable tool invocation identity is missing. Reconcile its effect through Project diagnostics, then Stop the old pending turn; never replay a replacement call under that identity.")
	ToolUnavailable            = platformCode("tool_unavailable", "The platform could not complete the call.")
	WorkspaceUnavailable       = platformCode("workspace_unavailable", "The Sandbox workspace could not be started or reached, so the call did not run.")
	WorkspaceNotEmpty          = platformCode("workspace_not_empty", "The Sandbox workspace holds files but no Git repository, so the repository was not checked out.")
	WorkspaceOriginMismatch    = platformCode("workspace_origin_mismatch", "The Git origin of the Sandbox workspace is not the Project's repository, so the repository was not checked out.")
	ToolError                  = platformCode("tool_error", "The tool failed with an unexpected error.")
	CommandInterrupted         = platformCode("command_interrupted", "The command was interrupted before its result was known; it may have run.")
	ExecutionConflict          = platformCode("execution_conflict", "The Sandbox's command ledger already holds another command under this call's identity.")
	WorkspaceDiskLow           = platformCode("workspace_disk_low", "The workspace volume has less than 1 GiB free, so no subagent worktree was created.")
	TestEnvironmentUnavailable = platformCodeWithOwnHint("test_environment_unavailable", "The application tests did not run: the test engine did not start, or other test runs held it until the stage limit passed, and nothing was committed. "+
		"Call commit_and_push once more, after any test run of your subagents ended; if it fails the same way again, call it with blocked=true and tell the user that the tests did not run.")
)

// Reads of the Project's platform Flows that the call names wrongly or too early.
var (
	FlowNotInProject = callCode("flow_not_in_project", "Only this Project's project-, setup-, release- and deploy- Flows and this Session's preview-, session-workspace- and sandbox-provision- Flows are readable.")
	FlowNotFound     = callCode("not_found", "The Flow does not exist. Check the FlowID.")
	LiveNotReady     = callCode("live_not_ready", "The Live application is not available to read. Read the delivery status for its state, then read again later.")
)

// Calls the Session itself decides.
var (
	UnknownOrDisabledTool          = callCode("unknown_or_disabled_tool", "That tool does not exist or is not enabled. Use one of the tools you were given.")
	MultipleWriteTodosCalls        = callCode("multiple_write_todos_calls", "Write the todo list once per response. Call write_todos with the complete list.")
	InvalidTodos                   = callCode("invalid_todos", "The todo list is invalid. Call write_todos again with a complete, valid list.")
	InvalidDurationSeconds         = callCode("invalid_duration_seconds", "The timer duration is invalid. Call start_timer again with a duration in range.")
	InvalidEffort                  = callCode("invalid_effort", "The effort level is invalid. Call set_effort again with a supported level.")
	InvalidUserInput               = callCode("invalid_user_input", "The questions or the requirements card are invalid. Correct them and call the tool again.")
	UserInputAlreadyPending        = callCode("user_input_already_pending", "A question batch or requirements card already waits for the user. Do not ask another until it is answered.")
	RejectedByUser                 = callCode("rejected_by_user", "The user declined this call. Do not repeat it; ask what they want instead or continue without it.")
	SupersededBySteeredUserMessage = callCode("superseded_by_steered_user_message", "The user sent a new message, which supersedes this call. Follow the new message.")
	SupersededByUserInput          = callCode("superseded_by_user_input", "The call was skipped while the Agent waits for the user's answers.")
	OutputLimitExceeded            = callCode("output_limit_exceeded", "Make the call again with a smaller input, or split the work across several calls.")
	InterruptedByUser              = callCode("interrupted_by_user", "The user pressed Stop. Wait for the user's next message before you continue the work.")
)
