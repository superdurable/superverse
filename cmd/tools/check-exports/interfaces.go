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
	"go/types"
	"strings"
)

// iface is an interface the program declares or uses: a named interface of a
// reached package, an instantiated generic interface, an interface literal of
// a production file, or a protocol a dependency checks dynamically.
type iface struct {
	t    *types.Interface
	pkg  string // the package that declares or spells it
	name string
}

// universe is what the analysis searches for implementations and implemented
// interfaces.
type universe struct {
	// byMethod indexes the interfaces by the names of their methods.
	byMethod map[string][]*iface
	// concrete are the non-interface named types, generic ones instantiated as
	// the program instantiates them.
	concrete []*types.Named
	// concreteByMethod indexes the concrete types by the names of the methods
	// of their pointers.
	concreteByMethod map[string][]*types.Named
}

// plainPackages are the type-checked packages of the module's package
// variants without tests, keyed by themselves.
func (p *program) plainPackages() map[*types.Package]bool {
	plain := map[*types.Package]bool{}
	for _, pkg := range p.sources {
		if pkg.ID == pkg.PkgPath {
			plain[pkg.Types] = true
		}
	}
	return plain
}

func (p *program) universe() *universe {
	u := &universe{byMethod: map[string][]*iface{}}
	plain := p.plainPackages()
	seenInterface := map[*types.Interface]bool{}
	addInterface := func(i *iface) {
		if i.t.NumMethods() == 0 || !i.t.IsMethodSet() || seenInterface[i.t] {
			return
		}
		seenInterface[i.t] = true
		for m := range i.t.Methods() {
			u.byMethod[m.Name()] = append(u.byMethod[m.Name()], i)
		}
	}
	seenNamed := map[*types.Named]bool{}
	addNamed := func(n *types.Named, pkg string) {
		if seenNamed[n] {
			return
		}
		seenNamed[n] = true
		if t, ok := n.Underlying().(*types.Interface); ok {
			addInterface(&iface{t: t, pkg: pkg, name: types.TypeString(n, nil)})
			return
		}
		if n.TypeParams().Len() == 0 || n.TypeArgs().Len() > 0 {
			u.concrete = append(u.concrete, n)
		}
	}
	if errorType, ok := types.Universe.Lookup("error").Type().(*types.Named); ok {
		addNamed(errorType, "builtin")
	}
	for _, t := range p.reached {
		if strings.HasPrefix(t.Path(), modulePath) && !plain[t] {
			continue
		}
		scope := t.Scope()
		for _, name := range scope.Names() {
			if typeName, ok := scope.Lookup(name).(*types.TypeName); ok && !typeName.IsAlias() {
				if n, ok := typeName.Type().(*types.Named); ok && n.TypeParams().Len() == 0 {
					addNamed(n, t.Path())
				}
			}
		}
	}
	for _, pkg := range p.sources {
		for ident, instance := range pkg.TypesInfo.Instances {
			if n, ok := instance.Type.(*types.Named); ok && n.Obj().Pkg() != nil && !strings.HasSuffix(p.fset.Position(ident.Pos()).Filename, "_test.go") {
				addNamed(n, n.Obj().Pkg().Path())
			}
		}
	}
	p.files(func(f file) {
		if f.test {
			return
		}
		ast.Inspect(f.syntax, func(n ast.Node) bool {
			if literal, ok := n.(*ast.InterfaceType); ok {
				if t, ok := f.pkg.TypesInfo.TypeOf(literal).(*types.Interface); ok {
					addInterface(&iface{t: t, pkg: f.pkg.PkgPath, name: "interface literal in " + f.pkg.PkgPath})
				}
			}
			return true
		})
	})
	for _, protocol := range p.dynamicProtocols() {
		addInterface(protocol)
	}
	return u
}

// dynamicProtocols are the methods that the standard library, gRPC and the
// Anthropic SDK look for with interface literals inside their own code, which
// the loaded export data does not show.
func (p *program) dynamicProtocols() []*iface {
	lookup := func(path, name string) types.Type {
		for _, t := range p.reached {
			if t.Path() == path {
				if object := t.Scope().Lookup(name); object != nil {
					return object.Type()
				}
			}
		}
		return nil
	}
	errorType := types.Universe.Lookup("error").Type()
	anyType := types.Universe.Lookup("any").Type()
	boolType, stringType, intType := types.Typ[types.Bool], types.Typ[types.String], types.Typ[types.Int]
	var out []*iface
	method := func(name string, params, results []types.Type) {
		vars := func(list []types.Type) *types.Tuple {
			out := make([]*types.Var, len(list))
			for i, t := range list {
				if t == nil {
					return nil
				}
				out[i] = types.NewParam(0, nil, "", t)
			}
			return types.NewTuple(out...)
		}
		in, result := vars(params), vars(results)
		if (in == nil && len(params) > 0) || (result == nil && len(results) > 0) {
			return
		}
		signature := types.NewSignatureType(nil, nil, nil, in, result, false)
		t := types.NewInterfaceType([]*types.Func{types.NewFunc(0, nil, name, signature)}, nil).Complete()
		out = append(out, &iface{t: t, pkg: "builtin", name: "the dynamic " + name + " protocol"})
	}
	method("Is", []types.Type{errorType}, []types.Type{boolType})
	method("As", []types.Type{anyType}, []types.Type{boolType})
	method("Unwrap", nil, []types.Type{errorType})
	method("Unwrap", nil, []types.Type{types.NewSlice(errorType)})
	method("Timeout", nil, []types.Type{boolType})
	method("Temporary", nil, []types.Type{boolType})
	if status := lookup("google.golang.org/grpc/internal/status", "Status"); status != nil {
		method("GRPCStatus", nil, []types.Type{types.NewPointer(status)})
	}
	method("Name", nil, []types.Type{stringType})
	method("Filename", nil, []types.Type{stringType})
	method("ContentType", nil, []types.Type{stringType})
	method("RawJSON", nil, []types.Type{stringType})
	method("IsKnown", nil, []types.Type{boolType})
	method("GetStatusCode", nil, []types.Type{intType})
	method("Equal", []types.Type{anyType}, []types.Type{boolType})
	return out
}

func implements(n *types.Named, i *types.Interface) bool {
	return types.Implements(n, i) || types.Implements(types.NewPointer(n), i)
}

// implementations returns the concrete types that implement an interface.
func (u *universe) implementations(i *types.Interface) []*types.Named {
	if u.concreteByMethod == nil {
		u.concreteByMethod = map[string][]*types.Named{}
		for _, n := range u.concrete {
			methods := types.NewMethodSet(types.NewPointer(n))
			for selection := range methods.Methods() {
				name := selection.Obj().Name()
				u.concreteByMethod[name] = append(u.concreteByMethod[name], n)
			}
		}
	}
	var out []*types.Named
	for _, n := range u.concreteByMethod[i.Method(0).Name()] {
		if implements(n, i) {
			out = append(out, n)
		}
	}
	return out
}
