// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
)

// AppManifestRegistrar registers Setup's manifest with the Project's
// configuration owner, which this package cannot import.
type AppManifestRegistrar interface {
	RegisterAppManifest(ctx context.Context, scope infra.ScopeRef, manifest *pb.AppManifestSnapshot) error
}

// admitSetup registers the Setup as a child of its Project, which must still
// be creating and own exactly this Setup.
func (f *ProjectSetupFlow) admitSetup(ctx context.Context, s Setup) error {
	if _, err := f.creatingProject(ctx, s); err != nil {
		return err
	}
	return f.Client.AdmitChild(ctx, s.ProjectID, projects.ChildSetup, s.FlowID())
}

// creatingProject reads the Project and checks that it still waits for this
// Setup; any later status ends the Setup's admission.
func (f *ProjectSetupFlow) creatingProject(ctx context.Context, s Setup) (projects.Project, error) {
	project, err := f.Client.Read(ctx, s.ProjectID)
	if err != nil {
		return projects.Project{}, fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if project.SetupFlowID != s.FlowID() {
		return projects.Project{}, fmt.Errorf("project %s belongs to Setup %s", s.ProjectID, project.SetupFlowID)
	}
	if project.ProjectStatus != projects.StatusCreating {
		return projects.Project{}, fmt.Errorf("%w: Project %s is %s", projects.ErrChildAdmissionClosed, s.ProjectID, project.ProjectStatus)
	}
	return project, nil
}

// repositoryIdentity names this Setup for the repository host. The repository's
// name carries a slug of the Project's name, which the Setup does not keep: the
// caller reads the immutable name from the Project, so a Setup that started
// before the name entered the repository's name needs no new Attribute.
func (s Setup) repositoryIdentity(project projects.Project) repohost.SetupIdentity {
	return repohost.SetupIdentity{ProjectID: s.ProjectID, ProjectName: project.Name, SetupID: s.SetupID}
}

// createManagedRepository creates or recovers this Setup's private repository
// under the name its Project's name and ID give it.
func (f *ProjectSetupFlow) createManagedRepository(ctx context.Context, s Setup) (projects.RepositoryBindingRef, error) {
	project, err := f.Client.Read(ctx, s.ProjectID)
	if err != nil {
		return projects.RepositoryBindingRef{}, fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	repository, err := f.Host.ManagedRepository(ctx, s.repositoryIdentity(project))
	if err != nil {
		return projects.RepositoryBindingRef{}, err
	}
	// Refusing an archived repository keeps a released RepositoryBindingFlow from
	// being claimed again: once its closed execution passes retention, Claim would
	// start the same FlowID afresh (see the repobinding README).
	if repository.Archived || repository.ID <= 0 || (s.Repository.RepositoryID > 0 && repository.ID != s.Repository.RepositoryID) {
		return projects.RepositoryBindingRef{}, fmt.Errorf("repository %s is archived or changed identity from %d to %d", repository.FullName, s.Repository.RepositoryID, repository.ID)
	}
	return projects.RepositoryBindingRef{RepositoryID: repository.ID, InstallationID: f.Host.InstallationID(), FullName: repository.FullName, URL: repository.CloneURL, DefaultBranch: "main"}, nil
}

// initializeTemplate pushes the accepted embedded template and returns its commit.
func (f *ProjectSetupFlow) initializeTemplate(ctx context.Context, s Setup) (string, error) {
	return f.Host.InitializeTemplate(ctx, repohost.Repository{ID: s.Repository.RepositoryID, FullName: s.Repository.FullName, DefaultBranch: s.Repository.DefaultBranch}, s.TemplateRevision, apptemplate.Current())
}

// reconcileProjectStorage provisions the Project's configuration storage and
// hands every identity it discovers to the Project before reporting what is
// still pending (infra.ErrPending) or an error, so a failed Setup leaves
// nothing without an owner.
func (f *ProjectSetupFlow) reconcileProjectStorage(ctx context.Context, s Setup) error {
	project, err := f.creatingProject(ctx, s)
	if err != nil {
		return err
	}
	storage, effectErr := f.Storage.EnsureStorage(ctx, s.ProjectID, project.Storage)
	if storage != project.Storage {
		err = f.Client.RecordProjectStorage(ctx, s.ProjectID, projects.ProjectStorageUpdate{SetupFlowID: s.FlowID(), Expected: project.Storage, Storage: storage})
		if err != nil {
			return fmt.Errorf("hand the project storage to the Project: %w", err)
		}
	}
	if effectErr != nil && !errors.Is(effectErr, infra.ErrPending) {
		return fmt.Errorf("ensure the project storage: %w", effectErr)
	}
	return effectErr
}

// registerAppManifest registers the source manifest in the Project's Live
// configuration scope.
func (f *ProjectSetupFlow) registerAppManifest(ctx context.Context, s Setup) error {
	project, err := f.creatingProject(ctx, s)
	if err != nil {
		return err
	}
	return f.Configurations.RegisterAppManifest(ctx, infra.ScopeRef{ProjectID: s.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: project.Storage}, s.AppManifest)
}

// configureRepository rechecks, right before the handoff, that the repository
// and its default branch are unchanged.
func (f *ProjectSetupFlow) configureRepository(ctx context.Context, s Setup) error {
	repository, err := f.Host.ReadRepository(ctx, s.Repository.FullName)
	if err != nil {
		return err
	}
	if repository.ID != s.Repository.RepositoryID || repository.Archived || repository.DefaultBranch != s.Repository.DefaultBranch {
		return fmt.Errorf("repository %s changed during setup: ID %d, default branch %s, archived %t", s.Repository.FullName, repository.ID, repository.DefaultBranch, repository.Archived)
	}
	return err
}

// commitProjectSetup hands the result to the Project, which then owns every
// resource, and ends the Setup's cleanup membership.
func (f *ProjectSetupFlow) commitProjectSetup(ctx context.Context, s Setup) error {
	if err := f.Client.CommitProjectSetup(ctx, s.ProjectID, projects.SetupCommit{SetupFlowID: s.FlowID(), Repository: s.Repository, AppManifest: s.AppManifest}); err != nil {
		return err
	}
	return f.Client.AcknowledgeChild(ctx, s.ProjectID, projects.ChildFlowReference{FlowID: s.FlowID(), FlowType: projects.ChildSetup.FlowType()})
}

// checkRepositoryHost refuses, with repohost.ErrOtherHost, a Setup whose
// recorded repository was bound under another repository host than host: host
// would read that repository as absent, and cleanup would take the absence for
// an archived repository.
func (s Setup) checkRepositoryHost(host repohost.Host) error {
	if s.Repository.RepositoryID == 0 {
		return nil
	}
	if err := repohost.CheckInstallation(host, s.Repository.InstallationID); err != nil {
		return fmt.Errorf("repository %s of Setup %s: %w", s.Repository.FullName, s.FlowID(), err)
	}
	return nil
}

// cleanupSetup archives the managed repository this Setup created but never
// handed off, then ends its binding. The Project acknowledges membership after
// cleanup commits.
func (f *ProjectSetupFlow) cleanupSetup(ctx context.Context, s Setup) error {
	project, err := f.Client.Read(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	// The Project owns a handed-off repository even while deleting it.
	if project.AppManifest != nil && project.RepositoryBinding == s.Repository {
		return nil
	}
	if err = s.checkRepositoryHost(f.Host); err != nil {
		return err
	}
	// The repository the Setup recorded is the one to archive and release, under
	// the name it was created with. A committed provider effect may have lost its
	// Step response before the Setup recorded anything: find that repository by
	// its deterministic name, then its exact claim.
	repository := s.Repository
	if repository.FullName == "" {
		found, err := f.Host.FindManagedRepository(ctx, s.repositoryIdentity(project))
		if err != nil {
			return err
		}
		if found != nil {
			repository.RepositoryID, repository.FullName = found.ID, found.FullName
		}
	}
	if repository.FullName == "" {
		return nil
	}
	if err = f.Host.ArchiveManaged(ctx, s.ProjectID, s.SetupID, repository.FullName); err != nil {
		return err
	}
	binding, err := f.Repositories.Find(ctx, repository.RepositoryID)
	if err != nil || binding == nil {
		return err
	}
	// This Setup's claim, or a claim whose start committed and whose RPC never did.
	ownClaim := binding.Status == repobinding.StatusBound && binding.ProjectID == s.ProjectID && binding.ClaimID == s.FlowID()
	if ownClaim || binding.Status == repobinding.StatusUnbound && binding.Generation == 0 {
		return f.Repositories.Release(ctx, s.ProjectID, repository.RepositoryID, binding.Generation)
	}
	return nil
}
