// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package sandboxtools

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The file tools run through the superverse-files helper of the owned sandboxd,
// a container of the coding image (contract revision 8): real files, real
// ripgrep and the path rules of the image. Each tool is called as the Runner
// does, with its error classified once, so the model's view of every failure is
// what a Session records.

// fileTools wraps the owned Sandbox and one scratch directory of the
// workspace for a test.
type fileTools struct {
	t         *testing.T
	ctx       context.Context
	execution *Execution
	// dir is the test's own directory, relative to the workspace root.
	dir string
}

func newFileTools(t *testing.T, timeout time.Duration) *fileTools {
	t.Helper()
	execution, _ := newActualSandboxExecution(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	tools := &fileTools{t: t, ctx: ctx, execution: execution, dir: "file-tools-" + time.Now().UTC().Format("150405-000000000")}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		tools.bash(cleanup, "rm -rf -- "+tools.dir, 30*time.Second)
	})
	return tools
}

// call runs one tool under a fresh call identity.
func (tools *fileTools) call(tool Tool, arguments map[string]any) Result {
	tools.t.Helper()
	return invoke(tools.ctx, tools.t, tools.execution, tool, arguments)
}

// succeeded runs a tool that must succeed and returns its text.
func (tools *fileTools) succeeded(tool Tool, arguments map[string]any) string {
	tools.t.Helper()
	result := tools.call(tool, arguments)
	if result.Failure != nil || result.KnownFailure {
		tools.t.Fatalf("%v failed: %s", arguments, result.Content)
	}
	return result.Content
}

// refused requires the failure of the model's call, with no effect and the text
// the model reads.
func (tools *fileTools) refused(tool Tool, arguments map[string]any, code toolfailure.Code) string {
	tools.t.Helper()
	result := tools.call(tool, arguments)
	failure := result.Failure
	if failure == nil || failure.Code != code || code.Owner() != toolfailure.OwnerCall || failure.Effect != toolfailure.EffectNone {
		tools.t.Fatalf("%v did not fail with %s (call, none): %+v", arguments, code, result)
	}
	if !strings.HasPrefix(result.Content, "Error: "+code.String()+" — ") || !strings.Contains(result.Content, "\nowner: call · effect: none\nhint: ") {
		tools.t.Fatalf("%s is not told to the model in the failure format: %s", code, result.Content)
	}
	return result.Content
}

// bash runs a command to its end in the workspace root and returns its output.
func (tools *fileTools) bash(ctx context.Context, command string, timeout time.Duration) string {
	tools.t.Helper()
	call := bashCall(tools.t, map[string]any{"command": command, "description": "Prepare a file tool check", "timeout_seconds": int(timeout / time.Second)})
	result, err := classifyToolError(invokeCall(ctx, tools.execution, BeginCommand, call))
	for err == nil && result.Pending {
		result, err = classifyToolError(invokeCall(ctx, tools.execution, CommandStatus, call))
	}
	if err != nil || result.Failure != nil || result.KnownFailure {
		tools.t.Fatalf("%q did not succeed: %+v, %v", command, result, err)
	}
	return result.Content
}

func (tools *fileTools) path(name string) string { return tools.dir + "/" + name }

// The file tools read, write, edit, search and list the workspace; every
// result is the text the model reads, within its budget, and every failure of
// the call is typed with its code, owner and effect.
func TestFileToolsActualSandbox(t *testing.T) {
	tools := newFileTools(t, 5*time.Minute)
	source := tools.path("a.go")
	original := "package a\n\nfunc Alpha() string { return \"x\" }\n\nfunc Beta() string { return \"x\" }\n"

	if text := tools.succeeded(Write, map[string]any{"path": source, "content": original}); text != fmt.Sprintf("Created %s (5 lines, %d bytes)", source, len(original)) {
		t.Fatalf("write: %q", text)
	}
	if text := tools.succeeded(Write, map[string]any{"path": source, "content": original}); !strings.HasPrefix(text, "Overwrote "+source) {
		t.Fatalf("a repeated write: %q", text)
	}
	if text := tools.succeeded(Read, map[string]any{"path": source}); text != "     1\tpackage a\n     2\t\n     3\tfunc Alpha() string { return \"x\" }\n     4\t\n     5\tfunc Beta() string { return \"x\" }\n" {
		t.Fatalf("read: %q", text)
	}
	if text := tools.succeeded(Read, map[string]any{"path": source, "offset": 3, "limit": 2}); text != "     3\tfunc Alpha() string { return \"x\" }\n     4\t\n[more lines follow; read on with offset 5]\n" {
		t.Fatalf("read of a range: %q", text)
	}

	// An ambiguous edit changes nothing and tells the model what to do; a
	// unique one is applied once, and the call repeated under its own identity,
	// as a Dex retry repeats it, answers with the first result and edits nothing
	// more.
	tools.refused(Edit, map[string]any{"path": source, "old_string": `return "x"`, "new_string": `return "y"`}, toolfailure.EditAmbiguous)
	if text := tools.succeeded(Read, map[string]any{"path": source}); text != "     1\tpackage a\n     2\t\n     3\tfunc Alpha() string { return \"x\" }\n     4\t\n     5\tfunc Beta() string { return \"x\" }\n" {
		t.Fatalf("a refused edit changed the file: %q", text)
	}
	edit := bashCall(t, map[string]any{"path": source, "old_string": `func Alpha() string { return "x" }`, "new_string": `func Alpha() string { return "first" }`})
	first, err := classifyToolError(invokeCall(tools.ctx, tools.execution, Edit, edit))
	if err != nil || first.Failure != nil || first.Content != fmt.Sprintf("Edited %s: 1 replacement(s), lines 3-3", source) {
		t.Fatalf("edit: %+v, %v", first, err)
	}
	repeated, err := classifyToolError(invokeCall(tools.ctx, tools.execution, Edit, edit))
	if err != nil || repeated.Failure != nil || repeated.Content != first.Content {
		t.Fatalf("a repeated edit call did not answer with its first result: %+v, %v", repeated, err)
	}
	if text := tools.succeeded(Read, map[string]any{"path": source}); strings.Count(text, `return "first"`) != 1 {
		t.Fatalf("the edit was not applied exactly once: %q", text)
	}
	// After the unique edit one occurrence of the old text remains, so the
	// model's next edit, a new call, finds it unique.
	tools.succeeded(Edit, map[string]any{"path": source, "old_string": `return "x"`, "new_string": `return "y"`})
	tools.refused(Edit, map[string]any{"path": source, "old_string": "delta", "new_string": "gamma"}, toolfailure.EditNoMatch)
	tools.refused(Edit, map[string]any{"path": source, "old_string": "Beta", "new_string": "Beta"}, toolfailure.EditNoop)
	repeats := tools.path("repeats.txt")
	tools.succeeded(Write, map[string]any{"path": repeats, "content": "one one one\n"})
	if text := tools.succeeded(Edit, map[string]any{"path": repeats, "old_string": "one", "new_string": "two", "replace_all": true}); text != fmt.Sprintf("Edited %s: 3 replacement(s), lines 1-1", repeats) {
		t.Fatalf("replace_all: %q", text)
	}

	// Search and listing.
	tools.succeeded(Write, map[string]any{"path": tools.path("items.txt"), "content": strings.Repeat("item\n", 10)})
	time.Sleep(50 * time.Millisecond)
	tools.succeeded(Write, map[string]any{"path": tools.path("newer.txt"), "content": "item newest\n"})
	if text := tools.succeeded(Grep, map[string]any{"pattern": "Alpha", "path": tools.dir}); text != source+"\n" {
		t.Fatalf("grep files_with_matches: %q", text)
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "alpha", "path": tools.dir, "case_insensitive": true, "output_mode": "content", "context": 1}); !strings.Contains(text, source+":3:func Alpha()") ||
		!strings.Contains(text, source+"-4-") {
		t.Fatalf("grep content with context: %q", text)
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "item", "path": tools.dir, "glob": "*.txt", "output_mode": "count"}); !strings.Contains(text, tools.path("items.txt")+":10\n") || !strings.Contains(text, tools.path("newer.txt")+":1\n") {
		t.Fatalf("grep count: %q", text)
	}
	page := tools.succeeded(Grep, map[string]any{"pattern": "item", "path": tools.path("items.txt"), "output_mode": "content", "head_limit": 3})
	if strings.Count(page, "\n") != 4 || !strings.HasSuffix(page, "[7 more lines; read on with offset 3]\n") {
		t.Fatalf("grep first page: %q", page)
	}
	if page = tools.succeeded(Grep, map[string]any{"pattern": "item", "path": tools.path("items.txt"), "output_mode": "content", "head_limit": 3, "offset": 3}); !strings.HasPrefix(page, tools.path("items.txt")+":4:item\n") {
		t.Fatalf("grep second page: %q", page)
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "nothing like this", "path": tools.dir}); text != "No matches found" {
		t.Fatalf("grep without a match: %q", text)
	}
	if text := tools.succeeded(Glob, map[string]any{"pattern": "*.txt", "path": tools.dir}); !strings.HasPrefix(text, tools.path("newer.txt")+"\n") || !strings.Contains(text, tools.path("items.txt")+"\n") {
		t.Fatalf("glob does not list the newest file first: %q", text)
	}
	tools.refused(Grep, map[string]any{"pattern": "("}, toolfailure.InvalidPattern)
	tools.refused(Grep, map[string]any{"pattern": "a", "glob": "{a"}, toolfailure.InvalidPattern)
	tools.refused(Glob, map[string]any{"pattern": "{a"}, toolfailure.InvalidPattern)

	// Failures of the path.
	tools.refused(Read, map[string]any{"path": tools.path("missing.txt")}, toolfailure.PathNotFound)
	tools.refused(Read, map[string]any{"path": tools.dir}, toolfailure.PathIsDirectory)
	tools.refused(Edit, map[string]any{"path": tools.path("missing.txt"), "old_string": "a", "new_string": "b"}, toolfailure.PathNotFound)
	tools.bash(tools.ctx, "printf 'binary\\0data' > "+tools.path("binary.dat"), 30*time.Second)
	tools.refused(Read, map[string]any{"path": tools.path("binary.dat")}, toolfailure.BinaryFile)
	tools.refused(Edit, map[string]any{"path": tools.path("binary.dat"), "old_string": "binary", "new_string": "text"}, toolfailure.BinaryFile)
	for _, path := range []string{"/etc/hostname", "../outside.txt", ".git/config"} {
		tools.refused(Read, map[string]any{"path": path}, toolfailure.PathOutsideRoots)
	}
	tools.refused(Write, map[string]any{"path": ".git/hooks/pre-commit", "content": "no"}, toolfailure.PathOutsideRoots)
	tools.refused(Write, map[string]any{"path": "/opt/superverse/dex-skills/injected.md", "content": "no"}, toolfailure.PathOutsideRoots)

	// A file whose permissions a command removed is the call's failure at once,
	// with the hint that says how to restore them, never a transient fault.
	locked := tools.path("locked.txt")
	tools.bash(tools.ctx, "printf 'locked\\n' > "+locked+" && chmod 000 "+locked, 30*time.Second)
	for _, call := range []struct {
		tool      Tool
		arguments map[string]any
	}{
		{Read, map[string]any{"path": locked}},
		{Edit, map[string]any{"path": locked, "old_string": "locked", "new_string": "open"}},
		{Write, map[string]any{"path": locked, "content": "open\n"}},
		{Grep, map[string]any{"pattern": "locked", "path": locked}},
	} {
		if text := tools.refused(call.tool, call.arguments, toolfailure.PathPermissionDenied); !strings.Contains(text, "cannot be") || !strings.Contains(text, "restore them with chmod") {
			t.Fatalf("the permission failure does not tell the model what to do: %s", text)
		}
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "Alpha", "path": tools.dir}); !strings.HasPrefix(text, source+"\n[not searched, because ripgrep could not read them:\n") ||
		!strings.Contains(text, locked+": Permission denied") {
		t.Fatalf("grep did not name the locked file it skipped: %q", text)
	}

	// Failures of the arguments are the schema's: nothing reaches the helper.
	for _, arguments := range []map[string]any{{"path": source, "limit": 2001}, {"path": source, "offset": 0}, {"path": source, "unknown": 1}, {}} {
		if result := tools.call(Read, arguments); result.Failure == nil || result.Failure.Code != toolfailure.InvalidArguments {
			t.Fatalf("%v was not refused as invalid_arguments: %+v", arguments, result)
		}
	}
	if result := tools.call(Write, map[string]any{"path": source, "content": "text\x00more"}); result.Failure == nil || result.Failure.Code != toolfailure.InvalidArguments {
		t.Fatalf("a NUL in the content was not refused: %+v", result)
	}
}

// Every result is sized by the helper before the Session records it: a read
// stops at 64 KiB with the offset to read on from and cuts a long line, a
// search stops at 32 KiB, a listing at 200 paths and 16 KiB.
func TestFileToolBudgetsActualSandbox(t *testing.T) {
	tools := newFileTools(t, 5*time.Minute)
	long := tools.path("long.txt")
	var content strings.Builder
	for line := 1; line <= 5000; line++ {
		fmt.Fprintf(&content, "line %04d %s\n", line, strings.Repeat("x", 30))
	}
	content.WriteString(strings.Repeat("y", 3000) + "\n")
	tools.succeeded(Write, map[string]any{"path": long, "content": content.String()})

	text := tools.succeeded(Read, map[string]any{"path": long})
	next := regexp.MustCompile(`\[more lines follow; read on with offset (\d+)\]\n$`).FindStringSubmatch(text)
	if len(text) > 64<<10 || next == nil || !strings.Contains(text, "line 0001 ") {
		t.Fatalf("a read of 5,000 lines is %d bytes and does not end with its next offset: ...%q", len(text), text[max(0, len(text)-120):])
	}
	if continued := tools.succeeded(Read, map[string]any{"path": long, "offset": mustAtoi(t, next[1]), "limit": 1}); !strings.HasPrefix(continued, fmt.Sprintf("%6s\tline %s ", next[1], next[1])) {
		t.Fatalf("the next offset %s does not continue the file: %q", next[1], continued)
	}
	if tail := tools.succeeded(Read, map[string]any{"path": long, "offset": 5001}); !strings.Contains(tail, "[line cut: 1000 more characters]") || !utf8.ValidString(tail) || len(tail) > 2200 {
		t.Fatalf("a line of 3,000 characters was not cut and marked: %d bytes", len(tail))
	}
	if text = tools.succeeded(Read, map[string]any{"path": long, "offset": 9000}); !strings.HasPrefix(text, "(no lines at offset 9000; the file has 5001 lines)") {
		t.Fatalf("a read past the end: %q", text)
	}

	lines := tools.succeeded(Grep, map[string]any{"pattern": "line", "path": long, "output_mode": "content"})
	if len(lines) > 32<<10 || !regexp.MustCompile(`\[\d+ more lines; read on with offset \d+\]\n$`).MatchString(lines) {
		t.Fatalf("a grep of 5,000 matches is %d bytes and does not say how to read on", len(lines))
	}

	tools.bash(tools.ctx, "mkdir -p "+tools.path("many")+" && cd "+tools.path("many")+" && for i in $(seq 1 300); do : > file-with-a-long-name-$i.txt; done", 60*time.Second)
	listing := tools.succeeded(Glob, map[string]any{"pattern": "*.txt", "path": tools.path("many")})
	if len(listing) > 16<<10 || strings.Count(listing, "\n") != 201 || !strings.Contains(listing, "[100 more paths; narrow the pattern or the path]\n") {
		t.Fatalf("a listing of 300 files has %d lines and %d bytes", strings.Count(listing, "\n"), len(listing))
	}
}

func mustAtoi(t *testing.T, text string) int {
	t.Helper()
	var value int
	if _, err := fmt.Sscanf(text, "%d", &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// The read-only roots are read with the same tools and never written: the
// installed skills, a Go module found with go list -m -f '{{.Dir}}' in the
// module cache, the Go standard library and the output file a command names.
func TestFileToolReadOnlyRootsActualSandbox(t *testing.T) {
	tools := newFileTools(t, 5*time.Minute)
	if text := tools.succeeded(Read, map[string]any{"path": "/opt/superverse/dex-skills/dex-app-builder/SKILL.md", "limit": 3}); !strings.HasPrefix(text, "     1\t") {
		t.Fatalf("the installed skills are not readable: %q", text)
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "^## Read the matching implementation reference", "path": "/opt/superverse/dex-skills", "output_mode": "content"}); !strings.Contains(text, "/opt/superverse/dex-skills/dex-app-builder/SKILL.md:") {
		t.Fatalf("a search of the installed skills: %q", text)
	}
	if text := tools.succeeded(Read, map[string]any{"path": "/usr/local/go/src/fmt/print.go", "limit": 2}); !strings.HasPrefix(text, "     1\t") {
		t.Fatalf("the Go standard library is not readable: %q", text)
	}

	// A module of the application's go.mod: restored from the image's local
	// proxy, then located with go list.
	module := tools.path("module")
	for _, name := range []string{"go.mod", "go.sum"} {
		contents, err := os.ReadFile("../../../../../app-template/" + name)
		if err != nil {
			t.Fatal(err)
		}
		tools.succeeded(Write, map[string]any{"path": module + "/" + name, "content": string(contents)})
	}
	const sdk = "github.com/superdurable/dex/sdk-go"
	listed := strings.Split(strings.TrimSpace(tools.bash(tools.ctx, "cd "+module+" && go mod download "+sdk+" && go list -m -f '{{.Dir}}' "+sdk, 2*time.Minute)), "\n")
	directory := listed[len(listed)-1]
	if !strings.HasPrefix(directory, "/tmp/superverse-tool-execution/go/pkg/mod/"+sdk+"@v") {
		t.Fatalf("go list names %q, not the module directory in the module cache", directory)
	}
	if text := tools.succeeded(Read, map[string]any{"path": directory + "/go.mod", "limit": 1}); text != "     1\tmodule "+sdk+"\n[more lines follow; read on with offset 2]\n" {
		t.Fatalf("the module's go.mod: %q", text)
	}
	if text := tools.succeeded(Grep, map[string]any{"pattern": "^func NewRegistry", "path": directory, "glob": "*.go", "output_mode": "content"}); !strings.Contains(text, directory+"/dex/") || !strings.Contains(text, ":func NewRegistry") {
		t.Fatalf("a search of the module source: %q", text)
	}
	if text := tools.succeeded(Glob, map[string]any{"pattern": "**/client*.go", "path": directory}); !strings.Contains(text, directory+"/dex/client.go\n") {
		t.Fatalf("a listing of the module source: %q", text)
	}
	tools.refused(Edit, map[string]any{"path": directory + "/go.mod", "old_string": "module", "new_string": "modul"}, toolfailure.PathOutsideRoots)
	tools.refused(Write, map[string]any{"path": directory + "/injected.go", "content": "package dex\n"}, toolfailure.PathOutsideRoots)

	// The full output of a command that a bash result names.
	output := tools.bash(tools.ctx, "yes 'a line that adds up to a long output' | head -n 3000", 30*time.Second)
	file := regexp.MustCompile(`full output: (/tmp/superverse-exec/runs/\S+/output\.log)\]`).FindStringSubmatch(output)
	if file == nil {
		t.Fatalf("the cut output names no file: ...%q", output[max(0, len(output)-200):])
	}
	if text := tools.succeeded(Read, map[string]any{"path": file[1], "offset": 2999, "limit": 2}); !strings.HasPrefix(text, "  2999\ta line that adds up") {
		t.Fatalf("the full output file is not readable: %q", text)
	}
}
