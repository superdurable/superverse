// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceRotatesOnRealFilesystem(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25000; i++ {
		if err := Append(dir, Call{LocalID: strings.Repeat("a", 128), Tool: "read", MCPAt: int64(i + 1), CodexAt: int64(i + 2), Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{File, File + ".1"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxBytes {
			t.Fatalf("%s grew to %d bytes", name, info.Size())
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode is %v", name, info.Mode().Perm())
		}
	}
	calls, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 || calls[len(calls)-1].MCPAt != 25000 {
		t.Fatalf("trace lost its latest call: %d rows", len(calls))
	}
}
