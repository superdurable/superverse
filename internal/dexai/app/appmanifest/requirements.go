// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package appmanifest

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"google.golang.org/protobuf/types/known/structpb"
)

// The connector and environment requirements of a source have one canonical
// JSON form. Its digests identify the requirements in the AppManifest
// snapshot, the source objects and the Release artifacts, so the form below
// must stay byte-stable: fields in alphabetical order and [] for an empty list.

// connectorRequirementJSON is the canonical JSON form of one connector requirement.
type connectorRequirementJSON struct {
	AuthMethodID    string               `json:"authMethodId"`
	ConnectionName  string               `json:"connectionName"`
	ConnectorID     string               `json:"connectorId"`
	ModulePath      string               `json:"modulePath"`
	Operations      []string             `json:"operations"`
	Optional        bool                 `json:"optional,omitempty"`
	TriggerBindings []triggerBindingJSON `json:"triggerBindings"`
	Version         string               `json:"version"`
}

// triggerBindingJSON is the canonical JSON form of one trigger binding.
type triggerBindingJSON struct {
	BindingName string `json:"bindingName"`
	TriggerName string `json:"triggerName"`
}

// canonicalRequirements returns sorted copies of requirements: connections by
// name, each with its operations sorted and deduplicated and its trigger
// bindings sorted. Release verification passes requirements decoded from a
// build's artifacts, so the canonical form is always computed, never assumed.
func canonicalRequirements(requirements []*pb.ConnectorRequirement) []*pb.ConnectorRequirement {
	canonical := make([]*pb.ConnectorRequirement, 0, len(requirements))
	for _, r := range requirements {
		bindings := slices.Clone(r.GetTriggerBindings())
		slices.SortFunc(bindings, func(a, b *pb.ConnectorTriggerBinding) int {
			return cmp.Or(cmp.Compare(a.GetTriggerName(), b.GetTriggerName()), cmp.Compare(a.GetBindingName(), b.GetBindingName()))
		})
		canonical = append(canonical, &pb.ConnectorRequirement{
			ConnectionName: r.GetConnectionName(), Optional: r.GetOptional(), ConnectorId: r.GetConnectorId(), ModulePath: r.GetModulePath(), Version: r.GetVersion(), AuthMethodId: r.GetAuthMethodId(),
			Operations: slices.Compact(slices.Sorted(slices.Values(r.GetOperations()))), TriggerBindings: bindings,
		})
	}
	slices.SortFunc(canonical, func(a, b *pb.ConnectorRequirement) int {
		return cmp.Compare(a.GetConnectionName(), b.GetConnectionName())
	})
	return canonical
}

// requirementsJSON is the canonical JSON form of requirements.
func requirementsJSON(requirements []*pb.ConnectorRequirement) []connectorRequirementJSON {
	entries := make([]connectorRequirementJSON, 0, len(requirements))
	for _, r := range canonicalRequirements(requirements) {
		bindings := make([]triggerBindingJSON, 0, len(r.TriggerBindings))
		for _, b := range r.TriggerBindings {
			bindings = append(bindings, triggerBindingJSON{BindingName: b.GetBindingName(), TriggerName: b.GetTriggerName()})
		}
		entries = append(entries, connectorRequirementJSON{
			AuthMethodID: r.AuthMethodId, ConnectionName: r.ConnectionName, ConnectorID: r.ConnectorId, ModulePath: r.ModulePath,
			Optional: r.Optional, Operations: append([]string{}, r.Operations...), TriggerBindings: bindings, Version: r.Version,
		})
	}
	return entries
}

// requirement is the connector requirement that a decoded entry describes.
func (r connectorRequirementJSON) requirement() *pb.ConnectorRequirement {
	bindings := make([]*pb.ConnectorTriggerBinding, 0, len(r.TriggerBindings))
	for _, b := range r.TriggerBindings {
		bindings = append(bindings, &pb.ConnectorTriggerBinding{TriggerName: b.TriggerName, BindingName: b.BindingName})
	}
	return &pb.ConnectorRequirement{ConnectionName: r.ConnectionName, Optional: r.Optional, ConnectorId: r.ConnectorID, ModulePath: r.ModulePath, Version: r.Version, AuthMethodId: r.AuthMethodID, Operations: r.Operations, TriggerBindings: bindings}
}

// CanonicalRequirementsJSON returns the canonical JSON of the connector and
// environment requirements, without any artifact envelope or source identity.
func CanonicalRequirementsJSON(requirements []*pb.ConnectorRequirement, environment *structpb.Struct) (connectors, application []byte) {
	return comm.Must(json.Marshal(requirementsJSON(requirements))), comm.Must(json.Marshal(environment.AsMap()))
}

// requirementsDigests returns the digests of the canonical connector and
// environment requirements.
func requirementsDigests(requirements []*pb.ConnectorRequirement, environment *structpb.Struct) (connectorDigest, environmentDigest string) {
	connectors, application := CanonicalRequirementsJSON(requirements, environment)
	return objectstore.Digest(connectors), objectstore.Digest(application)
}

// A Release carries its requirements as two artifacts, connector-contract.json
// and environment-contract.json. The app-manifest tool renders them in both
// builders and the Release verifier reads them back, so their schema versions
// and JSON names are a stored format.
const (
	connectorContractSchema   = "superverse.dev/connector-contract/v1"
	environmentContractSchema = "superverse.dev/environment-contract/v1"
)

// ConnectorContract is the connector-contract.json artifact of one Release.
type ConnectorContract struct {
	SchemaVersion string                     `json:"schemaVersion"`
	ProjectID     string                     `json:"projectId"`
	ReleaseID     string                     `json:"releaseId"`
	Connections   []connectorRequirementJSON `json:"connections"`
}

// EnvironmentContract is the environment-contract.json artifact of one Release.
type EnvironmentContract struct {
	SchemaVersion string         `json:"schemaVersion"`
	ProjectID     string         `json:"projectId"`
	ReleaseID     string         `json:"releaseId"`
	Application   map[string]any `json:"application"`
}

// ReleaseContracts are the contract artifacts of the Release releaseID of
// projectID, built from the source that manifest describes.
func ReleaseContracts(projectID, releaseID string, manifest *pb.AppManifestSnapshot) (ConnectorContract, EnvironmentContract) {
	return ConnectorContract{SchemaVersion: connectorContractSchema, ProjectID: projectID, ReleaseID: releaseID, Connections: requirementsJSON(manifest.Connectors)},
		EnvironmentContract{SchemaVersion: environmentContractSchema, ProjectID: projectID, ReleaseID: releaseID, Application: manifest.Environment.AsMap()}
}

// VerifyReleaseContracts accepts the contract artifacts of a build when they
// belong to the Release releaseID of projectID and carry exactly the
// requirements of the source manifest.
func VerifyReleaseContracts(projectID, releaseID string, connectorJSON, environmentJSON []byte, source *pb.AppManifestSnapshot) error {
	var connectors ConnectorContract
	var environment EnvironmentContract
	if json.Unmarshal(connectorJSON, &connectors) != nil || json.Unmarshal(environmentJSON, &environment) != nil {
		return errors.New("invalid release connector or environment JSON")
	}
	if connectors.SchemaVersion != connectorContractSchema || environment.SchemaVersion != environmentContractSchema ||
		connectors.ProjectID != projectID || environment.ProjectID != projectID || connectors.ReleaseID != releaseID || environment.ReleaseID != releaseID {
		return errors.New("release connector or environment artifact scope mismatch")
	}
	requirements := make([]*pb.ConnectorRequirement, 0, len(connectors.Connections))
	for _, entry := range connectors.Connections {
		requirements = append(requirements, entry.requirement())
	}
	// JSON decoding yields only valid UTF-8 and the value kinds a Struct holds.
	application := comm.Must(structpb.NewStruct(environment.Application))
	connectorDigest, environmentDigest := requirementsDigests(requirements, application)
	if connectorDigest != source.ConnectorContractDigest || environmentDigest != source.EnvironmentContractDigest {
		return errors.New("release requirements differ from the exact source manifest")
	}
	return nil
}
