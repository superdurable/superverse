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
	"strings"
)

// exemptSerializedFields keeps exported the struct fields whose names a
// serialization reads: fields with a struct tag and every field of a type the
// program serializes. A type is serialized when it is a type argument of a
// generic of the Dex SDK or of the module (Attribute, Channel, Stream and RPC
// payloads among them), when a value of it is passed as an empty interface
// to a function outside the formatting, error, test and synchronization
// packages, which includes every encoder, or when a serialized type contains
// it.
func (a *analysis) exemptSerializedFields() {
	serialized := map[*types.Named]bool{}
	var queue []*types.Named
	mark := func(t types.Type) {
		visitNamed(t, func(n *types.Named) {
			if n.Obj().Pkg() != nil && isInternal(n.Obj().Pkg().Path()) && !serialized[n.Origin()] {
				serialized[n.Origin()] = true
				queue = append(queue, n)
			}
		})
	}
	for _, pkg := range a.sources {
		for ident, instance := range pkg.TypesInfo.Instances {
			object := pkg.TypesInfo.Uses[ident]
			if object == nil || object.Pkg() == nil {
				continue
			}
			path := object.Pkg().Path()
			if strings.HasPrefix(path, "github.com/superdurable/dex/") || strings.HasPrefix(path, modulePath) {
				for t := range instance.TypeArgs.Types() {
					mark(t)
				}
			}
		}
	}
	a.conversions(func(c conversion) {
		if i, ok := c.to.Underlying().(*types.Interface); !ok || i.NumMethods() != 0 {
			return
		}
		if c.callee != nil && c.callee.Pkg() != nil && nonSerializing(c.callee.Pkg().Path()) {
			return
		}
		mark(c.from)
	})
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if s, ok := n.Underlying().(*types.Struct); ok {
			for field := range s.Fields() {
				mark(field.Type())
				// An embedded field takes its name from its type, and an
				// encoder skips it when that name is unexported, unless it
				// is a struct whose fields it promotes.
				_, promoted := types.Unalias(field.Type()).Underlying().(*types.Struct)
				if embedded := named(field.Type()); field.Embedded() && embedded != nil && (!promoted || embedded != types.Unalias(field.Type())) {
					a.exempt(a.declaration(embedded.Origin().Obj()), "serialized: embedded in the serialized type "+n.Obj().Name())
				}
			}
		} else {
			mark(n.Underlying())
		}
	}
	for _, d := range a.decls {
		if d.kind != kindField {
			continue
		}
		if d.tagged {
			a.exempt(d, "serialized: the field has a struct tag")
			continue
		}
		if n := named(d.owner.object.Type()); n != nil && serialized[n] {
			a.exempt(d, "serialized: a field of the serialized type "+n.Obj().Name())
		}
	}
}

// nonSerializing are the packages whose empty-interface parameters format,
// compare, wrap or carry values without reading their field names.
func nonSerializing(path string) bool {
	switch path {
	case "fmt", "errors", "log", "testing", "context", "sync", "sync/atomic", "sort", "slices", "maps", "strings", "strconv", "runtime":
		return true
	}
	return strings.HasPrefix(path, "github.com/stretchr/testify/")
}

// visitNamed calls visit for each named type a type spells, through
// pointers, containers, signatures and anonymous structs, and for the type
// arguments of each.
func visitNamed(t types.Type, visit func(*types.Named)) {
	seen := map[types.Type]bool{}
	var walk func(types.Type)
	walk = func(t types.Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Alias:
			walk(types.Unalias(t))
		case *types.Named:
			visit(t)
			for arg := range t.TypeArgs().Types() {
				walk(arg)
			}
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Key())
			walk(t.Elem())
		case *types.Chan:
			walk(t.Elem())
		case *types.Signature:
			for v := range t.Params().Variables() {
				walk(v.Type())
			}
			for v := range t.Results().Variables() {
				walk(v.Type())
			}
		case *types.Struct:
			for field := range t.Fields() {
				walk(field.Type())
			}
		case *types.Tuple:
			for v := range t.Variables() {
				walk(v.Type())
			}
		}
	}
	walk(t)
}

// exemptImplementations keeps exported the methods that implement an
// interface of another package, and the methods of an interface that another
// package implements. It links each method of an interface with the methods
// of its own package that implement it, which share its name.
func (a *analysis) exemptImplementations() {
	for _, d := range a.decls {
		if d.kind != kindMethod || !d.checked() {
			continue
		}
		signature, ok := d.object.Type().(*types.Signature)
		if !ok {
			continue
		}
		receiver := named(signature.Recv().Type())
		if receiver == nil || receiver.TypeParams().Len() > 0 {
			continue
		}
		for _, i := range a.universe.byMethod[d.object.Name()] {
			if i.pkg != d.pkg && implements(receiver, i.t) {
				a.exempt(d, "implements "+i.name)
				break
			}
		}
	}
	for _, d := range a.decls {
		if d.kind != kindInterface {
			continue
		}
		i, ok := d.object.Type().Underlying().(*types.Interface)
		if !ok || i.NumMethods() == 0 || !i.IsMethodSet() {
			continue
		}
		for _, n := range a.universe.implementations(i) {
			outside := n.Obj().Pkg().Path() != d.pkg
			for m := range i.Methods() {
				method := a.declaration(m)
				if method == nil {
					continue
				}
				if outside {
					a.exempt(method, "implemented by "+types.TypeString(n, nil))
					continue
				}
				object, _, _ := types.LookupFieldOrMethod(types.NewPointer(n), false, m.Pkg(), m.Name())
				if implementation := a.declaration(object); implementation != nil {
					a.groups[method] = append(a.groups[method], implementation)
					a.groups[implementation] = append(a.groups[implementation], method)
				}
			}
		}
	}
}
