// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package appmanifest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

// FlowDefinition is the strict Flow Definition Graph (FDG) 2.0 of one Flow
// that dex-app.yaml declares. Release and Preview bundles persist it, so its
// JSON names are part of those stored formats.
type FlowDefinition struct {
	FlowType      string          `json:"flowType"`
	SourcePath    string          `json:"sourcePath"`
	SchemaVersion string          `json:"schemaVersion"`
	Digest        string          `json:"digest"`
	Valid         bool            `json:"valid"`
	Graph         json.RawMessage `json:"graph"`
}

// ValidateFlowDefinition accepts a valid FDG 2.0 of its FlowType without
// diagnostics whose digest covers the compact graph bytes.
func ValidateFlowDefinition(definition FlowDefinition) error {
	var graph struct {
		SchemaVersion string            `json:"schemaVersion"`
		Valid         bool              `json:"valid"`
		Diagnostics   []json.RawMessage `json:"diagnostics"`
		Flow          struct {
			Name string `json:"name"`
		} `json:"flow"`
	}
	if err := json.Unmarshal(definition.Graph, &graph); err != nil {
		return err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, definition.Graph); err != nil {
		return err
	}
	if definition.FlowType == "" || definition.SourcePath == "" || !definition.Valid || definition.SchemaVersion != "2.0" ||
		graph.SchemaVersion != "2.0" || !graph.Valid || len(graph.Diagnostics) > 0 || graph.Flow.Name != definition.FlowType ||
		objectstore.Digest(compact.Bytes()) != definition.Digest {
		return errors.New("invalid or diagnostic-bearing Flow Definition")
	}
	return nil
}

// ValidateFlowDefinitions accepts a publishable bundle: at least one valid
// definition, in strictly increasing FlowType order, with unique source paths.
// FlowType order is the publication order that the bundle digest covers.
func ValidateFlowDefinitions(definitions []FlowDefinition) error {
	if len(definitions) == 0 {
		return errors.New("a Flow Definition bundle needs at least one definition")
	}
	paths := map[string]bool{}
	for index, definition := range definitions {
		if err := ValidateFlowDefinition(definition); err != nil {
			return err
		}
		if index > 0 && definitions[index-1].FlowType >= definition.FlowType || paths[definition.SourcePath] {
			return errors.New("definitions need unique source paths in strictly increasing FlowType order")
		}
		paths[definition.SourcePath] = true
	}
	return nil
}

// ActiveFlowDefinitionManifestKey is the object from which project Dex serves
// Flow Definitions.
const ActiveFlowDefinitionManifestKey = "flow-definitions/active-manifest"

// FlowDefinitionManifest is the record from which project Dex loads one
// published bundle: the bundle's graph at index i is the object GraphKey(i).
type FlowDefinitionManifest struct {
	SchemaVersion   string `json:"schemaVersion"`
	ReleaseID       string `json:"releaseId"`
	BundlePrefix    string `json:"bundlePrefix"`
	BundleDigest    string `json:"bundleDigest"`
	DefinitionCount int    `json:"definitionCount"`
}

// NewFlowDefinitionManifest describes the publication of a validated bundle
// under artifactID, a stable identifier of the published content.
func NewFlowDefinitionManifest(artifactID string, definitions []FlowDefinition) FlowDefinitionManifest {
	graphs := make([]json.RawMessage, len(definitions))
	for index, definition := range definitions {
		graphs[index] = definition.Graph
	}
	return FlowDefinitionManifest{SchemaVersion: "1.0", ReleaseID: artifactID, BundlePrefix: "flow-definitions/releases/" + artifactID + "/", BundleDigest: FlowDefinitionDigest(graphs), DefinitionCount: len(definitions)}
}

// GraphKey is the object key of the bundle's graph at index.
func (m FlowDefinitionManifest) GraphKey(index int) string {
	return m.BundlePrefix + GraphFileName(index)
}

// FlowDefinitionDigest identifies the published graphs of a bundle. It is the
// SHA-256 over each graph's file name and bytes in bundle order, each framed by
// its unsigned 64-bit big-endian length; serving Workers report the same value
// as their definition revision.
func FlowDefinitionDigest(graphs []json.RawMessage) string {
	var framed []byte
	for index, graph := range graphs {
		name := GraphFileName(index)
		framed = binary.BigEndian.AppendUint64(framed, uint64(len(name)))
		framed = append(framed, name...)
		framed = binary.BigEndian.AppendUint64(framed, uint64(len(graph)))
		framed = append(framed, graph...)
	}
	return objectstore.Digest(framed)
}

// GraphFileName names the bundle's graph at index. Zero padding keeps lexical
// and publication order equal; FlowDefinitionDigest frames each graph with it.
func GraphFileName(index int) string { return fmt.Sprintf("%04d.json", index) }
