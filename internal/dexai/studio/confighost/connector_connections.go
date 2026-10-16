// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// connectorDisplayNameTimeout bounds how long a connection list waits for
// uncached release metadata.
const connectorDisplayNameTimeout = 5 * time.Second

// connectionList is the Connectors page: every connection the AppManifest
// declares, with its stored configuration, credential state and uses.
type connectionList struct {
	ConfigurationRevision string           `json:"configurationRevision,omitempty"`
	ConfigurationState    string           `json:"configurationState,omitempty"`
	AppManifestRevision   uint64           `json:"appManifestRevision,omitempty"`
	DefinitionRevision    string           `json:"definitionRevision"`
	Connections           []connectionView `json:"connections"`
}

// connectionView is one declared connection name. Its module, release and
// method are the AppManifest declaration's; the TeamSpace connection it
// selects serves every release of its major version.
type connectionView struct {
	ConnectorID        string                     `json:"connectorId"`
	DisplayName        string                     `json:"displayName,omitempty"`
	AuthMethodID       string                     `json:"authMethodId,omitempty"`
	ConnectionName     string                     `json:"connectionName"`
	ModulePath         string                     `json:"modulePath,omitempty"`
	ModuleVersion      string                     `json:"moduleVersion,omitempty"`
	VersionLine        string                     `json:"versionLine"`
	Provider           string                     `json:"provider,omitempty"`
	Status             string                     `json:"status"`
	Configuration      map[string]json.RawMessage `json:"configuration,omitempty"`
	SharedConnectionID string                     `json:"sharedConnectionId,omitempty"`
	SelectedConnection *selectedConnection        `json:"selectedConnection,omitempty"`
	Uses               []connectionStepUse        `json:"uses"`
	TriggerUses        []connectionTriggerUse     `json:"triggerUses,omitempty"`
	// module is the declared module, whose release names the connection.
	module connectormanifest.Module
}

// The statuses of a connection name: no selection; a selection of another
// connector, major version or method, or of a deleted connection; a selected
// connection that needs authorization; and a usable one.
const (
	viewMissing         = "Missing"
	viewConflict        = "Conflict"
	viewReauthorization = "Reauthorization required"
	viewReady           = "Ready"
)

// connectionStepUse is a connector factory Step that uses the connection, with
// its use configuration. Configured is a saved configuration with a value for
// every required unit.
type connectionStepUse struct {
	FlowName        string                         `json:"flowName"`
	StepID          string                         `json:"stepId"`
	StepName        string                         `json:"stepName"`
	OperationID     string                         `json:"operationId"`
	OperationKind   string                         `json:"operationKind"`
	ConfigurationUI fdg.V2ConnectorConfigurationUI `json:"configurationUI"`
	Configuration   map[string]json.RawMessage     `json:"configuration"`
	Configured      bool                           `json:"configured"`
}

// savedConfiguration is the draft's configuration of the use among
// configurations, or an empty one when the use has none saved.
func (use connectionStepUse) savedConfiguration(configurations []connectorUseConfiguration) (map[string]json.RawMessage, bool) {
	for _, configuration := range configurations {
		if configuration.OperationID == use.OperationID && configuration.FlowType == use.FlowName && configuration.StepType == use.StepName {
			return configuration.Configuration, true
		}
	}
	return map[string]json.RawMessage{}, false
}

// connectionTriggerUse is a trigger binding the AppManifest declares for the
// connection, with the Connector UI units its Flow composes for it, when a
// Flow of the prepared source declares the binding. Configured is a saved
// binding with a value for every required unit.
type connectionTriggerUse struct {
	ConfigurationUI *fdg.V2ConnectorConfigurationUI `json:"configurationUI,omitempty"`
	TriggerName     string                          `json:"triggerName"`
	BindingName     string                          `json:"bindingName"`
	Configuration   map[string]json.RawMessage      `json:"configuration"`
	Configured      bool                            `json:"configured"`
}

func (host *ConfigurationHost) handleListConnections(response http.ResponseWriter, request *http.Request) {
	admission, admitted := requireReader(response, request)
	if !admitted {
		return
	}
	ctx := request.Context()
	manifest, err := host.project.readManifest(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err == nil && admission.nextPublish != nil {
		// The layer shows the next publish as it resolves, so an item that follows Preview shows what it follows.
		draft.document, err = host.nextPublishView(ctx, admission)
	}
	if err != nil {
		writeNextPublishError(response, err)
		return
	}
	bundle, err := host.project.preparedBundle(ctx, admission)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	stepUses := connectorStepUses(bundle)
	views := make([]connectionView, 0, len(manifest.Manifest.Connectors))
	for _, declaration := range manifest.Manifest.Connectors {
		view, viewErr := host.connectionView(ctx, draft, declaration, stepUses[declaration.module()], bundle)
		if viewErr != nil {
			writeStoreError(response, viewErr)
			return
		}
		views = append(views, view)
	}
	host.applyDisplayNames(ctx, views)
	writeJSON(response, http.StatusOK, connectionList{
		ConfigurationRevision: strconv.FormatUint(draft.document.Revision, 10), ConfigurationState: "Draft",
		AppManifestRevision: manifest.Revision, DefinitionRevision: bundle.Revision, Connections: views,
	})
}

// connectionView describes one declared connection name: Missing until it
// selects a connection, Conflict when its entry or the selected connection
// differs from the declaration's module, major version or method or the
// connection was deleted, and otherwise whether the connection's credential is
// ready. A different release of the major version never conflicts.
func (host *ConfigurationHost) connectionView(
	ctx context.Context, draft *connectionDraft, declaration projectConnectorDeclaration, uses []connectionStepUse, bundle *fdg.Bundle,
) (connectionView, error) {
	module := declaration.module()
	// The manifest owns the module, release and method selection, including the
	// empty single-method ID; a stored module or method that differs is a
	// Conflict until the actor saves the connection again.
	view := connectionView{
		ConnectorID: module.ConnectorID, ConnectionName: module.ConnectionName, ModulePath: module.ModulePath,
		ModuleVersion: module.ModuleVersion, AuthMethodID: declaration.AuthMethodID, Status: viewMissing, Uses: []connectionStepUse{}, module: module,
	}
	configurations, err := draft.useConfigurations(module.ConnectorID, module.ConnectionName)
	if err != nil {
		return view, err
	}
	for _, use := range uses {
		var saved bool
		use.Configuration, saved = use.savedConfiguration(configurations)
		use.Configured = saved && len(connectormanifest.MissingRequiredUnits(use.Configuration, use.ConfigurationUI)) == 0
		view.Uses = append(view.Uses, use)
	}
	view.VersionLine = connectormanifest.VersionLine(module.ModuleVersion)
	entry, found := draft.entry(module.Key())
	if found {
		if err = json.Unmarshal(entry.Configuration, &view.Configuration); err != nil {
			return view, err
		}
		view.Provider, view.SharedConnectionID = entry.Provider, entry.SharedConnectionID
	}
	selected, err := host.sharedSelection(ctx, entry)
	if err != nil {
		return view, err
	}
	switch {
	case selected == nil:
	case entry.ModulePath != module.ModulePath || entry.AuthMethodID != declaration.AuthMethodID || selected.ConnectionStatus != connectiondao.ConnectionActive ||
		selected.VersionLine != view.VersionLine || selected.AuthMethodID != declaration.AuthMethodID:
		view.Status = viewConflict
	case selected.CredentialStatus != connectiondao.CredentialReady:
		view.Status = viewReauthorization
	default:
		view.Status = viewReady
	}
	if selected != nil {
		view.SelectedConnection = &selectedConnection{
			Name: selected.Name, ConnectionStatus: string(selected.ConnectionStatus), CredentialStatus: string(selected.CredentialStatus),
			ClientSource: string(selected.ClientSource), ManifestVersion: selected.ManifestVersion,
		}
	}
	if len(declaration.TriggerBindings) == 0 {
		return view, nil
	}
	bindings, err := draft.triggerBindings(module.ConnectorID, module.ConnectionName)
	if err != nil {
		return view, err
	}
	for _, declared := range declaration.TriggerBindings {
		use := connectionTriggerUse{TriggerName: declared.TriggerName, BindingName: declared.BindingName, Configuration: map[string]json.RawMessage{}}
		if configurationUI, found := connectorTriggerUI(bundle, module, declared); found {
			use.ConfigurationUI = &configurationUI
		}
		for _, binding := range bindings {
			if binding.triggerName == declared.TriggerName && binding.bindingName == declared.BindingName {
				use.Configuration = binding.configuration
				use.Configured = use.ConfigurationUI != nil && len(connectormanifest.MissingRequiredUnits(use.Configuration, *use.ConfigurationUI)) == 0
			}
		}
		view.TriggerUses = append(view.TriggerUses, use)
	}
	return view, nil
}

// applyDisplayNames resolves every view's release concurrently within one
// timeout. A display name is presentational: without it Studio shows the
// Connector ID, and opening the connection reports the release error.
func (host *ConfigurationHost) applyDisplayNames(ctx context.Context, views []connectionView) {
	ctx, cancel := context.WithTimeout(ctx, connectorDisplayNameTimeout)
	defer cancel()
	var waitGroup sync.WaitGroup
	for index := range views {
		waitGroup.Go(func() {
			view := &views[index]
			if release, err := host.connectorReleases.Resolve(ctx, view.module); err == nil {
				view.DisplayName = strings.TrimSpace(release.Manifest.Metadata.DisplayName)
			}
		})
	}
	waitGroup.Wait()
}

// connectorStepUses groups the bundle's connector factory Steps by the
// connection module they use, in Flow and Step order.
func connectorStepUses(bundle *fdg.Bundle) map[connectormanifest.Module][]connectionStepUse {
	uses := make(map[connectormanifest.Module][]connectionStepUse)
	for _, step := range bundle.ConnectorSteps {
		connector := step.Connector
		configurationUI := connector.ConfigurationUI
		if configurationUI.Units == nil {
			configurationUI.Units = []fdg.V2ConnectorUIUnit{}
		}
		module := stepModule(connector)
		uses[module] = append(uses[module], connectionStepUse{
			FlowName: step.FlowType, StepID: step.StepID, StepName: step.StepName,
			OperationID: connector.OperationID, OperationKind: connector.OperationKind, ConfigurationUI: configurationUI,
		})
	}
	for _, list := range uses {
		sort.Slice(list, func(left, right int) bool {
			if list[left].FlowName == list[right].FlowName {
				return list[left].StepID < list[right].StepID
			}
			return list[left].FlowName < list[right].FlowName
		})
	}
	return uses
}

// connectorOperationStep finds the connector factory Step that uses module for
// operationID in flowType's stepType.
func connectorOperationStep(bundle *fdg.Bundle, module connectormanifest.Module, operationID, flowType, stepType string) (fdg.ConnectorStepMetadata, bool) {
	for _, step := range bundle.ConnectorSteps {
		if step.FlowType == flowType && step.StepName == stepType && step.Connector.OperationID == operationID && stepModule(step.Connector) == module {
			return step.Connector, true
		}
	}
	return fdg.ConnectorStepMetadata{}, false
}

// connectorTriggerUI returns the Connector UI units that a Flow of the bundle
// composes for a trigger binding of module, as Dex Web reads them.
func connectorTriggerUI(bundle *fdg.Bundle, module connectormanifest.Module, binding projectTriggerBinding) (fdg.V2ConnectorConfigurationUI, bool) {
	declared, found := bundle.ConnectorTriggerBinding(module.ConnectorID, module.ConnectionName, binding.TriggerName, binding.BindingName)
	if !found || declared.ModulePath != module.ModulePath || declared.ModuleVersion != module.ModuleVersion {
		return fdg.V2ConnectorConfigurationUI{}, false
	}
	if declared.ConfigurationUI.Units == nil {
		declared.ConfigurationUI.Units = []fdg.V2ConnectorUIUnit{}
	}
	return declared.ConfigurationUI, true
}

// stepModule is the Connector release module a connector factory Step uses.
func stepModule(connector fdg.ConnectorStepMetadata) connectormanifest.Module {
	return connectormanifest.Module{ConnectorID: connector.ConnectorID, ConnectionName: connector.ConnectionName, ModulePath: connector.ModulePath, ModuleVersion: connector.ModuleVersion}
}

// handlePutTriggerBinding saves the configuration of a trigger binding the
// AppManifest declares, through the Connector UI units the prepared source's
// Flow composes for the binding.
func (host *ConfigurationHost) handlePutTriggerBinding(response http.ResponseWriter, request *http.Request) {
	key := pathConnection(request)
	admission, declaration, admitted := host.admitConnection(response, request, key)
	if !admitted {
		return
	}
	ctx := request.Context()
	binding := projectTriggerBinding{TriggerName: request.PathValue("triggerName"), BindingName: request.PathValue("bindingName")}
	if !slices.Contains(declaration.TriggerBindings, binding) {
		writeCodedError(response, http.StatusNotFound, "CONNECTOR_TRIGGER_BINDING_UNSUPPORTED", "Trigger binding is not declared by the AppManifest")
		return
	}
	var body struct {
		Configuration map[string]json.RawMessage `json:"configuration"`
		FollowPreview bool                       `json:"followPreview"`
	}
	if decodeBody(response, request, 1<<20, &body) != nil || (body.Configuration == nil) != body.FollowPreview {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_REQUEST_INVALID", "Trigger configuration request is invalid")
		return
	}
	if body.FollowPreview {
		key := configitems.Key{Kind: configitems.KindTriggerBinding, ConnectorID: key.ConnectorID, ConnectionName: key.ConnectionName, TriggerName: binding.TriggerName, BindingName: binding.BindingName}
		host.writeFollowed(response, request, admission, key, map[string]any{"triggerName": binding.TriggerName, "bindingName": binding.BindingName})
		return
	}
	release, err := host.connectorReleases.Resolve(ctx, declaration.module())
	if err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	if _, published := release.Manifest.Trigger(binding.TriggerName); !published {
		writeCodedError(response, http.StatusConflict, "CONNECTOR_TRIGGER_SCHEMA_UNAVAILABLE", "The pinned connector release does not publish this trigger")
		return
	}
	bundle, err := host.project.preparedBundle(ctx, admission)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	configurationUI, found := connectorTriggerUI(bundle, declaration.module(), binding)
	if !found {
		writeCodedError(response, http.StatusConflict, "CONNECTOR_TRIGGER_SCHEMA_UNAVAILABLE", "No Flow of the prepared source declares a configurable binding of this trigger for the pinned connector release")
		return
	}
	if err = connectormanifest.ValidateUseConfiguration(body.Configuration, configurationUI); err != nil {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_TRIGGER_CONFIGURATION_INVALID", err.Error())
		return
	}
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if err = host.anchorConnection(ctx, admission, draft, declaration); err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	if err = draft.putTriggerBinding(ctx, admission, connectorTriggerBinding{
		connectorID: key.ConnectorID, connectionName: key.ConnectionName, triggerName: binding.TriggerName,
		bindingName: binding.BindingName, configuration: body.Configuration,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"configurationRevision": strconv.FormatUint(draft.document.Revision, 10), "triggerName": binding.TriggerName, "bindingName": binding.BindingName,
	})
}

// handlePutUseConfiguration saves how one connector factory Step of the
// prepared source's Flows uses its connection, through the Connector UI units
// the Flow composes for it.
func (host *ConfigurationHost) handlePutUseConfiguration(response http.ResponseWriter, request *http.Request) {
	key := pathConnection(request)
	admission, declaration, admitted := host.admitConnection(response, request, key)
	if !admitted {
		return
	}
	ctx := request.Context()
	bundle, err := host.project.preparedBundle(ctx, admission)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	configuration := connectorUseConfiguration{
		ConnectorID: key.ConnectorID, ConnectionName: key.ConnectionName, OperationID: request.PathValue("operationId"),
		FlowType: request.PathValue("flowType"), StepType: request.PathValue("stepType"),
	}
	step, found := connectorOperationStep(bundle, declaration.module(), configuration.OperationID, configuration.FlowType, configuration.StepType)
	if !found || len(step.ConfigurationUI.Units) == 0 {
		writeCodedError(response, http.StatusNotFound, "CONNECTOR_USE_CONFIGURATION_UNSUPPORTED", "Connector operation use is not configurable")
		return
	}
	var body struct {
		Configuration map[string]json.RawMessage `json:"configuration"`
		FollowPreview bool                       `json:"followPreview"`
	}
	if decodeBody(response, request, 1<<20, &body) != nil || (body.Configuration == nil) != body.FollowPreview {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_REQUEST_INVALID", "Connector use configuration request is invalid")
		return
	}
	if body.FollowPreview {
		key := configitems.Key{
			Kind: configitems.KindStepSetting, ConnectorID: key.ConnectorID, ConnectionName: key.ConnectionName,
			OperationID: configuration.OperationID, FlowType: configuration.FlowType, StepType: configuration.StepType,
		}
		host.writeFollowed(response, request, admission, key, map[string]any{
			"connectorId": key.ConnectorID, "connectionName": key.ConnectionName, "operationId": key.OperationID, "flowType": key.FlowType, "stepType": key.StepType,
		})
		return
	}
	if err = connectormanifest.ValidateUseConfiguration(body.Configuration, step.ConfigurationUI); err != nil {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_USE_CONFIGURATION_INVALID", err.Error())
		return
	}
	configuration.Configuration = body.Configuration
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if err = host.anchorConnection(ctx, admission, draft, declaration); err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	if err = draft.putUseConfiguration(ctx, admission, configuration); err != nil {
		writeStoreError(response, err)
		return
	}
	// The revision chains the next autosaved write of the same form.
	writeJSON(response, http.StatusOK, struct {
		connectorUseConfiguration
		ConfigurationRevision string `json:"configurationRevision"`
	}{configuration, strconv.FormatUint(draft.document.Revision, 10)})
}
