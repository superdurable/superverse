// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fdg

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

// A bundle holds one to maxBundleGraphs graphs of at most maxGraphBytes each.
const (
	maxBundleGraphs = 128
	maxGraphBytes   = 8 << 20
)

// ErrInvalidBundle reports graphs that do not form one valid FDG 2.0 bundle.
var ErrInvalidBundle = errors.New("invalid Flow Definition Graph bundle")

// Bundle is one validated Flow Definition Graph 2.0 bundle: the graphs of a
// prepared source, a Release build or a running Preview, in publication order.
type Bundle struct {
	// Revision is the bundle's definition revision, its BundleDigest.
	Revision string
	// Definitions holds the V2 contract of each Flow type.
	Definitions map[string]V2Definition
	// ConnectorSteps lists the connector factory Steps of every graph.
	ConnectorSteps []ConnectorStep
	// entries are the graphs of the definitions document, in publication order.
	entries []definitionsEntry
}

// DefinitionsJSON is the Studio definitions document that
// GetStudioDefinitions serves: every graph of the bundle with its V2
// contract. flowapi.FlowAPI.Catalog is the serving catalog of the same bundle.
func (bundle *Bundle) DefinitionsJSON() []byte {
	return comm.Must(json.Marshal(definitionsDocument{
		Configured: true, Source: "release", DefinitionRevision: bundle.Revision,
		DefinitionCount: len(bundle.entries), Definitions: bundle.entries,
	}))
}

// ConnectorStep is one connector factory Step of a Flow Definition Graph.
type ConnectorStep struct {
	FlowType, StepID, StepName string
	Connector                  ConnectorStepMetadata
}

// ConnectorStepMetadata is the connector operation a factory Step uses, as the
// analyzer records it in the Step node's metadata.
type ConnectorStepMetadata struct {
	ConnectorID     string                     `json:"connectorId"`
	OperationID     string                     `json:"operationId"`
	OperationKind   string                     `json:"operationKind"`
	ConnectionName  string                     `json:"connectionName"`
	ModulePath      string                     `json:"modulePath"`
	ModuleVersion   string                     `json:"moduleVersion"`
	ConfigurationUI V2ConnectorConfigurationUI `json:"configurationUI"`
}

// ParseBundle validates graphs as one bundle in publication order. Every graph
// must be a valid FDG 2.0 graph of Go source with a distinct Flow type.
func ParseBundle(graphs []json.RawMessage) (*Bundle, error) {
	if len(graphs) == 0 || len(graphs) > maxBundleGraphs {
		return nil, fmt.Errorf("%w: a bundle holds between one and %d graphs", ErrInvalidBundle, maxBundleGraphs)
	}
	bundle := &Bundle{
		Revision: appmanifest.FlowDefinitionDigest(graphs), Definitions: make(map[string]V2Definition, len(graphs)),
		entries: make([]definitionsEntry, 0, len(graphs)),
	}
	files := make(map[string]string, len(graphs))
	for index, graph := range graphs {
		file := appmanifest.GraphFileName(index)
		definition, steps, err := parseGraph(file, graph)
		if err != nil {
			return nil, fmt.Errorf("%w: graph %s: %w", ErrInvalidBundle, file, err)
		}
		if previous, repeated := files[definition.FlowName]; repeated {
			return nil, fmt.Errorf("%w: graphs %s and %s define Flow type %q", ErrInvalidBundle, previous, file, definition.FlowName)
		}
		files[definition.FlowName] = file
		bundle.Definitions[definition.FlowName] = *definition.V2
		bundle.ConnectorSteps = append(bundle.ConnectorSteps, steps...)
		bundle.entries = append(bundle.entries, definition)
	}
	return bundle, nil
}

// ConnectorTriggerBinding returns the first configurable binding, in publication
// order, that a graph declares for the connector connection, trigger and binding.
func (bundle *Bundle) ConnectorTriggerBinding(connectorID, connectionName, triggerName, bindingName string) (V2ConnectorTriggerBinding, bool) {
	for _, entry := range bundle.entries {
		for _, binding := range entry.V2.ConnectorTriggerBindings {
			if binding.ConfigurationEnabled && binding.ConnectorID == connectorID && binding.ConnectionName == connectionName &&
				binding.TriggerName == triggerName && binding.BindingName == bindingName {
				return binding, true
			}
		}
	}
	return V2ConnectorTriggerBinding{}, false
}

// definitionsDocument is the Studio definitions document.
type definitionsDocument struct {
	Configured         bool               `json:"configured"`
	Source             string             `json:"source,omitempty"`
	DefinitionRevision string             `json:"definitionRevision,omitempty"`
	DefinitionCount    int                `json:"definitionCount"`
	Definitions        []definitionsEntry `json:"definitions"`
}

// definitionsEntry is one graph of the definitions document.
type definitionsEntry struct {
	ID             string          `json:"id"`
	File           string          `json:"file"`
	SchemaVersion  string          `json:"schemaVersion"`
	FlowName       string          `json:"flowName"`
	SourceLanguage string          `json:"sourceLanguage"`
	SourcePath     string          `json:"sourcePath"`
	Valid          bool            `json:"valid"`
	Graph          json.RawMessage `json:"graph"`
	V2             *V2Definition   `json:"v2,omitempty"`
}

// graphDocument holds the exact top-level fields of an FDG 2.0 graph. Each one
// is decoded separately so that the graph bytes are scanned only once.
type graphDocument struct {
	SchemaVersion json.RawMessage `json:"schemaVersion"`
	Valid         json.RawMessage `json:"valid"`
	Source        json.RawMessage `json:"source"`
	Flow          json.RawMessage `json:"flow"`
	Nodes         json.RawMessage `json:"nodes"`
	Edges         json.RawMessage `json:"edges"`
	Diagnostics   json.RawMessage `json:"diagnostics"`
	Groups        json.RawMessage `json:"groups"`
	V2            json.RawMessage `json:"v2"`
}

type graphGroup struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	StepIDs []string `json:"stepIds"`
}

var graphGroupIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func parseGraph(file string, graph []byte) (definitionsEntry, []ConnectorStep, error) {
	if len(graph) > maxGraphBytes {
		return definitionsEntry{}, nil, fmt.Errorf("graph exceeds %d bytes", maxGraphBytes)
	}
	var document graphDocument
	if err := comm.DecodeStrictJSONNumbers(graph, &document); err != nil {
		return definitionsEntry{}, nil, err
	}
	var schemaVersion string
	var valid bool
	var source struct {
		Language *string `json:"language"`
		Path     *string `json:"path"`
	}
	var flow struct {
		Name        *string         `json:"name"`
		StartStepID json.RawMessage `json:"startStepId"`
		Span        json.RawMessage `json:"span"`
	}
	var nodes, edges, diagnostics []json.RawMessage
	var groups []graphGroup
	var definition *V2Definition
	for _, field := range []struct {
		name  string
		raw   json.RawMessage
		value any
	}{
		{"schemaVersion", document.SchemaVersion, &schemaVersion}, {"valid", document.Valid, &valid},
		{"source", document.Source, &source}, {"flow", document.Flow, &flow},
		{"nodes", document.Nodes, &nodes}, {"edges", document.Edges, &edges},
		{"diagnostics", document.Diagnostics, &diagnostics}, {"groups", document.Groups, &groups},
		{"v2", document.V2, &definition},
	} {
		if field.raw == nil {
			return definitionsEntry{}, nil, fmt.Errorf("graph is missing field %q", field.name)
		}
		if err := comm.DecodeStrictJSONNumbers(field.raw, field.value); err != nil {
			return definitionsEntry{}, nil, fmt.Errorf("field %q is malformed: %w", field.name, err)
		}
	}
	switch {
	case schemaVersion != "2.0":
		return definitionsEntry{}, nil, fmt.Errorf("schemaVersion %q is not 2.0", schemaVersion)
	case !valid:
		return definitionsEntry{}, nil, errors.New("graph is not valid")
	case source.Language == nil || source.Path == nil || *source.Language != "go":
		return definitionsEntry{}, nil, errors.New("source must name a Go language and path")
	case flow.Name == nil:
		return definitionsEntry{}, nil, errors.New("flow must have a name")
	case nodes == nil || edges == nil || diagnostics == nil || groups == nil || definition == nil:
		return definitionsEntry{}, nil, errors.New("nodes, edges, diagnostics, groups and v2 are required")
	}
	if err := validateGraphGroups(groups); err != nil {
		return definitionsEntry{}, nil, err
	}
	if err := validateV2Definition(*definition); err != nil {
		return definitionsEntry{}, nil, fmt.Errorf("v2 contract: %w", err)
	}
	flowName := strings.TrimSpace(*flow.Name)
	if flowName == "" {
		flowName = strings.TrimSuffix(file, ".json")
	}
	steps, err := readGraphNodes(flowName, nodes, definition.Start)
	if err != nil {
		return definitionsEntry{}, nil, err
	}
	return definitionsEntry{
		ID: file, File: file, SchemaVersion: schemaVersion, FlowName: flowName,
		SourceLanguage: *source.Language, SourcePath: *source.Path, Valid: valid, Graph: graph, V2: definition,
	}, steps, nil
}

func validateGraphGroups(groups []graphGroup) error {
	groupIDs := make(map[string]bool, len(groups))
	stepIDs := make(map[string]bool)
	for _, group := range groups {
		if !graphGroupIDPattern.MatchString(group.ID) || strings.TrimSpace(group.Label) == "" || len(group.StepIDs) == 0 {
			return errors.New("graph has an invalid group")
		}
		if groupIDs[group.ID] {
			return fmt.Errorf("graph repeats group %q", group.ID)
		}
		groupIDs[group.ID] = true
		for _, stepID := range group.StepIDs {
			if stepID == "" || stepIDs[stepID] {
				return errors.New("graph has an invalid or repeated grouped Step ID")
			}
			stepIDs[stepID] = true
		}
	}
	return nil
}

// stepPhase is the phase of a Step node: what the Step does when it runs. A
// Start Step either only executes or first waits for its conditions.
type stepPhase string

const (
	stepPhaseExecute        stepPhase = "execute"
	stepPhaseWaitForExecute stepPhase = "wait_for+execute"
)

// readGraphNodes resolves the Start Step's phase into start and returns the
// connector factory Steps, in one pass over the Step nodes.
func readGraphNodes(flowType string, nodes []json.RawMessage, start *V2StartDefinition) ([]ConnectorStep, error) {
	var steps []ConnectorStep
	startFound := false
	for _, raw := range nodes {
		var node struct {
			ID       string    `json:"id"`
			Kind     string    `json:"kind"`
			Name     string    `json:"name"`
			Phase    stepPhase `json:"phase"`
			Metadata struct {
				ConnectorFactory bool                   `json:"connectorFactory"`
				Connector        *ConnectorStepMetadata `json:"connector"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			return nil, fmt.Errorf("malformed node: %w", err)
		}
		if node.Kind != "step" {
			continue
		}
		if node.Metadata.ConnectorFactory && node.Metadata.Connector != nil {
			steps = append(steps, ConnectorStep{FlowType: flowType, StepID: node.ID, StepName: node.Name, Connector: *node.Metadata.Connector})
		}
		if start == nil || node.Name != start.StepType {
			continue
		}
		if startFound {
			return nil, fmt.Errorf("start Step %q is repeated", start.StepType)
		}
		startFound = true
		switch node.Phase {
		case stepPhaseExecute:
			start.SkipWaitFor = true
		case stepPhaseWaitForExecute:
			start.SkipWaitFor = false
		default:
			return nil, fmt.Errorf("start Step %q has unsupported phase %q", start.StepType, node.Phase)
		}
	}
	if start != nil && !startFound {
		return nil, fmt.Errorf("start Step %q is absent from the graph nodes", start.StepType)
	}
	return steps, nil
}
