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
	"go/constant"
	"go/types"
	"net/url"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"golang.org/x/tools/go/packages"
)

// checkModelCatalog checks explicit mappings because durable provider-qualified
// spellings intentionally differ from the historical protobuf names.
func checkModelCatalog(byPath map[string]*packages.Package) []problem {
	const conversationPath = "github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	declaring := byPath[conversationPath]
	set := declaring.Types.Scope().Lookup("Model").(*types.TypeName)
	_, values, err := setValues(byPath, set)
	if err != nil {
		fail(err)
	}
	config := modelprovider.ProviderConfig{}
	for _, model := range modelprovider.CompatibleModels() {
		config.Enabled = append(config.Enabled, model.Details().Provider)
	}
	var problems []problem
	at := declaring.Fset.Position(set.Pos())
	listed := map[string]bool{}
	public := map[string]bool{}
	for _, model := range conversation.Models() {
		public[string(model)] = true
	}
	for _, model := range conversation.ExecutionModels() {
		if !public[string(model)] && (!model.IsMock() || model == conversation.ModelMock) {
			problems = append(problems, problem{at, fmt.Sprintf("non-fixture model %q is missing from the public catalog", model)})
		}
		if listed[string(model)] {
			problems = append(problems, problem{at, fmt.Sprintf("duplicate catalog identity %q", model)})
		}
		listed[string(model)] = true
		profile := modelprovider.ProfileOf(model)
		capability := profile.Capabilities
		source, sourceErr := url.Parse(profile.SourceURL)
		_, dateErr := time.Parse(time.DateOnly, profile.CheckedOn)
		if sourceErr != nil || source.Scheme != "https" || source.Host == "" || dateErr != nil || profile.ContextWindow < profile.HardLimit {
			problems = append(problems, problem{at, fmt.Sprintf("invalid dated profile source or context budget for %q", model)})
		}
		if config.Availability(model).Selectable() && (!capability.SupportsImages || !capability.SupportsParallelTools) {
			problems = append(problems, problem{at, fmt.Sprintf("selectable model capability regression for %q", model)})
		}
		if capability.Effort.String() == "" || capability.Thinking.String() == "" || capability.StrictSchema.String() == "" || capability.PromptCache.String() == "" || capability.Fallback.String() == "" || capability.MidSystem.String() == "" || capability.Stream.String() == "" || capability.Replay.String() == "" || profile.Availability.State().String() == "" {
			problems = append(problems, problem{at, fmt.Sprintf("incomplete capability policy for %q", model)})
		}
	}
	for _, value := range values {
		if !listed[value] {
			problems = append(problems, problem{at, fmt.Sprintf("Model constant %q is absent from Models and profiles", value)})
		}
	}
	if len(listed) != len(values) {
		problems = append(problems, problem{at, "catalog identities and Model constants differ"})
	}
	api := byPath["github.com/superdurable/superversev2/internal/dexai/api"]
	mappings := map[string]bool{}
	enums := map[int64]bool{}
	for _, file := range api.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			mapping, ok := api.TypesInfo.TypeOf(literal).Underlying().(*types.Map)
			if !ok || !inSet(mapping.Elem(), set) {
				return true
			}
			key, ok := mapping.Key().(*types.Named)
			if !ok || key.Obj().Name() != "AgentModel" {
				return true
			}
			at = api.Fset.Position(literal.Pos())
			for _, element := range literal.Elts {
				pair := element.(*ast.KeyValueExpr)
				value := constant.StringVal(api.TypesInfo.Types[pair.Value].Value)
				number, exact := constant.Int64Val(api.TypesInfo.Types[pair.Key].Value)
				_, declaredEnum := pb.AgentModel_name[int32(number)]
				if !exact || number == 0 || !declaredEnum || !public[value] || enums[number] || mappings[value] {
					problems = append(problems, problem{at, "AgentModel mapping is not one-to-one"})
				}
				mappings[value], enums[number] = true, true
			}
			return true
		})
	}
	for _, value := range values {
		if public[value] && !mappings[value] {
			problems = append(problems, problem{at, fmt.Sprintf("AgentModel has no mapping for Model %q", value)})
		}
	}
	for number, name := range pb.AgentModel_name {
		if number != 0 && !enums[int64(number)] {
			problems = append(problems, problem{at, fmt.Sprintf("AgentModel %s has no domain mapping", name)})
		}
	}
	return problems
}
