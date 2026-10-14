// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"github.com/superdurable/superversev2/internal/projects"
)

// The effects that ProjectReleaseFlow Steps run, in Step order: Project
// membership, source preparation, build provider calls and cleanup.

var (
	errBuildsUnavailable     = errors.New("release builds are not configured")
	errBuildAdmissionChanged = errors.New("release build admission changed")
)

// admissionChanged reports a build Step whose build revision is no longer the
// Release's active one.
func admissionChanged(active, id string) error {
	return dex.ErrorWithStack(fmt.Errorf("%w: %s is active, not %s", errBuildAdmissionChanged, cmp.Or(active, "no build"), id))
}

// admitRelease registers the Release as a child of its Project before any
// build effect, so that Project deletion always finds and removes it.
func (f *ProjectReleaseFlow) admitRelease(ctx context.Context, project, release string) error {
	return f.projects.AdmitChild(ctx, project, projects.ChildRelease, comm.Must(FlowID(project, release)))
}

// errSourceRejected marks a refusal of source preparation that the same
// request meets again: the actor, Project, commit, provider result or rendered
// source does not qualify. The Step records it instead of retrying.
var errSourceRejected = errors.New("release source preparation was rejected")

// errSourceInvalid marks the refusals of errSourceRejected that reject the
// commit's own source, which the application owns: the provider's strict
// analysis of its manifest and Flow Definitions failed, or its dex-app.yaml is
// invalid. It wraps errSourceRejected with the same text.
var errSourceInvalid = fmt.Errorf("%w", errSourceRejected)

const (
	// sourcePreparationBudget bounds the wait for source preparation, as the
	// Local Kind fixture's own limit of a source-only build does.
	sourcePreparationBudget = 4 * time.Minute
	// sourceCallTimeout bounds the work before and after the wait for the
	// provider, during which the polling Step heartbeats through KeepAlive.
	sourceCallTimeout = 45 * time.Second
	// sourceWaitLimit is the wait limit of PrepareReleasePublishingSource,
	// which the engine enforces: the wait for the provider and the work before
	// and after it.
	sourceWaitLimit = sourcePreparationBudget + 2*sourceCallTimeout
)

// sourcePreparation is source preparation running on the build provider for
// the Project that authorized it.
type sourcePreparation struct {
	project projects.Project
	request build.BuildRequest
	version repohost.CommitVersion
}

// startSource authorizes the actor against the READY Project with the
// admitted repository binding, checks that the commit is on main and starts
// source preparation on the build provider, which renders the commit's
// manifest and strict Flow Definitions without an image. It dispatches only
// what inspection finds missing, so a retry after a lost response joins the
// accepted work.
func (f *ProjectReleaseFlow) startSource(ctx context.Context, s Snapshot) (sourcePreparation, error) {
	ctx, cancel := context.WithTimeout(ctx, sourceCallTimeout)
	defer cancel()
	project, err := f.projects.AuthorizeCurrent(ctx, s.Identity.ActorAccountID, s.ProjectID, projects.CapabilityBuild)
	if errors.Is(err, projects.ErrForbidden) {
		return sourcePreparation{}, fmt.Errorf("%w: the actor may no longer use Project %s: %w", errSourceRejected, s.ProjectID, err)
	}
	if err != nil {
		return sourcePreparation{}, fmt.Errorf("authorize the actor on Project %s: %w", s.ProjectID, err)
	}
	if project.ProjectStatus != projects.StatusReady || project.RepositoryBinding.RepositoryID != s.Repository.RepositoryID || project.RepositoryBinding.Generation != s.Repository.Generation {
		return sourcePreparation{}, fmt.Errorf("%w: Project %s is %s with repository %d generation %d, not READY with the admitted repository %d generation %d", errSourceRejected, s.ProjectID, project.ProjectStatus, project.RepositoryBinding.RepositoryID, project.RepositoryBinding.Generation, s.Repository.RepositoryID, s.Repository.Generation)
	}
	// Publishing is explicitly main, never the Session's working branch.
	err = f.host.ValidateReleaseCommit(ctx, s.Repository.FullName, "main", s.SourceCommit)
	if errors.Is(err, repohost.ErrCommitUnreachable) {
		return sourcePreparation{}, fmt.Errorf("%w: commit %s: %w", errSourceRejected, s.SourceCommit, err)
	}
	if err != nil {
		return sourcePreparation{}, fmt.Errorf("check that commit %s is on main: %w", s.SourceCommit, err)
	}
	version, err := f.host.DescribeCommit(ctx, s.Repository.FullName, s.SourceCommit)
	if err != nil {
		return sourcePreparation{}, fmt.Errorf("describe the version of commit %s: %w", s.SourceCommit, err)
	}
	request, err := f.sourceRequest(s)
	if err != nil {
		return sourcePreparation{}, fmt.Errorf("%w: %w", errSourceRejected, err)
	}
	observed, err := f.builder.Inspect(ctx, request)
	if err != nil {
		return sourcePreparation{}, fmt.Errorf("inspect source preparation: %w", err)
	}
	if observed.State == build.BuildMissing || observed.State == build.BuildFailed && observed.Attempt < request.Attempt {
		credential, err := f.host.RepositoryCredential(ctx, s.Repository.RepositoryID)
		if err != nil {
			return sourcePreparation{}, fmt.Errorf("mint the repository credential: %w", err)
		}
		if _, err = f.builder.Start(ctx, request, credential.Token); err != nil {
			return sourcePreparation{}, fmt.Errorf("start source preparation: %w", err)
		}
	}
	return sourcePreparation{project: project, request: request, version: version}, nil
}

// finishSource turns the provider's last observation of source preparation
// into the prepared source: it validates the rendered manifest and Flow
// Definitions, publishes them as immutable objects and registers them with the
// configuration owner.
func (f *ProjectReleaseFlow) finishSource(ctx context.Context, s Snapshot, p sourcePreparation, observed build.BuildResult) (PreparedReleaseSource, error) {
	ctx, cancel := context.WithTimeout(ctx, sourceCallTimeout)
	defer cancel()
	switch {
	case observed.State == build.BuildFailed:
		// The provider's strict analysis rejected the commit's source.
		return PreparedReleaseSource{}, fmt.Errorf("%w: the build provider reports %s: %s", errSourceInvalid, observed.State, cmp.Or(observed.Failure, "no reason given"))
	case observed.State != build.BuildReady:
		return PreparedReleaseSource{}, fmt.Errorf("%w: the build provider reports %s: %s", errSourceRejected, observed.State, cmp.Or(observed.Failure, "no reason given"))
	case observed.ImageDigest != "" || observed.ImageReference != "":
		return PreparedReleaseSource{}, fmt.Errorf("%w: the build provider rendered an image for source preparation", errSourceRejected)
	}
	manifest, err := appmanifest.ExtractAppManifest(s.SourceCommit, []byte(observed.Payloads.Manifest))
	if errors.Is(err, appmanifest.ErrInvalidAppManifest) {
		return PreparedReleaseSource{}, fmt.Errorf("%w: dex-app.yaml: %w", errSourceInvalid, err)
	}
	if err != nil {
		return PreparedReleaseSource{}, fmt.Errorf("%w: dex-app.yaml: %w", errSourceRejected, err)
	}
	owner := sourceArtifacts(s)
	definitions, err := validatePayloads(owner, manifest, observed.Payloads)
	if err != nil {
		return PreparedReleaseSource{}, fmt.Errorf("%w: %w", errSourceRejected, err)
	}
	refs, err := publishPayloads(ctx, f.objects, owner, observed.Payloads)
	if err != nil {
		return PreparedReleaseSource{}, fmt.Errorf("publish the source artifacts: %w", err)
	}
	// The manifest and its three exact objects are one accepted source identity;
	// configuration registration never resolves a mutable key.
	manifest.ManifestObject = objectReference(refs.Manifest)
	manifest.ConnectorContractObject = objectReference(refs.ConnectorContract)
	manifest.EnvironmentContractObject = objectReference(refs.EnvironmentContract)
	prepared, err := f.publishing.RegisterReleaseSource(ctx, p.project, PreparedReleaseSource{SourceCommit: s.SourceCommit, AppManifest: manifest, Artifacts: refs, Timings: providerTimings(observed.Timings), Version: p.version}, definitions)
	if err != nil {
		return PreparedReleaseSource{}, fmt.Errorf("register the prepared source: %w", err)
	}
	return prepared, nil
}

// sourceRequest is the provider request of source preparation: the source
// build revision without configuration, under the Release's start identity
// and recipe. Each admitted source retry appends its request, so their count
// is the provider attempt.
func (f *ProjectReleaseFlow) sourceRequest(s Snapshot) (build.BuildRequest, error) {
	request := build.BuildRequest{BuildRevisionID: build.SourceRevisionID, SourceOnly: true, RequestID: s.Identity.RequestID, Attempt: uint64(len(s.PreparedSource.Requests))}
	return f.providerRequest(s, request, s.BuildProfileVersion, s.BuildProfileDigest)
}

// revisionRequest is the provider request of one build revision: its frozen
// configuration, accepted fingerprint, recipe and attempt.
func (f *ProjectReleaseFlow) revisionRequest(s Snapshot, r ReleaseBuildRevision) (build.BuildRequest, error) {
	request := build.BuildRequest{BuildRevisionID: r.BuildRevisionID, RequestID: r.Fingerprint, Attempt: r.Operation.Attempt, ConfigurationDigest: r.Configuration.Object.Digest, ConfigurationRevision: r.Configuration.ConfigurationRevision}
	return f.providerRequest(s, request, r.BuildProfileVersion, r.BuildProfileDigest)
}

// providerRequest completes request with the Release's identity and the
// pinned recipe, and checks every identity before any provider call.
func (f *ProjectReleaseFlow) providerRequest(s Snapshot, request build.BuildRequest, profileVersion, profileDigest string) (build.BuildRequest, error) {
	profile, err := f.profiles.Get(profileVersion)
	if err != nil {
		return build.BuildRequest{}, fmt.Errorf("build profile %s: %w", profileVersion, err)
	}
	if profile.Digest != profileDigest {
		return build.BuildRequest{}, fmt.Errorf("build profile %s now has digest %s, not the accepted %s", profileVersion, profile.Digest, profileDigest)
	}
	request.ContractVersion, request.Environment = build.ContractVersion, f.environment
	request.ProjectID, request.ReleaseID, request.RepositoryFullName, request.SourceCommit = s.ProjectID, s.ReleaseID, s.Repository.FullName, s.SourceCommit
	request.ProfileVersion, request.Profile, request.ProfileDigest = profile.Version, profile.ID, profile.Digest
	request.ImageTag = build.ImageTag(request)
	if err = build.ValidateIdentity(request); err != nil {
		return build.BuildRequest{}, fmt.Errorf("build request %s: %w", request.BuildRevisionID, err)
	}
	return request, nil
}

// inspectBuild observes the accepted provider build. A confirmed failure of
// an earlier attempt reads as missing, so the explicitly admitted newer
// attempt can start.
func (f *ProjectReleaseFlow) inspectBuild(ctx context.Context, s Snapshot, r ReleaseBuildRevision) (buildObservation, error) {
	request, err := f.revisionRequest(s, r)
	if err != nil {
		return buildObservation{}, err
	}
	result, err := f.builder.Inspect(ctx, request)
	if err != nil {
		return buildObservation{}, fmt.Errorf("inspect build %s: %w", r.BuildRevisionID, err)
	}
	if result.State == build.BuildFailed && result.Attempt > 0 && request.Attempt > result.Attempt {
		result.State, result.Timings = build.BuildMissing, nil
	}
	return observation(request, result), nil
}

// observeBuild inspects the accepted provider build, unless the owning Project
// is being deleted: deleting reports that its removal of the Release, which
// cancels the build, is due.
func (f *ProjectReleaseFlow) observeBuild(ctx context.Context, s Snapshot, r ReleaseBuildRevision) (observation buildObservation, deleting bool, err error) {
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err != nil {
		return buildObservation{}, false, fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if !project.AdmitsChildren() {
		return buildObservation{}, true, nil
	}
	observation, err = f.inspectBuild(ctx, s, r)
	return observation, false, err
}

// startBuild dispatches the build once. Work the provider already accepted,
// including an unknown outcome, is observed instead of dispatched again.
func (f *ProjectReleaseFlow) startBuild(ctx context.Context, s Snapshot, r ReleaseBuildRevision) (buildObservation, error) {
	request, err := f.revisionRequest(s, r)
	if err != nil {
		return buildObservation{}, err
	}
	existing, err := f.builder.Inspect(ctx, request)
	if err != nil {
		return buildObservation{}, fmt.Errorf("inspect build %s: %w", r.BuildRevisionID, err)
	}
	if existing.State == build.BuildReady || existing.State == build.BuildRunning || existing.State == build.BuildUnknown {
		return observation(request, existing), nil
	}
	credential, err := f.host.RepositoryCredential(ctx, s.Repository.RepositoryID)
	if err != nil {
		return buildObservation{}, fmt.Errorf("mint the repository credential: %w", err)
	}
	result, err := f.builder.Start(ctx, request, credential.Token)
	if err != nil {
		return buildObservation{}, fmt.Errorf("start build %s: %w", r.BuildRevisionID, err)
	}
	return observation(request, result), nil
}

func observation(request build.BuildRequest, result build.BuildResult) buildObservation {
	return buildObservation{State: result.State, Operation: buildOperation{ID: result.OperationID, Attempt: request.Attempt}, ImageReference: result.ImageReference, ImageDigest: result.ImageDigest, Provenance: provenance{Provider: "release-builder", BuildID: result.OperationID, SourceCommit: request.SourceCommit, BuildProfileDigest: request.ProfileDigest, ImageReference: result.ImageReference, ImageDigest: result.ImageDigest}, Failure: result.Failure, Timings: providerTimings(result.Timings)}
}

// providerTimings are the phases a provider measured, at most
// maxProviderTimings of them.
func providerTimings(measured []build.BuildPhaseTiming) []dexflow.PhaseTiming {
	var timings []dexflow.PhaseTiming
	for _, t := range measured[:min(len(measured), maxProviderTimings)] {
		timings = append(timings, dexflow.PhaseTiming{Phase: t.Phase, StartedAt: t.StartedAt, Duration: time.Duration(t.DurationMs) * time.Millisecond})
	}
	return timings
}

// publishBuild validates the ready build's payloads against the admitted
// source and publishes them as the build revision's immutable artifacts.
func (f *ProjectReleaseFlow) publishBuild(ctx context.Context, s Snapshot, r ReleaseBuildRevision) (ArtifactRefs, error) {
	if !flowid.IsSHA256Digest(r.ImageDigest) {
		return ArtifactRefs{}, fmt.Errorf("build %s has no immutable image digest", r.BuildRevisionID)
	}
	request, err := f.revisionRequest(s, r)
	if err != nil {
		return ArtifactRefs{}, err
	}
	result, err := f.builder.Inspect(ctx, request)
	if err != nil {
		return ArtifactRefs{}, fmt.Errorf("inspect build %s: %w", r.BuildRevisionID, err)
	}
	if result.State != build.BuildReady || result.ImageDigest != r.ImageDigest || result.ImageReference != r.ImageReference {
		return ArtifactRefs{}, fmt.Errorf("build %s is now %s with image %q, not READY with the observed %q", r.BuildRevisionID, result.State, result.ImageReference, r.ImageReference)
	}
	owner := BuildSnapshot{Release: s, Build: r}.artifacts()
	if _, err = validatePayloads(owner, s.PreparedSource.AppManifest, result.Payloads); err != nil {
		return ArtifactRefs{}, fmt.Errorf("validate the artifacts of build %s: %w", r.BuildRevisionID, err)
	}
	refs, err := publishPayloads(ctx, f.objects, owner, result.Payloads)
	if err != nil {
		return ArtifactRefs{}, fmt.Errorf("publish the artifacts of build %s: %w", r.BuildRevisionID, err)
	}
	return refs, nil
}

// cancelBuild stops the provider build of an admitted cancellation and waits,
// in rounds without locks, until it settles: Cancel reads the provider's
// record before it stops anything, so a retried round only asks again. It
// returns the provider's last observation once the build settled, or why the
// provider cannot confirm that it stopped, which waiting does not change.
func (f *ProjectReleaseFlow) cancelBuild(ctx dex.Context, s Snapshot, r ReleaseBuildRevision, report func(stage string)) (*buildObservation, string, error) {
	request, err := f.revisionRequest(s, r)
	if err != nil {
		return nil, "", dex.ErrorWithStack(err)
	}
	var unconfirmed, waiting string
	err = dexflow.PollUntil(ctx, buildCancelInterval, buildCancelRoundTimeout, func(call context.Context) (bool, error) {
		err := f.builder.Cancel(call, request)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, build.ErrCancelUnconfirmed):
			unconfirmed = dexflow.FailureMessage("The build provider could not confirm that the cancelled build stopped.", err.Error())
			return true, nil
		case errors.Is(err, build.ErrPending):
			if err.Error() != waiting {
				waiting = err.Error()
				report(waiting)
			}
			return false, nil
		}
		return false, fmt.Errorf("cancel build %s: %w", r.BuildRevisionID, err)
	})
	if err != nil || unconfirmed != "" {
		return nil, unconfirmed, err
	}
	// The settled build's last phases, which the provider measured.
	defer dexflow.KeepAlive(ctx, nil)()
	inspect, cancel := context.WithTimeout(ctx, buildCancelRoundTimeout)
	defer cancel()
	observed, err := f.inspectBuild(inspect, s, r)
	if err != nil {
		return nil, "", dex.ErrorWithStack(err)
	}
	return &observed, "", nil
}

// Stages of Release cleanup, in order; progress names the pending one.
const (
	cleanupCancelBuild    = "cancel the active build"
	cleanupBuildProvider  = "clean up the build provider"
	cleanupReleaseObjects = "delete the Release objects"
)

// cleanupRelease makes one bounded round of Release cleanup. It cancels the
// active build, which may still run on the provider once its configuration
// is frozen; fences the build provider for the whole Release and reclaims
// every source and build namespace; then deletes the Release's objects in a
// bounded batch. The provider fence precedes deletion, so a replayed Start
// cannot resurrect a reclaimed commit or image. While work remains it returns
// build.ErrPending with the stage that waits.
func (f *ProjectReleaseFlow) cleanupRelease(ctx context.Context, s Snapshot, active *ReleaseBuildRevision) (string, error) {
	if f.builder == nil {
		return "", errBuildsUnavailable
	}
	if active != nil && active.Configuration != nil {
		request, err := f.revisionRequest(s, *active)
		if err == nil {
			err = f.builder.Cancel(ctx, request)
		}
		if err != nil {
			return cleanupCancelBuild, fmt.Errorf("%s %s: %w", cleanupCancelBuild, active.BuildRevisionID, err)
		}
	}
	request, err := f.sourceRequest(s)
	if err == nil {
		err = f.builder.CleanupRelease(ctx, request)
	}
	if err != nil {
		return cleanupBuildProvider, fmt.Errorf("%s: %w", cleanupBuildProvider, err)
	}
	done, err := f.objects.DeleteReleaseObjects(ctx, s.ProjectID, s.SourceCommit)
	if err == nil && !done {
		err = fmt.Errorf("%w: more object versions remain", build.ErrPending)
	}
	if err != nil {
		return cleanupReleaseObjects, fmt.Errorf("%s: %w", cleanupReleaseObjects, err)
	}
	return "", nil
}
