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

// CatalogFile is the generated catalog in the embedded examples directory,
// and ArchiveFile names an example's composed tree there; the default
// example's tree is app-template.tar.
const CatalogFile = "examples/catalog.json"

// ArchiveFile is the embedded archive of a non-default example's composed tree.
func ArchiveFile(id string) string { return "examples/" + id + ".tar" }

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

// ParseCategory reads a category name; false for any other name.
func ParseCategory(name string) (Category, bool) {
	for _, category := range categories {
		if category.name == name {
			return category, true
		}
	}
	return Category{}, false
}

// CatalogEntry is one example of the generated catalog, in the order the
// start page lists them, the default first.
type CatalogEntry struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Category     string   `json:"category"`
	Apps         []string `json:"apps"`
	Decider      string   `json:"decider"`
	Description  string   `json:"description"`
	ConnectorIDs []string `json:"connectorIds"`
	// Card is the example's confirm_requirements input: its requirements,
	// without each feature's evidence, tests and untested reason, and its
	// design.
	Card json.RawMessage `json:"card"`
}

// Example is one example of this server build.
type Example struct {
	ID, Title            string
	Category             Category
	Apps                 []string
	Decider, Description string
	ConnectorIDs         []string
	Card                 json.RawMessage
}

var catalog = sync.OnceValue(func() []Example {
	var entries []CatalogEntry
	comm.MustDo(json.Unmarshal(comm.Must(exampleFiles.ReadFile(CatalogFile)), &entries))
	examples := make([]Example, 0, len(entries))
	for _, entry := range entries {
		category, known := ParseCategory(entry.Category)
		if !known {
			panic(fmt.Sprintf("the embedded example %s has the unknown category %q", entry.ID, entry.Category))
		}
		examples = append(examples, Example{ID: entry.ID, Title: entry.Title, Category: category, Apps: entry.Apps, Decider: entry.Decider,
			Description: entry.Description, ConnectorIDs: entry.ConnectorIDs, Card: entry.Card})
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

// Examples lists the examples of this server build, the default first. A
// catalog that does not decode is a broken build, so the first call panics.
func Examples() []Example { return catalog() }

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
	named.Revision = exampleRevisionPrefix + id + "@" + tree.TreeID
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

// ForRevision returns the composed tree that revision names when this server
// build holds it: the default template, app-template@<tree>, or a chosen
// example, app-template/<id>@<tree>. A revision of another build is an error.
func ForRevision(revision string) (*repohost.Template, error) {
	if revision == Current().Revision {
		return Current(), nil
	}
	if id, tree, isExample := ParseExampleRevision(revision); isExample {
		if template, err := ExampleTemplate(id); err == nil && template.TreeID == tree {
			return template, nil
		}
	}
	return nil, fmt.Errorf("this server build holds no template of revision %s", revision)
}
