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
)

// conversion is a value of one type that the code uses as another: an
// argument, an assigned or returned value, a composite literal element, a sent
// value or an explicit conversion.
type conversion struct {
	from, to types.Type
	pos      token.Pos
	f        file
	// callee is the function that receives an argument, or nil.
	callee *types.Func
}

// conversions calls visit for each conversion to an interface type in every
// file of the module.
func (p *program) conversions(visit func(conversion)) {
	p.files(func(f file) {
		info := f.pkg.TypesInfo
		report := func(expr ast.Expr, to types.Type, callee *types.Func) {
			if to == nil || !types.IsInterface(to) {
				return
			}
			from := info.TypeOf(expr)
			if from == nil || types.Identical(from, to) {
				return
			}
			if basic, ok := from.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
				return
			}
			visit(conversion{from: from, to: to, pos: expr.Pos(), f: f, callee: callee})
		}
		var signatures []*types.Signature
		var stack []ast.Node
		ast.Inspect(f.syntax, func(n ast.Node) bool {
			if n == nil {
				if top := stack[len(stack)-1]; isFunction(top) {
					signatures = signatures[:len(signatures)-1]
				}
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			switch n := n.(type) {
			case *ast.FuncDecl:
				signature, _ := info.Defs[n.Name].Type().(*types.Signature)
				signatures = append(signatures, signature)
			case *ast.FuncLit:
				signature, _ := info.TypeOf(n).(*types.Signature)
				signatures = append(signatures, signature)
			case *ast.CallExpr:
				p.callConversions(info, n, report)
			case *ast.AssignStmt:
				if n.Tok == token.ASSIGN && len(n.Lhs) == len(n.Rhs) {
					for i := range n.Lhs {
						report(n.Rhs[i], info.TypeOf(n.Lhs[i]), nil)
					}
				}
			case *ast.ValueSpec:
				if n.Type != nil && len(n.Values) == len(n.Names) {
					for _, value := range n.Values {
						report(value, info.TypeOf(n.Type), nil)
					}
				}
			case *ast.ReturnStmt:
				if len(signatures) > 0 && signatures[len(signatures)-1] != nil {
					results := signatures[len(signatures)-1].Results()
					if results.Len() == len(n.Results) {
						for i, result := range n.Results {
							report(result, results.At(i).Type(), nil)
						}
					}
				}
			case *ast.SendStmt:
				if channel, ok := info.TypeOf(n.Chan).Underlying().(*types.Chan); ok {
					report(n.Value, channel.Elem(), nil)
				}
			case *ast.CompositeLit:
				compositeConversions(info, n, report)
			}
			return true
		})
	})
}

func isFunction(n ast.Node) bool {
	switch n.(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		return true
	}
	return false
}

func (p *program) callConversions(info *types.Info, call *ast.CallExpr, report func(ast.Expr, types.Type, *types.Func)) {
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		if len(call.Args) == 1 {
			report(call.Args[0], tv.Type, nil)
		}
		return
	}
	signature, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return
	}
	callee, _ := calleeObject(info, call.Fun).(*types.Func)
	params := signature.Params()
	for i, arg := range call.Args {
		var to types.Type
		switch {
		case signature.Variadic() && i >= params.Len()-1:
			slice, ok := params.At(params.Len() - 1).Type().Underlying().(*types.Slice)
			if call.Ellipsis.IsValid() || !ok {
				continue
			}
			to = slice.Elem()
		case i < params.Len():
			to = params.At(i).Type()
		default:
			continue
		}
		report(arg, to, callee)
	}
}

// calleeObject is the object a call's function expression names, through an
// explicit instantiation.
func calleeObject(info *types.Info, fun ast.Expr) types.Object {
	switch e := ast.Unparen(fun).(type) {
	case *ast.IndexExpr:
		return calleeObject(info, e.X)
	case *ast.IndexListExpr:
		return calleeObject(info, e.X)
	case *ast.Ident:
		return info.Uses[e]
	case *ast.SelectorExpr:
		return info.Uses[e.Sel]
	}
	return nil
}

func compositeConversions(info *types.Info, lit *ast.CompositeLit, report func(ast.Expr, types.Type, *types.Func)) {
	t := info.TypeOf(lit)
	if t == nil {
		return
	}
	if pointer, ok := t.Underlying().(*types.Pointer); ok {
		t = pointer.Elem()
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		for i, element := range lit.Elts {
			if kv, ok := element.(*ast.KeyValueExpr); ok {
				if field, ok := info.Uses[kv.Key.(*ast.Ident)].(*types.Var); ok {
					report(kv.Value, field.Type(), nil)
				}
			} else if i < u.NumFields() {
				report(element, u.Field(i).Type(), nil)
			}
		}
	case *types.Slice:
		elementConversions(lit, nil, u.Elem(), report)
	case *types.Array:
		elementConversions(lit, nil, u.Elem(), report)
	case *types.Map:
		elementConversions(lit, u.Key(), u.Elem(), report)
	}
}

func elementConversions(lit *ast.CompositeLit, key, value types.Type, report func(ast.Expr, types.Type, *types.Func)) {
	for _, element := range lit.Elts {
		if kv, ok := element.(*ast.KeyValueExpr); ok {
			if key != nil {
				report(kv.Key, key, nil)
			}
			element = kv.Value
		}
		report(element, value, nil)
	}
}
