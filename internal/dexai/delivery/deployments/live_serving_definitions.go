// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/superdurable/superversev2/internal/dexai/projects/infra"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

// DefinitionPublication is the generation's published Flow Definition bundle,
// stored once per Release build in the project's application objects. Its
// manifest is what Activate makes the active one.
type DefinitionPublication struct {
	BuildRevisionID    string                             `json:"buildRevisionId"`
	ProjectID          string                             `json:"projectId"`
	ReleaseID          string                             `json:"releaseId"`
	SourceBundleDigest string                             `json:"sourceBundleDigest"`
	Manifest           appmanifest.FlowDefinitionManifest `json:"manifest"`
}

// publicationArtifactID is the stable artifact ID of one Release build's
// published bundle. Its UUIDv5 name is a stored identity and must not change.
func publicationArtifactID(s Snapshot) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("superverse-v2/dex-web/"+s.ProjectID+"/"+publicationIdentity(s))).String()
}

func publicationIdentity(s Snapshot) string { return s.ReleaseID + "/" + s.BuildRevisionID }

func publicationKey(s Snapshot) string { return "publications/" + publicationIdentity(s) + ".json" }

// publishFlowDefinitions publishes the build's validated Flow Definitions as
// immutable objects of the project's storage without activating them.
func publishFlowDefinitions(ctx context.Context, storage infra.Storage, s Snapshot, definitions []appmanifest.FlowDefinition) error {
	objects, err := storage.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		return err
	}
	manifest := appmanifest.NewFlowDefinitionManifest(publicationArtifactID(s), definitions)
	for i, definition := range definitions {
		if _, err = objects.Put(ctx, manifest.GraphKey(i), definition.Graph, true); err != nil {
			return fmt.Errorf("publish the definition of %s: %w", definition.SourcePath, err)
		}
	}
	publication := DefinitionPublication{BuildRevisionID: s.BuildRevisionID, ProjectID: s.ProjectID, ReleaseID: s.ReleaseID, SourceBundleDigest: s.Inputs.BundleDigest, Manifest: manifest}
	if _, err = objects.Put(ctx, publicationKey(s), comm.Must(json.Marshal(publication)), true); err != nil {
		return fmt.Errorf("publish the definition publication %s: %w", publicationKey(s), err)
	}
	return nil
}

// ReadPublication reads the build's publication from the project's storage and
// requires it to be this exact build's.
func ReadPublication(ctx context.Context, storage infra.Storage, s Snapshot) (DefinitionPublication, error) {
	objects, err := storage.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		return DefinitionPublication{}, err
	}
	key := publicationKey(s)
	data, _, err := objects.Get(ctx, key, "")
	if err != nil {
		return DefinitionPublication{}, fmt.Errorf("read the definition publication %s: %w", key, err)
	}
	var publication DefinitionPublication
	if err = json.Unmarshal(data, &publication); err != nil {
		return DefinitionPublication{}, fmt.Errorf("decode the definition publication %s: %w", key, err)
	}
	if publication.BuildRevisionID != s.BuildRevisionID || publication.SourceBundleDigest != s.Inputs.BundleDigest ||
		publication.ProjectID != s.ProjectID || publication.ReleaseID != s.ReleaseID || publication.Manifest.ReleaseID != publicationArtifactID(s) || publication.Manifest.DefinitionCount < 1 {
		return DefinitionPublication{}, fmt.Errorf("the definition publication %s names build %s with bundle %s and %d definitions, not build %s with bundle %s", key, publication.BuildRevisionID, publication.SourceBundleDigest, publication.Manifest.DefinitionCount, s.BuildRevisionID, s.Inputs.BundleDigest)
	}
	return publication, nil
}

// PublishFlowDefinitionsForTest is publishFlowDefinitions for the tests.
func PublishFlowDefinitionsForTest(ctx context.Context, storage infra.Storage, s Snapshot, definitions []appmanifest.FlowDefinition) error {
	return publishFlowDefinitions(ctx, storage, s, definitions)
}
