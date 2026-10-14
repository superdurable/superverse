// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

type kind int

const (
	kindType kind = iota
	kindInterface
	kindFunc
	kindVar
	kindConst
	kindMethod
	kindInterfaceMethod
	kindField
)

// declaration is one exported identifier of a production file under
// internal/, or an interface declared there, and what the analysis finds
// about it.
type declaration struct {
	object   types.Object
	kind     kind
	pkg      string
	name     string // qualified for the report: package.Type.Member
	position token.Position
	// owner is the type that declares a method or field; an unexported owner
	// is a declaration that is not itself checked.
	owner *declaration
	// tagged reports a field with a struct tag.
	tagged bool

	// prodUse is the first use by another package's production code, testUse
	// the first by another package's test.
	prodUse, testUse *token.Position
	// reachedFrom is the needed declaration whose signature, field or method
	// set names this one.
	reachedFrom *declaration
	// exemption says why the identifier stays exported regardless of its uses.
	exemption string
}

func (d *declaration) needed() bool {
	return d.prodUse != nil || d.reachedFrom != nil || d.exemption != ""
}

// checked reports whether the export rules apply to the declaration: it is
// exported and is not a placeholder owner.
func (d *declaration) checked() bool { return d.object != nil && d.object.Exported() }

// declarations collects the exported identifiers of internal/ production
// files and every interface type declared there, keyed by position. Owners
// of members that are not exported are unchecked placeholders.
func (p *program) declarations() map[key]*declaration {
	decls := map[key]*declaration{}
	owners := map[types.Object]*declaration{}
	productionFiles := func(visit func(file, string)) {
		p.files(func(f file) {
			if isInternal(f.pkg.PkgPath) && f.pkg.ID == f.pkg.PkgPath && !f.test && !f.generated {
				visit(f, f.pkg.PkgPath[len(internalPrefix):])
			}
		})
	}
	add := func(f file, ident *ast.Ident, k kind, name string, owner *declaration) *declaration {
		object := f.pkg.TypesInfo.Defs[ident]
		if object == nil {
			return nil
		}
		d := &declaration{object: object, kind: k, pkg: f.pkg.PkgPath, name: name, position: p.fset.Position(ident.Pos()), owner: owner}
		decls[p.key(ident.Pos())] = d
		return d
	}
	ownerOf := func(f file, object types.Object, short string) *declaration {
		if d := owners[object]; d != nil {
			return d
		}
		d := &declaration{object: object, pkg: f.pkg.PkgPath, name: short + "." + object.Name()}
		owners[object] = d
		return d
	}
	productionFiles(func(f file, short string) {
		for _, decl := range f.syntax.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					k := kindType
					if _, ok := spec.Type.(*ast.InterfaceType); ok {
						k = kindInterface
					}
					var owner *declaration
					if spec.Name.IsExported() || k == kindInterface {
						owner = add(f, spec.Name, k, short+"."+spec.Name.Name, nil)
						owners[owner.object] = owner
					} else {
						owner = ownerOf(f, f.pkg.TypesInfo.Defs[spec.Name], short)
					}
					p.members(f, spec.Type, owner, add)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if name.IsExported() {
							k := kindVar
							if gen.Tok == token.CONST {
								k = kindConst
							}
							add(f, name, k, short+"."+name.Name, nil)
						}
					}
				}
			}
		}
	})
	productionFiles(func(f file, short string) {
		for _, decl := range f.syntax.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			if fn.Recv == nil {
				add(f, fn.Name, kindFunc, short+"."+fn.Name.Name, nil)
				continue
			}
			signature, ok := f.pkg.TypesInfo.Defs[fn.Name].Type().(*types.Signature)
			if !ok {
				continue
			}
			receiver := named(signature.Recv().Type())
			if receiver == nil {
				continue
			}
			owner := ownerOf(f, receiver.Obj(), short)
			add(f, fn.Name, kindMethod, owner.name+"."+fn.Name.Name, owner)
		}
	})
	return decls
}

// members adds the exported fields of a struct type expression and the
// exported methods of an interface type expression, with those of the struct
// and interface types nested in them.
func (p *program) members(f file, expr ast.Expr, owner *declaration, add func(file, *ast.Ident, kind, string, *declaration) *declaration) {
	ast.Inspect(expr, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructType:
			for _, field := range n.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() {
						if d := add(f, name, kindField, owner.name+"."+name.Name, owner); d != nil {
							d.tagged = field.Tag != nil && strings.Trim(field.Tag.Value, "`\" ") != ""
						}
					}
				}
			}
		case *ast.InterfaceType:
			for _, method := range n.Methods.List {
				for _, name := range method.Names {
					if name.IsExported() {
						add(f, name, kindInterfaceMethod, owner.name+"."+name.Name, owner)
					}
				}
			}
		}
		return true
	})
}
