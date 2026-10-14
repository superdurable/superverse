// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package toolinput

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput/generated"
	"github.com/superdurable/superversev2/internal/comm"
)

// Schema binds one provider-visible schema to its generated Go input type.
type Schema[T any] struct {
	inputSchema string
	validate    func(*T) error
}

// JSONSchema returns the compact provider-visible JSON Schema object.
func (schema Schema[T]) JSONSchema() string {
	return schema.inputSchema
}

// Decode strictly decodes and validates one tool input object.
func (schema Schema[T]) Decode(encoded string) (T, error) {
	input, err := decodeStrictArguments[T]([]byte(encoded))
	if err == nil {
		err = schema.validate(&input)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return input, nil
}

//go:embed schema/*.json
var toolInputSchemas embed.FS

var (
	Bash             = mustSchema(conversation.ToolBash, (*generated.Bash).Validate)
	Read             = mustSchema(conversation.ToolRead, (*generated.Read).Validate)
	Edit             = mustSchema(conversation.ToolEdit, (*generated.Edit).Validate)
	Write            = mustSchema(conversation.ToolWrite, (*generated.Write).Validate)
	Grep             = mustSchema(conversation.ToolGrep, (*generated.Grep).Validate)
	Glob             = mustSchema(conversation.ToolGlob, (*generated.Glob).Validate)
	ReadTaskOutput   = mustSchema(conversation.ToolReadTaskOutput, (*generated.ReadTaskOutput).Validate)
	StopTask         = mustSchema(conversation.ToolStopTask, (*generated.StopTask).Validate)
	ConnectorCatalog = mustSchema(conversation.ToolConnectorCatalog, (*generated.ConnectorCatalog).Validate)
	CommitAndPush    = mustSchema(conversation.ToolCommitAndPush, (*generated.CommitAndPush).Validate)
	// The platform tools' inputs.
	ReadDeliveryStatus  = mustSchema(conversation.ToolReadDeliveryStatus, (*generated.ReadDeliveryStatus).Validate)
	ReadPlatformFlow    = mustSchema(conversation.ToolReadPlatformFlow, (*generated.ReadPlatformFlow).Validate)
	ReadLiveAppRuns     = mustSchema(conversation.ToolReadLiveAppRuns, (*generated.ReadLiveAppRuns).Validate)
	WriteTodos          = mustSchema(conversation.ToolWriteTodos, (*generated.WriteTodos).Validate)
	StartTimer          = mustSchema(conversation.ToolStartTimer, (*generated.StartTimer).Validate)
	SetEffort           = mustSchema(conversation.ToolSetEffort, (*generated.SetEffort).Validate)
	ConfirmRequirements = mustSchema(conversation.ToolConfirmRequirements, (*generated.ConfirmRequirements).Validate)
	RequestUserInput    = mustSchema(conversation.ToolRequestUserInput, (*generated.RequestUserInput).Validate)
	// The delegation tools' inputs.
	StartAgent        = mustSchema(conversation.ToolStartAgent, (*generated.StartAgent).Validate)
	MergeAgentChanges = mustSchema(conversation.ToolMergeAgentChanges, (*generated.MergeAgentChanges).Validate)
)

// inputChecks decode the input of each tool strictly, by the tool's name. Every
// schema registers its own check when it loads.
var inputChecks = map[conversation.ToolName]func(string) error{}

// DecodeInput decodes the input of the named tool as the tool does when it
// runs, nested fields included, for checks that cannot run the tool.
func DecodeInput(tool conversation.ToolName, input string) error {
	check, found := inputChecks[tool]
	if !found {
		return fmt.Errorf("no input schema is registered for the tool %q", tool)
	}
	return check(input)
}

// mustSchema loads the embedded schema of the named tool and registers its
// check; a missing or malformed schema is a build defect.
func mustSchema[T any](name conversation.ToolName, validate func(*T) error) Schema[T] {
	encoded := comm.Must(toolInputSchemas.ReadFile("schema/" + string(name) + ".json"))
	var compact bytes.Buffer
	comm.MustDo(json.Compact(&compact, encoded))
	if compact.Bytes()[0] != '{' {
		panic(fmt.Sprintf("tool input schema %s must be one JSON object", name))
	}
	schema := Schema[T]{inputSchema: compact.String(), validate: validate}
	inputChecks[name] = func(encoded string) error {
		_, err := schema.Decode(encoded)
		return err
	}
	return schema
}
