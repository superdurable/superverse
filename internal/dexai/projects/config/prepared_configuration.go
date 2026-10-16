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
	"net/http"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
)

// A prepared source is a source commit's admitted AppManifest and Flow
// Definitions, registered once per scope. Live publishes a version by
// building its frozen copy from the next-publish layer, which a build
// freezes; Preview validates and freezes its canonical head.

// PreparedConfigurationSource is a registered prepared source: its metadata
// and the definition revision of its Flow Definitions.
type PreparedConfigurationSource struct {
	Metadata
	DefinitionRevision string `json:"definitionRevision"`
}

// RegisterPreparedSource registers the prepared source of manifest's commit
// with its Flow Definition graphs. A retry returns the registered source.
// Registering never publishes a serving catalog or changes a running
// configuration.
func (c *Configuration) RegisterPreparedSource(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, definitions []json.RawMessage) (PreparedConfigurationSource, error) {
	if manifest == nil || manifest.ManifestObject == nil || len(definitions) == 0 {
		return PreparedConfigurationSource{}, ErrNotReady
	}
	contents, err := c.readManifest(ctx, manifest)
	if err != nil {
		return PreparedConfigurationSource{}, err
	}
	registration := confighost.SourceRegistration{
		ManifestRegistration: confighost.ManifestRegistration{SourceCommit: manifest.SourceCommit, ManifestDigest: manifest.ManifestDigest, Manifest: string(contents)},
		Definitions:          definitions,
	}
	var source PreparedConfigurationSource
	if err = c.invokeHost(ctx, scope, "", http.MethodPut, sourcesRoute, registration, &source); err != nil {
		return PreparedConfigurationSource{}, err
	}
	if !source.matches(manifest) || source.DefinitionRevision == "" {
		return PreparedConfigurationSource{}, ErrNotReady
	}
	return source, nil
}

// ValidatePreparedPreview validates the configuration revision that metadata,
// read for the prepared source of manifest's commit, names, and freezes it for
// the Preview to start with. A head past that revision is ErrRevisionChanged,
// unless it is the settings refresh an earlier attempt of the same freeze wrote.
func (c *Configuration) ValidatePreparedPreview(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, metadata Metadata) (ValidationResult, error) {
	return c.validatePrepared(ctx, scope, manifest, metadata, metadata.ConfigurationRevision, false)
}

// ValidateAppliedPreview validates the exact version a Preview applied,
// applied with its configuration revision, against the prepared source of
// manifest's commit, whose metadata the caller read. It freezes nothing.
func (c *Configuration) ValidateAppliedPreview(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, metadata Metadata, applied objectstore.ObjectRef, revision uint64) (ValidationResult, error) {
	if !metadata.matches(manifest) {
		return ValidationResult{}, ErrNotReady
	}
	snapshot, err := infra.SnapshotRefFor(scope, applied)
	if err != nil {
		return ValidationResult{}, err
	}
	return c.validate(ctx, scope, manifest, confighost.ValidationRequest{
		Snapshot: &snapshot, AppManifestRevision: metadata.AppManifestRevision, ManifestDigest: metadata.ManifestDigest, ConfigurationRevision: revision,
	})
}

// CheckRequirements lists every requirement of the prepared source of
// manifest's commit that the configuration of scope does not meet, at once and
// by name only: a missing connection, authorization, trigger binding or
// environment variable. It reads only. A connector release that cannot be
// resolved is no gap but an error, as is storage that cannot be read.
func (c *Configuration) CheckRequirements(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot) ([]appmanifest.RequirementGap, error) {
	return c.checkRequirements(ctx, scope, manifest, confighost.RequirementsRequest{ManifestDigest: manifest.ManifestDigest})
}

// CheckAppliedRequirements lists the gaps of the exact version a Preview
// applied, as CheckRequirements does for the configuration head.
func (c *Configuration) CheckAppliedRequirements(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, applied objectstore.ObjectRef) ([]appmanifest.RequirementGap, error) {
	snapshot, err := infra.SnapshotRefFor(scope, applied)
	if err != nil {
		return nil, err
	}
	return c.checkRequirements(ctx, scope, manifest, confighost.RequirementsRequest{ManifestDigest: manifest.ManifestDigest, Snapshot: &snapshot})
}

func (c *Configuration) checkRequirements(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, request confighost.RequirementsRequest) ([]appmanifest.RequirementGap, error) {
	var report confighost.RequirementsReport
	if err := c.invokeHost(ctx, scope, manifest.SourceCommit, http.MethodPost, requirementsRoute, request, &report); err != nil {
		return nil, err
	}
	if report.Ready != (len(report.Gaps) == 0) {
		return nil, fmt.Errorf("the configuration host reported readiness %t with %d gaps", report.Ready, len(report.Gaps))
	}
	return report.Gaps, nil
}

// validatePrepared validates revision of the draft of the prepared source of
// manifest's commit, whose metadata the caller read. With checkOnly it only
// reports readiness; otherwise Preview freezes the revision, while a Live
// draft is frozen only for a build.
func (c *Configuration) validatePrepared(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, metadata Metadata, revision uint64, checkOnly bool) (ValidationResult, error) {
	if !metadata.matches(manifest) {
		return ValidationResult{}, ErrNotReady
	}
	return c.validate(ctx, scope, manifest, confighost.ValidationRequest{
		CheckOnly: checkOnly, AppManifestRevision: metadata.AppManifestRevision, ManifestDigest: metadata.ManifestDigest, ConfigurationRevision: revision,
	})
}

// validate sends request for the prepared source of manifest's commit. The
// result must be READY for exactly manifest at the requested revision.
func (c *Configuration) validate(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot, request confighost.ValidationRequest) (ValidationResult, error) {
	var result ValidationResult
	if err := c.invokeHost(ctx, scope, manifest.SourceCommit, http.MethodPost, validateRoute, request, &result); err != nil {
		return ValidationResult{}, err
	}
	if !result.matches(manifest) {
		return ValidationResult{}, ErrNotReady
	}
	if result.Status != "READY" || result.ConfigurationRevision != request.ConfigurationRevision {
		return ValidationResult{}, ErrConflict
	}
	return result, nil
}

// FreezeReleaseConfiguration freezes draftRevision of the Live draft of
// manifest's commit for the build buildID and returns the frozen version. A
// retry of the same build returns that version even after the draft changed.
func (c *Configuration) FreezeReleaseConfiguration(ctx context.Context, project projects.Project, manifest *pb.AppManifestSnapshot, buildID string, draftRevision uint64) (releases.ReadyConfiguration, error) {
	if manifest == nil || buildID == "" {
		return releases.ReadyConfiguration{}, ErrInvalid
	}
	scope := liveScope(project)
	result, err := c.validate(ctx, scope, manifest, confighost.ValidationRequest{
		RequestID: buildID, AppManifestRevision: confighost.PreparedAppManifestRevision, ManifestDigest: manifest.ManifestDigest, ConfigurationRevision: draftRevision,
	})
	if err != nil {
		return releases.ReadyConfiguration{}, err
	}
	if result.SnapshotConfigurationRevision == 0 {
		return releases.ReadyConfiguration{}, ErrConflict
	}
	object, err := infra.ScopedObjectRef(scope, result.Snapshot)
	if err != nil {
		return releases.ReadyConfiguration{}, err
	}
	return releases.ReadyConfiguration{
		Object: object, ReleaseID: manifest.SourceCommit, ConfigurationRevision: result.SnapshotConfigurationRevision, AppManifestRevision: result.AppManifestRevision,
		ManifestDigest: result.ManifestDigest, ConnectorContractDigest: result.ConnectorContractDigest, EnvironmentContractDigest: result.EnvironmentContractDigest,
	}, nil
}

// VerifyReleaseConfiguration verifies, before a Deployment uses it, that a
// Release's frozen configuration is readable at its exact version and still
// READY for the Release's prepared source.
func (c *Configuration) VerifyReleaseConfiguration(ctx context.Context, project projects.Project, ready releases.ReadyConfiguration) error {
	if project.ProjectStatus != projects.StatusReady || ready.ReleaseID == "" {
		return ErrNotReady
	}
	scope := liveScope(project)
	// The frozen version must be the scope's configuration head, readable at
	// its exact version with bytes that match its digest.
	readCtx, cancelRead := context.WithTimeout(ctx, hostRequestTimeout)
	_, err := c.Storage.ReadConfiguration(readCtx, scope, ready.Object)
	cancelRead()
	if err != nil {
		return err
	}
	snapshot, err := infra.SnapshotRefFor(scope, ready.Object)
	if err != nil {
		return err
	}
	request := confighost.ValidationRequest{
		Snapshot: &snapshot, AppManifestRevision: ready.AppManifestRevision, ManifestDigest: ready.ManifestDigest,
		ConfigurationRevision: ready.ConfigurationRevision,
	}
	var result ValidationResult
	// FreezeReleaseConfiguration records the source commit as the Release ID.
	if err = c.invokeHost(ctx, scope, ready.ReleaseID, http.MethodPost, validateRoute, request, &result); err != nil {
		return err
	}
	if result.Status != "READY" || result.Snapshot != snapshot || result.ConfigurationRevision != ready.ConfigurationRevision || result.ManifestDigest != ready.ManifestDigest {
		return ErrNotReady
	}
	return nil
}
