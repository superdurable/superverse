// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput/generated"
)

// commitAndPushReport is the structured completion result. It does not echo
// the call's arguments, which the conversation already holds. Commit is the
// pushed remote default-branch HEAD, so a blocked checkpoint also reports its
// commit. Gate is omitted for a blocked call, which verified nothing, and
// Tests unless the test stage ran.
type commitAndPushReport struct {
	Commit string                     `json:"commit"`
	Branch string                     `json:"branch"`
	Pushed bool                       `json:"pushed"`
	Gate   *sourceGateReport          `json:"gate,omitempty"`
	Tests  *git.ApplicationTestReport `json:"tests,omitempty"`
}

type sourceGateReport struct {
	Stages []git.SourceGateStage `json:"stages"`
}

// commitAndPush verifies and pushes the workspace source. A Git business
// failure is a typed failure; any other error may follow a remote effect and
// stays uncertain.
func (execution *Execution) commitAndPush(ctx context.Context, call Call) (Result, error) {
	arguments, err := toolinput.CommitAndPush.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = validateNonBlankText("summary", arguments.Summary, 32_768); err != nil {
		return Result{}, err
	}
	if err = validateEach("verification", arguments.Verification, 8_192, validateNonBlankText); err != nil {
		return Result{}, err
	}
	if err = validateEach("remaining_risks", arguments.RemainingRisks, 8_192, validateNonBlankText); err != nil {
		return Result{}, err
	}
	request := git.CommitRequest{CallID: call.Identity.CallID, Summary: arguments.Summary, Blocked: arguments.Blocked.Value}
	if request.Trace, err = traceRequirements(arguments.Requirements, arguments.RequirementsRevision, call.Requirements); err != nil {
		return Result{}, err
	}
	commit, err := execution.commits.CommitAndPush(ctx, execution.executor, execution.repository, request, call.Progress)
	if err != nil {
		return Result{}, err
	}
	report := commitAndPushReport{Commit: commit.CommitSHA, Branch: execution.repository.DefaultBranch, Pushed: true, Tests: commit.SourceGate.Tests}
	if !request.Blocked {
		report.Gate = &sourceGateReport{Stages: commit.SourceGate.Stages}
	}
	result := jsonResult(report, false)
	// The Session Step commits the verified remote HEAD with this result.
	result.HeadCommit = commit.CommitSHA
	return result, nil
}

// traceRequirements checks the form of the requirement trace against the
// current requirements the Session passed: every item names a distinct
// feature ID of the current version, and the trace names that version. An
// empty trace is valid in form, as for a small change with nothing recorded;
// the gate's test stage checks that the trace covers every current ID with
// passing tests.
func traceRequirements(items []generated.CommitAndPushRequirementsItem, revision generated.OptInt, confirmed *ConfirmedRequirements) (git.RequirementTrace, error) {
	var trace git.RequirementTrace
	if confirmed != nil {
		trace.Confirmed = slices.Clone(confirmed.IDs)
	}
	if len(items) == 0 {
		return trace, nil
	}
	if confirmed == nil {
		return trace, invalidArguments(errors.New("requirements: no requirements are recorded; pass none for a change without a requirements card"))
	}
	if !revision.Set || int64(revision.Value) != confirmed.Revision {
		return trace, invalidArguments(fmt.Errorf("requirements_revision must be the current version %d", confirmed.Revision))
	}
	named := make(map[string]bool, len(items))
	for index, item := range items {
		name := fmt.Sprintf("requirements[%d]", index)
		switch {
		case !slices.Contains(confirmed.IDs, item.ID):
			return trace, invalidArguments(fmt.Errorf("%s.id %q is not a feature of the current version %d", name, item.ID, confirmed.Revision))
		case named[item.ID]:
			return trace, invalidArguments(fmt.Errorf("%s.id %q appears twice", name, item.ID))
		}
		named[item.ID] = true
		if err := validateEach(name+".evidence", item.Evidence, 512, validateNonBlankText); err != nil {
			return trace, err
		}
		if err := validateEach(name+".tests", item.Tests, 256, validateNonBlankText); err != nil {
			return trace, err
		}
		if item.UntestedReason.Set {
			if err := validateNonBlankText(name+".untested_reason", item.UntestedReason.Value, 1_200); err != nil {
				return trace, err
			}
		}
		trace.Items = append(trace.Items, git.TracedRequirement{ID: item.ID, Tests: item.Tests, UntestedReason: item.UntestedReason.Value})
	}
	return trace, nil
}
