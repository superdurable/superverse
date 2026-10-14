// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// program is the loaded module: every package variant with its files, and the
// type-checked packages they reach.
type program struct {
	root string
	fset *token.FileSet
	// sources are the module's package variants that have syntax: each
	// package, its test variant and its external test package. The generated
	// test mains are left out.
	sources []*packages.Package
	// reached are the type-checked packages of the module and of everything it
	// imports, each once.
	reached []*types.Package
}

func newProgram(root string, loaded []*packages.Package) *program {
	p := &program{root: root}
	for _, pkg := range loaded {
		p.fset = pkg.Fset
		if strings.HasSuffix(pkg.PkgPath, ".test") || len(pkg.Syntax) == 0 {
			continue
		}
		p.sources = append(p.sources, pkg)
	}
	seen := map[*types.Package]bool{}
	var walk func(*types.Package)
	walk = func(t *types.Package) {
		if seen[t] {
			return
		}
		seen[t] = true
		p.reached = append(p.reached, t)
		for _, imported := range t.Imports() {
			walk(imported)
		}
	}
	for _, pkg := range p.sources {
		walk(pkg.Types)
	}
	return p
}

// key identifies a declaration across the package variants that type-check
// the same file: its file and offset.
type key struct {
	file   string
	offset int
}

func (p *program) key(pos token.Pos) key {
	position := p.fset.Position(pos)
	return key{position.Filename, position.Offset}
}

// file is one source file with the package variant that type-checked it.
type file struct {
	pkg       *packages.Package
	syntax    *ast.File
	name      string
	test      bool
	generated bool
}

// files calls visit for each file of each package variant.
func (p *program) files(visit func(file)) {
	for _, pkg := range p.sources {
		for _, syntax := range pkg.Syntax {
			name := p.fset.Position(syntax.Pos()).Filename
			visit(file{pkg: pkg, syntax: syntax, name: name, test: strings.HasSuffix(name, "_test.go"), generated: ast.IsGenerated(syntax)})
		}
	}
}

// short is a position relative to the repository root.
func (p *program) short(position token.Position) string {
	name, err := filepath.Rel(p.root, position.Filename)
	if err != nil {
		name = position.Filename
	}
	return fmt.Sprintf("%s:%d", name, position.Line)
}

func isInternal(path string) bool { return strings.HasPrefix(path, internalPrefix) }

// named returns the named type of t, through an alias and a pointer, or nil.
func named(t types.Type) *types.Named {
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		t = types.Unalias(pointer.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}
