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
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

const (
	staticStageTimeout = 10 * time.Minute
	// A stage report is the failure message the model reads, which holds at most
	// 8 KiB: the last 6 KiB of the check's output or 7 KiB of Flow definition
	// diagnostics, plus the stage and the instruction that follows.
	maximumStaticReportBytes      = 6 << 10
	maximumDefinitionsReportBytes = 7 << 10
)

// WorktreeDefinitionCheck renders, with the platform's dexcli, every Flow that
// the next commit of the working tree declares, as Preview and Release render a
// pushed commit, and returns them. A rejected source returns an error that
// wraps ErrFlowDefinitionsRejected and ends with the reason; any other error
// leaves the outcome unobserved.
type WorktreeDefinitionCheck func(ctx context.Context, client *sandboxd.SandboxClient) ([]RenderedFlow, error)

// ErrFlowDefinitionsRejected means the platform's Flow definition check
// rejected the working tree.
var ErrFlowDefinitionsRejected = errors.New("the platform rejected the Flow definitions")

// RenderedFlow is one Flow of the next commit as the definitions stage
// rendered it: its type and its connector Steps with their wired branches.
type RenderedFlow struct {
	SourcePath     string                  `json:"sourcePath"`
	FlowType       string                  `json:"flowType"`
	ConnectorSteps []renderedConnectorStep `json:"connectorSteps"`
}

// renderedConnectorStep is one connector Step of a rendered Flow.
type renderedConnectorStep struct {
	Step          string   `json:"step"`
	Connection    string   `json:"connection"`
	ConnectorID   string   `json:"connectorId"`
	ModulePath    string   `json:"modulePath"`
	ModuleVersion string   `json:"moduleVersion"`
	Operation     string   `json:"operation"`
	Branches      []string `json:"branches"`
}

// sourceGateStatus is how a stage of a passed gate ended.
type sourceGateStatus string

const (
	sourceGatePassed sourceGateStatus = "passed"
	// sourceGateSkipped: the clean HEAD was already accepted.
	sourceGateSkipped sourceGateStatus = "skipped"
)

// SourceGateStage is one stage of a passed commit gate.
type SourceGateStage struct {
	Stage      gateStage        `json:"stage"`
	Status     sourceGateStatus `json:"status"`
	DurationMS int64            `json:"duration_ms"`
}

// SourceGate is a passed commit gate: every stage and, when the test stage
// ran, its report.
type SourceGate struct {
	Stages []SourceGateStage
	Tests  *ApplicationTestReport
}

// gateStage names a stage of the gate, as its result and failures spell it.
type gateStage string

// The stages of the gate, in order.
const (
	stageManifest    gateStage = "manifest"
	stageStatic      gateStage = "static"
	stageDefinitions gateStage = "definitions"
	stageTests       gateStage = "tests"
)

// The commands of .superverse/template.json the gate runs. The working tree
// keeps every one HEAD declares.
const (
	checkStaticCommand     = "checkStatic"
	testApplicationCommand = "testApplication"
)

// gateCommands are the make targets HEAD declares: checkStatic always, and
// testApplication when the template has API tests.
type gateCommands struct {
	static, tests string
}

// verifyApplicationSource is the commit gate, run in the admitted Sandbox before
// any token is minted or file staged. Its stages stop at the first failure,
// whose message is that stage's report:
//
//   - manifest: dex-app.yaml meets the host's deployment schema, and the working
//     tree keeps the gate commands HEAD declares;
//   - static: the checkStatic make target;
//   - definitions: the platform's dexcli renders every declared Flow without a
//     diagnostic, as Preview and Release do, and no production package
//     imports a connector mock;
//   - tests: when HEAD declares testApplication, the API tests on a fresh test
//     engine, their Flow and branch coverage, the engine's health and the
//     requirement trace.
//
// A clean tree at the HEAD the Session already accepted runs the manifest stage
// only: it may predate this host schema, but its builds are not repeated. The
// gate keeps no cache.
func (operations *GitOperations) verifyApplicationSource(ctx context.Context, client *sandboxd.SandboxClient, repository Repository, request CommitRequest, progress func(string)) (SourceGate, error) {
	g := workspaceGit{client: client}
	status, err := g.git(ctx, "status", "--porcelain")
	if err != nil {
		return SourceGate{}, commandError("inspect source changes", status, err)
	}
	head, err := g.localHead(ctx)
	if err != nil {
		return SourceGate{}, err
	}
	accepted := strings.TrimSpace(status.stdout) == "" && head == repository.ExpectedHeadSHA
	gate := sourceGateRun{progress: progress}
	var commands gateCommands
	err = gate.stage(stageManifest, func() (stageErr error) {
		// The host owns the deployment schema. Retained template scripts may be
		// older, so a successful local build alone must not admit an invalid source.
		if stageErr = g.verifyAppManifest(ctx, head); stageErr != nil {
			return stageErr
		}
		commands, stageErr = g.declaredGateCommands(ctx)
		return stageErr
	})
	if err != nil {
		return SourceGate{}, err
	}
	if accepted {
		gate.skip(stageStatic, stageDefinitions)
		if commands.tests != "" {
			gate.skip(stageTests)
		}
		return gate.result, nil
	}
	if err = gate.stage(stageStatic, func() error { return g.runSourceCheck(ctx, commands.static) }); err != nil {
		return SourceGate{}, err
	}
	var flows []RenderedFlow
	err = gate.stage(stageDefinitions, func() error {
		var checkErr error
		flows, checkErr = operations.CheckDefinitions(ctx, client)
		if errors.Is(checkErr, ErrFlowDefinitionsRejected) {
			return sandboxd.NewToolFailure(toolfailure.FlowDefinitionInvalid, fmt.Errorf("%s\nPreview and Release render the pushed commit the same way; fix every reported problem, then retry commit_and_push", comm.TailUTF8(checkErr.Error(), maximumDefinitionsReportBytes)))
		}
		if checkErr != nil {
			return checkErr
		}
		return g.rejectProductionMocks(ctx)
	})
	if err != nil {
		return SourceGate{}, err
	}
	if commands.tests == "" {
		return gate.result, nil
	}
	err = gate.stage(stageTests, func() error {
		report, testErr := g.runApplicationTests(ctx, commands.tests, request, flows)
		gate.result.Tests = report
		return testErr
	})
	if err != nil {
		return SourceGate{}, err
	}
	return gate.result, nil
}

// sourceGateRun times the stages of one gate and reports each to progress.
type sourceGateRun struct {
	progress func(string)
	result   SourceGate
}

// stage runs one stage. A typed failure becomes the stage's report; any other
// error leaves the stage's outcome unobserved and keeps its cause.
func (run *sourceGateRun) stage(name gateStage, check func() error) error {
	run.progress("Source gate: " + string(name) + " started")
	started := time.Now()
	err := check()
	elapsed := time.Since(started)
	shown := elapsed.Round(time.Second)
	var failure *sandboxd.ToolFailure
	switch {
	case errors.As(err, &failure):
		run.progress(fmt.Sprintf("Source gate: %s failed after %s", name, shown))
		return failure.Rewrap(fmt.Errorf("stage %s failed after %s: %w", name, shown, failure.Unwrap()))
	case err != nil:
		run.progress(fmt.Sprintf("Source gate: %s was interrupted after %s", name, shown))
		return fmt.Errorf("source gate stage %s: %w", name, err)
	}
	run.progress(fmt.Sprintf("Source gate: %s passed in %s", name, shown))
	run.result.Stages = append(run.result.Stages, SourceGateStage{Stage: name, Status: sourceGatePassed, DurationMS: elapsed.Milliseconds()})
	return nil
}

func (run *sourceGateRun) skip(names ...gateStage) {
	for _, name := range names {
		run.result.Stages = append(run.result.Stages, SourceGateStage{Stage: name, Status: sourceGateSkipped})
	}
}

// verifyAppManifest reads dex-app.yaml, bounded to 1 MiB, and validates it with
// the parser Preview and Release use.
func (g workspaceGit) verifyAppManifest(ctx context.Context, head string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	application, err := g.client.ReadFile(ctx, sandboxd.ReadFileRequest{Path: "dex-app.yaml", Limit: 1 << 20})
	if err != nil {
		if isRejectedFileRequest(err) {
			return sandboxd.NewToolFailure(toolfailure.SourceCheckFailed, fmt.Errorf("read dex-app.yaml: %w", err))
		}
		return fmt.Errorf("read application source manifest: %w", err)
	}
	if !application.EOF {
		return sandboxd.NewToolFailure(toolfailure.SourceCheckFailed, errors.New("dex-app.yaml exceeds the 1 MiB manifest limit"))
	}
	if _, err := appmanifest.ExtractAppManifest(head, application.Data); err != nil {
		return sandboxd.NewToolFailure(toolfailure.SourceCheckFailed, fmt.Errorf("dex-app.yaml deployment schema: %w; repair the source manifest and any outdated local validator, then retry commit_and_push", err))
	}
	return nil
}

// declaredGateCommands returns the make targets HEAD's template manifest
// declares, once the working tree is shown to keep each of them: the next
// commit must not remove or change the gate that verifies it.
func (g workspaceGit) declaredGateCommands(ctx context.Context) (gateCommands, error) {
	manifest, err := g.readTemplateManifest(ctx)
	if errors.Is(err, errTemplateManifestUnreadable) {
		return gateCommands{}, sandboxd.NewToolFailure(toolfailure.SourceCheckUnavailable, err)
	}
	if err != nil || manifest.SchemaVersion != 1 || manifest.BuildProfile != "go-react-v1" {
		return gateCommands{}, sandboxd.NewToolFailure(toolfailure.SourceCheckUnavailable, errors.New("committed template has no supported application source checks"))
	}
	commands := gateCommands{static: manifest.Commands[checkStaticCommand], tests: manifest.Commands[testApplicationCommand]}
	if !makeCommandPattern.MatchString(commands.static) || (commands.tests != "" && !makeCommandPattern.MatchString(commands.tests)) {
		return gateCommands{}, sandboxd.NewToolFailure(toolfailure.SourceCheckUnavailable, errors.New("committed template declares an unsupported source check command"))
	}
	working, err := g.client.ReadFile(ctx, sandboxd.ReadFileRequest{Path: templateManifestPath, Limit: 64 << 10})
	if err != nil && !isRejectedFileRequest(err) {
		return gateCommands{}, fmt.Errorf("read the working template manifest: %w", err)
	}
	var kept templateManifest
	readable := err == nil && working.EOF && json.Unmarshal(working.Data, &kept) == nil
	for _, declared := range []struct{ name, command string }{{checkStaticCommand, commands.static}, {testApplicationCommand, commands.tests}} {
		if declared.command != "" && (!readable || kept.Commands[declared.name] != declared.command) {
			return gateCommands{}, sandboxd.NewToolFailure(toolfailure.SourceGateWeakened, fmt.Errorf("the working tree's %s must keep the %s command %q that HEAD declares; restore it, or call commit_and_push with blocked=true when the user asked to change the source gate", templateManifestPath, declared.name, declared.command))
		}
	}
	return commands, nil
}

// runSourceCheck runs the declared checkStatic make target. It runs repository
// code, so it gets the tool environment: its HOME-relative state must not land
// in the workspace that is staged next.
func (g workspaceGit) runSourceCheck(ctx context.Context, command string) error {
	result, err := g.run(ctx, sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), staticStageTimeout, strings.Fields(command)...)
	if err == nil {
		return nil
	}
	if result.exitCode == 0 {
		// A missing exit or cancelled transport must not be relabelled as an
		// observed compiler failure. The tool's classifier owns uncertainty.
		return commandError("application source check "+command, result, err)
	}
	return sandboxd.NewToolFailure(toolfailure.SourceCheckFailed, fmt.Errorf("%s exited with code %d\n%s\nFix the cause above, then retry commit_and_push", command, result.exitCode, commandTail(result, maximumStaticReportBytes)))
}

// isRejectedFileRequest reports whether the Sandbox refused a file request for
// a reason that lies in the request or the file, such as a missing file, rather
// than in the platform.
func isRejectedFileRequest(err error) bool {
	var failure *sandboxd.ToolFailure
	return errors.As(err, &failure) && failure.Code().Owner() == toolfailure.OwnerCall
}
