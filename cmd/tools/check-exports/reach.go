// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"go/types"
	"sort"
	"strings"
)

// reach marks what the needed declarations need: the types their
// signatures, fields and values name, the rest of their closed set, and the
// methods that must share a name with a needed method. An exemption alone
// keeps a declaration's own name, its closed set and its linked methods, but
// not the types it names.
func (a *analysis) reach() {
	// The constants and variables of a type declared with them are its closed
	// set of values: the type and its values stay exported together when any
	// of them is needed.
	sets := map[*types.TypeName][]*declaration{}
	setOf := map[*declaration]*types.TypeName{}
	for _, d := range a.decls {
		if d.prodUse != nil {
			a.queue = append(a.queue, d)
		}
		if d.kind != kindConst && d.kind != kindVar {
			continue
		}
		var typeName *types.TypeName
		switch t := d.object.Type().(type) {
		case *types.Alias:
			typeName = t.Obj()
		case *types.Named:
			typeName = t.Obj()
		}
		if typeName != nil && typeName.Pkg() == d.object.Pkg() {
			sets[typeName] = append(sets[typeName], d)
			setOf[d] = typeName
		}
	}
	for typeName, values := range sets {
		if d := a.declaration(typeName); d != nil && d.checked() {
			sets[typeName] = append(values, d)
			setOf[d] = typeName
		}
	}
	for len(a.queue) > 0 {
		d := a.queue[0]
		a.queue = a.queue[1:]
		for _, linked := range a.groups[d] {
			if !linked.needed() {
				linked.reachedFrom = d
				a.queue = append(a.queue, linked)
			}
		}
		for _, member := range sets[setOf[d]] {
			if !member.needed() {
				member.reachedFrom = d
				a.queue = append(a.queue, member)
			}
		}
		if d.prodUse == nil && d.reachedFrom == nil {
			continue
		}
		var t types.Type
		switch d.kind {
		case kindType, kindInterface:
			switch d.object.Type().Underlying().(type) {
			case *types.Struct, *types.Interface:
			default:
				t = d.object.Type().Underlying()
			}
		default:
			t = d.object.Type()
		}
		visitNamed(t, func(n *types.Named) {
			if named := a.declaration(n.Origin().Obj()); named != nil && named.checked() && !named.needed() {
				named.reachedFrom = d
				a.queue = append(a.queue, named)
			}
		})
	}
}

// interfaceViolations reports the interfaces under internal/ that no type of
// another package implements, and those that code fills with a value of a
// wider interface it could take instead.
func (a *analysis) interfaceViolations() []violation {
	narrowed := map[*types.TypeName]conversion{}
	a.conversions(func(c conversion) {
		n, ok := types.Unalias(c.to).(*types.Named)
		if !ok || c.f.test || n.Obj().Pkg() == nil || !isInternal(n.Obj().Pkg().Path()) || !types.IsInterface(c.from) {
			return
		}
		if _, parameter := c.from.(*types.TypeParam); parameter {
			return
		}
		if _, seen := narrowed[n.Obj()]; !seen {
			narrowed[n.Obj()] = c
		}
	})
	var out []violation
	for _, d := range a.decls {
		if d.kind != kindInterface {
			continue
		}
		n, ok := d.object.Type().(*types.Named)
		if !ok {
			continue
		}
		i, ok := n.Underlying().(*types.Interface)
		if !ok || i.NumMethods() == 0 || !i.IsMethodSet() || n.TypeParams().Len() > 0 {
			continue
		}
		if c, ok := narrowed[n.Obj()]; ok {
			out = append(out, violation{d.position, d.name, ruleInterface, "narrows " + types.TypeString(c.from, nil) + ", which " + a.short(a.fset.Position(c.pos)) + " passes as it; take that interface"})
			continue
		}
		var own []string
		outside := false
		for _, implementation := range a.universe.implementations(i) {
			path := implementation.Obj().Pkg().Path()
			if path != d.pkg {
				outside = true
				break
			}
			own = append(own, implementation.Obj().Name())
		}
		if outside {
			continue
		}
		sort.Strings(own)
		why := "no type implements it"
		if len(own) > 0 {
			why = "implemented only in its own package, by " + strings.Join(own, ", ")
		}
		out = append(out, violation{d.position, d.name, ruleInterface, why})
	}
	return out
}
