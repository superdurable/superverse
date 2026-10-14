// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package sandboxd_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// TestFileHelperActualSandbox runs every superverse-files operation through
// the real sandboxd of a coding image container that the test owns, with the
// path rules of the agent root and the read-only roots.
func TestFileHelperActualSandbox(t *testing.T) {
	container := sandboxdtest.StartCodingContainerForTest(t)
	runShell(t, container.Client().CommandLedger(), `set -e; cd /workspace; mkdir -p .git src; echo secret > .git/config
printf 'alpha\nbeta alpha\n' > src/b.go; printf 'x\0y' > binary.dat; ln -s /etc/hostname escape; ln -s .git gitlink`)
	type answer struct {
		Result  string `json:"result"`
		Path    string `json:"path"`
		Failure *struct {
			Code   string `json:"code"`
			Owner  string `json:"owner"`
			Effect string `json:"effect"`
		} `json:"failure"`
	}
	files := func(operation string, request map[string]any, extra ...string) answer {
		t.Helper()
		input, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		output, err := container.Client().RunHelperJSON(ctx, append([]string{"superverse-files", operation, "--root", "/workspace"}, extra...), input, 1<<20)
		if err != nil {
			t.Fatalf("%s %v: %v", operation, request, err)
		}
		var reply answer
		if err = json.Unmarshal(output, &reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	refused := func(reply answer, code toolfailure.Code) {
		t.Helper()
		if reply.Failure == nil || reply.Failure.Code != code.String() || reply.Failure.Owner != code.Owner().String() || reply.Failure.Effect != toolfailure.EffectNone.String() {
			t.Fatalf("expected the call failure %s, got %+v", code, reply)
		}
	}

	created := files("write", map[string]any{"path": "notes/a.txt", "content": "one\ntwo\nthree\n"})
	if created.Result != "Created notes/a.txt (3 lines, 14 bytes)" {
		t.Fatalf("write: %+v", created)
	}
	if again := files("write", map[string]any{"path": "notes/a.txt", "content": "one\ntwo\nthree\n"}); again.Result != "Overwrote notes/a.txt (3 lines, 14 bytes)" {
		t.Fatalf("repeated write: %+v", again)
	}
	if read := files("read", map[string]any{"path": "notes/a.txt", "offset": 2, "limit": 1}); read.Result != "     2\ttwo\n[more lines follow; read on with offset 3]\n" {
		t.Fatalf("read: %q", read.Result)
	}
	edited := files("edit", map[string]any{"path": "notes/a.txt", "old_string": "two\n", "new_string": "two\n2a\n"}, "--id", "edit-call-1")
	if edited.Result != "Edited notes/a.txt: 1 replacement(s), lines 2-3" {
		t.Fatalf("edit: %+v", edited)
	}
	if repeated := files("edit", map[string]any{"path": "notes/a.txt", "old_string": "two\n", "new_string": "two\n2a\n"}, "--id", "edit-call-1"); repeated.Result != edited.Result {
		t.Fatalf("a repeated edit call was not answered from its record: %+v", repeated)
	}
	if content := files("read", map[string]any{"path": "notes/a.txt"}); !strings.Contains(content.Result, "     3\t2a\n     4\tthree\n") || strings.Count(content.Result, "2a") != 1 {
		t.Fatalf("the edit was not applied exactly once: %q", content.Result)
	}
	refused(files("edit", map[string]any{"path": "src/b.go", "old_string": "alpha", "new_string": "gamma"}, "--id", "edit-call-2"), toolfailure.EditAmbiguous)
	refused(files("edit", map[string]any{"path": "src/b.go", "old_string": "delta", "new_string": "gamma"}, "--id", "edit-call-3"), toolfailure.EditNoMatch)
	refused(files("edit", map[string]any{"path": "src/b.go", "old_string": "beta", "new_string": "beta"}, "--id", "edit-call-4"), toolfailure.EditNoop)
	refused(files("read", map[string]any{"path": "binary.dat"}), toolfailure.BinaryFile)
	refused(files("read", map[string]any{"path": "src"}), toolfailure.PathIsDirectory)
	refused(files("read", map[string]any{"path": "missing.txt"}), toolfailure.PathNotFound)
	for _, path := range []string{".git/config", "gitlink/config", "escape", "/etc/hostname", "src/../../etc/hostname"} {
		refused(files("read", map[string]any{"path": path}), toolfailure.PathOutsideRoots)
	}
	refused(files("write", map[string]any{"path": "/opt/superverse/dex-skills/injected.md", "content": "no"}), toolfailure.PathOutsideRoots)
	refused(files("write", map[string]any{"path": ".git/hooks/pre-commit", "content": "no"}), toolfailure.PathOutsideRoots)
	if skill := files("read", map[string]any{"path": "/opt/superverse/dex-skills/dex-sdk/SKILL.md", "limit": 1}); !strings.HasPrefix(skill.Result, "     1\t") || skill.Path != "/opt/superverse/dex-skills/dex-sdk/SKILL.md" {
		t.Fatalf("a read-only root was not readable: %+v", skill)
	}
	if grep := files("grep", map[string]any{"pattern": "alpha", "output_mode": "content"}); grep.Result != "src/b.go:1:alpha\nsrc/b.go:2:beta alpha\n" {
		t.Fatalf("grep: %q", grep.Result)
	}
	if grep := files("grep", map[string]any{"pattern": "secret", "glob": "**"}); grep.Result != "No matches found" {
		t.Fatalf("grep reached into .git: %q", grep.Result)
	}
	refused(files("grep", map[string]any{"pattern": "("}), toolfailure.InvalidPattern)
	if glob := files("glob", map[string]any{"pattern": "**/*.txt"}); glob.Result != "notes/a.txt\n" {
		t.Fatalf("glob: %q", glob.Result)
	}
	refused(files("glob", map[string]any{"pattern": "{a"}), toolfailure.InvalidPattern)

	// Permissions that the agent's own commands changed deny the helper, which
	// shares their UID: a locked file, a directory without search permission
	// and a directory without write permission. Each operation on them is a
	// failure of the call that changes nothing, never a helper error; the path
	// rules still decide first, and a search below the path keeps what it found.
	runShell(t, container.Client().CommandLedger(), `set -e; cd /workspace; printf 'locked\n' > locked.txt; chmod 000 locked.txt
mkdir closed kept; printf 'inside\n' > closed/a.txt; chmod 000 closed; printf 'kept\n' > kept/b.txt; chmod 555 kept
mkdir /tmp/outside-closed; chmod 000 /tmp/outside-closed; ln -s /tmp/outside-closed escape-closed`)
	for _, path := range []string{"locked.txt", "closed/a.txt"} {
		refused(files("read", map[string]any{"path": path}), toolfailure.PathPermissionDenied)
		refused(files("write", map[string]any{"path": path, "content": "changed\n"}), toolfailure.PathPermissionDenied)
		refused(files("edit", map[string]any{"path": path, "old_string": "i", "new_string": "I"}, "--id", "edit-denied"), toolfailure.PathPermissionDenied)
	}
	refused(files("write", map[string]any{"path": "kept/new.txt", "content": "new\n"}), toolfailure.PathPermissionDenied)
	refused(files("edit", map[string]any{"path": "kept/b.txt", "old_string": "kept", "new_string": "changed"}, "--id", "edit-unwritable"), toolfailure.PathPermissionDenied)
	if kept := files("read", map[string]any{"path": "kept/b.txt"}); kept.Result != "     1\tkept\n" {
		t.Fatalf("a denied edit changed the file: %+v", kept)
	}
	refused(files("grep", map[string]any{"pattern": "locked", "path": "locked.txt"}), toolfailure.PathPermissionDenied)
	refused(files("grep", map[string]any{"pattern": "inside", "path": "closed"}), toolfailure.PathPermissionDenied)
	refused(files("glob", map[string]any{"pattern": "*.txt", "path": "closed"}), toolfailure.PathPermissionDenied)
	refused(files("read", map[string]any{"path": "escape-closed/a.txt"}), toolfailure.PathOutsideRoots)
	const skipped = "[not searched, because ripgrep could not read them:\n"
	if grep := files("grep", map[string]any{"pattern": "kept"}); !strings.HasPrefix(grep.Result, "kept/b.txt\n"+skipped) || !strings.Contains(grep.Result, "locked.txt: Permission denied") {
		t.Fatalf("grep did not keep its match and name what it skipped: %q", grep.Result)
	}
	if grep := files("grep", map[string]any{"pattern": "no such text"}); !strings.HasPrefix(grep.Result, "No matches found\n"+skipped) || !strings.Contains(grep.Result, "closed: Permission denied") {
		t.Fatalf("grep without a match did not name what it skipped: %q", grep.Result)
	}
	if glob := files("glob", map[string]any{"pattern": "**/*.txt"}); !strings.Contains(glob.Result, "kept/b.txt\n") || !strings.Contains(glob.Result, skipped+"closed: Permission denied") {
		t.Fatalf("glob did not keep its paths and name what it skipped: %q", glob.Result)
	}

	// The edit intent records are the helper's own state outside the roots: a
	// broken record directory stays a helper error, which is transient.
	runShell(t, container.Client().CommandLedger(), `set -e; rm -rf /tmp/superverse-files/edits; mkdir -p /tmp/superverse-files; touch /tmp/superverse-files/edits`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := []byte(`{"path": "notes/a.txt", "old_string": "three", "new_string": "3"}`)
	if _, err := container.Client().RunHelperJSON(ctx, []string{"superverse-files", "edit", "--root", "/workspace", "--id", "edit-no-records"}, request, 1<<20); !sandboxd.IsTransient(err) {
		t.Fatalf("a broken edit record directory is not a transient helper error: %v", err)
	}
}
