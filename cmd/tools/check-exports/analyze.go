// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"go/token"
	"go/types"
	"strings"
)

type violation struct {
	position   token.Position
	identifier string
	rule       string
	why        string
}

const (
	ruleInterface = "no package-internal interface"
	ruleExport    = "no needless export"
	ruleForTest   = "test-only exports say so"
)

// analysis holds the declarations with what the program reveals about them.
type analysis struct {
	*program
	decls    map[key]*declaration
	members  map[*declaration][]*declaration
	universe *universe
	// groups links the methods that must share their name: an interface's
	// method and the methods of its own package that implement it.
	groups map[*declaration][]*declaration
	queue  []*declaration
}

// analyze returns the violations and the number of declarations it checked.
func analyze(p *program) ([]violation, int) {
	a := &analysis{program: p, decls: p.declarations(), members: map[*declaration][]*declaration{}, groups: map[*declaration][]*declaration{}}
	for _, d := range a.decls {
		if d.owner != nil {
			a.members[d.owner] = append(a.members[d.owner], d)
		}
	}
	a.universe = p.universe()
	a.recordUses()
	a.exemptDex()
	a.exemptSerializedFields()
	a.exemptImplementations()
	violations := a.interfaceViolations()
	a.reach()
	return append(violations, a.exportViolations()...), len(a.decls)
}

func (a *analysis) declaration(object types.Object) *declaration {
	if object == nil || !object.Pos().IsValid() {
		return nil
	}
	return a.decls[a.key(object.Pos())]
}

// recordUses records the first use of each declaration by another package's
// production code and by another package's test.
func (a *analysis) recordUses() {
	for _, pkg := range a.sources {
		for ident, object := range pkg.TypesInfo.Uses {
			d := a.declaration(object)
			if d == nil || d.pkg == pkg.PkgPath {
				continue
			}
			position := a.fset.Position(ident.Pos())
			if strings.HasSuffix(position.Filename, "_test.go") {
				if d.testUse == nil {
					d.testUse = &position
				}
			} else if d.prodUse == nil {
				d.prodUse = &position
			}
		}
	}
}

// exempt marks a declaration as staying exported and queues it so that the
// methods sharing its name follow.
func (a *analysis) exempt(d *declaration, why string) {
	if d == nil || d.exemption != "" {
		return
	}
	d.exemption = why
	a.queue = append(a.queue, d)
}

// exportViolations reports the exported identifiers that are not needed, and
// the test-only ones whose name does not say so.
func (a *analysis) exportViolations() []violation {
	var out []violation
	for _, d := range a.decls {
		if !d.checked() || d.needed() {
			if d.checked() && d.prodUse != nil && strings.HasSuffix(d.object.Name(), "ForTest") {
				out = append(out, violation{d.position, d.name, ruleForTest, "named ForTest but used by production code at " + a.short(*d.prodUse)})
			}
			continue
		}
		switch {
		case d.testUse != nil && d.owner != nil && !a.productionFacing(d.owner):
			// A member of a type that only tests use: the type's name, or the
			// ForTest function that returns it, already says so.
		case d.testUse != nil && !strings.HasSuffix(d.object.Name(), "ForTest"):
			out = append(out, violation{d.position, d.name, ruleForTest, "used outside " + d.pkg[len(internalPrefix):] + " only by tests, first at " + a.short(*d.testUse)})
		case d.testUse == nil:
			out = append(out, violation{d.position, d.name, ruleExport, "used by no other package"})
		}
	}
	return out
}

// productionFacing reports whether another package's production code needs a
// type: the type itself, or one of its members.
func (a *analysis) productionFacing(owner *declaration) bool {
	if owner.needed() {
		return true
	}
	for _, member := range a.members[owner] {
		if member.needed() {
			return true
		}
	}
	return false
}
