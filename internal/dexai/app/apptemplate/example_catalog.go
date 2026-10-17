// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package apptemplate

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// DefaultExampleID is the example a Project starts from when its creation
// names none: the text summary, whose composed tree is app-template.tar and
// whose revision keeps the form app-template@<tree>.
const DefaultExampleID = "text-summary"

// exampleRevisionPrefix starts the template revision of a Project created
// from a chosen example: app-template/<id>@<tree>.
const exampleRevisionPrefix = "app-template/"

// ExampleRevision is the template revision of example id at Git tree tree:
// app-template/<id>@<tree>, the revision a Project created from it records.
func ExampleRevision(id, tree string) string { return exampleRevisionPrefix + id + "@" + tree }

// CatalogFile is the generated catalog in the embedded examples directory,
// and ArchiveFile names an example's composed tree there; the default
// example's tree is app-template.tar.
const CatalogFile = "examples/catalog.json"

// ArchiveFile is the embedded archive of a non-default example's composed tree.
func ArchiveFile(id string) string { return "examples/" + id + ".tar" }

// previousArchiveDirectory holds, for each example whose composed tree
// changed, the archive of its tree before the change, at most one per
// example: a server of the previous build may have accepted a Project of that
// tree, whose Setup a server of this build then runs.
const previousArchiveDirectory = "examples/previous"

// PreviousArchiveFile is the embedded archive of an example's previous tree,
// the default example's included.
func PreviousArchiveFile(id string) string { return previousArchiveDirectory + "/" + id + ".tar" }

//go:embed examples
var exampleFiles embed.FS

// Category is the start page's group of an example, a closed set.
type Category struct{ name string }

// The categories, as example.json spells them.
var (
	CategoryApprovals = Category{"Approvals"}
	CategoryFinance   = Category{"Finance"}
	CategorySupport   = Category{"Support"}
	CategoryPeople    = Category{"People"}
	CategoryContent   = Category{"Content"}
)

// categories lists every category.
var categories = []Category{CategoryApprovals, CategoryFinance, CategorySupport, CategoryPeople, CategoryContent}

// String is the category's name.
func (c Category) String() string { return c.name }

// CategoryNames are the names of every category, in the order the start
// page lists them, which the generator writes for Studio.
func CategoryNames() []string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, category.name)
	}
	return names
}

// ParseCategory reads a category name; false for any other name.
func ParseCategory(name string) (Category, bool) {
	for _, category := range categories {
		if category.name == name {
			return category, true
		}
	}
	return Category{}, false
}

// CatalogEntry is one example of the generated catalog, in catalog order,
// the default first: what the Session reads of it. What the start page shows
// is in the search index (SearchIndexFile).
type CatalogEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Card is the example's confirm_requirements input: its requirements,
	// without each feature's evidence, tests and untested reason, and its
	// design.
	Card json.RawMessage `json:"card"`
	// Trace is each feature of the card with the source that implements it
	// and the tests that prove it or why none does.
	Trace []TracedFeature `json:"trace"`
	// Files are the example's composed tree against the shared base.
	Files ExampleFiles `json:"files"`
	// Extends is the example whose composed tree this one starts from.
	Extends string `json:"extends,omitempty"`
	// Steps are the Steps of the example's Flow definitions as its own FDG
	// script renders them from its composed tree, the definitions Studio
	// draws of a Project that starts from it.
	Steps []FlowStep `json:"steps"`
}

// FlowStep is one Step of a rendered Flow definition: its Flow type and its
// Step type, both Go type names.
type FlowStep struct {
	FlowType string `json:"flow_type"`
	StepType string `json:"step_type"`
}

// TracedFeature is one requirement of an example's card with its evidence and
// either the API tests that prove it or why none does.
type TracedFeature struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	Evidence       []string `json:"evidence"`
	Tests          []string `json:"tests,omitempty"`
	UntestedReason string   `json:"untested_reason,omitempty"`
}

// ExampleFiles are the paths of an example's composed tree that differ from
// the shared base at the root of app-template/: the files it adds, those it
// replaces and those of the base it removes.
type ExampleFiles struct {
	Adds     []string `json:"adds"`
	Replaces []string `json:"replaces"`
	Removes  []string `json:"removes"`
}

// Example is one example of this server build.
type Example struct {
	ID, Title string
	Card      json.RawMessage
	Trace     []TracedFeature
	Files     ExampleFiles
	Extends   string
	Steps     []FlowStep
}

var catalog = sync.OnceValue(func() []Example {
	var entries []CatalogEntry
	comm.MustDo(json.Unmarshal(comm.Must(exampleFiles.ReadFile(CatalogFile)), &entries))
	examples := make([]Example, 0, len(entries))
	for _, entry := range entries {
		examples = append(examples, Example{ID: entry.ID, Title: entry.Title, Card: entry.Card, Trace: entry.Trace, Files: entry.Files, Extends: entry.Extends,
			Steps: entry.Steps})
	}
	return examples
})

// ErrExampleNotOffered reports an example this server build does not hold.
var ErrExampleNotOffered = errors.New("the example is not offered")

// trees decodes each non-default example's archive once, when it is first
// asked for.
var trees = sync.OnceValue(func() map[string]func() *repohost.Template {
	decoders := map[string]func() *repohost.Template{}
	for _, example := range catalog() {
		if example.ID != DefaultExampleID {
			decoders[example.ID] = sync.OnceValue(func() *repohost.Template {
				return comm.Must(Decode(comm.Must(exampleFiles.ReadFile(ArchiveFile(example.ID)))))
			})
		}
	}
	return decoders
})

// ExampleIDsForTest lists the examples of this server build, the default
// first. A catalog that does not decode is a broken build, so the first call
// panics.
func ExampleIDsForTest() []string {
	ids := []string{}
	for _, example := range catalog() {
		ids = append(ids, example.ID)
	}
	return ids
}

// FindExample returns the example id of this server build.
func FindExample(id string) (Example, bool) {
	for _, example := range catalog() {
		if example.ID == id {
			return example, true
		}
	}
	return Example{}, false
}

// ExampleTemplate returns the composed tree of example id under its template
// revision app-template/<id>@<tree>, or ErrExampleNotOffered for an example
// this build does not hold.
func ExampleTemplate(id string) (*repohost.Template, error) {
	if _, found := FindExample(id); !found {
		return nil, fmt.Errorf("%w: %q", ErrExampleNotOffered, id)
	}
	var tree *repohost.Template
	if id == DefaultExampleID {
		tree = Current()
	} else {
		tree = trees()[id]()
	}
	named := *tree
	named.Revision = ExampleRevision(id, tree.TreeID)
	return &named, nil
}

// ParseExampleRevision reads the template revision of a Project created from
// a chosen example; false for app-template@<tree> and any other value.
func ParseExampleRevision(revision string) (id, tree string, isExample bool) {
	rest, found := strings.CutPrefix(revision, exampleRevisionPrefix)
	if !found {
		return "", "", false
	}
	id, tree, found = strings.Cut(rest, "@")
	return id, tree, found && id != "" && flowid.IsCommitSHA(tree)
}

// previousTrees decodes each previous archive once, when it is first asked
// for, by example ID.
var previousTrees = sync.OnceValue(func() map[string]func() *repohost.Template {
	decoders := map[string]func() *repohost.Template{}
	entries, err := fs.ReadDir(exampleFiles, previousArchiveDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return decoders
	}
	for _, entry := range comm.Must(entries, err) {
		id, isArchive := strings.CutSuffix(entry.Name(), ".tar")
		if !isArchive {
			panic(fmt.Sprintf("the embedded previous archives hold %s", entry.Name()))
		}
		decoders[id] = sync.OnceValue(func() *repohost.Template {
			return comm.Must(Decode(comm.Must(exampleFiles.ReadFile(path.Join(previousArchiveDirectory, entry.Name())))))
		})
	}
	return decoders
})

// previousTemplate is the previous tree of example id under revision when it
// is tree, or nil.
func previousTemplate(id, tree, revision string) *repohost.Template {
	decode, found := previousTrees()[id]
	if !found || decode().TreeID != tree {
		return nil
	}
	named := *decode()
	named.Revision = revision
	return &named
}

// PreviousTemplateForTest is the previous tree this build keeps for example
// id under its revision, app-template/<id>@<tree> or app-template@<tree> for
// the default; false when it keeps none.
func PreviousTemplateForTest(id string) (*repohost.Template, bool) {
	decode, found := previousTrees()[id]
	if !found {
		return nil, false
	}
	revision := ExampleRevision(id, decode().TreeID)
	if id == DefaultExampleID {
		revision = revisionPrefix + decode().TreeID
	}
	return previousTemplate(id, decode().TreeID, revision), true
}

// ForRevision returns the composed tree that revision names when this server
// build holds it: the default template, app-template@<tree>, or a chosen
// example, app-template/<id>@<tree>, each at its current tree or, after a
// change, at its previous one. A revision of another build is an error.
func ForRevision(revision string) (*repohost.Template, error) {
	if revision == Current().Revision {
		return Current(), nil
	}
	if id, tree, isExample := ParseExampleRevision(revision); isExample {
		if template, err := ExampleTemplate(id); err == nil && template.TreeID == tree {
			return template, nil
		}
		if template := previousTemplate(id, tree, revision); template != nil {
			return template, nil
		}
	}
	if tree, isDefault := strings.CutPrefix(revision, revisionPrefix); isDefault && flowid.IsCommitSHA(tree) {
		if template := previousTemplate(DefaultExampleID, tree, revision); template != nil {
			return template, nil
		}
	}
	return nil, fmt.Errorf("this server build holds no template of revision %s", revision)
}
