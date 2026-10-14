// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command terraform-inputs evaluates the variable values a Terraform root module
// receives from variable files, with the HCL and cty libraries Terraform itself
// uses: optional attribute defaults are applied and every value is converted to
// its declared type, so the result is comparable with the variables of an HCP
// plan's JSON output.
//
//	terraform-inputs --root infra-as-code/terraform/kubernetes \
//	  --var-file infra-as-code/environments/test/kubernetes.tfvars
//
// It reads only the root's variable blocks; validation blocks, locals and
// resources are left to the real plan. A variable file must set only declared
// variables, once each, with literal values. Later files override earlier ones,
// as repeated -var-file arguments do. The output is one JSON document on stdout:
//
//	{"variables": {"<name>": {"required": bool, "sensitive": bool}},
//	 "values":    {"<name>": {"source": "file"|"default", "value": <json>}}}
//
// Errors name files, positions and variable names, never values.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type variable struct {
	typ       cty.Type
	defaults  *typeexpr.Defaults
	def       *cty.Value
	sensitive bool
	nullable  bool
}

type declared struct {
	Required  bool `json:"required"`
	Sensitive bool `json:"sensitive"`
}

type value struct {
	Source string          `json:"source"`
	Value  json.RawMessage `json:"value"`
}

type output struct {
	Variables map[string]declared `json:"variables"`
	Values    map[string]value    `json:"values"`
}

type varFiles []string

func (f *varFiles) String() string     { return strings.Join(*f, ",") }
func (f *varFiles) Set(v string) error { *f = append(*f, v); return nil }

func main() {
	root := flag.String("root", "", "Terraform root module directory")
	var files varFiles
	flag.Var(&files, "var-file", "variable file; repeat to layer files, later ones win")
	flag.Parse()
	if *root == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: terraform-inputs --root <dir> [--var-file <file>]...")
		os.Exit(2)
	}
	result, err := evaluate(*root, files)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "write result:", err)
		os.Exit(1)
	}
}

func evaluate(root string, files []string) (output, error) {
	parser := hclparse.NewParser()
	variables, err := declarations(parser, root)
	if err != nil {
		return output{}, err
	}
	result := output{Variables: map[string]declared{}, Values: map[string]value{}}
	for name, v := range variables {
		result.Variables[name] = declared{Required: v.def == nil, Sensitive: v.sensitive}
		if v.def != nil {
			if result.Values[name], err = encode(name, "default", *v.def); err != nil {
				return output{}, err
			}
		}
	}
	for _, path := range files {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			return output{}, describe(path, diags)
		}
		attributes, diags := file.Body.JustAttributes()
		if diags.HasErrors() {
			return output{}, describe(path, diags)
		}
		for name, attribute := range attributes {
			v, ok := variables[name]
			if !ok {
				return output{}, fmt.Errorf("%s: %q is not a declared variable", position(attribute.NameRange), name)
			}
			raw, diags := attribute.Expr.Value(nil)
			if diags.HasErrors() {
				return output{}, describe(path, diags)
			}
			converted, err := v.convert(raw)
			if err != nil {
				return output{}, fmt.Errorf("%s: variable %q: %w", position(attribute.NameRange), name, err)
			}
			if converted.IsNull() {
				if v.def == nil || !v.nullable {
					return output{}, fmt.Errorf("%s: variable %q must not be null", position(attribute.NameRange), name)
				}
			}
			if result.Values[name], err = encode(name, "file", converted); err != nil {
				return output{}, err
			}
		}
	}
	return result, nil
}

// declarations reads every variable block of the root module's .tf files.
func declarations(parser *hclparse.Parser, root string) (map[string]*variable, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*.tf"))
	if err != nil || len(paths) == 0 {
		return nil, fmt.Errorf("%s: no Terraform files", root)
	}
	sort.Strings(paths)
	fileSchema := &hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{{Type: "variable", LabelNames: []string{"name"}}}}
	variableSchema := &hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "type"}, {Name: "default"}, {Name: "sensitive"}, {Name: "nullable"}}}
	variables := map[string]*variable{}
	for _, path := range paths {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			return nil, describe(path, diags)
		}
		content, _, diags := file.Body.PartialContent(fileSchema)
		if diags.HasErrors() {
			return nil, describe(path, diags)
		}
		for _, block := range content.Blocks {
			name := block.Labels[0]
			if _, ok := variables[name]; ok {
				return nil, fmt.Errorf("%s: variable %q is declared twice", position(block.DefRange), name)
			}
			attributes, _, diags := block.Body.PartialContent(variableSchema)
			if diags.HasErrors() {
				return nil, describe(path, diags)
			}
			v := &variable{typ: cty.DynamicPseudoType, nullable: true}
			if attribute, ok := attributes.Attributes["type"]; ok {
				if v.typ, v.defaults, diags = typeexpr.TypeConstraintWithDefaults(attribute.Expr); diags.HasErrors() {
					return nil, describe(path, diags)
				}
			}
			for key, target := range map[string]*bool{"sensitive": &v.sensitive, "nullable": &v.nullable} {
				if attribute, ok := attributes.Attributes[key]; ok {
					flagValue, diags := attribute.Expr.Value(nil)
					if diags.HasErrors() || flagValue.Type() != cty.Bool || flagValue.IsNull() {
						return nil, fmt.Errorf("%s: variable %q: %s must be a literal bool", position(attribute.NameRange), name, key)
					}
					*target = flagValue.True()
				}
			}
			if attribute, ok := attributes.Attributes["default"]; ok {
				raw, diags := attribute.Expr.Value(nil)
				if diags.HasErrors() {
					return nil, describe(path, diags)
				}
				converted, err := v.convert(raw)
				if err != nil {
					return nil, fmt.Errorf("%s: variable %q default: %w", position(attribute.NameRange), name, err)
				}
				v.def = &converted
			}
			variables[name] = v
		}
	}
	return variables, nil
}

// convert applies optional attribute defaults before conversion, as Terraform does for non-null values.
func (v *variable) convert(raw cty.Value) (cty.Value, error) {
	if v.defaults != nil && !raw.IsNull() {
		raw = v.defaults.Apply(raw)
	}
	return convert.Convert(raw, v.typ)
}

func encode(name, source string, v cty.Value) (value, error) {
	if !v.IsWhollyKnown() {
		return value{}, fmt.Errorf("variable %q: value is not fully known", name)
	}
	raw, err := ctyjson.SimpleJSONValue{Value: v}.MarshalJSON()
	if err != nil {
		return value{}, fmt.Errorf("variable %q: encode value: %w", name, err)
	}
	return value{Source: source, Value: raw}, nil
}

// describe reports diagnostics by position and summary; details may quote source values.
func describe(path string, diags hcl.Diagnostics) error {
	var messages []string
	for _, diag := range diags {
		if diag.Severity != hcl.DiagError {
			continue
		}
		where := path
		if diag.Subject != nil {
			where = position(*diag.Subject)
		}
		messages = append(messages, where+": "+diag.Summary)
	}
	return errors.New(strings.Join(messages, "\n"))
}

func position(r hcl.Range) string {
	return fmt.Sprintf("%s:%d", r.Filename, r.Start.Line)
}
