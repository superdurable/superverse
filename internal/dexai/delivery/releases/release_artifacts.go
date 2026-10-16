// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
)

// artifactOwner is the source or build revision that release artifacts belong
// to, as the definition bundle names it. A build revision binds its frozen
// configuration; source preparation binds none.
type artifactOwner struct {
	BuildRevisionID       string `json:"buildRevisionId,omitempty"`
	ConfigurationDigest   string `json:"configurationDigest,omitempty"`
	ConfigurationRevision uint64 `json:"configurationRevision,omitempty"`
	ReleaseID             string `json:"releaseId"`
	ProjectID             string `json:"projectId"`
	SourceCommit          string `json:"sourceCommitSha"`
	BuildProfileDigest    string `json:"buildProfileDigest"`
}

// sourceArtifacts owns the artifacts of the Release's source preparation.
func sourceArtifacts(s Snapshot) artifactOwner {
	return artifactOwner{BuildRevisionID: build.SourceRevisionID, ReleaseID: s.ReleaseID, ProjectID: s.ProjectID, SourceCommit: s.SourceCommit, BuildProfileDigest: s.BuildProfileDigest}
}

// artifacts owns the artifacts of the build revision under its own recipe.
func (b BuildSnapshot) artifacts() artifactOwner {
	owner := artifactOwner{BuildRevisionID: b.Build.BuildRevisionID, ReleaseID: b.Release.ReleaseID, ProjectID: b.Release.ProjectID, SourceCommit: b.Release.SourceCommit, BuildProfileDigest: b.Build.BuildProfileDigest}
	if c := b.Build.Configuration; c != nil {
		owner.ConfigurationDigest, owner.ConfigurationRevision = c.Object.Digest, c.ConfigurationRevision
	}
	return owner
}

// definitionBundle is the flow-definitions.json artifact that both builders
// render: the Flow Definitions of one source or build revision. The builders
// write it, so its JSON names are fixed.
type definitionBundle struct {
	SchemaVersion string `json:"schemaVersion"`
	artifactOwner
	Definitions []appmanifest.FlowDefinition `json:"flowDefinitions"`
}

// definitionBundleSchema is the schema version of every definition bundle.
const definitionBundleSchema = "superverse.dev/flow-definition-bundle/v1"

// definitionsOf returns the bundle's Flow Definitions when the bundle belongs
// to exactly owner.
func (b definitionBundle) definitionsOf(owner artifactOwner) ([]appmanifest.FlowDefinition, error) {
	if b.artifactOwner != owner {
		return nil, fmt.Errorf("the definition bundle belongs to build %q of Release %q, not build %q of Release %q", b.BuildRevisionID, b.ReleaseID, owner.BuildRevisionID, owner.ReleaseID)
	}
	return b.definitions()
}

// definitions returns the bundle's Flow Definitions when its schema is the
// known one and they are valid.
func (b definitionBundle) definitions() ([]appmanifest.FlowDefinition, error) {
	if b.SchemaVersion != definitionBundleSchema {
		return nil, fmt.Errorf("the definition bundle has the unknown schema %q", b.SchemaVersion)
	}
	if err := appmanifest.ValidateFlowDefinitions(b.Definitions); err != nil {
		return nil, fmt.Errorf("the Flow Definitions are invalid: %w", err)
	}
	return b.Definitions, nil
}

// validatePayloads accepts build payloads that render exactly the admitted
// source for owner: the same dex-app.yaml, each Flow Definition it declares
// and its connector and environment requirements.
func validatePayloads(owner artifactOwner, admitted *pb.AppManifestSnapshot, p build.Payloads) ([]appmanifest.FlowDefinition, error) {
	for name, value := range map[string]string{"flow definitions": p.FlowDefinitions, "connector contract": p.ConnectorContract, "environment contract": p.EnvironmentContract, "manifest": p.Manifest} {
		if len(value) == 0 || len(value) > objectstore.MaxObjectBytes {
			return nil, fmt.Errorf("the %s artifact has %d bytes, outside 1..%d", name, len(value), objectstore.MaxObjectBytes)
		}
	}
	sourceManifest, err := appmanifest.ExtractAppManifest(owner.SourceCommit, []byte(p.Manifest))
	if err != nil {
		return nil, fmt.Errorf("dex-app.yaml: %w", err)
	}
	if admitted == nil || sourceManifest.ManifestDigest != admitted.ManifestDigest {
		return nil, fmt.Errorf("dex-app.yaml has digest %s, not the admitted source's", sourceManifest.ManifestDigest)
	}
	var bundle definitionBundle
	if err = json.Unmarshal([]byte(p.FlowDefinitions), &bundle); err != nil {
		return nil, fmt.Errorf("decode the definition bundle: %w", err)
	}
	definitions, err := bundle.definitionsOf(owner)
	if err != nil {
		return nil, err
	}
	if err = verifyDeclaredDefinitions(p.Manifest, definitions); err != nil {
		return nil, err
	}
	if err = appmanifest.VerifyReleaseContracts(owner.ProjectID, owner.ReleaseID, []byte(p.ConnectorContract), []byte(p.EnvironmentContract), sourceManifest); err != nil {
		return nil, fmt.Errorf("the connector and environment contracts: %w", err)
	}
	return definitions, nil
}

// verifyDeclaredDefinitions requires exactly one definition per Flow source
// that dex-app.yaml declares.
func verifyDeclaredDefinitions(manifestJSON string, definitions []appmanifest.FlowDefinition) error {
	var manifest appmanifest.Manifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("decode dex-app.yaml: %w", err)
	}
	declared := manifest.DeclaredFlowSources()
	for _, definition := range definitions {
		if !declared[definition.SourcePath] {
			return fmt.Errorf("the build rendered %s, which dex-app.yaml does not declare", definition.SourcePath)
		}
	}
	if len(definitions) != len(declared) {
		return fmt.Errorf("the build rendered %d Flow Definitions for %d declared Flow sources", len(definitions), len(declared))
	}
	return nil
}

// publishPayloads stores the four payloads as immutable objects of owner.
func publishPayloads(ctx context.Context, objects *objectstore.Store, owner artifactOwner, payloads build.Payloads) (ArtifactRefs, error) {
	var refs ArtifactRefs
	prefix := owner.ProjectID + "/releases/" + owner.ReleaseID + "/" + owner.BuildRevisionID + "/"
	for _, item := range []struct {
		name, content, media string
		ref                  *objectstore.ObjectRef
	}{{"flow-definitions.json", payloads.FlowDefinitions, objectstore.MediaTypeJSON, &refs.FlowDefinitions}, {"connector-contract.json", payloads.ConnectorContract, objectstore.MediaTypeJSON, &refs.ConnectorContract}, {"environment-contract.json", payloads.EnvironmentContract, objectstore.MediaTypeJSON, &refs.EnvironmentContract}, {"dex-app.yaml", payloads.Manifest, objectstore.MediaTypeYAML, &refs.Manifest}} {
		ref, err := objects.Put(ctx, prefix+item.name, item.media, []byte(item.content))
		if err != nil {
			return ArtifactRefs{}, err
		}
		*item.ref = ref
	}
	return refs, nil
}

func objectReference(ref objectstore.ObjectRef) *pb.ObjectReference {
	return &pb.ObjectReference{Key: ref.Key, Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType}
}

// readDefinitions reads the Flow Definitions of owner from its immutable bundle.
func readDefinitions(ctx context.Context, objects *objectstore.Store, ref objectstore.ObjectRef, owner artifactOwner) ([]appmanifest.FlowDefinition, error) {
	var bundle definitionBundle
	if err := objects.GetJSON(ctx, ref, &bundle); err != nil {
		return nil, err
	}
	return bundle.definitionsOf(owner)
}

// ReadDefinitions reads the Flow Definitions of a READY build revision.
func ReadDefinitions(ctx context.Context, objects *objectstore.Store, b BuildSnapshot) ([]appmanifest.FlowDefinition, error) {
	if b.Build.Status != Ready {
		return nil, errors.New("release artifacts are not ready")
	}
	return readDefinitions(ctx, objects, b.Build.Artifacts.FlowDefinitions, b.artifacts())
}

// PreparedDefinitions reads the exact Flow Definitions of the Release's
// prepared source. There are none while they are not readable: before the
// source is prepared, when the Release failed, and once it is being removed,
// which a removal's cancellation request starts. A failed build's diagnostic
// snapshot needs no artifact read.
func (c *ProjectReleaseFlowClient) PreparedDefinitions(ctx context.Context, s Snapshot) ([]appmanifest.FlowDefinition, error) {
	if s.Status == Failed || s.Status == Removing || s.Status == Removed || s.PreparedSource.Status != Ready {
		return nil, nil
	}
	return ReadRecordedDefinitions(ctx, c.flow.objects, s.PreparedSource.Artifacts.FlowDefinitions)
}

// ReadRecordedDefinitions reads the Flow Definitions of the bundle at ref,
// which a Flow recorded after it checked the bundle's owner: the source
// preparation of a Release (validatePayloads) or a deployment pinning its
// build's (ReadDefinitions), whose PinnedInputs Live Runs read. The store returns exactly the version and digest of ref,
// so a reader of the recorded reference, which may come from the read model,
// does not compare the owner again.
func ReadRecordedDefinitions(ctx context.Context, objects *objectstore.Store, ref objectstore.ObjectRef) ([]appmanifest.FlowDefinition, error) {
	var bundle definitionBundle
	if err := objects.GetJSON(ctx, ref, &bundle); err != nil {
		return nil, err
	}
	return bundle.definitions()
}
