// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The file tools read, edit, write and search the agent root and the read-only
// roots through the coding image's superverse-files helper, which applies the
// path rules, the size budgets of every result and, for edit, an intent record
// keyed by the call's identity. The helper's README is the contract of its
// request and answer; the tools only decode the model's arguments, run the
// helper once and map its typed failures.
const (
	filesProgram = "superverse-files"
	// maximumFilesAnswerBytes bounds one helper answer: a result of at most 64
	// KiB, JSON-escaped, and its fields.
	maximumFilesAnswerBytes = 1 << 20
	// Byte bounds that the schemas, which count characters, cannot express.
	maximumPathBytes    = 16 << 10
	maximumPatternBytes = 32 << 10
	maximumGlobBytes    = 4 << 10
	maximumEditBytes    = 4 << 20
	maximumWriteBytes   = 4 << 20
)

type readRequest struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type editRequest struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type writeRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type grepRequest struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Glob            string `json:"glob,omitempty"`
	OutputMode      string `json:"output_mode"`
	Context         int    `json:"context"`
	HeadLimit       int    `json:"head_limit"`
	Offset          int    `json:"offset"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

type globRequest struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (execution *Execution) read(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Read.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = validateText("path", input.Path, maximumPathBytes); err != nil {
		return Result{}, err
	}
	return execution.runFiles(ctx, call, "read", readRequest{Path: input.Path, Offset: input.Offset.Value, Limit: input.Limit.Value}, "")
}

// edit's helper records the edit under the call's identity, so a retried
// attempt finds the file edited and answers with the first result instead of
// editing it twice.
func (execution *Execution) edit(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Edit.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = errors.Join(validateText("path", input.Path, maximumPathBytes), validateText("old_string", input.OldString, maximumEditBytes),
		validateText("new_string", input.NewString, maximumEditBytes)); err != nil {
		return Result{}, err
	}
	request := editRequest{Path: input.Path, OldString: input.OldString, NewString: input.NewString, ReplaceAll: input.ReplaceAll.Value}
	return execution.runFiles(ctx, call, "edit", request, string(sandboxd.DeriveExecutionID(call.Identity)))
}

// write replaces a file with its content; writing the same content again
// changes nothing, so the call is safe to repeat.
func (execution *Execution) write(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Write.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = validateText("path", input.Path, maximumPathBytes); err != nil {
		return Result{}, err
	}
	if err = validateText("content", input.Content, maximumWriteBytes); err != nil {
		return Result{}, err
	}
	return execution.runFiles(ctx, call, "write", writeRequest{Path: input.Path, Content: input.Content}, "")
}

func (execution *Execution) grep(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Grep.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = errors.Join(validateText("pattern", input.Pattern, maximumPatternBytes), validateText("path", input.Path.Value, maximumPathBytes),
		validateText("glob", input.Glob.Value, maximumGlobBytes)); err != nil {
		return Result{}, err
	}
	request := grepRequest{Pattern: input.Pattern, Path: input.Path.Value, Glob: input.Glob.Value, OutputMode: string(input.OutputMode.Value),
		Context: input.Context.Value, HeadLimit: input.HeadLimit.Value, Offset: input.Offset.Value, CaseInsensitive: input.CaseInsensitive.Value}
	return execution.runFiles(ctx, call, "grep", request, "")
}

func (execution *Execution) glob(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Glob.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = errors.Join(validateText("pattern", input.Pattern, maximumGlobBytes), validateText("path", input.Path.Value, maximumPathBytes)); err != nil {
		return Result{}, err
	}
	return execution.runFiles(ctx, call, "glob", globRequest{Pattern: input.Pattern, Path: input.Path.Value}, "")
}

// filesAnswer is the one JSON object of an exit-code-0 helper answer: the
// result text, or the typed failure of the call. The confined path and the next
// line of a read are in the text the model reads, so the tools ignore them.
type filesAnswer struct {
	Result     string        `json:"result"`
	Failure    *filesFailure `json:"failure"`
	Path       string        `json:"path"`
	NextOffset int           `json:"nextOffset"`
}

type filesFailure struct {
	Code    string `json:"code"`
	Owner   string `json:"owner"`
	Effect  string `json:"effect"`
	Message string `json:"message"`
}

// filesFailures are the codes the helper reports: failures of the model's
// call and the platform fault platform_command_unavailable.
var filesFailures = []toolfailure.Code{
	toolfailure.InvalidArguments, toolfailure.PathNotFound, toolfailure.PathIsDirectory, toolfailure.PathOutsideRoots,
	toolfailure.PathPermissionDenied, toolfailure.BinaryFile, toolfailure.FileTooLarge, toolfailure.EditNoMatch, toolfailure.EditAmbiguous, toolfailure.EditNoop,
	toolfailure.InvalidPattern, toolfailure.PlatformCommandUnavailable,
}

// runFiles runs one helper operation in the agent root and maps its answer. A
// helper error is transient and repeated by Dex; a helper that answers outside
// its contract is a platform fault.
func (execution *Execution) runFiles(ctx context.Context, call Call, operation string, request any, editID string) (Result, error) {
	arguments := []string{filesProgram, operation, "--root", call.Root}
	if editID != "" {
		arguments = append(arguments, "--id", editID)
	}
	output, err := execution.executor.RunHelperJSON(ctx, arguments, comm.Must(json.Marshal(request)), maximumFilesAnswerBytes)
	if err != nil {
		return Result{}, err
	}
	var answer filesAnswer
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&answer); err != nil {
		return Result{}, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s answered %s with an unreadable answer: %w", filesProgram, operation, err))
	}
	if (answer.Failure == nil) == (answer.Result == "") {
		return Result{}, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s answered %s with both a result and a failure, or with neither", filesProgram, operation))
	}
	if answer.Failure != nil {
		return Result{}, filesFailureError(operation, answer.Failure)
	}
	return Result{Content: answer.Result}, nil
}

// filesFailureError is the ToolFailure of a typed helper failure: one of the
// helper's codes, answered with its owner and the effect none.
func filesFailureError(operation string, failure *filesFailure) error {
	code, isKnown := toolfailure.Parse(failure.Code)
	if isKnown && slices.Contains(filesFailures, code) && failure.Owner == code.Owner().String() && failure.Effect == toolfailure.EffectNone.String() {
		return sandboxd.NewToolFailure(code, errors.New(failure.Message))
	}
	return sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s answered %s with the unknown failure %q (%s, effect %s)",
		filesProgram, operation, failure.Code, failure.Owner, failure.Effect))
}
