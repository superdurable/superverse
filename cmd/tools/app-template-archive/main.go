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
// internal/dexai/app/apptemplate embeds, with the catalog of the examples, the
// search index and the match rules of the closest-example decision, and the
// Studio initial design of the default one. It keeps main's archive of each
// example whose tree changed, or that it removes, as its previous archive,
// and decides every labelled request,
// failing when a pick differs from its expected answer. It runs through
// `go generate ./internal/dexai/app/apptemplate`; the output is deterministic
// so scripts/check-app-template.sh can detect drift.
//
// With -compose <id> -into <directory> it writes the composed tree of one
// example into directory instead, for the template's own checks; files of the
// directory that the tree does not hold are removed, except the ignored
// dependencies and generated code, so a repeated check reuses them. With
// -list -changed <file> it lists only the default example and the examples the
// repository paths of file (one per line) reach, for the validation workflow.
// With -propose-match <id> it prints a match block for one example, which its
// author reviews before committing it in example.json.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

func main() {
	compose := flag.String("compose", "", "write the composed tree of this example")
	into := flag.String("into", "", "the directory -compose writes into")
	list := flag.Bool("list", false, "print the example IDs, the default first")
	changed := flag.String("changed", "", "with -list, a file of changed repository paths: print only the examples they reach")
	propose := flag.String("propose-match", "", "print a match block of this example for its author to review")
	flag.Parse()
	var err error
	switch {
	case *list:
		err = listExamples(context.Background(), *changed)
	case *propose != "":
		err = proposeMatchOf(context.Background(), *propose)
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

// matching is the generated search index and match rules of the examples,
// encoded as written and decoded as the server reads them.
type matching struct {
	indexJSON, rulesJSON []byte
	index                *apptemplate.SearchIndex
	rules                *apptemplate.MatchRules
}

// buildMatching builds the search index and the match rules from the shared
// files of app-template/examples and every example.
func buildMatching(templateDirectory string, examples []composedExample) (matching, error) {
	var words searchWordsSource
	if err := readShared(templateDirectory, searchWordsFile, &words); err != nil {
		return matching{}, err
	}
	var facets facetsSource
	if err := readShared(templateDirectory, facetsFile, &facets); err != nil {
		return matching{}, err
	}
	rules, err := buildMatchRules(examples, facets)
	if err != nil {
		return matching{}, err
	}
	built := matching{}
	if built.indexJSON, err = lineJSON(buildSearchIndex(examples, apptemplate.NewSearchVocabulary(words.StopWords, words.Synonyms)), "postings"); err != nil {
		return matching{}, err
	}
	if built.rulesJSON, err = stableJSON(rules); err != nil {
		return matching{}, err
	}
	if built.index, err = apptemplate.DecodeSearchIndex(built.indexJSON); err != nil {
		return matching{}, err
	}
	if built.rules, err = apptemplate.DecodeMatchRules(built.rulesJSON, built.index); err != nil {
		return matching{}, err
	}
	return built, nil
}

func proposeMatchOf(ctx context.Context, id string) error {
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	templateDirectory := filepath.Join(root, "app-template")
	examples, err := readTemplate(ctx, templateDirectory)
	if err != nil {
		return err
	}
	built, err := buildMatching(templateDirectory, examples)
	if err != nil {
		return err
	}
	for _, example := range examples {
		if example.manifest.ID == id {
			return proposeMatch(example, built.rules)
		}
	}
	return fmt.Errorf("app-template has no example %q", id)
}

// run composes every example, validates it, decides the labelled requests and
// writes the embedded outputs and Studio's copies.
func run(ctx context.Context) error {
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	templateDirectory := filepath.Join(root, "app-template")
	examples, err := readTemplate(ctx, templateDirectory)
	if err != nil {
		return err
	}
	built, err := buildMatching(templateDirectory, examples)
	if err != nil {
		return err
	}
	var labelled struct {
		Requests []labelledRequest `json:"requests"`
	}
	if err = readShared(templateDirectory, labelledRequestsFile, &labelled); err != nil {
		return err
	}
	searches, err := checkLabelled(labelled.Requests, examples, built.index, built.rules)
	if err != nil {
		return err
	}
	outputs := filepath.Join(root, "internal", "dexai", "app", "apptemplate")
	base, err := previousBase(ctx, root)
	if err != nil {
		return err
	}
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
		previous := filepath.Join(outputs, filepath.FromSlash(apptemplate.PreviousArchiveFile(example.manifest.ID)))
		kept, keepErr := keepPrevious(ctx, root, base, target, previous, archive)
		if keepErr != nil {
			return fmt.Errorf("example %s: %w", example.manifest.ID, keepErr)
		}
		written[previous] = kept
		if err = writeOutput(target, archive); err != nil {
			return err
		}
		written[target] = true
	}
	if defaultTree == nil {
		return fmt.Errorf("app-template has no default example %s", apptemplate.DefaultExampleID)
	}
	if err = keepRemoved(ctx, root, base, outputs, examples, written); err != nil {
		return err
	}
	catalog, err := stableJSON(entries)
	if err != nil {
		return err
	}
	labelledJSON, err := lineJSON(struct {
		Requests []labelledSearch `json:"requests"`
	}{searches}, "requests")
	if err != nil {
		return err
	}
	studio := filepath.Join(root, "web", "studio", "lib", "generated", "example-catalog")
	for target, contents := range map[string][]byte{
		filepath.Join(studio, "example-categories.ts"):                          studioCategories(),
		filepath.Join(outputs, filepath.FromSlash(apptemplate.CatalogFile)):     catalog,
		filepath.Join(outputs, filepath.FromSlash(apptemplate.SearchIndexFile)): built.indexJSON,
		filepath.Join(outputs, filepath.FromSlash(apptemplate.MatchRulesFile)):  built.rulesJSON,
		filepath.Join(studio, path.Base(apptemplate.SearchIndexFile)):           built.indexJSON,
		filepath.Join(studio, "labelled-search.json"):                           labelledJSON,
	} {
		if err = writeOutput(target, contents); err != nil {
			return err
		}
		written[target] = true
	}
	if err = removeStale(filepath.Join(outputs, "examples"), written); err != nil {
		return err
	}
	if err = removeStale(studio, written); err != nil {
		return err
	}
	return renderDesign(ctx, root, defaultTree)
}

// studioCategories is the TypeScript module of the categories that Studio's
// start page lists, generated from the closed set apptemplate declares, so
// that Studio does not declare it again.
func studioCategories() []byte {
	quoted := make([]string, 0, len(apptemplate.CategoryNames()))
	for _, name := range apptemplate.CategoryNames() {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return fmt.Appendf(nil, `// Code generated by cmd/tools/app-template-archive from internal/dexai/app/apptemplate; DO NOT EDIT.

/** The start page's groups of the examples, in the order it lists them, as the server declares them. */
export const EXAMPLE_CATEGORIES = [%s] as const;

/** One of EXAMPLE_CATEGORIES, as the examples and the placeholders name it. */
export type ExampleCategory = (typeof EXAMPLE_CATEGORIES)[number];
`, strings.Join(quoted, ", "))
}

// previousBase is the commit whose archives become the previous ones: main
// as the branch started from it, the merge base of HEAD and origin/main; in a
// merge commit without origin/main, such as the pull request check's checkout,
// its first parent; else HEAD. Every commit of a branch thus keeps main's
// tree of an example it changes, however often it changes it.
func previousBase(ctx context.Context, root string) (string, error) {
	if base, err := exec.CommandContext(ctx, "git", "-C", root, "merge-base", "HEAD", "origin/main").Output(); err == nil {
		return strings.TrimSpace(string(base)), nil
	}
	if err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "--quiet", "HEAD^2").Run(); err == nil {
		return "HEAD^1", nil
	}
	if err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "--quiet", "HEAD").Run(); err != nil {
		return "", fmt.Errorf("find the commit whose archives become the previous ones: %w", err)
	}
	return "HEAD", nil
}

// keepPrevious writes the previous archive of an example whose new archive
// is archive: the archive base holds at target when its tree differs, or else
// the previous archive base holds, if any. It reports whether it wrote one.
// Comparing with a commit, not the working tree, keeps a repeated generation
// from replacing a released tree with an uncommitted one.
func keepPrevious(ctx context.Context, root, base, target, previous string, archive []byte) (bool, error) {
	committed, found, err := committedFile(ctx, root, base, target)
	if err != nil || !found {
		return false, err
	}
	tree, err := apptemplate.Decode(committed)
	if err != nil {
		return false, fmt.Errorf("read the committed archive %s: %w", target, err)
	}
	if tree.TreeID != comm.Must(apptemplate.Decode(archive)).TreeID {
		return true, writeOutput(previous, committed)
	}
	kept, found, err := committedFile(ctx, root, base, previous)
	if err != nil || !found {
		return false, err
	}
	return true, writeOutput(previous, kept)
}

// keepRemoved keeps the last archive of each example that base holds and the
// template no longer composes as its previous archive: base's archive of it,
// else base's previous archive of it. Setups of Projects that a server of the
// build that still offered it accepted then find its tree.
func keepRemoved(ctx context.Context, root, base, outputs string, examples []composedExample, written map[string]bool) error {
	composed := map[string]bool{}
	for _, example := range examples {
		composed[example.manifest.ID] = true
	}
	removed := map[string]bool{}
	for _, directory := range []string{"examples", path.Dir(apptemplate.PreviousArchiveFile(""))} {
		names, err := committedNames(ctx, root, base, filepath.Join(outputs, filepath.FromSlash(directory)))
		if err != nil {
			return err
		}
		for _, name := range names {
			if id, isArchive := strings.CutSuffix(name, ".tar"); isArchive && !composed[id] {
				removed[id] = true
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(removed)) {
		previous := filepath.Join(outputs, filepath.FromSlash(apptemplate.PreviousArchiveFile(id)))
		for _, source := range []string{filepath.Join(outputs, filepath.FromSlash(apptemplate.ArchiveFile(id))), previous} {
			contents, found, err := committedFile(ctx, root, base, source)
			if err != nil {
				return err
			}
			if found {
				written[previous] = true
				if err = writeOutput(previous, contents); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// committedNames are the names of the files directly in directory at
// revision.
func committedNames(ctx context.Context, root, revision, directory string) ([]string, error) {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return nil, err
	}
	listed, err := exec.CommandContext(ctx, "git", "-C", root, "ls-tree", "--name-only", revision, "--", filepath.ToSlash(relative)+"/").Output()
	if err != nil {
		return nil, fmt.Errorf("list %s at %s: %w", relative, revision, err)
	}
	var names []string
	for line := range strings.Lines(string(listed)) {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, path.Base(name))
		}
	}
	return names, nil
}

// committedFile reads a file as revision holds it; false when it holds none.
func committedFile(ctx context.Context, root, revision, name string) ([]byte, bool, error) {
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return nil, false, err
	}
	listed, err := exec.CommandContext(ctx, "git", "-C", root, "ls-tree", "--name-only", revision, "--", filepath.ToSlash(relative)).Output()
	if err != nil {
		return nil, false, fmt.Errorf("list %s at %s: %w", relative, revision, err)
	}
	if strings.TrimSpace(string(listed)) == "" {
		return nil, false, nil
	}
	contents, err := exec.CommandContext(ctx, "git", "-C", root, "show", revision+":"+filepath.ToSlash(relative)).Output()
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %s: %w", relative, revision, err)
	}
	return contents, true, nil
}

// writeOutput writes a generated file, creating its directory.
func writeOutput(target string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	return os.WriteFile(target, contents, 0o600)
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

// stableJSON encodes a generated document with stable formatting.
func stableJSON(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// lineJSON encodes a generated document with one line per top-level field
// and, for the field long, one line per element, which keeps a large index
// small and its diffs readable.
func lineJSON(value any, long string) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	var order []string
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	for decoder.More() {
		key, keyErr := decoder.Token()
		if keyErr != nil {
			return nil, keyErr
		}
		order = append(order, key.(string))
		var skipped json.RawMessage
		if err = decoder.Decode(&skipped); err != nil {
			return nil, err
		}
	}
	var out bytes.Buffer
	out.WriteString("{\n")
	for index, key := range order {
		fmt.Fprintf(&out, "  %q: ", key)
		if key == long {
			writeElements(&out, fields[key])
		} else {
			out.Write(fields[key])
		}
		if index < len(order)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.Bytes(), nil
}

// writeElements writes a JSON array or object with one element per line.
func writeElements(out *bytes.Buffer, value json.RawMessage) {
	var elements []json.RawMessage
	if json.Unmarshal(value, &elements) == nil {
		out.WriteString("[\n")
		for index, element := range elements {
			out.WriteString("    ")
			out.Write(element)
			if index < len(elements)-1 {
				out.WriteString(",")
			}
			out.WriteString("\n")
		}
		out.WriteString("  ]")
		return
	}
	var members map[string]json.RawMessage
	comm.MustDo(json.Unmarshal(value, &members))
	keys := slices.Sorted(maps.Keys(members))
	out.WriteString("{\n")
	for index, key := range keys {
		fmt.Fprintf(out, "    %s: ", comm.Must(json.Marshal(key)))
		out.Write(members[key])
		if index < len(keys)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("  }")
}

// removeStale removes the generated files below directory that this run did
// not write, such as those of examples that no longer exist, and the
// directories it leaves empty.
func removeStale(directory string, written map[string]bool) error {
	found, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range found {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if err = removeStale(path, written); err != nil {
				return err
			}
			if rest, readErr := os.ReadDir(path); readErr != nil || len(rest) > 0 {
				continue
			}
		} else if written[path] {
			continue
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
