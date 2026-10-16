// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"
	"errors"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

// PublishAppManifest reads dex-app.yaml at commit and publishes the manifest
// and its connector and environment contracts as immutable source objects of
// the Project. Setup publishes the Project's initial source this way, and the
// Project's Preview its Session's workspace HEAD; neither changes Live.
func PublishAppManifest(ctx context.Context, host repohost.Host, objects *objectstore.Store, projectID, repositoryFullName, commit string) (*pb.AppManifestSnapshot, error) {
	// The commit is a provider or workspace observation, not a value this process derived.
	if !flowid.IsCommitSHA(commit) {
		return nil, errors.New("exact lowercase source commit is required")
	}
	contents, err := host.ReadFile(ctx, repositoryFullName, commit, "dex-app.yaml")
	if err != nil {
		return nil, err
	}
	manifest, err := appmanifest.ExtractAppManifest(commit, contents)
	if err != nil {
		return nil, err
	}
	connectors, environment := appmanifest.CanonicalRequirementsJSON(manifest.Connectors, manifest.Environment)
	prefix := "projects/" + projectID + "/source/" + commit + "/"
	for _, object := range []struct {
		key    string
		data   []byte
		target **pb.ObjectReference
	}{
		{"manifest.json", contents, &manifest.ManifestObject},
		{"connector-contract.json", connectors, &manifest.ConnectorContractObject},
		{"environment-contract.json", environment, &manifest.EnvironmentContractObject},
	} {
		ref, err := objects.Put(ctx, prefix+object.key, objectstore.MediaTypeJSON, object.data)
		if err != nil {
			return nil, err
		}
		*object.target = &pb.ObjectReference{Key: ref.Key, Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType}
	}
	return manifest, nil
}
