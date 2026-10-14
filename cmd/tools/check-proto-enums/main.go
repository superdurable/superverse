// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command check-proto-enums proves that every domain value the business API
// maps to a protobuf enum has its enum value. boundary.Enum maps a value of a
// closed domain set to the value named by the enum's prefix and the
// upper-cased domain value, and panics on a value the enum does not declare;
// this check finds that gap before a server runs.
//
// For every call boundary.Enum[E](value) under internal/ it reads the closed
// set of value's type: the constants of a string type or of an alias of
// string, or the package-level variables of a struct type whose String method
// a call names, each built from a constant string where the set is declared.
// It reports each domain value whose enum value is missing as
// `file:line: enum: missing VALUE (set)`. It runs from the repository root.
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/superdurable/superversev2/gen/superverse/v2" // registers the business protocol's descriptors
	"golang.org/x/tools/go/packages"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	boundaryPackage = "github.com/superdurable/superversev2/internal/api/boundary"
	protoPackage    = "superverse.v2"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:  root,
	}, "./internal/...")
	if err != nil {
		fail(err)
	}
	byPath := map[string]*packages.Package{}
	broken := false
	for _, p := range loaded {
		byPath[p.PkgPath] = p
		for _, e := range p.Errors {
			fmt.Fprintln(os.Stderr, e)
			broken = true
		}
	}
	if broken {
		fail(errors.New("internal/ does not load"))
	}
	var problems []problem
	calls := 0
	for _, p := range loaded {
		for _, file := range p.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				enum, ok := enumOf(p.TypesInfo, call)
				if !ok {
					return true
				}
				calls++
				problems = append(problems, check(p, byPath, call, enum)...)
				return true
			})
		}
	}
	sort.Slice(problems, func(i, j int) bool {
		a, b := problems[i].position, problems[j].position
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	for _, v := range problems {
		file, err := filepath.Rel(root, v.position.Filename)
		if err != nil {
			file = v.position.Filename
		}
		fmt.Printf("%s:%d: %s\n", file, v.position.Line, v.message)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "check-proto-enums: %d domain values have no protobuf enum value\n", len(problems))
		os.Exit(1)
	}
	fmt.Printf("Proto enums: the %d mappings of boundary.Enum cover every value of their domain sets\n", calls)
}

type problem struct {
	position token.Position
	message  string
}

// enumOf reports whether call is boundary.Enum[E](value) and returns E.
func enumOf(info *types.Info, call *ast.CallExpr) (*types.Named, bool) {
	var function ast.Expr
	var typeArgument ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		function, typeArgument = fun.X, fun.Index
	case *ast.IndexListExpr:
		function, typeArgument = fun.X, fun.Indices[0]
	default:
		return nil, false
	}
	selector, ok := function.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	called, ok := info.Uses[selector.Sel].(*types.Func)
	if !ok || called.Pkg() == nil || called.Pkg().Path() != boundaryPackage || called.Name() != "Enum" {
		return nil, false
	}
	enum, ok := info.TypeOf(typeArgument).(*types.Named)
	return enum, ok
}

// check returns a problem for every value of the call's domain set that the
// enum does not declare, or one problem when the set cannot be read.
func check(p *packages.Package, byPath map[string]*packages.Package, call *ast.CallExpr, enum *types.Named) []problem {
	at := p.Fset.Position(call.Pos())
	descriptor, err := enumDescriptor(enum)
	if err != nil {
		return []problem{{at, err.Error()}}
	}
	set, values, err := domainValues(p.TypesInfo, byPath, call.Args[0])
	if err != nil {
		return []problem{{at, fmt.Sprintf("%s: %v", descriptor.FullName(), err)}}
	}
	enumValues := descriptor.Values()
	prefix := strings.TrimSuffix(string(enumValues.ByNumber(0).Name()), "UNSPECIFIED")
	var missing []problem
	for _, value := range values {
		name := prefix + strings.ToUpper(value)
		if value != "" && enumValues.ByName(protoreflect.Name(name)) == nil {
			missing = append(missing, problem{at, fmt.Sprintf("%s: missing %s (%s)", descriptor.FullName(), name, set)})
		}
	}
	return missing
}

// enumDescriptor is the registered descriptor of a generated enum type: the
// Go name joins a nested enum's messages with underscores.
func enumDescriptor(enum *types.Named) (protoreflect.EnumDescriptor, error) {
	name := protoreflect.FullName(protoPackage + "." + strings.ReplaceAll(enum.Obj().Name(), "_", "."))
	found, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err != nil {
		return nil, fmt.Errorf("%s is not a registered enum: %w", name, err)
	}
	descriptor, ok := found.(protoreflect.EnumDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not an enum", name)
	}
	return descriptor, nil
}

// domainValues reads the closed set of a mapped value: the constants of its
// type, or, for a call of a String method, the package-level variables of the
// receiver's type.
func domainValues(info *types.Info, byPath map[string]*packages.Package, value ast.Expr) (string, []string, error) {
	if call, ok := value.(*ast.CallExpr); ok && len(call.Args) == 0 {
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "String" {
			if set := closedSet(info.TypeOf(selector.X)); set != nil {
				return setValues(byPath, set)
			}
		}
	}
	set := closedSet(info.TypeOf(value))
	if set == nil {
		return "", nil, fmt.Errorf("the mapped value is a %s, not a value of a closed set type", info.TypeOf(value))
	}
	return setValues(byPath, set)
}

// closedSet is the type name that declares a value's closed set: a defined
// type, or an alias, which go/types keeps apart from its target.
func closedSet(t types.Type) *types.TypeName {
	switch typed := t.(type) {
	case *types.Pointer:
		return closedSet(typed.Elem())
	case *types.Named:
		return typed.Obj()
	case *types.Alias:
		return typed.Obj()
	}
	return nil
}

// inSet reports whether t is the type that set names, compared by its
// declaration: an alias of string is identical to every other string.
func inSet(t types.Type, set *types.TypeName) bool {
	return closedSet(t) == set
}

// setValues lists the values declared for a closed set type: its constants
// when it is a string type or an alias of string, its package-level variables
// otherwise.
func setValues(byPath map[string]*packages.Package, set *types.TypeName) (string, []string, error) {
	name := set.Pkg().Path() + "." + set.Name()
	declaring := byPath[set.Pkg().Path()]
	if declaring == nil {
		return name, nil, fmt.Errorf("%s is declared outside internal/", name)
	}
	var values []string
	if basic, ok := set.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
		scope := declaring.Types.Scope()
		for _, identifier := range scope.Names() {
			if constant_, ok := scope.Lookup(identifier).(*types.Const); ok && inSet(constant_.Type(), set) {
				values = append(values, constant.StringVal(constant_.Val()))
			}
		}
	} else {
		var err error
		if values, err = variableValues(declaring, set); err != nil {
			return name, nil, err
		}
	}
	if len(values) == 0 {
		return name, nil, fmt.Errorf("%s declares no value", name)
	}
	return name, values, nil
}

// variableValues reads the package-level variables of a struct set type, each
// built from a constant string: a composite literal whose name field or first
// element, or a constructor call whose first argument, is the value.
func variableValues(declaring *packages.Package, set *types.TypeName) ([]string, error) {
	var values []string
	for _, file := range declaring.Syntax {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, identifier := range valueSpec.Names {
					object := declaring.TypesInfo.Defs[identifier]
					if object == nil || object.Parent() != declaring.Types.Scope() || !inSet(object.Type(), set) {
						continue
					}
					if index >= len(valueSpec.Values) {
						return nil, fmt.Errorf("%s has no initializer to read its value from", identifier.Name)
					}
					value, ok := firstString(declaring.TypesInfo, valueSpec.Values[index])
					if !ok {
						return nil, fmt.Errorf("the value of %s is not a constant string where the set is declared", identifier.Name)
					}
					values = append(values, value)
				}
			}
		}
	}
	return values, nil
}

// firstString is the constant string that builds a set value: the field name
// or the first element of a composite literal, or the first argument of a
// constructor call, through an address-of and a nested definition.
func firstString(info *types.Info, expression ast.Expr) (string, bool) {
	if typed, ok := info.Types[expression]; ok && typed.Value != nil {
		if typed.Value.Kind() != constant.String {
			return "", false
		}
		return constant.StringVal(typed.Value), true
	}
	switch built := expression.(type) {
	case *ast.UnaryExpr:
		if built.Op == token.AND {
			return firstString(info, built.X)
		}
	case *ast.CompositeLit:
		for _, element := range built.Elts {
			if keyed, ok := element.(*ast.KeyValueExpr); ok {
				if key, ok := keyed.Key.(*ast.Ident); ok && key.Name == "name" {
					return firstString(info, keyed.Value)
				}
			}
		}
		if len(built.Elts) > 0 {
			if keyed, ok := built.Elts[0].(*ast.KeyValueExpr); ok {
				return firstString(info, keyed.Value)
			}
			return firstString(info, built.Elts[0])
		}
	case *ast.CallExpr:
		if len(built.Args) > 0 {
			return firstString(info, built.Args[0])
		}
	}
	return "", false
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "check-proto-enums:", err)
	os.Exit(2)
}
