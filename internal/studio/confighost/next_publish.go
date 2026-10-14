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
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// Live's settings for the next publish live in one sparse configuration
// document of the Live scope, the next-publish layer, which holds only what
// Live chose for itself. An item it does not hold follows Preview. A version's
// frozen copy (release-drafts/<commit>/configuration/head) is built from it
// when the version is published, and only a build freezes that copy.

// itemCannotFollowCode refuses following Preview for an item that can never
// follow, or outside Live's next-publish layer.
const itemCannotFollowCode = "CONFIGURATION_ITEM_CANNOT_FOLLOW"

// ItemCannotFollowCodeForTest is itemCannotFollowCode for the tests of other packages.
const ItemCannotFollowCodeForTest = itemCannotFollowCode

// NextPublish addresses Live's next-publish layer. The platform reads what
// the layer resolves with outside the Live host's prefix and hands it over:
// current Live, the version the Preview applied without its secrets and
// credential broker (nil before the first Apply), and, when the request names
// Preview's prepared source rather than one of the Live scope, that source.
type NextPublish struct {
	Live    LiveConfiguration
	Preview *connectorconfig.Configuration
	Source  *PreparedSource
}

// previewRevision is the configuration revision of the version the Preview
// applied, zero before the first Apply.
func (next *NextPublish) previewRevision() uint64 {
	if next.Preview == nil {
		return 0
	}
	return next.Preview.Revision
}

// PreparedSource is a prepared source one host read for another host of the
// same Project, which never reads outside its own prefix: its declarations
// (AppManifest and Flow Definitions) only.
type PreparedSource struct{ source preparedSource }

// PreparedSource reads the scope's prepared source of commit.
func (host *ConfigurationHost) PreparedSource(ctx context.Context, commit string) (*PreparedSource, error) {
	source, err := host.project.readPreparedSource(ctx, &configurationRequest{ConfigurationAdmission: ConfigurationAdmission{Internal: true, SourceCommit: commit}})
	if err != nil {
		return nil, err
	}
	return &PreparedSource{source: source}, nil
}

// LiveConfiguration names the deployment serving Live and the frozen
// configuration it pins; the zero value is a Project with nothing on Live.
type LiveConfiguration struct {
	DeploymentID string                       `json:"deploymentId,omitempty"`
	Snapshot     *connectorconfig.SnapshotRef `json:"snapshot,omitempty"`
}

// BindingUse is a trigger binding's non-secret configuration with the
// TeamSpace connection its connection name selects.
type BindingUse struct {
	Key                configitems.Key `json:"key"`
	SharedConnectionID string          `json:"sharedConnectionId"`
	Configuration      json.RawMessage `json:"configuration"`
}

// NextPublishRequest asks how Live's next publish resolves for the request's
// prepared source, whose AppManifest ManifestDigest names.
type NextPublishRequest struct {
	ManifestDigest string `json:"manifestDigest"`
}

// NextPublishReport is the next publish of the request's source: the layer
// revision and Preview's applied revision it resolves from, every item, the
// requirement gaps of the resolved configuration, the frozen copy when the
// source is a prepared source of the Live scope, and the bindings the items
// show for the platform's checks across Projects.
type NextPublishReport struct {
	LayerRevision   uint64                       `json:"layerRevision"`
	PreviewRevision uint64                       `json:"previewRevision"`
	Items           []configitems.Item           `json:"items"`
	Gaps            []appmanifest.RequirementGap `json:"gaps"`
	Copy            *CopyState                   `json:"copy,omitempty"`
	Bindings        []BindingUse                 `json:"bindings,omitempty"`
}

// CopyState is a version's frozen copy: its revision, zero before the first,
// and whether Live, Preview's applied version or the layer changed since it
// was built.
type CopyState struct {
	Revision uint64 `json:"revision"`
	Stale    bool   `json:"stale"`
}

// CopyRequest builds the request commit's frozen copy from the layer at
// ExpectedLayerRevision (zero while it is absent) and Preview's applied
// version at ExpectedPreviewRevision.
type CopyRequest struct {
	ManifestDigest          string `json:"manifestDigest"`
	ExpectedLayerRevision   uint64 `json:"expectedLayerRevision"`
	ExpectedPreviewRevision uint64 `json:"expectedPreviewRevision"`
}

// CopyReport is the copy written, with the layer revision it was built from.
type CopyReport struct {
	ConfigurationRevision uint64 `json:"configurationRevision"`
	LayerRevision         uint64 `json:"layerRevision"`
}

// copyDerivation records what a version's copy was built from, metadata
// only: current Live's deployment, the layer revision and Preview's applied
// revision, with the copy revision it wrote.
type copyDerivation struct {
	LiveDeploymentID string    `json:"liveDeploymentId,omitempty"`
	LayerRevision    uint64    `json:"layerRevision"`
	PreviewRevision  uint64    `json:"previewRevision"`
	CopyRevision     uint64    `json:"copyRevision"`
	DerivedAt        time.Time `json:"derivedAt"`
}

// nextPublishLayer is the store of the Live scope's next-publish layer.
func (project *projectConfiguration) nextPublishLayer() *connectorconfig.ConfigurationStore {
	return project.redirectedHead(project.prefix + "/next-publish/configuration/head")
}

func (project *projectConfiguration) derivationKey(commit string) string {
	return project.prefix + "/release-drafts/" + commit + "/derivation"
}

// currentLive reads the version current Live pins, empty without one.
func (project *projectConfiguration) currentLive(ctx context.Context, live LiveConfiguration) (connectorconfig.Configuration, error) {
	if live.Snapshot == nil {
		return connectorconfig.Configuration{}, nil
	}
	return project.canonical.ReadSnapshot(ctx, *live.Snapshot)
}

// seedLayer is the layer of a Project without one, as its first write or
// publish stores it.
func (project *projectConfiguration) seedLayer(ctx context.Context, next *NextPublish) (connectorconfig.Configuration, error) {
	current, err := project.currentLive(ctx, next.Live)
	if err != nil {
		return connectorconfig.Configuration{}, err
	}
	seed := configitems.Seed(current, next.Preview, endpointBound)
	seed.SchemaVersion, seed.Scope = connectorconfig.ConfigurationSchemaVersion, project.scope
	return seed, nil
}

func endpointBound(connectorID string) bool {
	_, bound := connectormanifest.EndpointBoundReason(connectorID)
	return bound
}

// declaredItems lists the items a prepared source declares, in declaration
// order: per connection its selection, its trigger bindings and the settings
// of the connector Steps that compose Connector UI units, then the
// environment; each with why it can never follow Preview.
func declaredItems(manifest projectAppManifest, bundle *fdg.Bundle) []configitems.Declared {
	var items []configitems.Declared
	for _, declaration := range manifest.Connectors {
		connection := configitems.Declared{Key: configitems.ConnectionKey(declaration.ConnectorID, declaration.ConnectionName)}
		if endpointBound(declaration.ConnectorID) {
			connection.Refusal = appmanifest.FollowRefusedEndpointBound
		}
		items = append(items, connection)
		for _, binding := range declaration.TriggerBindings {
			items = append(items, configitems.Declared{Key: configitems.Key{
				Kind: configitems.KindTriggerBinding, ConnectorID: declaration.ConnectorID, ConnectionName: declaration.ConnectionName,
				TriggerName: binding.TriggerName, BindingName: binding.BindingName,
			}})
		}
		for _, step := range bundle.ConnectorSteps {
			if stepModule(step.Connector) == declaration.module() && len(step.Connector.ConfigurationUI.Units) > 0 {
				items = append(items, configitems.Declared{Key: configitems.Key{
					Kind: configitems.KindStepSetting, ConnectorID: declaration.ConnectorID, ConnectionName: declaration.ConnectionName,
					OperationID: step.Connector.OperationID, FlowType: step.FlowType, StepType: step.StepName,
				}})
			}
		}
	}
	for _, declaration := range manifest.Application.Environment {
		variable := configitems.Declared{Key: configitems.Key{Kind: configitems.KindEnvironment, EnvironmentName: declaration.Name}}
		if declaration.Secret {
			variable.Refusal = appmanifest.FollowRefusedSecret
		}
		items = append(items, variable)
	}
	return items
}

// followRefusal is why the request's source says key can never follow
// Preview, the zero refusal when it can.
func followRefusal(manifest projectAppManifest, bundle *fdg.Bundle, key configitems.Key) appmanifest.FollowRefusal {
	for _, declared := range declaredItems(manifest, bundle) {
		if declared.Key == key {
			return declared.Refusal
		}
	}
	return appmanifest.FollowRefusal{}
}

// nextPublish is Live's next publish for the request's prepared source.
type nextPublish struct {
	manifest projectManifestRecord
	bundle   *fdg.Bundle
	// layer is the stored layer, or at revision zero the seed it starts from.
	layer    connectorconfig.Configuration
	resolved connectorconfig.Configuration
	items    []configitems.Item
}

// resolveNextPublish resolves the next publish of the request's prepared
// source, which must have manifestDigest.
func (host *ConfigurationHost) resolveNextPublish(ctx context.Context, admission *configurationRequest, manifestDigest string) (*nextPublish, error) {
	if admission.nextPublish == nil || admission.SourceCommit == "" {
		return nil, &notReadyError{http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "A next-publish request names Live's layer and the prepared source it is checked against"}
	}
	source, err := host.project.readPreparedSource(ctx, admission)
	if err != nil {
		return nil, err
	}
	if source.Manifest.ManifestDigest != manifestDigest {
		return nil, connectorconfig.ErrConflict
	}
	bundle, err := fdg.ParseBundle(source.Definitions)
	if err != nil {
		return nil, &notReadyError{http.StatusServiceUnavailable, "FLOW_DEFINITION_INVALID", "Flow Definition source is invalid: " + err.Error()}
	}
	result := &nextPublish{manifest: source.Manifest, bundle: bundle}
	if result.layer, err = host.project.readConfiguration(ctx, admission); err != nil {
		return nil, err
	}
	current, err := host.project.currentLive(ctx, admission.nextPublish.Live)
	if err != nil {
		return nil, err
	}
	result.resolved, result.items = configitems.Resolve(result.layer, current, admission.nextPublish.Preview, declaredItems(source.Manifest.Manifest, bundle))
	result.resolved.SchemaVersion, result.resolved.Scope, result.resolved.Revision = result.layer.SchemaVersion, result.layer.Scope, result.layer.Revision
	return result, nil
}

// handleNextPublish reports how Live's next publish resolves for the
// request's prepared source. It is read-only.
func (host *ConfigurationHost) handleNextPublish(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	ctx := request.Context()
	var input NextPublishRequest
	if decodeBody(response, request, 1<<16, &input) != nil {
		writeCodedError(response, http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Next publish request is invalid")
		return
	}
	next, err := host.resolveNextPublish(ctx, admission, input.ManifestDigest)
	if err != nil {
		writeNextPublishError(response, err)
		return
	}
	report := NextPublishReport{LayerRevision: next.layer.Revision, PreviewRevision: admission.nextPublish.previewRevision(), Items: next.items}
	refreshed, deleted, err := host.refreshSelections(ctx, next.resolved, next.manifest)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	for index, item := range report.Items {
		if item.Kind == configitems.KindConnection && item.State != configitems.NoLongerUsed && slices.Contains(deleted, item.Key) {
			report.Items[index].Warnings = append(report.Items[index].Warnings, configitems.ConnectionDeleted)
		}
	}
	draft := &connectionDraft{project: host.project, store: admission.drafts, document: refreshed}
	if report.Gaps, err = host.requirementGaps(ctx, draft, next.manifest, next.bundle); err != nil {
		writeNextPublishError(response, err)
		return
	}
	markRefusedGaps(report.Gaps, report.Items, next.layer)
	if admission.nextPublish.Source == nil {
		if report.Copy, err = host.copyState(ctx, admission, next.layer.Revision); err != nil {
			writeStoreError(response, err)
			return
		}
	}
	report.Bindings = bindingUses(next.resolved)
	writeJSON(response, http.StatusOK, report)
}

// markRefusedGaps gives each gap of an item that cannot follow Preview and
// that the layer holds no value of the item's follow refusal.
func markRefusedGaps(gaps []appmanifest.RequirementGap, items []configitems.Item, layer connectorconfig.Configuration) {
	for index, gap := range gaps {
		var key configitems.Key
		switch gap.Kind {
		case appmanifest.GapEnvironmentRequired:
			key = configitems.Key{Kind: configitems.KindEnvironment, EnvironmentName: gap.EnvironmentName}
		case appmanifest.GapConnectionRequired:
			key = configitems.ConnectionKey(gap.ConnectorID, gap.ConnectionName)
		default:
			continue
		}
		item := slices.IndexFunc(items, func(item configitems.Item) bool { return item.Key == key })
		if item >= 0 && !items[item].FollowRefusal.IsZero() && !configitems.Holds(layer, key) {
			gaps[index].FollowRefusal = items[item].FollowRefusal
		}
	}
}

// copyState reads the request commit's frozen copy and whether it is stale
// against current Live, Preview's applied version and the layer at
// layerRevision.
func (host *ConfigurationHost) copyState(ctx context.Context, admission *configurationRequest, layerRevision uint64) (*CopyState, error) {
	copied, err := host.project.readConfiguration(ctx, &configurationRequest{drafts: host.project.commitDraft(admission.SourceCommit)})
	if err != nil {
		return nil, err
	}
	derivation, _, err := host.project.readDerivation(ctx, admission.SourceCommit)
	if err != nil {
		return nil, err
	}
	next := admission.nextPublish
	stale := derivation == nil || derivation.CopyRevision != copied.Revision || derivation.LiveDeploymentID != next.Live.DeploymentID ||
		derivation.LayerRevision != layerRevision || derivation.PreviewRevision != next.previewRevision()
	return &CopyState{Revision: copied.Revision, Stale: stale}, nil
}

// readDerivation reads what the copy of commit was built from with its ETag,
// nil before the first copy. A record of a per-version draft from before the
// layer carries no layer or copy revision, so its copy reads as stale.
func (project *projectConfiguration) readDerivation(ctx context.Context, commit string) (*copyDerivation, string, error) {
	object, err := project.objects.ReadObject(ctx, project.derivationKey(commit), "")
	if errors.Is(err, connectorconfig.ErrObjectNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var derivation copyDerivation
	if err = json.Unmarshal(object.Contents, &derivation); err != nil {
		return nil, "", err
	}
	return &derivation, object.ETag, nil
}

// handleNextPublishCopy builds the request commit's frozen copy from the
// layer: the items the version declares, resolved, its connections selected
// again for the Live scope against the version's declarations, with Live's
// own credential broker. It is refused when the layer or Preview's applied
// version moved past what the caller showed. A Project without a layer stores
// its seed first, create-if-absent. The copy is written by revision and its
// derivation (release-drafts/<commit>/derivation) by compare-and-swap on its
// ETag.
func (host *ConfigurationHost) handleNextPublishCopy(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	ctx := request.Context()
	var input CopyRequest
	if decodeBody(response, request, 1<<16, &input) != nil || admission.nextPublish == nil || admission.nextPublish.Source != nil || admission.TeamSpaceID == "" {
		writeCodedError(response, http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Next publish copy request is invalid")
		return
	}
	next, err := host.resolveNextPublish(ctx, admission, input.ManifestDigest)
	if err != nil {
		writeNextPublishError(response, err)
		return
	}
	switch {
	case next.layer.Revision != input.ExpectedLayerRevision:
		writeCodedError(response, http.StatusConflict, "PROJECT_CONFIGURATION_REVISION_CONFLICT", "Live's next publish changed; review it again")
		return
	case admission.nextPublish.previewRevision() != input.ExpectedPreviewRevision:
		writeCodedError(response, http.StatusConflict, "PROJECT_CONFIGURATION_REVISION_CONFLICT", "Preview applied another version; review the next publish again")
		return
	}
	if next.layer.Revision == 0 {
		// The first publish stores the seed, which a concurrent first write may have stored.
		if next.layer, err = host.project.storeSeed(ctx, admission); err != nil {
			writeStoreError(response, err)
			return
		}
	}
	copied := declaredSettings(next.resolved, next.manifest.Manifest, next.bundle)
	if copied, err = host.selectForLive(ctx, admission, copied, next.manifest); err != nil {
		writeConnectionError(response, err)
		return
	}
	store := host.project.commitDraft(admission.SourceCommit)
	current, err := host.project.readConfiguration(ctx, &configurationRequest{drafts: store})
	if err == nil && !configitems.SameItems(copied, current) {
		current, _, err = store.WriteConfiguration(ctx, current.Revision, copied)
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	_, etag, err := host.project.readDerivation(ctx, admission.SourceCommit)
	if err == nil {
		derivation := copyDerivation{
			LiveDeploymentID: admission.nextPublish.Live.DeploymentID, LayerRevision: next.layer.Revision, PreviewRevision: input.ExpectedPreviewRevision,
			CopyRevision: current.Revision, DerivedAt: time.Now().UTC(),
		}
		_, err = host.project.writeObject(ctx, host.project.derivationKey(admission.SourceCommit), etag, comm.Must(json.Marshal(derivation)))
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, CopyReport{ConfigurationRevision: current.Revision, LayerRevision: next.layer.Revision})
}

// selectForLive selects every connection of a copy again for the Live scope
// against the version's declaration, which checks the TeamSpace connection
// and copies its settings, and gives the copy Live's credential broker. A
// selection whose connection is gone or no longer fits is dropped, so
// readiness reports the name as a gap; a refused one fails the copy.
func (host *ConfigurationHost) selectForLive(ctx context.Context, admission *configurationRequest, document connectorconfig.Configuration, manifest projectManifestRecord) (connectorconfig.Configuration, error) {
	selected := document.Connections[:0:0]
	for _, entry := range document.Connections {
		declaration, declared := manifest.declaration(entry.ConnectorID, entry.ConnectionName)
		if !declared || entry.SharedConnectionID == "" {
			continue
		}
		release, err := host.connectorReleases.Resolve(ctx, declaration.module())
		if err != nil {
			return document, err
		}
		connection, settings, err := host.shared.Connections.Select(ctx, connections.Selection{
			TeamSpaceID: admission.TeamSpaceID, ConnectionID: entry.SharedConnectionID, ProjectID: host.project.scope.ProjectID,
			Scope: host.project.scopeKind(), Declared: declaration.module(), DeclaredMethodID: declaration.AuthMethodID,
		})
		if _, refused := connections.Refusal(err); !refused && (errors.Is(err, connections.ErrNotFound) || errors.Is(err, connections.ErrInvalid)) {
			continue
		}
		if err != nil {
			return document, err
		}
		selected = append(selected, connectorconfig.ConnectionConfiguration{
			ConnectorID: entry.ConnectorID, ConnectionName: entry.ConnectionName, ModulePath: declaration.ModulePath, Provider: release.Manifest.Spec.Provider,
			AuthMethodID: declaration.AuthMethodID, SharedConnectionID: connection.ConnectionID, Configuration: comm.Must(json.Marshal(settings)),
		})
	}
	document.Connections = selected
	// A binding or Step setting of a dropped connection name goes with it.
	kept := func(connectorID, connectionName string) bool {
		return slices.ContainsFunc(selected, func(entry connectorconfig.ConnectionConfiguration) bool {
			return entry.ConnectorID == connectorID && entry.ConnectionName == connectionName
		})
	}
	document.TriggerBindings = slices.DeleteFunc(slices.Clone(document.TriggerBindings), func(binding connectorconfig.TriggerConfiguration) bool {
		return !kept(binding.ConnectorID, binding.ConnectionName)
	})
	document.OperationConfigurations = slices.DeleteFunc(slices.Clone(document.OperationConfigurations), func(operation connectorconfig.OperationConfiguration) bool {
		return !kept(operation.ConnectorID, operation.ConnectionName)
	})
	if len(selected) > 0 && document.CredentialBroker == nil {
		broker, err := host.credentialBroker(ctx, admission)
		if err != nil {
			return document, err
		}
		document.CredentialBroker = broker
	}
	return document, nil
}

// anchorConnection gives the layer the request edits the anchor of
// declaration's connection name when it holds no entry of it, so that a
// binding or Step setting of Live's own is saved while the name follows
// Preview. Outside the layer it does nothing.
func (host *ConfigurationHost) anchorConnection(ctx context.Context, admission *configurationRequest, draft *connectionDraft, declaration projectConnectorDeclaration) error {
	if admission.nextPublish == nil {
		return nil
	}
	if _, found := draft.entry(declaration.module().Key()); found {
		return nil
	}
	release, err := host.connectorReleases.Resolve(ctx, declaration.module())
	if err != nil {
		return err
	}
	draft.replaceConnection(configitems.Anchor(declaration.ConnectorID, declaration.ConnectionName, declaration.ModulePath, release.Manifest.Spec.Provider, declaration.AuthMethodID))
	return nil
}

// nextPublishView is the document a read of the layer shows: the resolved
// next publish for the request's prepared source at the layer's revision, so
// an item that follows Preview shows the value it follows.
func (host *ConfigurationHost) nextPublishView(ctx context.Context, admission *configurationRequest) (connectorconfig.Configuration, error) {
	source, err := host.project.readPreparedSource(ctx, admission)
	if err != nil {
		return connectorconfig.Configuration{}, err
	}
	next, err := host.resolveNextPublish(ctx, admission, source.Manifest.ManifestDigest)
	if err != nil {
		return connectorconfig.Configuration{}, err
	}
	return next.resolved, nil
}

// refuseFollow writes the refusal of following Preview for key and reports
// whether it refused: outside Live's next-publish layer, and for an item that
// can never follow.
func (host *ConfigurationHost) refuseFollow(response http.ResponseWriter, request *http.Request, admission *configurationRequest, key configitems.Key) bool {
	if admission.nextPublish == nil {
		writeCodedError(response, http.StatusConflict, itemCannotFollowCode, "Only Live's next publish follows Preview")
		return true
	}
	bundle, err := host.project.preparedBundle(request.Context(), admission)
	if err != nil {
		writeDefinitionError(response, err)
		return true
	}
	source, err := host.project.readPreparedSource(request.Context(), admission)
	if err != nil {
		writeStoreError(response, err)
		return true
	}
	switch followRefusal(source.Manifest.Manifest, bundle, key) {
	case appmanifest.FollowRefusedSecret:
		writeCodedError(response, http.StatusConflict, itemCannotFollowCode, "A secret is never copied from Preview; enter Live's own value")
		return true
	case appmanifest.FollowRefusedEndpointBound:
		reason, _ := connectormanifest.EndpointBoundReason(key.ConnectorID)
		writeCodedError(response, http.StatusConflict, itemCannotFollowCode, "Live needs its own connection: "+reason)
		return true
	}
	return false
}

// followPreview removes keys from the layer the request edits, over the
// revision the form was read at, so they follow Preview.
func (host *ConfigurationHost) followPreview(ctx context.Context, admission *configurationRequest, keys ...configitems.Key) (*connectionDraft, error) {
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		return nil, err
	}
	draft.document = configitems.Without(draft.document, keys...)
	return draft, draft.save(ctx, admission)
}

// writeFollowed makes key follow Preview in the layer the request edits and
// answers fields with the layer's new revision.
func (host *ConfigurationHost) writeFollowed(response http.ResponseWriter, request *http.Request, admission *configurationRequest, key configitems.Key, fields map[string]any) {
	if host.refuseFollow(response, request, admission, key) {
		return
	}
	draft, err := host.followPreview(request.Context(), admission, key)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	fields["followPreview"], fields["configurationRevision"] = true, strconv.FormatUint(draft.document.Revision, 10)
	writeJSON(response, http.StatusOK, fields)
}

// refreshSelections is document as a freeze would write it, with its
// selected connections' current settings, and the connection items whose
// selected TeamSpace connection no longer exists or is no longer ACTIVE,
// which keep their stored settings.
func (host *ConfigurationHost) refreshSelections(ctx context.Context, document connectorconfig.Configuration, manifest projectManifestRecord) (connectorconfig.Configuration, []configitems.Key, error) {
	selectable := document
	selectable.Connections = slices.Clone(document.Connections)
	var deleted []configitems.Key
	for index, entry := range selectable.Connections {
		selected, err := host.sharedSelection(ctx, entry)
		if err != nil {
			return connectorconfig.Configuration{}, nil, err
		}
		if entry.SharedConnectionID != "" && (selected == nil || selected.ConnectionStatus != connectiondao.ConnectionActive) {
			deleted = append(deleted, configitems.ConnectionKey(entry.ConnectorID, entry.ConnectionName))
			selectable.Connections[index].SharedConnectionID = ""
		}
	}
	refreshed, _, _, err := host.refreshSharedSettings(ctx, selectable, manifest)
	if err != nil {
		return connectorconfig.Configuration{}, nil, err
	}
	for index := range refreshed.Connections {
		refreshed.Connections[index].SharedConnectionID = document.Connections[index].SharedConnectionID
	}
	return refreshed, deleted, nil
}

// bindingUses returns each trigger binding of document whose connection name
// selects a TeamSpace connection.
func bindingUses(document connectorconfig.Configuration) []BindingUse {
	selected := configitems.Selected(document)
	var uses []BindingUse
	for _, binding := range document.TriggerBindings {
		if connection := selected[configitems.ConnectionKey(binding.ConnectorID, binding.ConnectionName)]; connection != "" {
			key := configitems.Key{Kind: configitems.KindTriggerBinding, ConnectorID: binding.ConnectorID, ConnectionName: binding.ConnectionName, TriggerName: binding.TriggerName, BindingName: binding.BindingName}
			uses = append(uses, BindingUse{Key: key, SharedConnectionID: connection, Configuration: binding.Configuration})
		}
	}
	return uses
}

// writeNextPublishError reports a next-publish request that cannot be served.
func writeNextPublishError(response http.ResponseWriter, err error) {
	if failure, refused := errors.AsType[*notReadyError](err); refused {
		writeCodedError(response, failure.status, failure.code, failure.message)
		return
	}
	writeStoreError(response, err)
}

// PreviewItemsRequest asks for the items of the Preview's configuration head
// compared with Applied, the version the Preview last applied, or nil before
// the first Apply.
type PreviewItemsRequest struct {
	ManifestDigest string                       `json:"manifestDigest"`
	Applied        *connectorconfig.SnapshotRef `json:"applied,omitempty"`
}

// PreviewItemsReport is the items of the Preview's configuration head at its
// revision, and how many of them an Apply would change.
type PreviewItemsReport struct {
	ConfigurationRevision uint64             `json:"configurationRevision"`
	Items                 []configitems.Item `json:"items"`
	UnappliedChangeCount  int                `json:"unappliedChangeCount"`
}

// handlePreviewItems reports the items of the Preview's configuration head
// against the version the Preview applied. It is read-only: the head is
// compared with its selected connections' current settings, as an Apply
// freezes it; a selection whose connection is gone is CHANGED with the
// CONNECTION_DELETED warning.
func (host *ConfigurationHost) handlePreviewItems(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	ctx := request.Context()
	var input PreviewItemsRequest
	if decodeBody(response, request, 1<<16, &input) != nil || admission.liveDraft || admission.nextPublish != nil || admission.SourceCommit == "" {
		writeCodedError(response, http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Preview configuration items request is invalid")
		return
	}
	manifest, err := host.project.readManifest(ctx, admission)
	if err == nil && manifest.ManifestDigest != input.ManifestDigest {
		err = connectorconfig.ErrConflict
	}
	var draft *connectionDraft
	if err == nil {
		draft, err = host.project.loadConnectionDraft(ctx, admission)
	}
	applied := connectorconfig.Configuration{}
	if err == nil && input.Applied != nil {
		applied, err = host.project.canonical.ReadSnapshot(ctx, *input.Applied)
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	head, deleted, err := host.refreshSelections(ctx, draft.document, manifest)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	items := configitems.PreviewItems(head, applied, deleted)
	unapplied := 0
	for _, item := range items {
		if item.State != configitems.Unchanged {
			unapplied++
		}
	}
	writeJSON(response, http.StatusOK, PreviewItemsReport{ConfigurationRevision: draft.document.Revision, Items: items, UnappliedChangeCount: unapplied})
}
