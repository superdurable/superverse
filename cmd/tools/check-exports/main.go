// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command check-exports enforces the export rules of AGENTS.md on every
// package under internal/:
//
//   - no package-internal interface: an interface whose implementations are
//     all in its own package, or that only narrows an existing interface;
//   - no needless export: an exported identifier that no other package's
//     production code uses, directly or through an exported signature, field
//     or method set that another package's production code uses;
//   - test-only exports say so: an exported identifier that other packages use
//     only in their tests ends in ForTest.
//
// It loads the module with its tests and every build tag, resolves each use by
// type information and reports each violation as
// `file:line: identifier: rule (why)`. It runs from the repository root.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

const (
	modulePath     = "github.com/superdurable/superversev2"
	internalPrefix = modulePath + "/internal/"
	dexPackage     = "github.com/superdurable/dex/sdk-go/dex"
)

// buildTags are every build tag the module's files use, so that each file is
// loaded and each of its uses counts.
const buildTags = "live,hosted,integration,apptest"

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:        root,
		Tests:      true,
		BuildFlags: []string{"-tags=" + buildTags},
	}, "./...")
	if err != nil {
		fail(err)
	}
	broken := false
	packages.Visit(loaded, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			fmt.Fprintln(os.Stderr, e)
			broken = true
		}
	})
	if broken {
		fail(errors.New("the module does not load"))
	}
	violations, checked := analyze(newProgram(root, loaded))
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i].position, violations[j].position
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	for _, v := range violations {
		file, err := filepath.Rel(root, v.position.Filename)
		if err != nil {
			file = v.position.Filename
		}
		fmt.Printf("%s:%d: %s: %s (%s)\n", file, v.position.Line, v.identifier, v.rule, v.why)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "check-exports: %d violations of the export rules (AGENTS.md, Agent and engineering invariants)\n", len(violations))
		os.Exit(1)
	}
	fmt.Printf("Exports: the %d exported identifiers and interfaces of internal/ follow the export rules\n", checked)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "check-exports:", err)
	os.Exit(1)
}
