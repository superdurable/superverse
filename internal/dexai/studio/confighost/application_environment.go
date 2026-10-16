// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"net/http"
	"slices"
	"sort"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
)

// environmentView is the application environment form: each declared variable
// with its ordinary value or only whether its secret is set, and the stored
// variables the manifest no longer declares.
type environmentView struct {
	AppManifestRevision   uint64                 `json:"appManifestRevision"`
	ConfigurationRevision uint64                 `json:"configurationRevision"`
	Fields                []environmentFieldView `json:"fields"`
	Obsolete              []string               `json:"obsolete"`
}

type environmentFieldView struct {
	connectorconfig.EnvironmentDeclaration
	Value      *string `json:"value,omitempty"`
	Configured bool    `json:"configured"`
}

func newEnvironmentView(manifest projectManifestRecord, document connectorconfig.Configuration) environmentView {
	view := environmentView{
		AppManifestRevision: manifest.Revision, ConfigurationRevision: document.Revision,
		Fields: make([]environmentFieldView, 0, len(manifest.Manifest.Application.Environment)), Obsolete: []string{},
	}
	declared := map[string]bool{}
	for _, declaration := range manifest.Manifest.Application.Environment {
		declared[declaration.Name] = true
		entry, present := document.Environment[declaration.Name]
		field := environmentFieldView{EnvironmentDeclaration: declaration, Configured: present && (entry.SecretRef != nil) == declaration.Secret}
		if !declaration.Secret && entry.SecretRef == nil {
			field.Value = entry.Value
		}
		view.Fields = append(view.Fields, field)
	}
	for name := range document.Environment {
		if !declared[name] {
			view.Obsolete = append(view.Obsolete, name)
		}
	}
	sort.Strings(view.Obsolete)
	return view
}

func (host *ConfigurationHost) handleGetEnvironment(response http.ResponseWriter, request *http.Request) {
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
	if err == nil && admission.nextPublish != nil {
		// The layer shows the next publish as it resolves, so a variable that follows Preview shows its value.
		document, err = host.nextPublishView(request.Context(), admission)
	}
	if err != nil {
		writeNextPublishError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newEnvironmentView(manifest, document))
}

// environmentChange sets ordinary values, sets secrets and removes variables,
// and on Live's next-publish layer drops Live's own value of FollowPreview's
// variables so they follow Preview; each variable appears at most once. On
// the layer a removed variable follows Preview too.
type environmentChange struct {
	Values        map[string]string `json:"values"`
	Secrets       map[string]string `json:"secrets"`
	Remove        []string          `json:"remove"`
	FollowPreview []string          `json:"followPreview"`
}

// handlePutEnvironment applies an actor's environment change to the draft the
// form was read from. Secrets become new immutable application-secret objects.
func (host *ConfigurationHost) handlePutEnvironment(response http.ResponseWriter, request *http.Request) {
	admission, ok := requireActor(response, request)
	if !ok {
		return
	}
	ctx := request.Context()
	manifest, err := host.project.readManifest(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	document, err := host.project.readConfiguration(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if manifest.Revision != admission.AppManifestRevision || document.Revision != admission.ConfigurationRevision {
		writeStoreError(response, connectorconfig.ErrConflict)
		return
	}
	var change environmentChange
	if decodeBody(response, request, 1<<20, &change) != nil {
		writeCodedError(response, http.StatusBadRequest, "APPLICATION_ENVIRONMENT_INVALID", "Application environment request is invalid")
		return
	}
	if message := validateEnvironmentChange(manifest, document, change); message != "" {
		writeCodedError(response, http.StatusBadRequest, "APPLICATION_ENVIRONMENT_INVALID", message)
		return
	}
	for _, name := range change.FollowPreview {
		if host.refuseFollow(response, request, admission, configitems.Key{Kind: configitems.KindEnvironment, EnvironmentName: name}) {
			return
		}
	}
	if document.Environment == nil {
		document.Environment = map[string]connectorconfig.EnvironmentValue{}
	}
	for name, value := range change.Values {
		document.Environment[name] = connectorconfig.EnvironmentValue{Value: &value}
	}
	for name, value := range change.Secrets {
		reference, writeErr := admission.drafts.CreateApplicationSecret(ctx, name, value)
		if writeErr != nil {
			writeStoreError(response, writeErr)
			return
		}
		document.Environment[name] = connectorconfig.EnvironmentValue{SecretRef: &reference}
	}
	for _, name := range slices.Concat(change.Remove, change.FollowPreview) {
		delete(document.Environment, name)
	}
	written, err := host.project.writeConfiguration(ctx, admission, document)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newEnvironmentView(manifest, written))
}

// validateEnvironmentChange returns why change is invalid, or "". A removal may
// name a variable a newer manifest no longer declares.
func validateEnvironmentChange(manifest projectManifestRecord, document connectorconfig.Configuration, change environmentChange) string {
	declarations := map[string]connectorconfig.EnvironmentDeclaration{}
	for _, field := range manifest.Manifest.Application.Environment {
		declarations[field.Name] = field
	}
	touched := map[string]bool{}
	for name, value := range change.Values {
		field, declared := declarations[name]
		if !declared || field.Secret || connectorconfig.ValidateEnvironmentValue(field, value) != nil {
			return "An ordinary environment value is invalid"
		}
		touched[name] = true
	}
	for name, value := range change.Secrets {
		field, declared := declarations[name]
		if !declared || !field.Secret || touched[name] || connectorconfig.ValidateEnvironmentValue(field, value) != nil {
			return "A private environment value is invalid"
		}
		touched[name] = true
	}
	for _, name := range change.Remove {
		_, declared := declarations[name]
		_, configured := document.Environment[name]
		if (!declared && !configured) || touched[name] {
			return "Environment removal is invalid"
		}
		touched[name] = true
	}
	for _, name := range change.FollowPreview {
		if _, declared := declarations[name]; !declared || touched[name] {
			return "A variable that follows Preview is invalid"
		}
		touched[name] = true
	}
	if len(touched) == 0 {
		return "No environment changes were supplied"
	}
	return ""
}
