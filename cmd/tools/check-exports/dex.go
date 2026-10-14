// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"errors"
	"go/ast"
	"go/types"
)

// dexPrimitives are the generic types of the Dex SDK whose package-level
// variables are a Flow's Attributes, Channels and Streams.
var dexPrimitives = map[string]bool{"Attribute": true, "AttributeMap": true, "Channel": true, "ChannelMap": true, "Stream": true}

// exemptDex keeps exported what the Dex identities and the Flow graphs spell.
// Dex persists the Go names of the Flow types the module registers (the
// types that implement dex.Flow), of the Step types passed to
// dex.DefineStep and dex.DefineStartStep and of the methods passed to
// dex.DefineRPC; the methods of Flow and Step types go with them. The graph
// dexcli derives from a Flow DSL file (the file declaring a Flow type) also
// spells the Go names of the Flow's primitives (package-level variables of a
// dex.Attribute, AttributeMap, Channel, ChannelMap or Stream type), of their
// value types, and of what its decision conditions and waits name.
func (a *analysis) exemptDex() {
	var flow *types.Interface
	for _, t := range a.reached {
		if t.Path() == dexPackage {
			flow, _ = t.Scope().Lookup("Flow").Type().Underlying().(*types.Interface)
		}
	}
	if flow == nil {
		fail(errors.New("the Dex SDK declares no Flow interface"))
	}
	exemptType := func(d *declaration, why string) {
		a.exempt(d, why)
		for _, member := range a.members[d] {
			if member.kind == kindMethod {
				a.exempt(member, "a method of the "+why)
			}
		}
	}
	flowFiles := map[string]bool{}
	for _, d := range a.decls {
		if n, ok := d.object.Type().(*types.Named); ok && d.kind == kindType && implements(n, flow) {
			exemptType(d, "Dex Flow type "+d.object.Name())
			flowFiles[d.position.Filename] = true
		}
	}
	a.files(func(f file) {
		if !isInternal(f.pkg.PkgPath) || f.pkg.ID != f.pkg.PkgPath || f.test {
			return
		}
		info := f.pkg.TypesInfo
		ast.Inspect(f.syntax, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			callee, ok := calleeObject(info, call.Fun).(*types.Func)
			if !ok || callee.Pkg() == nil || callee.Pkg().Path() != dexPackage {
				return true
			}
			switch callee.Name() {
			case "DefineStep", "DefineStartStep":
				if n := named(info.TypeOf(call.Args[0])); n != nil {
					if d := a.declaration(n.Obj()); d != nil {
						exemptType(d, "Dex Step type "+n.Obj().Name())
					}
				}
			case "DefineRPC":
				if selector, ok := ast.Unparen(call.Args[0]).(*ast.SelectorExpr); ok {
					a.exempt(a.declaration(info.Uses[selector.Sel]), "Dex RPC "+selector.Sel.Name)
				}
			}
			return true
		})
		if flowFiles[f.name] {
			a.exemptGraphNames(f)
		}
	})
	for t := range a.plainPackages() {
		if !isInternal(t.Path()) {
			continue
		}
		for _, name := range t.Scope().Names() {
			v, ok := t.Scope().Lookup(name).(*types.Var)
			if !ok {
				continue
			}
			n, ok := v.Type().(*types.Named)
			if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != dexPackage || !dexPrimitives[n.Obj().Name()] {
				continue
			}
			a.exempt(a.declaration(v), "the Dex "+n.Obj().Name()+" whose name the Flow graph spells")
			for arg := range n.TypeArgs().Types() {
				visitNamed(arg, func(value *types.Named) {
					a.exempt(a.declaration(value.Origin().Obj()), "the value type of the Dex "+n.Obj().Name()+" "+v.Name()+", which the Flow graph spells")
				})
			}
		}
	}
}

// exemptGraphNames exempts the identifiers that a Flow DSL file names in its
// decision conditions (if conditions, switch tags and case expressions) and
// in its WaitFor methods, whose source text the Flow graph records.
func (a *analysis) exemptGraphNames(f file) {
	info := f.pkg.TypesInfo
	names := func(node ast.Node) {
		if node == nil {
			return
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				if d := a.declaration(info.Uses[ident]); d != nil && d.pkg == f.pkg.PkgPath {
					a.exempt(d, "named by a decision or wait of the Flow graph of "+a.short(a.fset.Position(f.syntax.Pos())))
				}
			}
			return true
		})
	}
	for _, decl := range f.syntax.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Recv != nil && fn.Name.Name == "WaitFor" {
			names(fn.Body)
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.IfStmt:
				names(n.Cond)
			case *ast.SwitchStmt:
				names(n.Tag)
			case *ast.CaseClause:
				for _, expr := range n.List {
					names(expr)
				}
			}
			return true
		})
	}
}
