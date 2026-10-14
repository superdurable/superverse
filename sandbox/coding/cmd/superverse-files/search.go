// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/confinement"
)

// Bounds of the search operations.
const (
	maximumGrepBytes    = 32 << 10
	maximumGlobPaths    = 200
	maximumGlobBytes    = 16 << 10
	maximumSearchOutput = 8 << 20
	maximumContextLines = 20
	searchTimeout       = 45 * time.Second
	// maximumSkippedBytes bounds the note that names what ripgrep could not read.
	maximumSkippedBytes = 1 << 10
)

// ripgrep is the search engine of grep and glob: no configuration file, hidden
// files included and .gitignore honored. Every command line ends its globs
// with skipGit, which therefore takes precedence over a caller's glob.
var (
	ripgrep = []string{"rg", "--no-config", "--hidden", "--color", "never"}
	skipGit = []string{"--glob", "!.git"}
)

type grepRequest struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Glob            string `json:"glob"`
	OutputMode      string `json:"output_mode"`
	Context         int    `json:"context"`
	HeadLimit       int    `json:"head_limit"`
	Offset          int    `json:"offset"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

// grep searches a regular expression under a path: the matching files (the
// default), the matching lines with optional context, or the match count per
// file. offset and head_limit page the result lines; the result holds at most
// 32 KiB.
func grep(roots confinement.Roots, request grepRequest) (answer, error) {
	mode := map[string]string{"": "--files-with-matches", "files_with_matches": "--files-with-matches", "count": "--count"}
	flagOfMode, known := mode[request.OutputMode]
	switch {
	case request.Pattern == "":
		return answer{}, refuse(invalidArguments, "pattern must not be empty")
	case !known && request.OutputMode != "content":
		return answer{}, refuse(invalidArguments, "output_mode must be files_with_matches, content or count")
	case request.Context < 0 || request.Context > maximumContextLines || request.HeadLimit < 0 || request.Offset < 0:
		return answer{}, refuse(invalidArguments, "context must be between 0 and %d, and head_limit and offset must not be negative", maximumContextLines)
	}
	arguments := append(slices.Clone(ripgrep), "--sort", "path")
	if request.OutputMode == "content" {
		arguments = append(arguments, "--line-number", "--with-filename", "--no-heading")
		if request.Context > 0 {
			arguments = append(arguments, "--context", strconv.Itoa(request.Context))
		}
	} else {
		arguments = append(arguments, flagOfMode)
	}
	if request.CaseInsensitive {
		arguments = append(arguments, "--ignore-case")
	}
	arguments = append(append(arguments, globArguments(request.Glob)...), skipGit...)
	if err := preflight(append(slices.Clone(ripgrep), globArguments(request.Glob)...), "-e", request.Pattern); err != nil {
		return answer{}, err
	}
	confined, target, err := searchTarget(roots, request.Path, true)
	if err != nil {
		return answer{}, err
	}
	lines, skipped, err := search(roots.Agent(), append(arguments, "-e", request.Pattern, "--", target))
	if err != nil {
		return answer{}, err
	}
	if len(lines) == 0 {
		return answer{Result: withSkipped("No matches found", skipped), Path: display(confined)}, nil
	}
	return answer{Result: page(lines, request.Offset, request.HeadLimit, maximumGrepBytes-len(skipped)) + skipped, Path: display(confined)}, nil
}

type globRequest struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

// glob lists the files under a path that match a glob, most recently modified
// first: at most 200 paths and 16 KiB.
func glob(roots confinement.Roots, request globRequest) (answer, error) {
	if request.Pattern == "" {
		return answer{}, refuse(invalidArguments, "pattern must not be empty")
	}
	if err := preflight(append(slices.Clone(ripgrep), "--files", "--glob", request.Pattern)); err != nil {
		return answer{}, err
	}
	confined, target, err := searchTarget(roots, request.Path, false)
	if err != nil {
		return answer{}, err
	}
	paths, skipped, err := search(roots.Agent(), append(append(slices.Clone(ripgrep), "--files", "--glob", request.Pattern), append(slices.Clone(skipGit), "--", target)...))
	if err != nil {
		return answer{}, err
	}
	if len(paths) == 0 {
		return answer{Result: withSkipped("No files found", skipped), Path: display(confined)}, nil
	}
	modified := make(map[string]time.Time, len(paths))
	for _, path := range paths {
		absolute := path
		if !filepath.IsAbs(path) {
			absolute = filepath.Join(roots.Agent(), path)
		}
		if info, err := os.Lstat(absolute); err == nil {
			modified[path] = info.ModTime()
		}
	}
	slices.SortStableFunc(paths, func(left, right string) int { return modified[right].Compare(modified[left]) })
	var out strings.Builder
	for index, path := range paths {
		if index == maximumGlobPaths || out.Len()+len(path)+1 > maximumGlobBytes-128-len(skipped) {
			fmt.Fprintf(&out, "[%d more paths; narrow the pattern or the path]\n", len(paths)-index)
			break
		}
		out.WriteString(path + "\n")
	}
	return answer{Result: out.String() + skipped, Path: display(confined)}, nil
}

func globArguments(glob string) []string {
	if glob == "" {
		return nil
	}
	return []string{"--glob", glob}
}

// preflight lets ripgrep parse the pattern and globs against /dev/null: a
// pattern or glob it cannot parse is the call's invalid_pattern.
func preflight(arguments []string, pattern ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, arguments[0], append(append(arguments[1:], pattern...), "--", "/dev/null")...) //nolint:gosec // ripgrep with fixed flags; the pattern is an argument
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil || errors.As(err, &exit) && exit.ExitCode() == 1:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == 2:
		return refuse(invalidPattern, "ripgrep cannot parse the pattern: %s", firstLines(stderr.String(), 6))
	case errors.Is(err, exec.ErrNotFound):
		return refuse(platformCommandUnavailable, "ripgrep is missing from the Sandbox image")
	default:
		return err
	}
}

// searchTarget confines the search path, requires that the search may open
// it (a directory to list it and, when the search readsFiles, a file to read
// it), and names it for ripgrep: relative to the agent root inside it, so
// results name paths that way.
func searchTarget(roots confinement.Roots, path string, readsFiles bool) (confinement.Path, string, error) {
	confined, err := confine(roots, path, confinement.Read)
	if err != nil {
		return confinement.Path{}, "", err
	}
	info, err := os.Stat(confined.Absolute)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return confinement.Path{}, "", denied(err, path, "searched")
	case err != nil:
		return confinement.Path{}, "", refuse(pathNotFound, "%s does not exist", path)
	}
	if info.IsDir() || readsFiles && info.Mode().IsRegular() {
		if err = openable(confined.Absolute); err != nil {
			return confinement.Path{}, "", denied(err, path, "searched")
		}
	}
	if confined.ReadOnly {
		return confined, confined.Absolute, nil
	}
	return confined, confined.Relative, nil
}

// openable opens path and closes it: a directory must be listable, a file
// readable.
func openable(path string) error {
	file, err := os.Open(path) //nolint:gosec // a confined path of the agent's roots
	if err != nil {
		return err
	}
	return file.Close()
}

// search runs ripgrep in the agent root and returns its output lines, with a
// leading "./" removed, and a note that names what it could not read. Exit
// code 1 is no match. Exit code 2 says ripgrep could not read some paths below
// the search path, such as a file whose permissions deny reading it, and
// searched the rest: the lines it found are kept, and the note names the first
// paths it skipped from its messages.
func search(directory string, arguments []string) ([]string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...) //nolint:gosec // ripgrep with fixed flags; the pattern is an argument
	command.Dir = directory
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err = command.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, "", refuse(platformCommandUnavailable, "ripgrep is missing from the Sandbox image")
		}
		return nil, "", err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximumSearchOutput))
	if len(output) == maximumSearchOutput {
		_ = command.Process.Kill()
	}
	err = errors.Join(readErr, command.Wait())
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, "", err
	}
	skipped := ""
	if exit != nil && exit.ExitCode() == 2 {
		skipped = skippedNote(stderr.String())
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line != "" {
			lines = append(lines, strings.TrimPrefix(line, "./"))
		}
	}
	return lines, skipped, nil
}

// skippedNote is the last line of a search result that names the first paths
// ripgrep could not read, from its messages, at most 1 KiB; empty when it
// printed none.
func skippedNote(messages string) string {
	text := firstLines(messages, 3)
	if text == "" {
		return ""
	}
	if len(text) > maximumSkippedBytes {
		text = strings.ToValidUTF8(text[:maximumSkippedBytes], "") + " ..."
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimPrefix(line, "./")
	}
	return "[not searched, because ripgrep could not read them:\n" + strings.Join(lines, "\n") + "]\n"
}

// withSkipped appends the note of skipped paths, if any, to a result line.
func withSkipped(result, skipped string) string {
	if skipped == "" {
		return result
	}
	return result + "\n" + skipped
}

// page skips offset lines, keeps at most limit lines (all when zero) and at
// most maximumBytes, and says how to read on when it stopped early.
func page(lines []string, offset, limit, maximumBytes int) string {
	if offset >= len(lines) {
		return fmt.Sprintf("(no results at offset %d; there are %d)\n", offset, len(lines))
	}
	selected := lines[offset:]
	if limit > 0 && limit < len(selected) {
		selected = selected[:limit]
	}
	var out strings.Builder
	for index, line := range selected {
		if out.Len()+len(line)+1 > maximumBytes-128 {
			fmt.Fprintf(&out, "[%d more lines; read on with offset %d]\n", len(lines)-offset-index, offset+index)
			return out.String()
		}
		out.WriteString(line + "\n")
	}
	if rest := len(lines) - offset - len(selected); rest > 0 {
		fmt.Fprintf(&out, "[%d more lines; read on with offset %d]\n", rest, offset+len(selected))
	}
	return out.String()
}

func firstLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.Join(lines[:min(count, len(lines))], "\n")
}
