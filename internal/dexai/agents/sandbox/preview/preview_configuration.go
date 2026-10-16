// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// PreviewConfiguration binds a Project's Preview to its validated
// configuration and its storage grants in the Project's Live storage.
type PreviewConfiguration struct {
	projects      *projects.ProjectFlowClient
	host          repohost.Host
	objects       *objectstore.Store
	configuration *projectconfig.Configuration
	storage       infra.Storage
	grants        infra.Grants
	cleanup       infra.Cleanup
}

func NewPreviewConfiguration(p *projects.ProjectFlowClient, host repohost.Host, objects *objectstore.Store, configuration *projectconfig.Configuration, storage infra.Storage, grants infra.Grants, cleanup infra.Cleanup) *PreviewConfiguration {
	return &PreviewConfiguration{projects: p, host: host, objects: objects, configuration: configuration, storage: storage, grants: grants, cleanup: cleanup}
}

// previewKeys are the names of the Project's Preview.
func previewKeys(id sandbox.Identity) infra.PreviewKeys {
	return infra.PreviewKeysOf(id.ProjectID)
}

// scopeRef is the Project's Preview configuration in its Live storage of
// namespaceUID and bucket.
func (p *PreviewConfiguration) scopeRef(id sandbox.Identity, namespaceUID, bucket string) infra.ScopeRef {
	return infra.ScopeRef{ProjectID: id.ProjectID, Scope: previewKeys(id).Scope, Storage: p.storage.LiveStorage(id.ProjectID, namespaceUID, bucket)}
}

// previewScope reads the Project and returns it with its Preview
// configuration in the Live storage the Project records.
func (p *PreviewConfiguration) previewScope(ctx context.Context, id sandbox.Identity) (infra.ScopeRef, projects.Project, error) {
	project, err := p.projects.Read(ctx, id.ProjectID)
	if err != nil {
		return infra.ScopeRef{}, project, fmt.Errorf("read the Project: %w", err)
	}
	live := project.Storage
	return p.scopeRef(id, live.Boundary.UID, live.StorageBucket), project, nil
}

// registerPreviewAppManifest checks that main reaches commit, as a Release
// does, publishes its AppManifest and registers it with the Project's Preview
// configuration scope, and returns it with the commit's version.
func (p *PreviewConfiguration) registerPreviewAppManifest(ctx context.Context, s Snapshot, commit string) (*pb.AppManifestSnapshot, repohost.CommitVersion, error) {
	scope, project, err := p.previewScope(ctx, s.Identity)
	if err != nil {
		return nil, repohost.CommitVersion{}, err
	}
	if project.ProjectStatus != projects.StatusReady {
		return nil, repohost.CommitVersion{}, fmt.Errorf("%w: the Project is %s", ErrClosed, project.ProjectStatus)
	}
	repository := project.RepositoryBinding.FullName
	if err = p.host.ValidateReleaseCommit(ctx, repository, releaseBranch, commit); err != nil {
		return nil, repohost.CommitVersion{}, fmt.Errorf("check that %s reaches %s: %w", releaseBranch, commit, err)
	}
	version, err := p.host.DescribeCommit(ctx, repository, commit)
	if err != nil {
		return nil, repohost.CommitVersion{}, fmt.Errorf("describe %s: %w", commit, err)
	}
	manifest, err := projects.PublishAppManifest(ctx, p.host, p.objects, s.Identity.ProjectID, repository, commit)
	if err != nil {
		return nil, repohost.CommitVersion{}, fmt.Errorf("publish the AppManifest of %s at %s: %w", repository, commit, err)
	}
	if err = p.configuration.RegisterAppManifest(ctx, scope, manifest); err != nil {
		return nil, repohost.CommitVersion{}, fmt.Errorf("register the AppManifest of %s: %w", commit, err)
	}
	return manifest, version, nil
}

// releaseBranch is the only branch whose commits the Preview prepares, as a
// Release builds only commits it reaches.
const releaseBranch = "main"

// readPreviewSourceVersion reads the Git-owned identity before compilation,
// without publishing a manifest or changing the configuration head.
func (p *PreviewConfiguration) readPreviewSourceVersion(ctx context.Context, id sandbox.Identity, commit string) (repohost.CommitVersion, error) {
	project, err := p.projects.Read(ctx, id.ProjectID)
	if err != nil {
		return repohost.CommitVersion{}, fmt.Errorf("read the Project: %w", err)
	}
	if project.ProjectStatus != projects.StatusReady {
		return repohost.CommitVersion{}, fmt.Errorf("%w: the Project is %s", ErrClosed, project.ProjectStatus)
	}
	repository := project.RepositoryBinding.FullName
	if err = p.host.ValidateReleaseCommit(ctx, repository, releaseBranch, commit); err != nil {
		return repohost.CommitVersion{}, fmt.Errorf("check that %s reaches %s: %w", releaseBranch, commit, err)
	}
	version, err := p.host.DescribeCommit(ctx, repository, commit)
	if err != nil {
		return repohost.CommitVersion{}, fmt.Errorf("describe %s: %w", commit, err)
	}
	return version, nil
}

// preparePreview validates the configuration of the prepared commit and pins
// it by immutable references, with the Live storage that holds it, the
// Sandbox principal and the grant that serves it. An applying start freezes
// exactly the revision its command names, or without one, the first
// deployment's, the head's; any other start revalidates the applied version
// and freezes nothing. A grant the new pin drops is revoked first.
func (p *PreviewConfiguration) preparePreview(ctx context.Context, s Snapshot) (PreviewConfigurationRef, error) {
	scope, _, err := p.previewScope(ctx, s.Identity)
	if err != nil {
		return PreviewConfigurationRef{}, err
	}
	commit := s.PreparedSource.SourceCommit
	metadata, err := p.configuration.ReadMetadata(ctx, scope, commit)
	if err != nil {
		return PreviewConfigurationRef{}, err
	}
	var result projectconfig.ValidationResult
	var object objectstore.ObjectRef
	if s.appliesConfiguration() {
		metadata.ConfigurationRevision = cmp.Or(s.ActiveCommand.ConfigurationRevision, metadata.ConfigurationRevision)
		if result, err = p.configuration.ValidatePreparedPreview(ctx, scope, s.AppManifest, metadata); err != nil {
			return PreviewConfigurationRef{}, err
		}
		if object, err = infra.ScopedObjectRef(scope, result.Snapshot); err != nil {
			return PreviewConfigurationRef{}, err
		}
	} else {
		object = s.Applied.Object()
		if result, err = p.configuration.ValidateAppliedPreview(ctx, scope, s.AppManifest, metadata, object, s.Applied.ConfigurationRevision); err != nil {
			return PreviewConfigurationRef{}, err
		}
	}
	next := PreviewConfigurationRef{
		ObjectKey: object.Key, ObjectVersion: object.Version, Digest: object.Digest, SourceCommit: commit,
		ManifestDigest: result.ManifestDigest, ConnectorContractDigest: result.ConnectorContractDigest, EnvironmentContractDigest: result.EnvironmentContractDigest,
		// The frozen document's revision, which a settings refresh may have
		// advanced past the one applied.
		ConfigurationRevision: cmp.Or(result.SnapshotConfigurationRevision, result.ConfigurationRevision), AppManifestRevision: result.AppManifestRevision, Generation: s.Generation,
		StorageBoundaryUID: scope.Storage.Boundary.UID, StorageBucket: scope.Storage.StorageBucket,
		Principal: infra.Principal{Boundary: s.Resources.Boundary.InfraRef(), Identity: s.Resources.Identity.InfraRef()},
	}
	if dropped := s.Configuration.grantFor(&next, s.Preview.Revision, previewKeys(s.Identity).GrantWorkloadID); dropped.Ref != "" {
		if err = p.grants.RevokeWorkload(ctx, p.previewGrant(s, dropped)); err != nil {
			return PreviewConfigurationRef{}, fmt.Errorf("revoke the replaced Preview grant %s: %w", dropped.Ref, err)
		}
	}
	return next, nil
}

// checkRequirements lists every gap between what the prepared commit's
// AppManifest declares and the configuration the active start runs with:
// the head for an applying start, else the applied version. Names only.
func (p *PreviewConfiguration) checkRequirements(ctx context.Context, s Snapshot) ([]appmanifest.RequirementGap, error) {
	scope, _, err := p.previewScope(ctx, s.Identity)
	if err != nil {
		return nil, err
	}
	if s.appliesConfiguration() {
		return p.configuration.CheckRequirements(ctx, scope, s.AppManifest)
	}
	return p.configuration.CheckAppliedRequirements(ctx, scope, s.AppManifest, s.Applied.Object())
}

// materializePreview issues the storage grant of the pinned configuration and
// returns the application environment it grants; ready is false while the
// grant is still reconciling.
func (p *PreviewConfiguration) materializePreview(ctx context.Context, s Snapshot) (map[string]string, bool, error) {
	environment, err := p.grants.GrantEnvironment(ctx, p.previewGrant(s, s.Configuration.Grant))
	if errors.Is(err, infra.ErrPending) {
		return nil, false, nil
	}
	return environment, err == nil, err
}

// revokeSupersededGrant revokes the grant the running application replaced;
// done is false while the revocation is still reconciling.
func (p *PreviewConfiguration) revokeSupersededGrant(ctx context.Context, s Snapshot) (bool, error) {
	if s.Configuration.SupersededGrant.Ref == "" {
		return true, nil
	}
	return reconciled(p.grants.RevokeWorkload(ctx, p.previewGrant(s, s.Configuration.SupersededGrant)))
}

// revokePreview revokes the storage grant of the pinned configuration and the
// one it superseded; done is false while a revocation is still reconciling.
func (p *PreviewConfiguration) revokePreview(ctx context.Context, s Snapshot) (bool, error) {
	if done, err := p.revokeSupersededGrant(ctx, s); err != nil || !done {
		return false, err
	}
	if s.Configuration.Grant.Ref == "" {
		return true, nil
	}
	return reconciled(p.grants.RevokeWorkload(ctx, p.previewGrant(s, s.Configuration.Grant)))
}

// DeletePreviewData deletes the Project's Preview data: its storage grants
// bound to its Sandbox namespace, its configuration in the Project's Live
// storage and its prepared source objects; done is false while that is still
// reconciling. The deleting Project's release calls it with the Sandbox
// namespace name, with no Preview snapshot.
func (p *PreviewConfiguration) DeletePreviewData(ctx context.Context, projectID, namespace string) (bool, error) {
	project, err := p.projects.Read(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("read the Project: %w", err)
	}
	live := project.Storage
	done, err := reconciled(p.cleanup.DestroyPreviewGrants(ctx, projectID, live, namespace))
	if err != nil || !done {
		return false, err
	}
	if done, err = reconciled(p.cleanup.DestroyPreviewConfiguration(ctx, projectID, live)); err != nil || !done {
		return false, err
	}
	done, err = p.objects.DeletePrefix(ctx, infra.PreviewKeysOf(projectID).SourcePrefix)
	if err != nil {
		return false, fmt.Errorf("delete the prepared Preview sources: %w", err)
	}
	return done, nil
}

// reconciled turns infra's ErrPending into not yet done.
func reconciled(err error) (bool, error) {
	if errors.Is(err, infra.ErrPending) {
		return false, nil
	}
	return err == nil, err
}

// previewGrant is the storage workload of one grant generation of the pinned
// configuration, bound to the principal the configuration pinned.
func (p *PreviewConfiguration) previewGrant(s Snapshot, grant PreviewGrant) infra.Workload {
	config, keys := s.Configuration, previewKeys(s.Identity)
	return infra.Workload{
		ProjectID: s.Identity.ProjectID, WorkloadID: keys.GrantWorkloadID, Scope: keys.Scope, Generation: grant.Generation,
		ConfigurationRevision: config.ConfigurationRevision, GrantID: grant.Ref,
		Storage:       p.storage.LiveStorage(s.Identity.ProjectID, config.StorageBoundaryUID, config.StorageBucket),
		Configuration: objectstore.ObjectRef{Key: config.ObjectKey, Version: config.ObjectVersion, Digest: config.Digest, MediaType: objectstore.MediaTypeJSON},
		Principal:     &config.Principal,
	}
}
