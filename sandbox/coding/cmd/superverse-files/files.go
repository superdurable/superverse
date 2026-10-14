// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/sandbox/coding/internal/confinement"
	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// Bounds of the file operations.
const (
	maximumReadLines     = 2000
	maximumLineRunes     = 2000
	maximumReadBytes     = 64 << 10
	maximumEditFileBytes = 8 << 20
	maximumWriteBytes    = 4 << 20
	binaryProbeBytes     = 8 << 10
	editIntentRetention  = 2 * time.Hour
)

// editIntents holds one record per edit call identity, so a retried edit
// finds its first result instead of applying the edit twice.
const editIntents = "/tmp/superverse-files/edits"

type readRequest struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// read prints lines of a text file in cat -n format: the line number, a tab
// and the line. A line longer than 2,000 characters is cut and marked. When
// lines remain, the last line names the next offset.
func read(roots confinement.Roots, request readRequest) (answer, error) {
	if request.Offset == 0 {
		request.Offset = 1
	}
	if request.Limit == 0 {
		request.Limit = maximumReadLines
	}
	if request.Offset < 1 || request.Limit < 1 || request.Limit > maximumReadLines {
		return answer{}, refuse(invalidArguments, "offset must be at least 1 and limit between 1 and %d", maximumReadLines)
	}
	confined, err := confine(roots, request.Path, confinement.Read)
	if err != nil {
		return answer{}, err
	}
	if _, err = statFile(confined, request.Path, "read"); err != nil {
		return answer{}, err
	}
	file, err := os.Open(confined.Absolute)
	if err != nil {
		return answer{}, denied(err, request.Path, "read")
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64<<10)
	if probe, _ := reader.Peek(binaryProbeBytes); bytes.IndexByte(probe, 0) >= 0 {
		return answer{}, refuse(binaryFile, "%s is not a text file", request.Path)
	}
	var out strings.Builder
	number, next := 0, 0
	for {
		line, err := reader.ReadString('\n')
		if line == "" && errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return answer{}, err
		}
		number++
		if number < request.Offset {
			continue
		}
		if number >= request.Offset+request.Limit {
			next = number
			break
		}
		formatted, err := numberedLine(number, line, request.Path)
		if err != nil {
			return answer{}, err
		}
		if out.Len()+len(formatted) > maximumReadBytes-128 {
			next = number
			break
		}
		out.WriteString(formatted)
	}
	switch {
	case number == 0:
		out.WriteString("(empty file)\n")
	case number < request.Offset:
		fmt.Fprintf(&out, "(no lines at offset %d; the file has %d lines)\n", request.Offset, number)
	case next > 0:
		fmt.Fprintf(&out, "[more lines follow; read on with offset %d]\n", next)
	}
	return answer{Result: out.String(), Path: display(confined), NextOffset: next}, nil
}

// numberedLine formats one line, cutting it after maximumLineRunes characters.
func numberedLine(number int, line, path string) (string, error) {
	line = strings.TrimRight(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if !utf8.ValidString(line) || strings.IndexByte(line, 0) >= 0 {
		return "", refuse(binaryFile, "%s is not valid UTF-8 text", path)
	}
	if utf8.RuneCountInString(line) > maximumLineRunes {
		runes := []rune(line)
		line = string(runes[:maximumLineRunes]) + fmt.Sprintf(" [line cut: %d more characters]", len(runes)-maximumLineRunes)
	}
	return fmt.Sprintf("%6d\t%s\n", number, line), nil
}

type writeRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// write replaces a file with content, creating it and its directories when
// absent. Writing the same content again changes nothing.
func write(roots confinement.Roots, request writeRequest) (answer, error) {
	if len(request.Content) > maximumWriteBytes {
		return answer{}, refuse(invalidArguments, "content exceeds %d bytes", maximumWriteBytes)
	}
	confined, err := confine(roots, request.Path, confinement.Write)
	if err != nil {
		return answer{}, err
	}
	verb, mode := "Created", fs.FileMode(0o644)
	current, err := os.ReadFile(confined.Absolute)
	switch {
	case err == nil:
		verb = "Overwrote"
		info, statErr := os.Stat(confined.Absolute)
		if statErr != nil {
			return answer{}, statErr
		}
		mode = info.Mode().Perm()
	case errors.Is(err, syscall.EISDIR):
		return answer{}, refuse(pathIsDirectory, "%s is a directory", request.Path)
	case !errors.Is(err, fs.ErrNotExist):
		return answer{}, denied(err, request.Path, "written")
	}
	if err == nil && string(current) == request.Content {
		return answer{Result: describeWrite(verb, display(confined), request.Content), Path: display(confined)}, nil
	}
	if err = os.MkdirAll(filepath.Dir(confined.Absolute), 0o750); err != nil {
		return answer{}, denied(err, request.Path, "written")
	}
	if err = replaceFile(confined.Absolute, []byte(request.Content), mode); err != nil {
		return answer{}, denied(err, request.Path, "written")
	}
	return answer{Result: describeWrite(verb, display(confined), request.Content), Path: display(confined)}, nil
}

func describeWrite(verb, path, content string) string {
	return fmt.Sprintf("%s %s (%d lines, %d bytes)", verb, path, lineCount(content), len(content))
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(content, "\n"), "\n") + 1
}

type editRequest struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// editIntent records one applied edit by its call identity: the file before
// and after, and the result.
type editIntent struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
	Result string `json:"result"`
}

// edit replaces old_string with new_string in a text file: exactly one
// occurrence, or every one with replace_all. Its intent record makes a repeat
// of the same call identity return the first result once the file holds it,
// and apply the edit once more only when the file is still as before.
func edit(roots confinement.Roots, id string, request editRequest) (answer, error) {
	if request.OldString == request.NewString {
		return answer{}, refuse(editNoop, "old_string and new_string are equal")
	}
	if request.OldString == "" {
		return answer{}, refuse(invalidArguments, "old_string must not be empty")
	}
	confined, err := confine(roots, request.Path, confinement.Write)
	if err != nil {
		return answer{}, err
	}
	info, err := statFile(confined, request.Path, "edited")
	if err != nil {
		return answer{}, err
	}
	if info.Size() > maximumEditFileBytes {
		return answer{}, refuse(fileTooLarge, "%s has %d bytes; edit files of at most %d bytes", request.Path, info.Size(), maximumEditFileBytes)
	}
	content, err := os.ReadFile(confined.Absolute)
	if err != nil {
		return answer{}, denied(err, request.Path, "edited")
	}
	collectIntents()
	intentPath := filepath.Join(editIntents, id+".json")
	var intent editIntent
	if supervision.ReadJSON(intentPath, &intent) == nil && intent.Path == confined.Absolute {
		switch digest(content) {
		case intent.After:
			return answer{Result: intent.Result, Path: display(confined)}, nil
		case intent.Before:
			return answer{Result: intent.Result, Path: display(confined)}, denied(applyEdit(confined.Absolute, intent, content, info.Mode().Perm(), request), request.Path, "edited")
		}
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return answer{}, refuse(binaryFile, "%s is not valid UTF-8 text", request.Path)
	}
	text := string(content)
	count := strings.Count(text, request.OldString)
	switch {
	case count == 0:
		return answer{}, refuse(editNoMatch, "old_string was not found in %s", request.Path)
	case count > 1 && !request.ReplaceAll:
		return answer{}, refuse(editAmbiguous, "old_string occurs %d times in %s; add context to make it unique or set replace_all", count, request.Path)
	}
	replacements := 1
	updated := strings.Replace(text, request.OldString, request.NewString, 1)
	if request.ReplaceAll {
		replacements = count
		updated = strings.ReplaceAll(text, request.OldString, request.NewString)
	}
	if len(updated) > maximumEditFileBytes {
		return answer{}, refuse(fileTooLarge, "the edited %s would exceed %d bytes", request.Path, maximumEditFileBytes)
	}
	// The replacements span from the first one, whose offset is unchanged, to
	// the end of the last one, which earlier replacements shifted.
	first := strings.Index(text, request.OldString)
	last := first
	if request.ReplaceAll {
		last = strings.LastIndex(text, request.OldString) + (replacements-1)*(len(request.NewString)-len(request.OldString))
	}
	firstLine := strings.Count(updated[:first], "\n") + 1
	lastLine := strings.Count(updated[:max(last, last+len(request.NewString)-1)], "\n") + 1
	intent = editIntent{Path: confined.Absolute, Before: digest(content), After: digest([]byte(updated)),
		Result: fmt.Sprintf("Edited %s: %d replacement(s), lines %d-%d", display(confined), replacements, firstLine, lastLine)}
	// The intent record is the helper's own state, outside the agent's roots:
	// a failure to keep it is a helper error.
	if err = os.MkdirAll(editIntents, 0o700); err != nil {
		return answer{}, err
	}
	if err = supervision.WriteJSON(intentPath, intent); err != nil {
		return answer{}, err
	}
	return answer{Result: intent.Result, Path: display(confined)}, denied(replaceFile(confined.Absolute, []byte(updated), info.Mode().Perm()), request.Path, "edited")
}

// applyEdit repeats a recorded edit on a file that is still as before it.
func applyEdit(path string, intent editIntent, content []byte, mode fs.FileMode, request editRequest) error {
	updated := strings.Replace(string(content), request.OldString, request.NewString, 1)
	if request.ReplaceAll {
		updated = strings.ReplaceAll(string(content), request.OldString, request.NewString)
	}
	if digest([]byte(updated)) != intent.After {
		return fmt.Errorf("the recorded edit of %s no longer reproduces", path)
	}
	return replaceFile(path, []byte(updated), mode)
}

// replaceFile writes data beside path and renames it over path, so readers
// see the old or the new file whole.
func replaceFile(path string, data []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".superverse-files-*")
	if err != nil {
		return err
	}
	_, err = temporary.Write(data)
	err = errors.Join(err, temporary.Chmod(mode), temporary.Close())
	if err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temporary.Name())
	}
	return err
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// collectIntents removes intent records older than editIntentRetention.
func collectIntents() {
	entries, err := os.ReadDir(editIntents)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-editIntentRetention)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(editIntents, entry.Name()))
		}
	}
}
