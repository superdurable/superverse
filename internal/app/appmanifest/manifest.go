// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package appmanifest

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"google.golang.org/protobuf/types/known/structpb"
)

// Manifest is dex-app.yaml, the JSON document at the root of an application
// repository that declares how the application is served, which Flows it
// defines and which connectors it uses.
type Manifest struct {
	SchemaVersion string `json:"schemaVersion"`
	Application   struct {
		Port        int                      `json:"port"`
		HealthPath  string                   `json:"healthPath"`
		Environment []environmentDeclaration `json:"environment,omitempty"`
	} `json:"application"`
	FlowDefinitions []struct {
		SourcePath string `json:"sourcePath"`
	} `json:"flowDefinitions"`
	Connectors []struct {
		ConnectionName  string   `json:"connectionName"`
		ConnectorID     string   `json:"connectorId"`
		ModulePath      string   `json:"modulePath"`
		Version         string   `json:"version"`
		AuthMethodID    *string  `json:"authMethodId"`
		Operations      []string `json:"operations"`
		TriggerBindings []struct {
			TriggerName string `json:"triggerName"`
			BindingName string `json:"bindingName"`
		} `json:"triggerBindings,omitempty"`
	} `json:"connectors"`
}

// connectorModuleRoot is the module path prefix of every published connector.
const connectorModuleRoot = "github.com/superdurable/dex-connectors-library/connectors/"

// WorkerPort is where the application's Dex Worker listens beside its HTTP
// port, so the application may not declare it.
const WorkerPort = 8811

// ErrInvalidAppManifest marks a dex-app.yaml whose content the checks below
// reject: the application's own source, which only a change of the source
// repairs. It wraps the reason, which names the field.
var ErrInvalidAppManifest = errors.New("invalid dex-app.yaml")

// ExtractAppManifest validates the dex-app.yaml of commit as the template's
// own source check does, without executing any repository script, and returns
// its canonical requirements with their digests. The digests exclude Release
// identity. Content the checks reject is ErrInvalidAppManifest.
func ExtractAppManifest(commit string, contents []byte) (*pb.AppManifestSnapshot, error) {
	if !flowid.IsCommitSHA(commit) {
		return nil, errors.New("full source commit SHA is required")
	}
	manifest, err := decodeManifest(contents)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAppManifest, err)
	}
	connectors, err := connectorRequirements(manifest)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAppManifest, err)
	}
	environment, err := environmentRequirements(manifest, len(connectors) > 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAppManifest, err)
	}
	connectorDigest, environmentDigest := requirementsDigests(connectors, environment)
	return &pb.AppManifestSnapshot{SourceCommit: commit, ManifestDigest: objectstore.Digest(contents), ConnectorContractDigest: connectorDigest, EnvironmentContractDigest: environmentDigest, Revision: 1, Connectors: connectors, Environment: environment}, nil
}

// decodeManifest strictly decodes one manifest document and checks its
// schema, how the application is served and where its Flows are defined.
func decodeManifest(contents []byte) (Manifest, error) {
	var manifest Manifest
	if err := comm.DecodeStrictJSON(contents, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("not strict JSON: %w", err)
	}
	application := manifest.Application
	switch {
	case manifest.SchemaVersion != "superverse.dev/dex-app/v1" || len(manifest.FlowDefinitions) == 0 || manifest.Connectors == nil:
		return Manifest{}, errors.New("manifest schema, flow definitions and connectors are required")
	// The application runs unprivileged, so it cannot bind a port below 1024.
	case application.Port < 1024 || application.Port > 65535 || application.Port == WorkerPort:
		return Manifest{}, fmt.Errorf("application.port must be 1024 to 65535 and not the Worker port %d", WorkerPort)
	// The health path is appended to probe and route URLs and copied into an
	// ingress annotation: a leading // would name a host, and a query,
	// fragment or line break would change the request.
	case !strings.HasPrefix(application.HealthPath, "/") || strings.HasPrefix(application.HealthPath, "//") || strings.ContainsAny(application.HealthPath, "?#\r\n"):
		return Manifest{}, errors.New("application.healthPath must start with a single / and contain no ?, #, CR or LF")
	}
	declared := map[string]bool{}
	for index, definition := range manifest.FlowDefinitions {
		p := definition.SourcePath
		if p == "" || strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.Contains(p, "..") || !strings.HasSuffix(p, ".go") {
			return Manifest{}, errors.New("each Flow Definition path must identify a repository-relative Go source file")
		}
		if declared[p] {
			return Manifest{}, fmt.Errorf("flowDefinitions[%d].sourcePath %q is declared twice", index, p)
		}
		declared[p] = true
	}
	return manifest, nil
}

// DeclaredFlowSources is the set of Go sources whose Flows the manifest
// declares. ExtractAppManifest admits each source once.
func (m Manifest) DeclaredFlowSources() map[string]bool {
	sources := make(map[string]bool, len(m.FlowDefinitions))
	for _, definition := range m.FlowDefinitions {
		sources[definition.SourcePath] = true
	}
	return sources
}

// connectorRequirements validates every declared connector and returns their
// canonical requirements. The Agent repairs dex-app.yaml from these messages,
// so each one names its exact field.
func connectorRequirements(manifest Manifest) ([]*pb.ConnectorRequirement, error) {
	requirements := make([]*pb.ConnectorRequirement, 0, len(manifest.Connectors))
	names := map[string]bool{}
	for index, connector := range manifest.Connectors {
		field := fmt.Sprintf("connectors[%d]", index)
		switch {
		case connector.ConnectionName == "":
			return nil, fmt.Errorf("%s.connectionName is required", field)
		case names[connector.ConnectionName]:
			return nil, fmt.Errorf("%s.connectionName %q is declared twice", field, connector.ConnectionName)
		case connector.ConnectorID == "":
			return nil, fmt.Errorf("%s.connectorId is required", field)
		case connector.Version == "":
			return nil, fmt.Errorf("%s.version is required", field)
		case connector.AuthMethodID == nil:
			return nil, fmt.Errorf(`%s.authMethodId is required; use "" when the connector release has one authentication method`, field)
		case len(connector.Operations) == 0 && len(connector.TriggerBindings) == 0:
			return nil, fmt.Errorf("%s needs operations or triggerBindings", field)
		case !publishedConnectorModule(connector.ModulePath):
			return nil, fmt.Errorf("%s.modulePath must be the exact published module under %s", field, connectorModuleRoot)
		case slices.Contains(connector.Operations, ""):
			return nil, errors.New("empty connector operation")
		}
		names[connector.ConnectionName] = true
		bindings := make([]*pb.ConnectorTriggerBinding, 0, len(connector.TriggerBindings))
		seen := map[string]bool{}
		for _, binding := range connector.TriggerBindings {
			key := binding.TriggerName + "\x00" + binding.BindingName
			if binding.TriggerName == "" || binding.BindingName == "" || seen[key] {
				return nil, errors.New("invalid or duplicate trigger binding")
			}
			seen[key] = true
			bindings = append(bindings, &pb.ConnectorTriggerBinding{TriggerName: binding.TriggerName, BindingName: binding.BindingName})
		}
		requirements = append(requirements, &pb.ConnectorRequirement{ConnectionName: connector.ConnectionName, ConnectorId: connector.ConnectorID, ModulePath: connector.ModulePath, Version: connector.Version, AuthMethodId: *connector.AuthMethodID, Operations: connector.Operations, TriggerBindings: bindings})
	}
	return canonicalRequirements(requirements), nil
}

// environmentRequirements describes how the application is served and the
// environment fields it declares.
func environmentRequirements(manifest Manifest, hasConnectors bool) (*structpb.Struct, error) {
	environment := map[string]any{"port": float64(manifest.Application.Port), "healthPath": manifest.Application.HealthPath, "publicBaseUrlRequired": true, "connectorConfigurationRequired": hasConnectors}
	fields, err := environmentDeclarations(manifest.Application.Environment)
	if err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		environment["environment"] = fields
	}
	// Decoded JSON and validated declarations hold only valid UTF-8 and the
	// value kinds a Struct holds.
	return comm.Must(structpb.NewStruct(environment)), nil
}

// publishedConnectorModule accepts the module path of a published connector,
// which locates its generic manifest without a platform connector catalog.
func publishedConnectorModule(module string) bool {
	suffix, found := strings.CutPrefix(module, connectorModuleRoot)
	if !found {
		return false
	}
	for _, segment := range strings.Split(suffix, "/") {
		if segment == "" || strings.Trim(segment, "abcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			return false
		}
	}
	return true
}
