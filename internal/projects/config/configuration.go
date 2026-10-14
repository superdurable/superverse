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
	"errors"
	"log/slog"
	"net/http"
	"sync"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost"
)

// The API and the calling Flows classify configuration failures by these
// errors; infra.ErrAccessDenied rejects a scope or object outside the
// accepted resources.
var (
	ErrInvalid  = errors.New("invalid project configuration target")
	ErrNotReady = errors.New("project configuration requires review")
	ErrConflict = errors.New("project configuration revision changed")
	// ErrRevisionChanged is the ErrConflict of a Preview start whose
	// configuration head moved past the revision it applies.
	ErrRevisionChanged = errors.New("project configuration changed after the Preview start was requested")
	errNotFound        = errors.New("project configuration does not exist")
	// ErrInvalidDefinitions reports Flow Definitions the configuration host
	// rejects; registering the same source again fails the same way.
	ErrInvalidDefinitions = errors.New("flow definitions are invalid")
)

// Configuration coordinates the configuration of every project scope. The
// composition root assigns its dependencies.
type Configuration struct {
	Projects *projects.ProjectFlowClient
	Releases *releases.ProjectReleaseFlowClient
	Objects  *objectstore.Store
	Storage  infra.Storage
	Preview  PreviewReader
	// Shared reaches the TeamSpace connections that connection names select.
	Shared confighost.SharedConnections
	// ReleaseCandidates is where the release candidates of the server's
	// connector contributions are downloaded from.
	ReleaseCandidates string
	// Logger records the reads of other Projects that a warning skipped.
	Logger *slog.Logger

	hostsMu sync.Mutex
	hosts   map[string]*pooledHost
	closed  bool
}

// PreviewReader reads the Project's Preview: the source of its configuration,
// the AppManifest the Preview prepared for the Session's HEAD, from stored
// state alone, and the configuration version it last applied, the zero ref
// before the first Apply or without a Preview. The configuration lives in the
// Project's Live storage.
type PreviewReader interface {
	ReadPreviewTarget(ctx context.Context, projectID string) (*pb.AppManifestSnapshot, error)
	ReadAppliedPreviewConfiguration(ctx context.Context, projectID string) (objectstore.ObjectRef, error)
}

// Metadata is the safe metadata of a configuration: the AppManifest it is
// edited for and its revision.
type Metadata struct {
	AppManifestRevision       uint64                `json:"appManifestRevision"`
	ConfigurationRevision     uint64                `json:"configurationRevision"`
	SourceCommit              string                `json:"sourceCommit"`
	ManifestDigest            string                `json:"manifestDigest"`
	ConnectorContractDigest   string                `json:"connectorContractDigest"`
	EnvironmentContractDigest string                `json:"environmentContractDigest"`
	Scope                     connectorconfig.Scope `json:"scope"`
}

// matches reports whether metadata is for exactly manifest.
func (metadata Metadata) matches(manifest *pb.AppManifestSnapshot) bool {
	return metadata.AppManifestRevision != 0 && metadata.SourceCommit == manifest.SourceCommit && metadata.ManifestDigest == manifest.ManifestDigest &&
		metadata.ConnectorContractDigest == manifest.ConnectorContractDigest && metadata.EnvironmentContractDigest == manifest.EnvironmentContractDigest
}

// ValidationResult is a validated configuration revision with the exact
// frozen version it resolves to.
type ValidationResult struct {
	Metadata
	Status                        string                      `json:"status"`
	Snapshot                      connectorconfig.SnapshotRef `json:"snapshot"`
	SnapshotConfigurationRevision uint64                      `json:"snapshotConfigurationRevision"`
}

// liveScope is the project's Live scope.
func liveScope(project projects.Project) infra.ScopeRef {
	return infra.ScopeRef{ProjectID: project.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: project.Storage}
}

// ReadMetadata reads the metadata of the scope's configuration head or, with
// a sourceCommit, of that commit's prepared source.
func (c *Configuration) ReadMetadata(ctx context.Context, scope infra.ScopeRef, sourceCommit string) (Metadata, error) {
	var metadata Metadata
	err := c.invokeHost(ctx, scope, sourceCommit, http.MethodGet, configurationRoute, nil, &metadata)
	return metadata, err
}

// RegisterAppManifest makes manifest the scope's AppManifest head unless it
// already is: Setup registers the Live AppManifest, and a Session the
// AppManifest of its Preview checkout.
func (c *Configuration) RegisterAppManifest(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot) error {
	if manifest == nil || manifest.ManifestObject == nil {
		return ErrNotReady
	}
	current, err := c.ReadMetadata(ctx, scope, "")
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	if err == nil && current.matches(manifest) {
		return nil
	}
	contents, err := c.readManifest(ctx, manifest)
	if err != nil {
		return err
	}
	registration := confighost.ManifestRegistration{
		ExpectedRevision: current.AppManifestRevision, SourceCommit: manifest.SourceCommit, ManifestDigest: manifest.ManifestDigest, Manifest: string(contents),
	}
	var registered Metadata
	if err = c.invokeHost(ctx, scope, "", http.MethodPut, manifestRoute, registration, &registered); err != nil {
		// An uncertain storage write may still have registered exactly this
		// manifest.
		if observed, readErr := c.ReadMetadata(ctx, scope, ""); readErr == nil && observed.matches(manifest) {
			return nil
		}
		return err
	}
	if !registered.matches(manifest) {
		return ErrNotReady
	}
	return nil
}

// readManifest reads the exact AppManifest bytes that manifest pins. The
// store verifies the object's digest; manifest's own digest must name the
// same bytes.
func (c *Configuration) readManifest(ctx context.Context, manifest *pb.AppManifestSnapshot) ([]byte, error) {
	object := manifest.ManifestObject
	contents, err := c.Objects.Get(ctx, objectstore.ObjectRef{Key: object.Key, Version: object.Version, Digest: object.Digest, MediaType: object.MediaType})
	if err != nil {
		return nil, err
	}
	if objectstore.Digest(contents) != manifest.ManifestDigest {
		return nil, ErrNotReady
	}
	return contents, nil
}

// The tests name these values.
var (
	ErrNotFoundForTest = errNotFound
)
