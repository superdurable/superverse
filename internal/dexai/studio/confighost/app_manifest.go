// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
)

const projectManifestSchema = "dex.dev/project-app-manifest/v1"

// projectManifestRecord is a scope's admitted AppManifest with its contract
// digests, as stored in the AppManifest head and in prepared sources.
type projectManifestRecord struct {
	SchemaVersion             string                `json:"schemaVersion"`
	Scope                     connectorconfig.Scope `json:"scope"`
	Revision                  uint64                `json:"appManifestRevision"`
	SourceCommit              string                `json:"sourceCommit"`
	ManifestDigest            string                `json:"manifestDigest"`
	ConnectorContractDigest   string                `json:"connectorContractDigest"`
	EnvironmentContractDigest string                `json:"environmentContractDigest"`
	Manifest                  projectAppManifest    `json:"manifest"`
}

type projectAppManifest struct {
	SchemaVersion string `json:"schemaVersion"`
	Application   struct {
		Port        int                                      `json:"port"`
		HealthPath  string                                   `json:"healthPath"`
		Environment []connectorconfig.EnvironmentDeclaration `json:"environment,omitempty"`
	} `json:"application"`
	FlowDefinitions []struct {
		SourcePath string `json:"sourcePath"`
	} `json:"flowDefinitions"`
	Connectors []projectConnectorDeclaration `json:"connectors"`
}

// projectConnectorDeclaration is one connection the application declares.
type projectConnectorDeclaration struct {
	TriggerBindings []projectTriggerBinding `json:"triggerBindings,omitempty"`
	ModulePath      string                  `json:"modulePath"`
	ConnectionName  string                  `json:"connectionName"`
	Optional        bool                    `json:"optional,omitempty"`
	ConnectorID     string                  `json:"connectorId"`
	Version         string                  `json:"version"`
	// AuthMethodID is the one authentication method the application selected;
	// empty selects a release's single method.
	AuthMethodID string   `json:"authMethodId"`
	Operations   []string `json:"operations"`
}

type projectTriggerBinding struct {
	TriggerName string `json:"triggerName"`
	BindingName string `json:"bindingName"`
}

// module is the exact Connector release module the declaration uses.
func (declaration projectConnectorDeclaration) module() connectormanifest.Module {
	return connectormanifest.Module{ConnectorID: declaration.ConnectorID, ConnectionName: declaration.ConnectionName, ModulePath: declaration.ModulePath, ModuleVersion: declaration.Version}
}

// ManifestRegistration is the exact AppManifest a scope registers for a source commit.
type ManifestRegistration struct {
	ExpectedRevision uint64 `json:"expectedRevision"`
	SourceCommit     string `json:"sourceCommit"`
	ManifestDigest   string `json:"manifestDigest"`
	Manifest         string `json:"manifest"`
}

// parseProjectManifest admits a registration through the same source
// validation and contract digests as setup and build admission.
func parseProjectManifest(registration ManifestRegistration) (projectManifestRecord, error) {
	contents := []byte(registration.Manifest)
	if objectstore.Digest(contents) != registration.ManifestDigest {
		return projectManifestRecord{}, errors.New("the AppManifest digest does not match its contents")
	}
	snapshot, err := appmanifest.ExtractAppManifest(registration.SourceCommit, contents)
	if err != nil {
		return projectManifestRecord{}, fmt.Errorf("the AppManifest source declarations are invalid: %w", err)
	}
	var manifest projectAppManifest
	if err = comm.DecodeStrictJSON(contents, &manifest); err != nil {
		return projectManifestRecord{}, fmt.Errorf("the AppManifest contains invalid or unsupported fields: %w", err)
	}
	// Source admission sorted the connections and normalized their operations
	// and trigger bindings.
	manifest.Connectors = make([]projectConnectorDeclaration, 0, len(snapshot.GetConnectors()))
	for _, requirement := range snapshot.GetConnectors() {
		declaration := projectConnectorDeclaration{
			ModulePath: requirement.GetModulePath(), ConnectionName: requirement.GetConnectionName(), ConnectorID: requirement.GetConnectorId(),
			Optional: requirement.GetOptional(), Version: requirement.GetVersion(), AuthMethodID: requirement.GetAuthMethodId(), Operations: requirement.GetOperations(),
		}
		for _, binding := range requirement.GetTriggerBindings() {
			declaration.TriggerBindings = append(declaration.TriggerBindings, projectTriggerBinding{TriggerName: binding.GetTriggerName(), BindingName: binding.GetBindingName()})
		}
		if !declaration.module().IsPublishedRelease() || !validConnectionName(declaration.ConnectionName) {
			return projectManifestRecord{}, errors.New("an AppManifest connector is not an exact official Connector release or release candidate")
		}
		for _, binding := range declaration.TriggerBindings {
			if !validConnectionName(binding.TriggerName) || !validConnectionName(binding.BindingName) {
				return projectManifestRecord{}, errors.New("an AppManifest trigger binding name is invalid")
			}
		}
		manifest.Connectors = append(manifest.Connectors, declaration)
	}
	manifest.Application.Environment, err = connectorconfig.ValidateAndSortEnvironmentDeclarations(manifest.Application.Environment)
	if err != nil {
		return projectManifestRecord{}, err
	}
	return projectManifestRecord{
		SchemaVersion: projectManifestSchema, SourceCommit: registration.SourceCommit, ManifestDigest: registration.ManifestDigest,
		ConnectorContractDigest: snapshot.GetConnectorContractDigest(), EnvironmentContractDigest: snapshot.GetEnvironmentContractDigest(),
		Manifest: manifest,
	}, nil
}

// validConnectionName admits a connection, trigger or binding name Studio can
// address in a route segment.
func validConnectionName(name string) bool {
	return name != "" && len(name) <= 128 && strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
}

// handleGetConfiguration returns the scope's configuration metadata.
func (host *ConfigurationHost) handleGetConfiguration(response http.ResponseWriter, request *http.Request) {
	admission, admitted := requireReader(response, request)
	if !admitted {
		return
	}
	manifest, err := host.project.readManifest(request.Context(), admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	document, err := host.project.readConfiguration(request.Context(), admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, host.project.summary(manifest, document.Revision))
}

// handlePutManifest registers the scope's current AppManifest; the platform
// calls it when Setup or a Session checks out a new source.
func (host *ConfigurationHost) handlePutManifest(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	if admission.SourceCommit != "" {
		writeCodedError(response, http.StatusConflict, "APP_MANIFEST_IMMUTABLE", "Prepared source manifests are immutable")
		return
	}
	var registration ManifestRegistration
	if err := decodeBody(response, request, 1<<20, &registration); err != nil {
		writeCodedError(response, http.StatusBadRequest, "APP_MANIFEST_INVALID", "AppManifest request is invalid: "+err.Error())
		return
	}
	manifest, err := parseProjectManifest(registration)
	if err != nil {
		writeCodedError(response, http.StatusBadRequest, "APP_MANIFEST_INVALID", err.Error())
		return
	}
	current, head, err := host.project.readManifestHead(request.Context())
	firstRegistration := errors.Is(err, connectorconfig.ErrObjectNotFound) && registration.ExpectedRevision == 0
	if err != nil && !firstRegistration {
		writeStoreError(response, err)
		return
	}
	if current.Revision != registration.ExpectedRevision {
		writeStoreError(response, connectorconfig.ErrConflict)
		return
	}
	manifest.Revision, manifest.Scope = current.Revision+1, host.project.scope
	if _, err = host.project.writeObject(request.Context(), host.project.manifestKey(), head.ETag, comm.Must(json.Marshal(manifest))); err != nil {
		writeStoreError(response, err)
		return
	}
	document, err := host.project.readConfiguration(request.Context(), admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, host.project.summary(manifest, document.Revision))
}
