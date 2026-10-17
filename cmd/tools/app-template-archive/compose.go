// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

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
	"path"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// The layout of app-template/: the shared base at its root, and one folder
// per example below examplesDirectory with its manifest.
const (
	examplesDirectory = "examples"
	manifestFile      = "example.json"
	// examplesReadme documents the folders for their authors; no tree holds it.
	examplesReadme = "README.md"
)

// templateFile is one committable file of a composed tree.
type templateFile struct {
	name       string
	executable bool
	contents   []byte
}

// composedExample is one example's manifest and its composed tree in path
// order.
type composedExample struct {
	manifest exampleManifest
	files    []templateFile
}

// readTemplate reads the base and every example folder of templateDirectory
// and composes each example: its parent's tree (the base, or the example it
// extends) without the files it removes, with its own files added or
// replacing. The default example comes first, the others in ID order.
func readTemplate(ctx context.Context, templateDirectory string) ([]composedExample, error) {
	listed, err := committableFiles(ctx, templateDirectory)
	if err != nil {
		return nil, err
	}
	base := map[string]templateFile{}
	overlays := map[string]map[string]templateFile{}
	manifests := map[string]exampleManifest{}
	for _, file := range listed {
		rest, isExample := strings.CutPrefix(file.name, examplesDirectory+"/")
		if !isExample {
			base[file.name] = file
			continue
		}
		if rest == examplesReadme {
			continue
		}
		id, name, found := strings.Cut(rest, "/")
		if !found {
			return nil, fmt.Errorf("app-template/%s is outside an example folder", file.name)
		}
		if name == manifestFile {
			manifest, manifestErr := decodeManifest(file.contents)
			if manifestErr != nil {
				return nil, fmt.Errorf("app-template/%s: %w", file.name, manifestErr)
			}
			if manifest.ID != id {
				return nil, fmt.Errorf("app-template/%s names the example %q, not its folder %q", file.name, manifest.ID, id)
			}
			manifests[id] = manifest
			continue
		}
		if overlays[id] == nil {
			overlays[id] = map[string]templateFile{}
		}
		file.name = name
		overlays[id][name] = file
	}
	for id := range overlays {
		if _, found := manifests[id]; !found {
			return nil, fmt.Errorf("app-template/examples/%s has no %s", id, manifestFile)
		}
	}
	composed := map[string]map[string]templateFile{}
	var composeOne func(id string, seen map[string]bool) (map[string]templateFile, error)
	composeOne = func(id string, seen map[string]bool) (map[string]templateFile, error) {
		if tree, done := composed[id]; done {
			return tree, nil
		}
		manifest, found := manifests[id]
		if !found {
			return nil, fmt.Errorf("no example %q to extend", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("example %s extends itself", id)
		}
		seen[id] = true
		parent := base
		if manifest.Extends != "" {
			if parent, err = composeOne(manifest.Extends, seen); err != nil {
				return nil, err
			}
		}
		tree := make(map[string]templateFile, len(parent))
		for name, file := range parent {
			tree[name] = file
		}
		for _, name := range manifest.Removes {
			if _, held := tree[name]; !held {
				return nil, fmt.Errorf("example %s removes %s, which its parent does not hold", id, name)
			}
			if _, replaced := overlays[id][name]; replaced {
				return nil, fmt.Errorf("example %s both removes and replaces %s", id, name)
			}
			delete(tree, name)
		}
		for name, file := range overlays[id] {
			if previous, held := tree[name]; held && previous.executable == file.executable && bytes.Equal(previous.contents, file.contents) {
				return nil, fmt.Errorf("example %s holds %s unchanged from its parent; remove it from the folder", id, name)
			}
			tree[name] = file
		}
		composed[id] = tree
		return tree, nil
	}
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if (ids[i] == apptemplate.DefaultExampleID) != (ids[j] == apptemplate.DefaultExampleID) {
			return ids[i] == apptemplate.DefaultExampleID
		}
		return ids[i] < ids[j]
	})
	examples := make([]composedExample, 0, len(ids))
	for _, id := range ids {
		tree, composeErr := composeOne(id, map[string]bool{})
		if composeErr != nil {
			return nil, composeErr
		}
		files := make([]templateFile, 0, len(tree))
		for _, file := range tree {
			files = append(files, file)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
		example := composedExample{manifest: manifests[id], files: files}
		if err = validateExample(example); err != nil {
			return nil, fmt.Errorf("example %s: %w", id, err)
		}
		examples = append(examples, example)
	}
	return examples, nil
}

// committableFiles reads the template's tracked files plus the new files
// .gitignore keeps, in path order. A listed file deleted from the working
// tree and not yet committed is skipped.
func committableFiles(ctx context.Context, templateDirectory string) ([]templateFile, error) {
	list := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	list.Dir = templateDirectory
	listed, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("list template files: %w", err)
	}
	names := []string{}
	for _, name := range strings.Split(string(listed), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	root, err := os.OpenRoot(templateDirectory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	files := make([]templateFile, 0, len(names))
	for _, name := range names {
		info, statErr := root.Lstat(name)
		if errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("template file %s is not a regular file", name)
		}
		contents, readErr := root.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		files = append(files, templateFile{name: name, executable: info.Mode().Perm()&0o111 != 0, contents: contents})
	}
	return files, nil
}

// pack archives a composed tree in path order: regular files only, PAX, mode
// 0644 or 0755 (which Decode turns into the Git mode), modification time 0.
func pack(files []templateFile) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		mode := int64(0o644)
		if file.executable {
			mode = 0o755
		}
		header := &tar.Header{Typeflag: tar.TypeReg, Name: file.name, Mode: mode, Size: int64(len(file.contents)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.contents); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

func mustPack(files []templateFile) []byte {
	archive, err := pack(files)
	if err != nil {
		panic(err)
	}
	return archive
}

// regeneratedPrefixes are the ignored paths of a composed directory that a
// repeated -compose keeps: restored dependencies and generated code.
var regeneratedPrefixes = []string{".local/", "web/node_modules/", "web/dist/", "internal/api/generated/", "web/src/api/generated/"}

// materialize writes a composed tree into directory and removes the other
// files there, except the regenerated ones.
func materialize(files []templateFile, directory string) error {
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	held := map[string]bool{}
	for _, file := range files {
		held[file.name] = true
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || name == "." {
			return walkErr
		}
		for _, prefix := range regeneratedPrefixes {
			if name+"/" == prefix || strings.HasPrefix(name, prefix) {
				return fs.SkipDir
			}
		}
		if !entry.IsDir() && !held[name] {
			return root.Remove(name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, file := range files {
		if err = root.MkdirAll(path.Dir(file.name), 0o750); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if file.executable {
			mode = 0o755
		}
		if current, readErr := root.ReadFile(file.name); readErr != nil || !bytes.Equal(current, file.contents) {
			if err = root.WriteFile(file.name, file.contents, mode); err != nil {
				return err
			}
		}
		if err = root.Chmod(file.name, mode); err != nil {
			return err
		}
	}
	return nil
}

// fileOf returns a composed file by name.
func fileOf(files []templateFile, name string) (templateFile, bool) {
	index := sort.Search(len(files), func(i int) bool { return files[i].name >= name })
	if index < len(files) && files[index].name == name {
		return files[index], true
	}
	return templateFile{}, false
}

// connectorIDs are the connectors the composed dex-app.yaml declares.
func connectorIDs(files []templateFile) ([]string, error) {
	manifest, found := fileOf(files, "dex-app.yaml")
	if !found {
		return nil, errors.New("the composed tree has no dex-app.yaml")
	}
	var document struct {
		Connectors []struct {
			ConnectorID string `json:"connectorId"`
		} `json:"connectors"`
	}
	if err := json.Unmarshal(manifest.contents, &document); err != nil {
		return nil, fmt.Errorf("read dex-app.yaml: %w", err)
	}
	ids := []string{}
	for _, connector := range document.Connectors {
		ids = append(ids, connector.ConnectorID)
	}
	return ids, nil
}

// testNames are the test functions of the composed API tests.
func testNames(files []templateFile) map[string]bool {
	names := map[string]bool{}
	for _, file := range files {
		if path.Dir(file.name) != "internal/apptest" || !strings.HasSuffix(file.name, "_test.go") {
			continue
		}
		for _, line := range strings.Split(string(file.contents), "\n") {
			if name, found := strings.CutPrefix(line, "func Test"); found {
				if open := strings.Index(name, "("); open > 0 {
					names["Test"+name[:open]] = true
				}
			}
		}
	}
	return names
}
