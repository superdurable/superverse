// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command superverse-evolve is the agent's measurement of an agent review in
// the coding Sandbox: it checks a goals file, and evaluates, compares and
// gates over the review's folder that place_evolve_export placed, never over
// any other runs or goals. README.md is the contract.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
)

const usage = `superverse-evolve measures an agent review over its placed folder of Live runs.

Commands:
  check [file]                                  validate a goals file (default .superverse/goals.md);
                                                every problem with its line
  metrics --review <id>                         evaluate every goal over the review's runs, as JSON
  compare --review <id> --before <file> --after <file> [--strata <f1,f2>] [--target <goal id>]
          [--before-commit <commit>] [--out <file>]
                                                compare a backtest's decision lines, as backtest.json
  open-runs --review <id> --before-defs <dir|file> --after-defs <dir|file> [--out <file>]
                                                check a change of Flow definitions against open runs

--review names the folder .local/superverse-evolve/<id>/ of the repository the current directory is
in, as place_evolve_export placed it in the workspace and start_agent inputs link it into a
subagent's worktree: runs.jsonl, the review's export of the Live runs; manifest.json; metrics.json;
and goals.md, the goals of the Live build. Only those runs and goals count. A folder whose manifest
does not name the Live source and this review, or whose runs.jsonl does not have the manifest's
runs_sha256, is refused. compare evaluates at metrics.json's evaluated_at, so its before values
reproduce the review's metrics. compare and open-runs write the export binding: the folder's
review, goals commit, runs digest and count, --before-commit (default the goals commit, the Live
build the before side's code came from) and the current HEAD as the base commit. Both print JSON
without --out and exit 1 when a gate refuses the proposal.

compare reads the decision lines a backtest program writes, one JSON line per run:
{"flow_id": "...", "decisions": {"<name>": ...}, "points": {"<name>": {"step": "<Step>", "entry": 2}},
"numbers": {"steps.<Step>": 2, ...}}. --strata names the fields an estimate's matching runs share;
--target the goal the change targets (default every goal with a direction). open-runs reads FDG
2.0 graphs or the list dexcli renders.

Exit codes: 0 done, 1 an error or a refusal, 2 a usage error.
`

// errUsage is a command line this tool does not accept; it exits 2.
var errUsage = errors.New("usage")

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	err := command(args, stdout, stderr)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "superverse-evolve: %v\n\n%s", err, usage)
		return 2
	}
	fmt.Fprintln(stderr, "superverse-evolve:", err)
	return 1
}

func command(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: a command is required", errUsage)
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout)
	case "metrics":
		return runMetrics(args[1:], stdout)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "open-runs":
		return runOpenRuns(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		return flag.ErrHelp
	}
	return fmt.Errorf("%w: unknown command %q; export and reads of other runs are not commands of this tool", errUsage, args[0])
}

// flags is a command's flag set; a flag it does not declare, such as --runs,
// --goals or --dex, is a usage error.
func flags(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func parse(set *flag.FlagSet, args []string, positional int) error {
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %s: %w", errUsage, set.Name(), err)
	}
	if set.NArg() > positional {
		return fmt.Errorf("%w: %s: unexpected arguments %q", errUsage, set.Name(), set.Args()[positional:])
	}
	return nil
}

// required fails with a usage error when a flag the command needs is empty.
func required(set *flag.FlagSet, names ...string) error {
	for _, name := range names {
		if set.Lookup(name).Value.String() == "" {
			return fmt.Errorf("%w: %s: --%s is required", errUsage, set.Name(), name)
		}
	}
	return nil
}

func runCheck(args []string, stdout io.Writer) error {
	set := flags("check")
	if err := parse(set, args, 1); err != nil {
		return err
	}
	path := goals.FilePath
	if set.NArg() == 1 {
		path = set.Arg(0)
	}
	file, err := os.ReadFile(path) // #nosec G304 -- the agent names a goals file of its own workspace.
	if err != nil {
		return err
	}
	spec, problems := goals.Parse(file)
	if len(problems) != 0 {
		var message bytes.Buffer
		fmt.Fprintf(&message, "%s has %d problem(s):", path, len(problems))
		for _, problem := range problems {
			fmt.Fprintf(&message, "\n  %s", problem)
		}
		return errors.New(message.String())
	}
	fmt.Fprintf(stdout, "%s: %d goal(s) over %v; the export reads %v\n", path, len(spec.Goals), spec.Flows, spec.ExportFields())
	return nil
}

// readFolder reads the review's placed folder in the repository of the
// current directory.
func readFolder(reviewID string) (goals.ReviewFolder, error) {
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return goals.ReviewFolder{}, fmt.Errorf("find the repository root: %w", err)
	}
	return goals.ReadReviewFolder(root, reviewID)
}

// bind completes the folder's binding with the before and base commits.
func bind(folder goals.ReviewFolder, beforeCommit string) (*goals.ExportBinding, error) {
	binding := folder.Binding
	binding.BeforeCommit = binding.GoalsCommit
	if beforeCommit != "" {
		binding.BeforeCommit = beforeCommit
	}
	base, err := git("rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read the worktree's base commit: %w", err)
	}
	binding.BaseCommit = base
	return &binding, nil
}

func git(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output() // #nosec G204 -- fixed git read commands.
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runMetrics(args []string, stdout io.Writer) error {
	set := flags("metrics")
	review := set.String("review", "", "the review whose placed folder to read")
	if err := parse(set, args, 0); err != nil {
		return err
	}
	if err := required(set, "review"); err != nil {
		return err
	}
	folder, err := readFolder(*review)
	if err != nil {
		return err
	}
	metrics, err := goals.Evaluate(folder.Spec, folder.Records, folder.EvaluatedAt)
	if err != nil {
		return err
	}
	return writeJSON(stdout, metrics, "")
}

func runCompare(args []string, stdout, stderr io.Writer) error {
	set := flags("compare")
	review := set.String("review", "", "the review whose placed folder to read")
	beforePath := set.String("before", "", "the backtest's decision lines on the Live build's code")
	afterPath := set.String("after", "", "the backtest's decision lines on the proposal's tree")
	strata := set.String("strata", "", "comma-separated fields an estimate's matching runs share")
	target := set.String("target", "", "the goal the change targets; default every goal with a direction")
	beforeCommit := set.String("before-commit", "", "the Live build commit the before side's code came from; default the goals commit")
	out := set.String("out", "", "write backtest.json here instead of printing it")
	if err := parse(set, args, 0); err != nil {
		return err
	}
	if err := required(set, "review", "before", "after"); err != nil {
		return err
	}
	folder, err := readFolder(*review)
	if err != nil {
		return err
	}
	before, err := readDecisions(*beforePath)
	if err != nil {
		return err
	}
	after, err := readDecisions(*afterPath)
	if err != nil {
		return err
	}
	options := goals.CompareOptions{Target: *target, At: folder.EvaluatedAt}
	if *strata != "" {
		options.Strata = strings.Split(*strata, ",")
	}
	backtest, err := goals.Compare(folder.Spec, folder.Records, before, after, options)
	if err != nil {
		return err
	}
	if backtest.Export, err = bind(folder, *beforeCommit); err != nil {
		return err
	}
	if err = writeJSON(stdout, backtest, *out); err != nil {
		return err
	}
	return reportGates(stderr, backtest.Gates)
}

func runOpenRuns(args []string, stdout, stderr io.Writer) error {
	set := flags("open-runs")
	review := set.String("review", "", "the review whose placed folder to read")
	beforePath := set.String("before-defs", "", "the Live build's Flow definitions: a rendered definitions file or a directory of them")
	afterPath := set.String("after-defs", "", "the Flow definitions of the proposal's tree")
	out := set.String("out", "", "write open-runs.json here instead of printing it")
	if err := parse(set, args, 0); err != nil {
		return err
	}
	if err := required(set, "review", "before-defs", "after-defs"); err != nil {
		return err
	}
	folder, err := readFolder(*review)
	if err != nil {
		return err
	}
	before, err := goals.ReadDefinitions(*beforePath)
	if err != nil {
		return err
	}
	after, err := goals.ReadDefinitions(*afterPath)
	if err != nil {
		return err
	}
	check, err := goals.CheckOpenRuns(folder.Records, before, after)
	if err != nil {
		return err
	}
	if check.Export, err = bind(folder, ""); err != nil {
		return err
	}
	if err = writeJSON(stdout, check, *out); err != nil {
		return err
	}
	return reportGates(stderr, check.Gates)
}

func readDecisions(path string) ([]goals.DecisionLine, error) {
	file, err := os.Open(path) // #nosec G304 -- the agent names its backtest's output.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	lines, err := goals.ReadDecisions(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return lines, nil
}

// writeJSON prints the value, or writes it to out when out is set.
func writeJSON(stdout io.Writer, value any, out string) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if out == "" {
		_, err = stdout.Write(encoded)
		return err
	}
	return os.WriteFile(out, encoded, 0o644) // #nosec G306 -- a tracked file of the proposal.
}

// reportGates names the gates that fired on standard error and fails when one refuses the proposal.
func reportGates(stderr io.Writer, gates []goals.Gate) error {
	for _, gate := range gates {
		if !gate.Refuses {
			fmt.Fprintf(stderr, "%s: %s\n", gate.Code, gate.Message)
		}
	}
	if err := goals.Refusal(gates); err != nil {
		return fmt.Errorf("the proposal is refused:\n%w", err)
	}
	return nil
}
