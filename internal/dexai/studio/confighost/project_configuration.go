// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
)

// projectConfiguration owns the stored configuration of one project scope:
// its AppManifest head, prepared sources, commit drafts and build freezes.
// Its connection names select TeamSpace connections, whose credentials the
// platform keeps.
type projectConfiguration struct {
	objects connectorconfig.ObjectStore
	// canonical reads and writes the scope's canonical head, whose versions
	// frozen snapshots pin.
	canonical *connectorconfig.ConfigurationStore
	scope     connectorconfig.Scope
	prefix    string
}

func newProjectConfiguration(objects connectorconfig.ObjectStore, scope connectorconfig.Scope) (*projectConfiguration, error) {
	prefix, err := scope.Prefix()
	if err != nil {
		return nil, err
	}
	// The host reads and writes only below its own scope's prefix, whatever
	// the store it is given admits; another scope's values are handed over.
	objects = &scopedObjects{objects: objects, prefix: prefix + "/"}
	// A configuration store fails only for a scope Prefix has rejected.
	canonical := comm.Must(connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: scope}))
	return &projectConfiguration{objects: objects, canonical: canonical, scope: scope, prefix: prefix}, nil
}

// scopedObjects admits only keys below one scope's prefix; any other is
// infra.ErrAccessDenied before it reaches storage.
type scopedObjects struct {
	objects connectorconfig.ObjectStore
	prefix  string
}

func (store *scopedObjects) admit(key string) error {
	if !strings.HasPrefix(key, store.prefix) {
		return fmt.Errorf("%w: configuration key %s is outside the scope", infra.ErrAccessDenied, key)
	}
	return nil
}

func (store *scopedObjects) ReadObject(ctx context.Context, key, version string) (connectorconfig.Object, error) {
	if err := store.admit(key); err != nil {
		return connectorconfig.Object{}, err
	}
	return store.objects.ReadObject(ctx, key, version)
}

func (store *scopedObjects) CreateObject(ctx context.Context, key string, contents []byte) (connectorconfig.Object, error) {
	if err := store.admit(key); err != nil {
		return connectorconfig.Object{}, err
	}
	return store.objects.CreateObject(ctx, key, contents)
}

func (store *scopedObjects) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (connectorconfig.Object, error) {
	if err := store.admit(key); err != nil {
		return connectorconfig.Object{}, err
	}
	return store.objects.CompareAndSwapObject(ctx, key, expectedETag, contents)
}

// admit fixes what one request reads and edits. A next-publish request edits
// the Live scope's next-publish layer and is checked against the source it
// names, which the admission hands over when it is not one of this scope; a
// request with a Live source commit reads that commit's frozen copy, which
// publishing writes; every other request edits the scope's canonical head,
// which for Live is the version current Live froze.
func (project *projectConfiguration) admit(admission ConfigurationAdmission) *configurationRequest {
	request := &configurationRequest{ConfigurationAdmission: admission, drafts: project.canonical}
	switch {
	case admission.NextPublish != nil:
		request.drafts, request.nextPublish = project.nextPublishLayer(), admission.NextPublish
		if named := admission.NextPublish.Source; named != nil {
			request.source = &named.source
		}
	case project.scope.Kind == "live" && admission.SourceCommit != "":
		request.drafts, request.liveDraft = project.commitDraft(admission.SourceCommit), true
	}
	return request
}

// commitDraft is the store of a Live commit's frozen copy.
func (project *projectConfiguration) commitDraft(commit string) *connectorconfig.ConfigurationStore {
	return project.redirectedHead(project.prefix + "/release-drafts/" + commit + "/configuration/head")
}

// redirectedHead is a configuration store whose head is key instead of the
// scope's canonical head.
func (project *projectConfiguration) redirectedHead(key string) *connectorconfig.ConfigurationStore {
	objects := &redirectedHeadObjects{objects: project.objects, head: project.prefix + "/configuration/head", target: key}
	return comm.Must(connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: project.scope}))
}

// readManifest reads the AppManifest of the request's prepared source, or
// without a source commit the scope's current AppManifest head.
func (project *projectConfiguration) readManifest(ctx context.Context, request *configurationRequest) (projectManifestRecord, error) {
	if request.SourceCommit != "" {
		source, err := project.readPreparedSource(ctx, request)
		return source.Manifest, err
	}
	record, _, err := project.readManifestHead(ctx)
	return record, err
}

// readManifestHead reads the scope's AppManifest head with the object version
// a registration replaces.
func (project *projectConfiguration) readManifestHead(ctx context.Context) (projectManifestRecord, connectorconfig.Object, error) {
	object, err := project.objects.ReadObject(ctx, project.manifestKey(), "")
	if err != nil {
		return projectManifestRecord{}, connectorconfig.Object{}, err
	}
	var record projectManifestRecord
	if err = comm.DecodeStrictJSON(object.Contents, &record); err != nil {
		return record, object, err
	}
	if record.SchemaVersion != projectManifestSchema || record.Scope != project.scope || record.Revision == 0 || object.Version == "" || object.ETag == "" {
		return record, object, errors.New("project AppManifest identity is invalid")
	}
	return record, object, nil
}

func (project *projectConfiguration) manifestKey() string {
	return project.prefix + "/app-manifest/head"
}

// readConfiguration reads the configuration the request edits; before its
// first write it is the empty configuration at revision zero, and the
// next-publish layer the seed computed from what runs.
func (project *projectConfiguration) readConfiguration(ctx context.Context, request *configurationRequest) (connectorconfig.Configuration, error) {
	document, _, err := request.drafts.ReadConfiguration(ctx)
	if errors.Is(err, connectorconfig.ErrObjectNotFound) && request.nextPublish != nil {
		return project.seedLayer(ctx, request.nextPublish)
	}
	if errors.Is(err, connectorconfig.ErrObjectNotFound) {
		return connectorconfig.Configuration{
			SchemaVersion: connectorconfig.ConfigurationSchemaVersion, Scope: project.scope,
			Connections: []connectorconfig.ConnectionConfiguration{}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
			OperationConfigurations: []connectorconfig.OperationConfiguration{},
		}, nil
	}
	return document, err
}

// writeObject creates key, or replaces the version etag names. A lost
// response that wrote exactly contents counts as written.
func (project *projectConfiguration) writeObject(ctx context.Context, key, etag string, contents []byte) (connectorconfig.Object, error) {
	var result connectorconfig.Object
	var err error
	if etag == "" {
		result, err = project.objects.CreateObject(ctx, key, contents)
	} else {
		result, err = project.objects.CompareAndSwapObject(ctx, key, etag, contents)
	}
	if errors.Is(err, connectorconfig.ErrOutcomeUnknown) {
		observed, readErr := project.objects.ReadObject(ctx, key, "")
		if readErr != nil {
			return result, errors.Join(err, readErr)
		}
		if bytes.Equal(observed.Contents, contents) {
			return observed, nil
		}
	}
	return result, err
}

// configurationSummary is the safe metadata of a scope's configuration that
// the platform's Flows read. Every field after the scope is optional.
type configurationSummary struct {
	AppManifestRevision           uint64                       `json:"appManifestRevision"`
	SourceCommit                  string                       `json:"sourceCommit"`
	ManifestDigest                string                       `json:"manifestDigest"`
	ConnectorContractDigest       string                       `json:"connectorContractDigest"`
	EnvironmentContractDigest     string                       `json:"environmentContractDigest"`
	ConfigurationRevision         uint64                       `json:"configurationRevision"`
	Scope                         connectorconfig.Scope        `json:"scope"`
	Status                        string                       `json:"status,omitempty"`
	Snapshot                      *connectorconfig.SnapshotRef `json:"snapshot,omitempty"`
	SnapshotConfigurationRevision uint64                       `json:"snapshotConfigurationRevision,omitempty"`
	DefinitionRevision            string                       `json:"definitionRevision,omitempty"`
}

func (project *projectConfiguration) summary(manifest projectManifestRecord, configurationRevision uint64) configurationSummary {
	return configurationSummary{
		AppManifestRevision: manifest.Revision, SourceCommit: manifest.SourceCommit, ManifestDigest: manifest.ManifestDigest,
		ConnectorContractDigest: manifest.ConnectorContractDigest, EnvironmentContractDigest: manifest.EnvironmentContractDigest,
		ConfigurationRevision: configurationRevision, Scope: project.scope,
	}
}

type connectorTriggerBinding struct {
	connectorID, connectionName, triggerName, bindingName string
	configuration                                         map[string]json.RawMessage
}

// connectorUseConfiguration configures one connector factory Step's use of its connection.
type connectorUseConfiguration struct {
	ConnectorID    string                     `json:"connectorId"`
	ConnectionName string                     `json:"connectionName"`
	OperationID    string                     `json:"operationId"`
	FlowType       string                     `json:"flowType"`
	StepType       string                     `json:"stepType"`
	Configuration  map[string]json.RawMessage `json:"configuration"`
}

// connectionDraft is the configuration document a request edits. Its writes
// carry the configuration and credential revisions the actor's form was read
// at, so a form that went stale cannot overwrite a newer change.
type connectionDraft struct {
	project  *projectConfiguration
	store    *connectorconfig.ConfigurationStore
	document connectorconfig.Configuration
}

func (project *projectConfiguration) loadConnectionDraft(ctx context.Context, request *configurationRequest) (*connectionDraft, error) {
	document, err := project.readConfiguration(ctx, request)
	if err != nil {
		return nil, err
	}
	return &connectionDraft{project: project, store: request.drafts, document: document}, nil
}

// entry returns the draft's entry of a connection name.
func (draft *connectionDraft) entry(key connectorconfig.ConnectionKey) (connectorconfig.ConnectionConfiguration, bool) {
	index := slices.IndexFunc(draft.document.Connections, func(entry connectorconfig.ConnectionConfiguration) bool {
		return entry.ConnectorID == key.ConnectorID && entry.ConnectionName == key.ConnectionName
	})
	if index < 0 {
		return connectorconfig.ConnectionConfiguration{}, false
	}
	return draft.document.Connections[index], true
}

func (draft *connectionDraft) triggerBindings(connectorID, connectionName string) ([]connectorTriggerBinding, error) {
	bindings := []connectorTriggerBinding{}
	for _, entry := range draft.document.TriggerBindings {
		if entry.ConnectorID != connectorID || entry.ConnectionName != connectionName {
			continue
		}
		binding := connectorTriggerBinding{connectorID: connectorID, connectionName: connectionName, triggerName: entry.TriggerName, bindingName: entry.BindingName}
		if err := json.Unmarshal(entry.Configuration, &binding.configuration); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func (draft *connectionDraft) putTriggerBinding(ctx context.Context, admission *configurationRequest, binding connectorTriggerBinding) error {
	entry := connectorconfig.TriggerConfiguration{
		ConnectorID: binding.connectorID, ConnectionName: binding.connectionName, TriggerName: binding.triggerName,
		BindingName: binding.bindingName, Configuration: comm.Must(json.Marshal(binding.configuration)),
	}
	draft.document.TriggerBindings = upsert(draft.document.TriggerBindings, entry, func(current connectorconfig.TriggerConfiguration) bool {
		return current.ConnectorID == entry.ConnectorID && current.ConnectionName == entry.ConnectionName &&
			current.TriggerName == entry.TriggerName && current.BindingName == entry.BindingName
	})
	return draft.save(ctx, admission)
}

func (draft *connectionDraft) useConfigurations(connectorID, connectionName string) ([]connectorUseConfiguration, error) {
	configurations := []connectorUseConfiguration{}
	for _, entry := range draft.document.OperationConfigurations {
		if entry.ConnectorID != connectorID || entry.ConnectionName != connectionName {
			continue
		}
		configuration := connectorUseConfiguration{ConnectorID: connectorID, ConnectionName: connectionName, OperationID: entry.OperationID, FlowType: entry.FlowType, StepType: entry.StepType}
		if err := json.Unmarshal(entry.Configuration, &configuration.Configuration); err != nil {
			return nil, err
		}
		configurations = append(configurations, configuration)
	}
	return configurations, nil
}

func (draft *connectionDraft) putUseConfiguration(ctx context.Context, admission *configurationRequest, configuration connectorUseConfiguration) error {
	entry := connectorconfig.OperationConfiguration{
		ConnectorID: configuration.ConnectorID, ConnectionName: configuration.ConnectionName, OperationID: configuration.OperationID,
		FlowType: configuration.FlowType, StepType: configuration.StepType, Configuration: comm.Must(json.Marshal(configuration.Configuration)),
	}
	draft.document.OperationConfigurations = upsert(draft.document.OperationConfigurations, entry, func(current connectorconfig.OperationConfiguration) bool {
		return current.ConnectorID == entry.ConnectorID && current.ConnectionName == entry.ConnectionName &&
			current.OperationID == entry.OperationID && current.FlowType == entry.FlowType && current.StepType == entry.StepType
	})
	return draft.save(ctx, admission)
}

func (draft *connectionDraft) replaceConnection(entry connectorconfig.ConnectionConfiguration) {
	draft.document.Connections = upsert(draft.document.Connections, entry, func(current connectorconfig.ConnectionConfiguration) bool {
		return current.ConnectorID == entry.ConnectorID && current.ConnectionName == entry.ConnectionName
	})
}

// save writes the draft over the configuration revision the form was read at.
func (draft *connectionDraft) save(ctx context.Context, admission *configurationRequest) error {
	if draft.document.Revision != admission.ConfigurationRevision {
		return connectorconfig.ErrConflict
	}
	document, err := draft.project.writeConfiguration(ctx, admission, draft.document)
	if err != nil {
		return err
	}
	draft.document = document
	return nil
}

// writeConfiguration writes document, edited from the configuration the
// request read at its admission's revision. A form read before the
// next-publish layer existed edits its seed: the seed is stored first,
// create-if-absent, as the layer's first revision, and the edit is written
// over it, so concurrent first writes store one seed and each change
// applies over it, one revision at a time.
func (project *projectConfiguration) writeConfiguration(ctx context.Context, request *configurationRequest, document connectorconfig.Configuration) (connectorconfig.Configuration, error) {
	expected := request.ConfigurationRevision
	if expected == 0 && request.nextPublish != nil {
		seed, err := project.storeSeed(ctx, request)
		if err != nil {
			return connectorconfig.Configuration{}, err
		}
		expected = seed.Revision
	}
	written, _, err := request.drafts.WriteConfiguration(ctx, expected, document)
	return written, err
}

// storeSeed stores the seed of the request's absent next-publish layer,
// create-if-absent, and returns it as stored. A layer another request stored
// is accepted only while it is still exactly the seed.
func (project *projectConfiguration) storeSeed(ctx context.Context, request *configurationRequest) (connectorconfig.Configuration, error) {
	seed, err := project.seedLayer(ctx, request.nextPublish)
	if err != nil {
		return connectorconfig.Configuration{}, err
	}
	stored, _, err := request.drafts.WriteConfiguration(ctx, 0, seed)
	if !errors.Is(err, connectorconfig.ErrConflict) {
		return stored, err
	}
	current, _, readErr := request.drafts.ReadConfiguration(ctx)
	if readErr != nil {
		return connectorconfig.Configuration{}, readErr
	}
	if current.Revision != 1 || !configitems.SameItems(current, seed) {
		return connectorconfig.Configuration{}, connectorconfig.ErrConflict
	}
	return current, nil
}

// upsert replaces the entry of items that same selects, or appends entry.
func upsert[T any](items []T, entry T, same func(T) bool) []T {
	if index := slices.IndexFunc(items, same); index >= 0 {
		items[index] = entry
		return items
	}
	return append(items, entry)
}
