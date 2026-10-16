// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command app-template-archive packs app-template/ into the archive embedded by
// internal/dexai/app/apptemplate and renders the matching Studio initial design. It runs
// through `go generate ./internal/dexai/app/apptemplate`; the output is deterministic so
// scripts/check-app-template.sh can detect drift.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "app-template-archive:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	top, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("find repository root: %w", err)
	}
	root := strings.TrimSpace(string(top))
	templateDirectory := filepath.Join(root, "app-template")
	archive, err := packTemplate(ctx, templateDirectory)
	if err != nil {
		return err
	}
	// The revision comes from decoding the archive exactly as the server does.
	template, err := apptemplate.Decode(archive)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "internal", "dexai", "app", "apptemplate", "app-template.tar"), archive, 0o600); err != nil {
		return err
	}
	// The template's own FDG script owns strict rendering and digests.
	render := exec.CommandContext(ctx, "python3", "scripts/render-flow-definitions.py", "--output", "/dev/stdout")
	render.Dir, render.Stderr = templateDirectory, os.Stderr
	definitions, err := render.Output()
	if err != nil {
		return fmt.Errorf("render template Flow definitions: %w", err)
	}
	var design bytes.Buffer
	encoder := json.NewEncoder(&design)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(struct {
		TemplateRevision string          `json:"templateRevision"`
		Definitions      json.RawMessage `json:"definitions"`
	}{template.Revision, definitions}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "web", "studio", "lib", "generated", "template-design", "app-template.json"), design.Bytes(), 0o600)
}

// packTemplate archives the committable template files in path order.
func packTemplate(ctx context.Context, templateDirectory string) ([]byte, error) {
	// Committable membership: tracked files plus new files that .gitignore keeps.
	list := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	list.Dir = templateDirectory
	listed, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("list template files: %w", err)
	}
	paths := []string{}
	for _, name := range strings.Split(string(listed), "\x00") {
		if name != "" {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	files, err := os.OpenRoot(templateDirectory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = files.Close() }()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, name := range paths {
		if err = packFile(writer, files, name); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

// packFile adds one template file to the archive. A listed file deleted from
// the working tree and not yet committed is skipped.
func packFile(writer *tar.Writer, files *os.Root, name string) error {
	info, err := files.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("template file %s is not a regular file", name)
	}
	contents, err := files.ReadFile(name)
	if err != nil {
		return err
	}
	// The archive keeps only the executable bit, which Decode turns into the Git mode.
	mode := int64(0o644)
	if info.Mode().Perm()&0o111 != 0 {
		mode = 0o755
	}
	header := &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, Size: int64(len(contents)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
	if err = writer.WriteHeader(header); err != nil {
		return err
	}
	_, err = writer.Write(contents)
	return err
}
