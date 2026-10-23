// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// fdgSchemaVersion is the Flow Definition Graph schema the commit gate renders.
const fdgSchemaVersion = "2.0"

// The FDG node kinds the open-run check reads.
const (
	nodeKindStep      = "step"
	nodeKindAttribute = "attribute"
)

// Definitions are the Step types and Attribute value types of each Flow type of an
// application, read from its rendered Flow Definition Graphs.
type Definitions struct {
	flows map[string]flowDefinition
}

type flowDefinition struct {
	steps      map[string]bool
	attributes map[string]string
}

// fdgGraph holds what the check reads of one FDG 2.0 graph.
type fdgGraph struct {
	SchemaVersion string `json:"schemaVersion"`
	Valid         *bool  `json:"valid"`
	Flow          struct {
		Name string `json:"name"`
	} `json:"flow"`
	Nodes []struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Resource struct {
			ValueType string `json:"valueType"`
			Map       bool   `json:"map"`
		} `json:"resource"`
	} `json:"nodes"`
}

// renderedDefinition is one entry of the rendered definitions list, which holds the graph of a Flow type.
type renderedDefinition struct {
	Graph json.RawMessage `json:"graph"`
}

// ReadDefinitions reads the Flow definitions at path: one JSON file, or every .json file directly
// in a directory. A file holds one FDG 2.0 graph (dexcli visualize --json, docs/flows), the list
// that the commit gate's render-flow-definitions.py --output writes, or an object whose
// "definitions" holds that list.
func ReadDefinitions(path string) (Definitions, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Definitions{}, err
	}
	files := []string{path}
	if info.IsDir() {
		files, err = filepath.Glob(filepath.Join(path, "*.json"))
		if err != nil {
			return Definitions{}, err
		}
		sort.Strings(files)
	}
	definitions := Definitions{flows: map[string]flowDefinition{}}
	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 -- the caller names the definitions to compare.
		if err != nil {
			return Definitions{}, err
		}
		graphs, err := definitionGraphs(data)
		if err != nil {
			return Definitions{}, fmt.Errorf("%s: %w", file, err)
		}
		for _, graph := range graphs {
			if err := definitions.add(graph); err != nil {
				return Definitions{}, fmt.Errorf("%s: %w", file, err)
			}
		}
	}
	if len(definitions.flows) == 0 {
		return Definitions{}, fmt.Errorf("%s holds no Flow definition", path)
	}
	return definitions, nil
}

// definitionGraphs returns the graphs of one definitions file in any of its three forms.
func definitionGraphs(data []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	var list []renderedDefinition
	switch {
	case bytes.HasPrefix(trimmed, []byte("[")):
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
	case bytes.HasPrefix(trimmed, []byte("{")):
		var probe struct {
			Definitions []renderedDefinition `json:"definitions"`
			Nodes       json.RawMessage      `json:"nodes"`
		}
		if err := json.Unmarshal(trimmed, &probe); err != nil {
			return nil, err
		}
		if probe.Nodes != nil {
			return []json.RawMessage{trimmed}, nil
		}
		list = probe.Definitions
	}
	if len(list) == 0 {
		return nil, errors.New("not an FDG 2.0 graph or a list of rendered Flow definitions")
	}
	graphs := make([]json.RawMessage, 0, len(list))
	for i, entry := range list {
		if entry.Graph == nil {
			return nil, fmt.Errorf("definition %d has no graph", i+1)
		}
		graphs = append(graphs, entry.Graph)
	}
	return graphs, nil
}

func (d Definitions) add(data json.RawMessage) error {
	var graph fdgGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return err
	}
	name := bareTypeName(graph.Flow.Name)
	switch {
	case graph.SchemaVersion != fdgSchemaVersion:
		return fmt.Errorf("graph %q has schema version %q, not %s", name, graph.SchemaVersion, fdgSchemaVersion)
	case name == "":
		return errors.New("a graph names no Flow type")
	case graph.Valid == nil || !*graph.Valid:
		return fmt.Errorf("the graph of %s is not valid; render it with the commit gate's check first", name)
	}
	if _, repeated := d.flows[name]; repeated {
		return fmt.Errorf("two graphs define the Flow type %s", name)
	}
	flow := flowDefinition{steps: map[string]bool{}, attributes: map[string]string{}}
	for _, node := range graph.Nodes {
		switch node.Kind {
		case nodeKindStep:
			flow.steps[bareTypeName(node.Name)] = true
		case nodeKindAttribute:
			valueType := node.Resource.ValueType
			if node.Resource.Map {
				valueType = "AttributeMap of " + valueType
			}
			flow.attributes[node.Name] = valueType
		}
	}
	d.flows[name] = flow
	return nil
}

// bareTypeName drops a Go package qualifier that older renderers put on Flow and Step type
// names, which are Go identifiers; records carry the bare names.
func bareTypeName(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}

// OpenRunCheck is how a change of the Flow definitions meets the runs open at the export: it
// holds counts only, since the application repository commits it with the proposal.
type OpenRunCheck struct {
	// Export names the runs the check read; a review accepts only its own.
	Export   *ExportBinding `json:"export,omitempty"`
	OpenRuns int            `json:"open_runs"`
	// Steps are the Step types open runs are in, with whether the new definition keeps each.
	Steps []openStep `json:"steps"`
	// ChangedAttributes are the Attributes both definitions declare with different value types.
	ChangedAttributes []changedAttribute `json:"changed_attributes"`
	Gates             []Gate             `json:"gates"`
}

// openStep is a Step type that open runs are in.
type openStep struct {
	FlowType string `json:"flow_type"`
	Step     string `json:"step"`
	OpenRuns int    `json:"open_runs"`
	Kept     bool   `json:"kept"`
}

// changedAttribute is an Attribute whose value type the change alters, with the open runs of its Flow type.
type changedAttribute struct {
	FlowType  string `json:"flow_type"`
	Attribute string `json:"attribute"`
	Before    string `json:"before"`
	After     string `json:"after"`
	OpenRuns  int    `json:"open_runs"`
}

// CheckOpenRuns compares the before and after Flow definitions with every open record's active
// Steps: removing or renaming a Step an open run is in, or changing an existing Attribute's
// value type, fires open_runs_blocked.
func CheckOpenRuns(records []runexport.Record, before, after Definitions) (OpenRunCheck, error) {
	result := OpenRunCheck{Steps: []openStep{}, ChangedAttributes: []changedAttribute{}, Gates: []Gate{}}
	openByFlow := map[string]int{}
	inStep := map[[2]string]int{}
	for _, record := range records {
		if _, ok := before.flows[record.FlowType]; !ok {
			return OpenRunCheck{}, fmt.Errorf("the before definitions have no Flow type %s, which the export's runs have", record.FlowType)
		}
		if record.ClosedAt != nil {
			continue
		}
		result.OpenRuns++
		openByFlow[record.FlowType]++
		for _, step := range record.ActiveSteps {
			inStep[[2]string{record.FlowType, step}]++
		}
	}
	var blocked []string
	for key, n := range inStep {
		kept := after.flows[key[0]].steps[key[1]]
		result.Steps = append(result.Steps, openStep{FlowType: key[0], Step: key[1], OpenRuns: n, Kept: kept})
		if !kept {
			blocked = append(blocked, fmt.Sprintf("Step %s.%s is removed or renamed (open runs in it: %d)", key[0], key[1], n))
		}
	}
	sort.Slice(result.Steps, func(i, j int) bool {
		a, b := result.Steps[i], result.Steps[j]
		return a.FlowType < b.FlowType || (a.FlowType == b.FlowType && a.Step < b.Step)
	})
	for _, flowType := range sortedKeys(before.flows) {
		flow, kept := after.flows[flowType]
		if !kept {
			continue
		}
		for _, name := range sortedKeys(before.flows[flowType].attributes) {
			was, now := before.flows[flowType].attributes[name], flow.attributes[name]
			if now == "" || now == was {
				continue
			}
			result.ChangedAttributes = append(result.ChangedAttributes, changedAttribute{
				FlowType: flowType, Attribute: name, Before: was, After: now, OpenRuns: openByFlow[flowType],
			})
			blocked = append(blocked, fmt.Sprintf("Attribute %s.%s changes from %s to %s (open runs: %d)", flowType, name, was, now, openByFlow[flowType]))
		}
	}
	if len(blocked) > 0 {
		sort.Strings(blocked)
		result.Gates = append(result.Gates, GateOpenRunsBlocked.fire("%s", strings.Join(blocked, "; ")))
	}
	return result, nil
}
