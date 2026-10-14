// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// previewDefinitionBundleSchema identifies a stored Preview definition bundle.
const previewDefinitionBundleSchema = "superverse.dev/preview-definition-bundle/v2"

// ErrDefinitionMismatch reports Preview definitions that do not match the
// source they are pinned to: a corrupt or foreign bundle, or a configuration
// host that registered another definition revision. Retrying cannot fix it;
// the source must be prepared again.
var ErrDefinitionMismatch = errors.New("preview definitions do not match their pinned source")

// ErrSourceNotPrepared reports a commit the Project's Preview has not
// prepared: no definition bundle holds it. Only preparing that source, or
// deploying it, prepares it.
var ErrSourceNotPrepared = errors.New("the current source is not prepared")

// previewDefinitionBundle is the immutable object that holds the Flow
// Definitions one commit of a Project's workspace declares, with the commit's
// AppManifest. The Preview stores it once the configuration host registered
// the source, so a stored bundle names a source whose configuration exists.
type previewDefinitionBundle struct {
	SchemaVersion string                       `json:"schemaVersion"`
	ProjectID     string                       `json:"projectId"`
	SourceCommit  string                       `json:"sourceCommit"`
	AppManifest   *pb.AppManifestSnapshot      `json:"appManifest"`
	Definitions   []appmanifest.FlowDefinition `json:"definitions"`
}

// bundleKey is where the Preview stores the definition bundle of a commit of
// the Project.
func bundleKey(project, commit string) string {
	return infra.PreviewKeysOf(project).SourcePrefix + commit + "/definitions.json"
}

// errManifestMismatch reports a deployment's exported source tree whose
// dex-app.yaml is not the AppManifest published from the same commit: the
// workspace repository and the repository host disagree about the commit.
var errManifestMismatch = errors.New("the source tree's dex-app.yaml is not the published AppManifest")

// preparePreviewDefinitions generates the Flow Definitions of the instance's
// clean HEAD in the workspace, accepts exactly the Flows manifest, the HEAD's
// AppManifest, declares, registers the prepared source with the Preview
// configuration and stores them with manifest as one immutable bundle. It
// returns the READY preparation, which the caller records together with
// manifest.
func (p *PreviewConfiguration) preparePreviewDefinitions(ctx context.Context, state Snapshot, manifest *pb.AppManifestSnapshot, workspaces *workspace.Capabilities) (PreparedPreviewSource, error) {
	generated, err := workspaces.PreparePreviewDefinitions(ctx, state.binding(), state.PreparedSource.RequestID, state.HeadCommit)
	if err != nil {
		return state.PreparedSource, err
	}
	return p.registerDefinitions(ctx, state, manifest, state.PreparedSource, state.HeadCommit, generated)
}

// preparePinnedPreviewDefinitions prepares the pinned commit of the active
// deployment from tree, the commit's exported source tree: it generates the
// commit's Flow Definitions, requires the tree's dex-app.yaml to be manifest,
// the AppManifest published from the commit (errManifestMismatch), and
// registers them as PreparePreviewDefinitions does. It returns the READY
// preparation of the instance, whose RequestID is the deployment's command
// and which the caller records together with manifest.
func (p *PreviewConfiguration) preparePinnedPreviewDefinitions(ctx context.Context, state Snapshot, manifest *pb.AppManifestSnapshot, workspaces *workspace.Capabilities, tree string) (PreparedPreviewSource, error) {
	command := state.ActiveCommand
	prepared := PreparedPreviewSource{RequestID: command.ID, WorkspaceRequestID: state.WorkspaceRequestID, Generation: state.Generation}
	generated, err := workspaces.PreparePinnedPreviewDefinitions(ctx, state.binding(), command.ID, tree, command.Commit)
	if err != nil {
		return prepared, err
	}
	if generated.ManifestDigest != manifest.ManifestDigest {
		return prepared, fmt.Errorf("%w: the tree of %s holds %s, the AppManifest %s", errManifestMismatch, command.Commit, generated.ManifestDigest, manifest.ManifestDigest)
	}
	return p.registerDefinitions(ctx, state, manifest, prepared, command.Commit, generated.Definitions)
}

// registerDefinitions accepts exactly the Flows that manifest, the
// AppManifest of commit, declares among the generated definitions, registers
// the prepared source with the Preview configuration and then stores the
// definitions and manifest as the commit's immutable bundle, which the
// Preview configuration reads without a workspace. It returns prepared READY
// with them.
func (p *PreviewConfiguration) registerDefinitions(ctx context.Context, state Snapshot, manifest *pb.AppManifestSnapshot, prepared PreparedPreviewSource, commit string, generated []workspace.PreviewDefinition) (PreparedPreviewSource, error) {
	manifestObject := manifest.ManifestObject
	var declared appmanifest.Manifest
	if err := p.objects.GetJSON(ctx, objectstore.ObjectRef{Key: manifestObject.Key, Version: manifestObject.Version, Digest: manifestObject.Digest, MediaType: manifestObject.MediaType}, &declared); err != nil {
		return prepared, fmt.Errorf("read the AppManifest %s: %w", manifestObject.Key, err)
	}
	expected := declared.DeclaredFlowSources()
	bundle := previewDefinitionBundle{SchemaVersion: previewDefinitionBundleSchema, ProjectID: state.Identity.ProjectID, SourceCommit: commit, AppManifest: manifest}
	names := map[string]bool{}
	for _, entry := range generated {
		definition, invalid := generatedDefinition(entry)
		if invalid != nil {
			return prepared, fmt.Errorf("%w: definition of %s: %w", workspace.ErrPreviewDefinitionsRejected, entry.SourcePath, invalid)
		}
		if !expected[definition.SourcePath] || names[definition.FlowType] {
			return prepared, fmt.Errorf("%w: unexpected or duplicate definition of %s (Flow %s)", workspace.ErrPreviewDefinitionsRejected, definition.SourcePath, definition.FlowType)
		}
		delete(expected, definition.SourcePath)
		names[definition.FlowType] = true
		bundle.Definitions = append(bundle.Definitions, definition)
	}
	if len(expected) != 0 {
		return prepared, fmt.Errorf("%w: no definition was generated for %s", workspace.ErrPreviewDefinitionsRejected, strings.Join(slices.Sorted(maps.Keys(expected)), ", "))
	}
	sort.Slice(bundle.Definitions, func(i, j int) bool { return bundle.Definitions[i].FlowType < bundle.Definitions[j].FlowType })
	graphs := make([]json.RawMessage, 0, len(bundle.Definitions))
	for _, definition := range bundle.Definitions {
		graphs = append(graphs, definition.Graph)
	}
	scope, _, err := p.previewScope(ctx, state.Identity)
	if err != nil {
		return prepared, err
	}
	configuration, err := p.configuration.RegisterPreparedSource(ctx, scope, manifest, graphs)
	if err != nil {
		return prepared, fmt.Errorf("register the prepared source with the configuration host: %w", err)
	}
	revision := appmanifest.FlowDefinitionDigest(graphs)
	if configuration.DefinitionRevision != revision {
		return prepared, fmt.Errorf("%w: the configuration host registered definition revision %s for the bundle's %s", ErrDefinitionMismatch, configuration.DefinitionRevision, revision)
	}
	key := bundleKey(state.Identity.ProjectID, commit)
	ref, err := p.objects.PutJSON(ctx, key, bundle)
	if err != nil {
		return prepared, fmt.Errorf("store the Preview definition bundle %s: %w", key, err)
	}
	prepared.Status, prepared.SourceCommit, prepared.Definitions, prepared.DefinitionRevision, prepared.Failure = previewSourceReady, commit, ref, revision, nil
	return prepared, nil
}

// generatedDefinition validates one graph the workspace generated as a
// strict FDG 2.0 Flow Definition, with its compact bytes and digest.
func generatedDefinition(entry workspace.PreviewDefinition) (appmanifest.FlowDefinition, error) {
	var graph struct {
		Flow struct {
			Name string `json:"name"`
		} `json:"flow"`
	}
	if err := json.Unmarshal(entry.Graph, &graph); err != nil {
		return appmanifest.FlowDefinition{}, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, entry.Graph); err != nil {
		return appmanifest.FlowDefinition{}, err
	}
	definition := appmanifest.FlowDefinition{FlowType: graph.Flow.Name, SourcePath: entry.SourcePath, Graph: compact.Bytes(), Digest: objectstore.Digest(compact.Bytes()), SchemaVersion: "2.0", Valid: true}
	return definition, appmanifest.ValidateFlowDefinition(definition)
}

// readPreviewDefinitions reads the definitions of the instance's READY preparation.
func (p *PreviewConfiguration) readPreviewDefinitions(ctx context.Context, state Snapshot) ([]appmanifest.FlowDefinition, error) {
	source := state.PreparedSource
	if source.Status != previewSourceReady {
		return nil, ErrNotReady
	}
	return ReadPreviewDefinitionBundle(ctx, p.objects, state.Identity, source.Definitions, source.SourceCommit, source.DefinitionRevision)
}

// ReadPreviewDefinitionBundle reads the Preview definition bundle that ref
// pins and verifies that it belongs to the Project, was generated from commit
// and has the definition revision. Studio Runs reads a running Preview's
// bundle through it too. Any mismatch is ErrDefinitionMismatch.
func ReadPreviewDefinitionBundle(ctx context.Context, objects *objectstore.Store, identity sandbox.Identity, ref objectstore.ObjectRef, commit, revision string) ([]appmanifest.FlowDefinition, error) {
	var bundle previewDefinitionBundle
	if err := objects.GetJSON(ctx, ref, &bundle); err != nil {
		return nil, fmt.Errorf("read the Preview definition bundle %s: %w", ref.Key, err)
	}
	graphs, err := bundle.graphs(ref.Key, identity.ProjectID, commit)
	if err != nil {
		return nil, err
	}
	if digest := appmanifest.FlowDefinitionDigest(graphs); digest != revision {
		return nil, fmt.Errorf("%w: bundle %s has definition revision %s, not the pinned %s", ErrDefinitionMismatch, ref.Key, digest, revision)
	}
	return bundle.Definitions, nil
}

// readPreparedSource reads the bundle the Project's Preview stored for commit
// and returns the commit's AppManifest. It reads storage alone. A commit the
// Preview never prepared is ErrSourceNotPrepared, and a bundle that does not
// hold the commit is ErrDefinitionMismatch.
func (p *PreviewConfiguration) readPreparedSource(ctx context.Context, project, commit string) (*pb.AppManifestSnapshot, error) {
	key := bundleKey(project, commit)
	var bundle previewDefinitionBundle
	ref, err := p.objects.GetCurrentJSON(ctx, key, &bundle)
	if errors.Is(err, objectstore.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrSourceNotPrepared, commit)
	}
	if err != nil {
		return nil, fmt.Errorf("read the Preview definition bundle %s: %w", key, err)
	}
	if _, err = bundle.graphs(ref.Key, project, commit); err != nil {
		return nil, err
	}
	return bundle.AppManifest, nil
}

// graphs verifies that the bundle stored at key holds the valid, distinct
// and ordered definitions of commit of the Project with the commit's
// AppManifest, and returns their graphs in order. Any mismatch is
// ErrDefinitionMismatch.
func (bundle previewDefinitionBundle) graphs(key, project, commit string) ([]json.RawMessage, error) {
	if bundle.SchemaVersion != previewDefinitionBundleSchema || bundle.ProjectID != project || bundle.SourceCommit != commit ||
		bundle.AppManifest.GetSourceCommit() != commit || len(bundle.Definitions) == 0 {
		return nil, fmt.Errorf("%w: bundle %s holds %d definitions of %s at %s (schema %q); expected %s at %s", ErrDefinitionMismatch, key,
			len(bundle.Definitions), bundle.ProjectID, bundle.SourceCommit, bundle.SchemaVersion, project, commit)
	}
	names, paths := map[string]bool{}, map[string]bool{}
	graphs := make([]json.RawMessage, 0, len(bundle.Definitions))
	for index, definition := range bundle.Definitions {
		if err := appmanifest.ValidateFlowDefinition(definition); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDefinitionMismatch, err)
		}
		if names[definition.FlowType] || paths[definition.SourcePath] || index > 0 && bundle.Definitions[index-1].FlowType >= definition.FlowType {
			return nil, fmt.Errorf("%w: bundle %s repeats or misorders Flow %s (%s)", ErrDefinitionMismatch, key, definition.FlowType, definition.SourcePath)
		}
		names[definition.FlowType], paths[definition.SourcePath] = true, true
		graphs = append(graphs, definition.Graph)
	}
	return graphs, nil
}
