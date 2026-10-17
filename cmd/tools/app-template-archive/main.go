// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command app-template-archive composes app-template/ into the examples a
// Project starts from: the shared base plus each folder of
// app-template/examples/, and packs every composed tree into the archives that
// internal/dexai/app/apptemplate embeds, with the catalog of the examples and
// the Studio initial design of the default one. It runs through
// `go generate ./internal/dexai/app/apptemplate`; the output is deterministic
// so scripts/check-app-template.sh can detect drift.
//
// With -compose <id> -into <directory> it writes the composed tree of one
// example into directory instead, for the template's own checks; files of the
// directory that the tree does not hold are removed, except the ignored
// dependencies and generated code, so a repeated check reuses them. With
// -list -changed <file> it lists only the default example and the examples the
// repository paths of file (one per line) reach, for the validation workflow.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

func main() {
	compose := flag.String("compose", "", "write the composed tree of this example")
	into := flag.String("into", "", "the directory -compose writes into")
	list := flag.Bool("list", false, "print the example IDs, the default first")
	changed := flag.String("changed", "", "with -list, a file of changed repository paths: print only the examples they reach")
	flag.Parse()
	var err error
	switch {
	case *list:
		err = listExamples(context.Background(), *changed)
	case *compose != "":
		err = composeInto(context.Background(), *compose, *into)
	default:
		err = run(context.Background())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "app-template-archive:", err)
		os.Exit(1)
	}
}

// repositoryRoot is the checkout this command runs in.
func repositoryRoot(ctx context.Context) (string, error) {
	top, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("find repository root: %w", err)
	}
	return strings.TrimSpace(string(top)), nil
}

func listExamples(ctx context.Context, changedFile string) error {
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	examples, err := readTemplate(ctx, filepath.Join(root, "app-template"))
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(examples))
	for _, example := range examples {
		ids = append(ids, example.manifest.ID)
	}
	if changedFile != "" {
		changed, readErr := readChangedPaths(changedFile)
		if readErr != nil {
			return fmt.Errorf("read the changed paths: %w", readErr)
		}
		ids = changedExamples(examples, changed)
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return nil
}

func composeInto(ctx context.Context, id, directory string) error {
	if directory == "" {
		return errors.New("-compose needs -into <directory>")
	}
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	examples, err := readTemplate(ctx, filepath.Join(root, "app-template"))
	if err != nil {
		return err
	}
	for _, example := range examples {
		if example.manifest.ID == id {
			return materialize(example.files, directory)
		}
	}
	return fmt.Errorf("app-template has no example %q", id)
}

// run composes every example, validates it and writes the embedded outputs.
func run(ctx context.Context) error {
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	examples, err := readTemplate(ctx, filepath.Join(root, "app-template"))
	if err != nil {
		return err
	}
	outputs := filepath.Join(root, "internal", "dexai", "app", "apptemplate")
	entries := make([]apptemplate.CatalogEntry, 0, len(examples))
	written := map[string]bool{}
	var defaultTree []templateFile
	for _, example := range examples {
		archive, archiveErr := pack(example.files)
		if archiveErr != nil {
			return archiveErr
		}
		// The tree identity comes from decoding the archive exactly as the server does.
		if _, archiveErr = apptemplate.Decode(archive); archiveErr != nil {
			return fmt.Errorf("example %s: %w", example.manifest.ID, archiveErr)
		}
		entry, entryErr := catalogEntry(example)
		if entryErr != nil {
			return fmt.Errorf("example %s: %w", example.manifest.ID, entryErr)
		}
		entries = append(entries, entry)
		target := filepath.Join(outputs, filepath.FromSlash(apptemplate.ArchiveFile(example.manifest.ID)))
		if example.manifest.ID == apptemplate.DefaultExampleID {
			target, defaultTree = filepath.Join(outputs, "app-template.tar"), example.files
		}
		if err = os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if err = os.WriteFile(target, archive, 0o600); err != nil {
			return err
		}
		written[target] = true
	}
	if defaultTree == nil {
		return fmt.Errorf("app-template has no default example %s", apptemplate.DefaultExampleID)
	}
	if err = writeCatalog(filepath.Join(outputs, filepath.FromSlash(apptemplate.CatalogFile)), entries); err != nil {
		return err
	}
	written[filepath.Join(outputs, filepath.FromSlash(apptemplate.CatalogFile))] = true
	if err = removeStale(filepath.Join(outputs, "examples"), written); err != nil {
		return err
	}
	return renderDesign(ctx, root, defaultTree)
}

// renderDesign renders the Flow definitions of the default example with its
// own FDG script, in a composed copy, as Studio's initial design.
func renderDesign(ctx context.Context, root string, files []templateFile) error {
	directory, err := os.MkdirTemp("", "app-template-default-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	if err = materialize(files, directory); err != nil {
		return err
	}
	template, err := apptemplate.Decode(mustPack(files))
	if err != nil {
		return err
	}
	render := exec.CommandContext(ctx, "python3", "scripts/render-flow-definitions.py", "--output", "/dev/stdout")
	render.Dir, render.Stderr = directory, os.Stderr
	definitions, err := render.Output()
	if err != nil {
		return fmt.Errorf("render the default example's Flow definitions: %w", err)
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

// writeCatalog writes the catalog with stable formatting.
func writeCatalog(target string, entries []apptemplate.CatalogEntry) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		return err
	}
	return os.WriteFile(target, encoded.Bytes(), 0o600)
}

// removeStale removes generated files of examples that no longer exist.
func removeStale(directory string, written map[string]bool) error {
	found, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range found {
		path := filepath.Join(directory, entry.Name())
		if !written[path] {
			if err = os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}
