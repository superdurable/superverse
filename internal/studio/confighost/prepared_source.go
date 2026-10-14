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
	"strings"

	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
)

// A prepared source is one source commit's admitted AppManifest and Flow
// Definitions, stored once and never replaced. Live builds a version's frozen
// copy from its next-publish layer when the version is published, and a build
// freezes that copy into the scope's canonical head; Preview edits its
// canonical head directly.

// PreparedAppManifestRevision is the AppManifest revision of every prepared
// source, whose AppManifest is registered once and never changes.
const PreparedAppManifestRevision = 1

// SourceRegistration prepares a source commit: its AppManifest and its Flow
// Definition graphs.
type SourceRegistration struct {
	ManifestRegistration
	Definitions []json.RawMessage `json:"definitions"`
}

// preparedSource is a stored prepared source. A source prepared before the
// next-publish layer also holds the seed of its per-version draft, which
// reading it ignores.
type preparedSource struct {
	Manifest    projectManifestRecord `json:"manifest"`
	Definitions []json.RawMessage     `json:"definitions"`
}

// handlePrepareSource stores the prepared source of the registration's
// commit once and initializes the configuration it edits once. A retry that
// finds the same source stored succeeds; it never publishes a serving catalog
// or changes a running configuration.
func (host *ConfigurationHost) handlePrepareSource(response http.ResponseWriter, request *http.Request) {
	if _, internal := requireInternal(response, request); !internal {
		return
	}
	var registration SourceRegistration
	if err := decodeBody(response, request, 32<<20, &registration); err != nil {
		writeCodedError(response, http.StatusBadRequest, "RELEASE_CONFIGURATION_INVALID", "Prepared source registration is invalid: "+err.Error())
		return
	}
	manifest, err := parseProjectManifest(registration.ManifestRegistration)
	if err != nil {
		writeCodedError(response, http.StatusBadRequest, "APP_MANIFEST_INVALID", err.Error())
		return
	}
	bundle, err := fdg.ParseBundle(registration.Definitions)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	manifest.Revision, manifest.Scope = PreparedAppManifestRevision, host.project.scope
	source := preparedSource{Manifest: manifest, Definitions: registration.Definitions}
	// The manifest's commit, which source admission checked, selects the
	// prepared source.
	ctx := request.Context()
	prepared := host.project.admit(ConfigurationAdmission{Internal: true, SourceCommit: manifest.SourceCommit})
	if err = host.project.storePreparedSource(ctx, prepared, &source, bundle.Revision); err != nil {
		writeStoreError(response, err)
		return
	}
	draft, err := host.project.initializeDraft(ctx, prepared)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	summary := host.project.summary(manifest, draft.Revision)
	summary.DefinitionRevision = bundle.Revision
	writeJSON(response, http.StatusOK, summary)
}

// storePreparedSource creates the request commit's prepared source. When the
// commit already has one with the same manifest and definition revision,
// source becomes the stored one.
func (project *projectConfiguration) storePreparedSource(ctx context.Context, request *configurationRequest, source *preparedSource, definitionRevision string) error {
	encoded := comm.Must(objectstore.EncodeJSON(source))
	_, err := project.objects.CreateObject(ctx, project.preparedSourceKey(request.SourceCommit), encoded)
	if !errors.Is(err, connectorconfig.ErrConflict) && !errors.Is(err, connectorconfig.ErrOutcomeUnknown) {
		return err
	}
	accepted, readErr := project.readPreparedSource(ctx, request)
	if readErr != nil || accepted.Manifest.ManifestDigest != source.Manifest.ManifestDigest || appmanifest.FlowDefinitionDigest(accepted.Definitions) != definitionRevision {
		return err
	}
	*source = accepted
	return nil
}

// readPreparedSource reads the request's prepared source once per request.
func (project *projectConfiguration) readPreparedSource(ctx context.Context, request *configurationRequest) (preparedSource, error) {
	if request.source != nil {
		return *request.source, nil
	}
	object, err := project.objects.ReadObject(ctx, project.preparedSourceKey(request.SourceCommit), "")
	if err != nil {
		return preparedSource{}, err
	}
	var source preparedSource
	// The seed fields of a source prepared before the layer are ignored.
	if err = json.Unmarshal(object.Contents, &source); err != nil {
		return source, err
	}
	if source.Manifest.SourceCommit != request.SourceCommit || source.Manifest.Scope != project.scope || source.Manifest.Revision != PreparedAppManifestRevision {
		return source, errors.New("prepared source identity differs")
	}
	request.source = &source
	return source, nil
}

// preparedBundle parses the Flow Definitions of the request's prepared source.
func (project *projectConfiguration) preparedBundle(ctx context.Context, request *configurationRequest) (*fdg.Bundle, error) {
	source, err := project.readPreparedSource(ctx, request)
	if err != nil {
		return nil, err
	}
	return fdg.ParseBundle(source.Definitions)
}

// initializeDraft returns the configuration the request commit edits, the
// Preview's head or a Live version's frozen copy, writing it once as an empty
// document when it is absent; an existing one is never replaced. A Live
// copy's settings come from the next-publish layer when the version is
// published.
func (project *projectConfiguration) initializeDraft(ctx context.Context, request *configurationRequest) (connectorconfig.Configuration, error) {
	draft, err := project.readConfiguration(ctx, request)
	if err != nil || draft.Revision != 0 {
		return draft, err
	}
	draft, _, err = request.drafts.WriteConfiguration(ctx, 0, draft)
	if errors.Is(err, connectorconfig.ErrConflict) {
		// A concurrent preparation initialized it first.
		draft, _, err = request.drafts.ReadConfiguration(ctx)
	}
	return draft, err
}

// declaredSettings keeps the settings of seed that manifest and bundle
// declare identically, as a version's frozen copy keeps them: a connection
// name's selection of the declared module and method, whichever Connector
// release the manifest pins, with the scope's credential broker; a kept
// connection's declared trigger bindings and its use configurations that a
// connector factory Step accepts; and environment values of the declared
// kind.
func declaredSettings(seed connectorconfig.Configuration, manifest projectAppManifest, bundle *fdg.Bundle) connectorconfig.Configuration {
	declarations := make(map[connectorconfig.ConnectionKey]projectConnectorDeclaration, len(manifest.Connectors))
	for _, declaration := range manifest.Connectors {
		declarations[declaration.module().Key()] = declaration
	}
	// A kept selection still needs the scope's credential broker.
	result := connectorconfig.Configuration{
		Connections: []connectorconfig.ConnectionConfiguration{}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
		OperationConfigurations: []connectorconfig.OperationConfiguration{}, Environment: map[string]connectorconfig.EnvironmentValue{},
		CredentialBroker: seed.CredentialBroker,
	}
	kept := make(map[connectorconfig.ConnectionKey]bool, len(seed.Connections))
	for _, connection := range seed.Connections {
		key := connectorconfig.ConnectionKey{ConnectorID: connection.ConnectorID, ConnectionName: connection.ConnectionName}
		if declaration, declared := declarations[key]; declared && connection.ModulePath == declaration.ModulePath && connection.AuthMethodID == declaration.AuthMethodID {
			result.Connections = append(result.Connections, connection)
			kept[key] = true
		}
	}
	for _, binding := range seed.TriggerBindings {
		key := connectorconfig.ConnectionKey{ConnectorID: binding.ConnectorID, ConnectionName: binding.ConnectionName}
		if kept[key] && slices.Contains(declarations[key].TriggerBindings, projectTriggerBinding{TriggerName: binding.TriggerName, BindingName: binding.BindingName}) {
			result.TriggerBindings = append(result.TriggerBindings, binding)
		}
	}
	for _, operation := range seed.OperationConfigurations {
		key := connectorconfig.ConnectionKey{ConnectorID: operation.ConnectorID, ConnectionName: operation.ConnectionName}
		if !kept[key] {
			continue
		}
		step, found := connectorOperationStep(bundle, declarations[key].module(), operation.OperationID, operation.FlowType, operation.StepType)
		var values map[string]json.RawMessage
		if found && json.Unmarshal(operation.Configuration, &values) == nil && connectormanifest.ValidateUseConfiguration(values, step.ConfigurationUI) == nil {
			result.OperationConfigurations = append(result.OperationConfigurations, operation)
		}
	}
	for _, declaration := range manifest.Application.Environment {
		if value, exists := seed.Environment[declaration.Name]; exists && (value.SecretRef != nil) == declaration.Secret {
			result.Environment[declaration.Name] = value
		}
	}
	return result
}

// buildFreeze is the immutable receipt of one build's frozen
// configuration, which recovers a lost freeze response.
type buildFreeze struct {
	DraftRevision         uint64                      `json:"draftRevision"`
	ManifestDigest        string                      `json:"manifestDigest"`
	Fingerprint           string                      `json:"fingerprint"`
	Snapshot              connectorconfig.SnapshotRef `json:"snapshot"`
	ConfigurationRevision uint64                      `json:"configurationRevision"`
}

// freezeBuild writes a Live commit's draft document to the scope's canonical
// head as the build's exact version, under a receipt keyed by the build ID, so
// the same build recovers the same version even after the draft changes. The
// validate handler recovers a retried build before it gets here; a concurrent
// freeze of the same build is recovered when creating the receipt conflicts.
func (project *projectConfiguration) freezeBuild(ctx context.Context, manifest projectManifestRecord, document connectorconfig.Configuration, buildID string) (connectorconfig.SnapshotRef, uint64, error) {
	var frozen connectorconfig.Configuration
	var object connectorconfig.Object
	var err error
	for range 3 {
		current, _, readErr := project.canonical.ReadConfiguration(ctx)
		if readErr != nil && !errors.Is(readErr, connectorconfig.ErrObjectNotFound) {
			return connectorconfig.SnapshotRef{}, 0, readErr
		}
		frozen, object, err = project.canonical.WriteConfiguration(ctx, current.Revision, document)
		if !errors.Is(err, connectorconfig.ErrConflict) {
			break
		}
	}
	if err != nil {
		return connectorconfig.SnapshotRef{}, 0, err
	}
	receipt := buildFreeze{
		DraftRevision: document.Revision, ManifestDigest: manifest.ManifestDigest,
		Fingerprint:           objectstore.Digest([]byte(manifest.SourceCommit + "/" + manifest.ManifestDigest + "/" + objectstore.Digest(comm.Must(json.Marshal(document))))),
		Snapshot:              connectorconfig.SnapshotRef{Key: object.Key, Version: object.Version, Digest: objectstore.Digest(object.Contents), MediaType: objectstore.MediaTypeJSON},
		ConfigurationRevision: frozen.Revision,
	}
	receiptKey := project.buildFreezeKey(manifest.SourceCommit, buildID)
	if _, err = project.objects.CreateObject(ctx, receiptKey, comm.Must(json.Marshal(receipt))); err != nil {
		if !errors.Is(err, connectorconfig.ErrConflict) && !errors.Is(err, connectorconfig.ErrOutcomeUnknown) {
			return connectorconfig.SnapshotRef{}, 0, err
		}
		accepted, readErr := project.readBuildFreeze(ctx, receiptKey)
		if readErr != nil {
			return connectorconfig.SnapshotRef{}, 0, readErr
		}
		if accepted.Fingerprint != receipt.Fingerprint {
			return connectorconfig.SnapshotRef{}, 0, connectorconfig.ErrConflict
		}
		receipt = accepted
	}
	return receipt.Snapshot, receipt.ConfigurationRevision, nil
}

// readBuildFreeze reads a build's freeze receipt and checks that its
// snapshot is still readable.
func (project *projectConfiguration) readBuildFreeze(ctx context.Context, key string) (buildFreeze, error) {
	object, err := project.objects.ReadObject(ctx, key, "")
	if err != nil {
		return buildFreeze{}, err
	}
	var receipt buildFreeze
	if err = comm.DecodeStrictJSON(object.Contents, &receipt); err != nil {
		return receipt, err
	}
	document, err := project.canonical.ReadSnapshot(ctx, receipt.Snapshot)
	if err != nil {
		return receipt, err
	}
	if document.Revision != receipt.ConfigurationRevision {
		return receipt, connectorconfig.ErrConflict
	}
	return receipt, nil
}

// redirectedHeadObjects maps a Live scope's canonical configuration head to
// another head, a version's frozen copy or the next-publish layer, so the SDK
// configuration store edits that document. Every other key, such as the
// scope's secrets, is unchanged.
type redirectedHeadObjects struct {
	objects      connectorconfig.ObjectStore
	head, target string
}

func (store *redirectedHeadObjects) ReadObject(ctx context.Context, key, version string) (connectorconfig.Object, error) {
	object, err := store.objects.ReadObject(ctx, store.objectKey(key), version)
	object.Key = key
	return object, err
}

func (store *redirectedHeadObjects) CreateObject(ctx context.Context, key string, contents []byte) (connectorconfig.Object, error) {
	object, err := store.objects.CreateObject(ctx, store.objectKey(key), contents)
	object.Key = key
	return object, err
}

func (store *redirectedHeadObjects) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (connectorconfig.Object, error) {
	object, err := store.objects.CompareAndSwapObject(ctx, store.objectKey(key), expectedETag, contents)
	object.Key = key
	return object, err
}

func (store *redirectedHeadObjects) objectKey(key string) string {
	if key == store.head {
		return store.target
	}
	return key
}

func (project *projectConfiguration) preparedSourceKey(commit string) string {
	return project.prefix + "/configuration-sources/" + commit + "/source"
}

func (project *projectConfiguration) buildFreezeKey(commit, buildID string) string {
	return project.prefix + "/release-drafts/" + commit + "/freezes/" + buildID
}

func validReleaseBuildID(value string) bool {
	id, err := uuid.Parse(strings.TrimPrefix(value, "build-"))
	return err == nil && id.Version() == 4 && value == "build-"+id.String()
}
