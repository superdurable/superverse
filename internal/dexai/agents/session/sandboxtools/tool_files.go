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
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
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

// placeAttachment writes the file of a JSON attachment the Session's Project
// bound, its redacted copy, through the helper's write at
// conversation.AttachmentWorkspacePath, and answers where. Writing it again
// writes the same bytes.
func (execution *Execution) placeAttachment(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.PlaceAttachment.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	file := call.Attachment
	switch {
	case file == nil || file.Label.ID != input.AttachmentID:
		return Result{}, sandboxd.NewToolFailure(toolfailure.AttachmentNotFound, fmt.Errorf("this Project holds no readable attachment %s", input.AttachmentID))
	case file.Label.Kind != conversation.AttachmentKindJSON:
		return Result{}, invalidArguments(fmt.Errorf("attachment %s is a %s file; place_attachment places JSON files", input.AttachmentID, file.Label.Kind))
	}
	path := conversation.AttachmentWorkspacePath(file.Label)
	// The helper writes inside the agent root, the workspace of the main agent, which is the only caller.
	if _, err = execution.runFiles(ctx, call, "write", writeRequest{Path: strings.TrimPrefix(path, "/workspace/"), Content: string(file.Data)}, ""); err != nil {
		return Result{}, err
	}
	return Result{Content: string(comm.Must(json.Marshal(struct {
		Path       string `json:"path"`
		Bytes      int    `json:"bytes"`
		Digest     string `json:"digest"`
		Redactions int    `json:"redactions"`
	}{path, len(file.Data), file.Digest, file.Redactions})))}, nil
}

// EvolveExport is an agent review's export as the Session read it for
// place_evolve_export: the stored manifest.json with the digest of runs.jsonl,
// the stream of runs.jsonl (RunsBytes bytes of RunsCount runs, one per FlowID
// in canonical order), metrics.json and goals.md at the goals commit, the
// Live build's. NotLive, when set, is why the stored manifest is not this
// review's export of the Live runs.
type EvolveExport struct {
	ReviewID    string
	GoalsCommit string
	RunsSHA256  string
	Manifest    []byte
	Runs        io.Reader
	RunsBytes   int64
	RunsCount   int
	Metrics     []byte
	Goals       []byte
	NotLive     string
}

// Bounds of a placed export: the chunks of runs.jsonl that sandboxd writes,
// and the whole of runs.jsonl.
const (
	evolveChunkBytes         = 4 << 20
	maximumEvolveExportBytes = 256 << 20
	maximumEvolveAnswerBytes = 64 << 10
)

//go:embed evolve_export.py
var evolveExportProgram string

// placeEvolveExport places the export of the review a place_evolve_export
// call names in conversation.EvolveWorkspacePath: runs.jsonl, streamed in
// chunks of at most 4 MiB through sandboxd and concatenated by the helper,
// which checks its SHA-256 against the export's, with manifest.json,
// metrics.json and goals.md, all read-only; the helper keeps the newest two
// review folders. Placing it again writes the same bytes.
func (execution *Execution) placeEvolveExport(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.PlaceEvolveExport.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	export := call.EvolveExport
	switch {
	case export == nil || export.ReviewID != input.ReviewID:
		return Result{}, sandboxd.NewToolFailure(toolfailure.EvolveReviewStale, fmt.Errorf("review %s of this Project has no stored export to place", input.ReviewID))
	case export.NotLive != "":
		return Result{}, sandboxd.NewToolFailure(toolfailure.EvolveExportNotLive, errors.New(export.NotLive))
	}
	if export.RunsBytes > maximumEvolveExportBytes {
		return Result{}, sandboxd.NewToolFailure(toolfailure.EvolveExportTooLarge, fmt.Errorf("runs.jsonl of %s takes %d bytes; at most %d are placed", input.ReviewID, export.RunsBytes, maximumEvolveExportBytes))
	}
	folder := conversation.EvolveWorkspaceDirectory(input.ReviewID)
	chunks := &chunkWriter{ctx: ctx, client: execution.executor, directory: folder + "/.parts"}
	// The stream fails at its end unless it held the runs the review evaluated; the helper checks the chunks.
	if _, err = io.Copy(chunks, io.LimitReader(export.Runs, maximumEvolveExportBytes+1)); err == nil {
		err = chunks.flush()
	}
	if err != nil {
		return Result{}, fmt.Errorf("place the runs of %s: %w", input.ReviewID, err)
	}
	digest, count := export.RunsSHA256, export.RunsCount
	for name, data := range map[string][]byte{"manifest.json": export.Manifest, "metrics.json": export.Metrics, "goals.md": export.Goals} {
		if err = execution.executor.WriteFile(ctx, sandboxd.WorkspacePath(folder+"/.parts/"+name), data, "0600"); err != nil {
			return Result{}, fmt.Errorf("write %s of %s: %w", name, input.ReviewID, err)
		}
	}
	request := struct {
		RunsSHA256 string `json:"runsSha256"`
		Chunks     int    `json:"chunks"`
	}{digest, chunks.count}
	output, err := execution.executor.RunHelperJSON(ctx, []string{"python3", "-c", evolveExportProgram, path.Join(sandboxd.WorkspaceRoot, folder)},
		comm.Must(json.Marshal(request)), maximumEvolveAnswerBytes)
	if err != nil {
		return Result{}, err
	}
	var placed struct {
		RunsSHA256 string   `json:"runs_sha256"`
		Bytes      int64    `json:"bytes"`
		Pruned     []string `json:"pruned"`
	}
	if err = json.Unmarshal(output, &placed); err != nil || placed.RunsSHA256 != digest {
		return Result{}, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the export helper answered %q for %s", output, input.ReviewID))
	}
	return jsonResult(evolvePlacement{Folder: conversation.EvolveWorkspacePath(input.ReviewID), Files: []string{"runs.jsonl", "manifest.json", "metrics.json", "goals.md"},
		Runs: count, Bytes: placed.Bytes, GoalsCommit: export.GoalsCommit, RunsSHA256: digest, Goals: goalValues(export.Metrics), Removed: placed.Pruned,
		Next: "Read goals.md (the Live build's goals) and metrics.json; give this folder to subagents as start_agent inputs. Only these runs and goals count."}, false), nil
}

// evolvePlacement is what place_evolve_export answers.
type evolvePlacement struct {
	Folder      string            `json:"folder"`
	Files       []string          `json:"files"`
	Runs        int               `json:"runs"`
	Bytes       int64             `json:"bytes"`
	GoalsCommit string            `json:"goals_commit"`
	RunsSHA256  string            `json:"runs_sha256"`
	Goals       []evolveGoalValue `json:"goals,omitempty"`
	Removed     []string          `json:"removed_folders,omitempty"`
	Next        string            `json:"next"`
}

// evolveGoalValue is one goal's value as metrics.json holds it.
type evolveGoalValue struct {
	ID    string   `json:"id"`
	Value *float64 `json:"value"`
	Runs  int      `json:"runs"`
}

// goalValues are the goal values of metrics.json, none when it does not decode.
func goalValues(metrics []byte) []evolveGoalValue {
	var decoded goals.Metrics
	if json.Unmarshal(metrics, &decoded) != nil {
		return nil
	}
	values := make([]evolveGoalValue, 0, len(decoded.Goals))
	for _, goal := range decoded.Goals {
		values = append(values, evolveGoalValue{ID: goal.ID, Value: goal.Value, Runs: goal.Runs})
	}
	return values
}

// chunkWriter writes what it is given through sandboxd as numbered files of
// at most evolveChunkBytes in directory, runs-0000 first.
type chunkWriter struct {
	ctx       context.Context
	client    *sandboxd.SandboxClient
	directory string
	buffer    []byte
	count     int
}

func (w *chunkWriter) Write(data []byte) (int, error) {
	for rest := data; len(rest) > 0; {
		taken := min(len(rest), evolveChunkBytes-len(w.buffer))
		w.buffer, rest = append(w.buffer, rest[:taken]...), rest[taken:]
		if len(w.buffer) == evolveChunkBytes {
			if err := w.flush(); err != nil {
				return 0, err
			}
		}
	}
	return len(data), nil
}

// flush writes the buffered bytes as the next chunk; an empty export writes one empty chunk.
func (w *chunkWriter) flush() error {
	if len(w.buffer) == 0 && w.count > 0 {
		return nil
	}
	target := sandboxd.WorkspacePath(fmt.Sprintf("%s/runs-%04d", w.directory, w.count))
	if err := w.client.WriteFile(w.ctx, target, w.buffer, "0600"); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	w.buffer, w.count = w.buffer[:0], w.count+1
	return nil
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
