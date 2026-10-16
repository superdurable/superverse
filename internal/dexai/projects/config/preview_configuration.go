// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
)

// previewItemsRoute is the internal route of the Preview's item diff.
const previewItemsRoute = "/api/v2/project-configuration/preview-items"

// PreviewItems returns the items of the Project's Preview configuration head
// compared with the version the Preview last applied, and how many differ.
func (c *Configuration) PreviewItems(ctx context.Context, actor, projectID string) (confighost.PreviewItemsReport, error) {
	target, err := c.resolveTarget(ctx, actor, &pb.ProjectConfigurationTarget{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW}, ReadAccess)
	if err != nil {
		return confighost.PreviewItemsReport{}, err
	}
	request := confighost.PreviewItemsRequest{ManifestDigest: target.manifest.ManifestDigest}
	applied, err := c.Preview.ReadAppliedPreviewConfiguration(ctx, projectID)
	if err != nil {
		return confighost.PreviewItemsReport{}, err
	}
	if applied.Key != "" {
		snapshot, snapshotErr := infra.SnapshotRefFor(target.scope, applied)
		if snapshotErr != nil {
			return confighost.PreviewItemsReport{}, snapshotErr
		}
		request.Applied = &snapshot
	}
	var report confighost.PreviewItemsReport
	err = c.invokeHost(ctx, target.scope, target.manifest.SourceCommit, http.MethodPost, previewItemsRoute, request, &report)
	return report, err
}

// appliedPreview is the non-secret document of the version the Project's
// Preview last applied, with its configuration revision, or nil before the
// first Apply. A Live host cannot read Preview's objects, so it is read here
// at its exact version and digest and handed over without its secret
// references and credential broker.
func (c *Configuration) appliedPreview(ctx context.Context, project projects.Project) (*connectorconfig.Configuration, error) {
	applied, err := c.Preview.ReadAppliedPreviewConfiguration(ctx, project.ProjectID)
	if err != nil || applied.Key == "" {
		return nil, err
	}
	data, err := c.Storage.ReadConfiguration(ctx, previewScope(project), applied)
	if err != nil {
		return nil, fmt.Errorf("read the applied Preview configuration of Project %s: %w", project.ProjectID, err)
	}
	var document connectorconfig.Configuration
	if err = json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode the applied Preview configuration of Project %s: %w", project.ProjectID, err)
	}
	return withoutSecrets(document), nil
}

// previewScope is the project's Preview scope, whose configuration lives in
// the Project's Live storage.
func previewScope(project projects.Project) infra.ScopeRef {
	return infra.ScopeRef{ProjectID: project.ProjectID, Scope: infra.PreviewKeysOf(project.ProjectID).Scope, Storage: project.Storage}
}

// withoutSecrets is document without its secret environment references and
// its credential broker, which never cross scopes.
func withoutSecrets(document connectorconfig.Configuration) *connectorconfig.Configuration {
	document.CredentialBroker = nil
	document.Environment = maps.Clone(document.Environment)
	maps.DeleteFunc(document.Environment, func(_ string, value connectorconfig.EnvironmentValue) bool { return value.SecretRef != nil })
	return &document
}
