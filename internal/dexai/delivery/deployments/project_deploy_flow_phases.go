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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// The effects that ProjectDeployFlow Steps run, in Step order. Every provider
// phase first proves that the deployment still owns the Project's deployment
// lease.

const (
	// convergenceInterval spaces the rounds of a phase that waits for its
	// provider, which a user is watching.
	convergenceInterval = 3 * time.Second
	// convergenceRoundTimeout bounds one round, which reads the Project and
	// makes several Kubernetes or cloud calls.
	convergenceRoundTimeout = 40 * time.Second
	// temporalCheckTimeout bounds the read-only check that names why a
	// Temporal branch passed its wait limit: a Project read and one
	// data-plane query.
	temporalCheckTimeout = 20 * time.Second
)

// errLeaseLost reports a deployment that no longer owns the Project's
// deployment lease, which only Project deletion takes from it.
var errLeaseLost = errors.New("deployment no longer owns the project lease")

// pending reports whether err, the outcome of one round, means that the
// provider is still converging.
func pending(err error) bool {
	return errors.Is(err, infra.ErrPending) || errors.Is(err, projects.ErrCleanupPending)
}

// converge runs round every convergenceInterval, each within
// convergenceRoundTimeout, through dexflow.PollUntil until it is no longer
// pending, and reports each new pending reason. It returns the last round's
// result. The engine ends a phase still pending at the Step's wait limit and
// moves the Step to its failure route.
func converge[T any](ctx dex.Context, round func(context.Context) (T, error), report func(reason string)) (T, error) {
	var result T
	var reason string
	err := dexflow.PollUntil(ctx, convergenceInterval, convergenceRoundTimeout, func(call context.Context) (bool, error) {
		var err error
		if result, err = round(call); !pending(err) {
			return true, err
		}
		if err.Error() != reason {
			reason = err.Error()
			report(reason)
		}
		return false, nil
	})
	return result, err
}

// admitDeployment registers the deployment as a child of its READY Project
// before any provider effect, so that Project deletion always finds it.
func (f *ProjectDeployFlow) admitDeployment(ctx context.Context, s Snapshot) error {
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err != nil {
		return fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if !project.AdmitsChildren() {
		return projects.ErrChildAdmissionClosed
	}
	if project.ProjectStatus != projects.StatusReady {
		return fmt.Errorf("project %s is %s, not READY for deployment", s.ProjectID, project.ProjectStatus)
	}
	if err = f.projects.AdmitChild(ctx, s.ProjectID, projects.ChildDeploy, comm.Must(FlowID(s.ProjectID, s.DeploymentID))); err != nil {
		return fmt.Errorf("register the deployment with Project %s: %w", s.ProjectID, err)
	}
	return nil
}

// ownedProject reads the READY Project whose active deployment lease is
// still this deployment's; otherwise the lease is lost (errLeaseLost).
func (f *ProjectDeployFlow) ownedProject(ctx context.Context, s Snapshot) (projects.Project, error) {
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err != nil {
		return project, fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if project.ProjectStatus != projects.StatusReady || project.ActiveDeployment == nil || *project.ActiveDeployment != s.Lease || s.Lease.Generation == 0 {
		return projects.Project{}, fmt.Errorf("%w: Project %s is %s with active lease %+v, not %+v", errLeaseLost, s.ProjectID, project.ProjectStatus, project.ActiveDeployment, s.Lease)
	}
	return project, nil
}

// verifyReleaseAndConfiguration pins the exact READY build: its immutable
// image, Flow Definitions and manifest, the application endpoint it declares
// and its frozen configuration, which the configuration owner verifies. It
// also pins the Project's storage, and its Live execution environment once
// complete, which every later phase runs on, and the Live generation it
// replaces: the lease holds the Project's Live pointer at
// ExpectedLiveRevision until this deployment commits.
func (f *ProjectDeployFlow) verifyReleaseAndConfiguration(ctx context.Context, s Snapshot) (Inputs, DeploymentResources, ReplacedGeneration, error) {
	inputs, resources, project, err := f.verifyRelease(ctx, s)
	if err != nil {
		return Inputs{}, DeploymentResources{}, ReplacedGeneration{}, err
	}
	var replaced ReplacedGeneration
	if live := project.CurrentLive; live != nil && live.Revision == s.ExpectedLiveRevision && live.DeploymentID != s.DeploymentID {
		replaced = ReplacedGeneration{DeploymentID: live.DeploymentID, Generation: live.Revision}
	}
	return inputs, resources, replaced, nil
}

// verifyRelease pins the exact READY build and its configuration for
// verifyReleaseAndConfiguration and returns the Project it read.
func (f *ProjectDeployFlow) verifyRelease(ctx context.Context, s Snapshot) (Inputs, DeploymentResources, projects.Project, error) {
	project, err := f.ownedProject(ctx, s)
	if err != nil {
		return Inputs{}, DeploymentResources{}, project, err
	}
	release, err := f.releases.ReadBuildSnapshot(ctx, s.ProjectID, s.ReleaseID, s.BuildRevisionID)
	if err != nil {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("read build %s of Release %s: %w", s.BuildRevisionID, s.ReleaseID, err)
	}
	build := release.Build
	if build.Status != releases.Ready || !infra.ImmutableImage.MatchString(build.ImageReference) || !strings.HasSuffix(build.ImageReference, "@"+build.ImageDigest) || build.Artifacts.FlowDefinitions.Digest == "" {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("build %s is %s with image %q, not READY with an immutable image and Flow Definitions", s.BuildRevisionID, build.Status, build.ImageReference)
	}
	manifest := release.Release.PreparedSource.AppManifest
	if manifest == nil || manifest.Environment == nil {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("release %s has no prepared application manifest", s.ReleaseID)
	}
	// Source preparation admitted the declared port and health path.
	environment := manifest.Environment.Fields
	port, health := int(environment["port"].GetNumberValue()), environment["healthPath"].GetStringValue()
	config := build.Configuration
	if config == nil || config.ReleaseID != s.ReleaseID || config.ConfigurationRevision != s.ConfigurationRevision || config.Object.Version == "" || config.Object.Digest == "" {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("build %s froze no exact configuration of Release %s at revision %d", s.BuildRevisionID, s.ReleaseID, s.ConfigurationRevision)
	}
	if err = f.configuration.VerifyReleaseConfiguration(ctx, project, *config); err != nil {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("verify configuration revision %d: %w", s.ConfigurationRevision, err)
	}
	if _, err = releases.ReadDefinitions(ctx, f.objects, release); err != nil {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("read the Flow Definitions of build %s: %w", s.BuildRevisionID, err)
	}
	if _, err = f.objects.Get(ctx, build.Artifacts.Manifest); err != nil {
		return Inputs{}, DeploymentResources{}, project, fmt.Errorf("read the manifest of build %s: %w", s.BuildRevisionID, err)
	}
	inputs := Inputs{BuildRevisionID: s.BuildRevisionID, ApplicationPort: port, HealthPath: health, ImageReference: build.ImageReference, ImageDigest: build.ImageDigest, BundleDigest: build.Artifacts.FlowDefinitions.Digest, Configuration: *config, Definitions: build.Artifacts.FlowDefinitions, Manifest: build.Artifacts.Manifest}
	resources := DeploymentResources{Storage: project.Storage}
	if project.LiveExecution.Complete() {
		resources.Execution = project.LiveExecution
	}
	return inputs, resources, project, nil
}

// publishDefinitions publishes the pinned build's Flow Definitions, which the
// release must still hold unchanged, to the project's storage.
func (f *ProjectDeployFlow) publishDefinitions(ctx context.Context, s Snapshot) error {
	if _, err := f.ownedProject(ctx, s); err != nil {
		return err
	}
	release, err := f.releases.ReadBuildSnapshot(ctx, s.ProjectID, s.ReleaseID, s.BuildRevisionID)
	if err != nil {
		return fmt.Errorf("read build %s of Release %s: %w", s.BuildRevisionID, s.ReleaseID, err)
	}
	if release.Build.Artifacts.FlowDefinitions != s.Inputs.Definitions {
		return fmt.Errorf("build %s now holds Flow Definitions %s, not the pinned %s", s.BuildRevisionID, release.Build.Artifacts.FlowDefinitions.Digest, s.Inputs.Definitions.Digest)
	}
	definitions, err := releases.ReadDefinitions(ctx, f.objects, release)
	if err != nil {
		return fmt.Errorf("read the Flow Definitions of build %s: %w", s.BuildRevisionID, err)
	}
	return publishFlowDefinitions(ctx, f.storage, s, definitions)
}

// prepareBranch converges one branch of a preparation round through round and
// returns the branch's result. Two outcomes are decisions, not errors: a lost
// lease ends the branch Cancelled, and a refusal that waiting cannot change,
// an ownership conflict, a Temporal credential only an operator repairs, a
// selected connection that was deleted or a workload grant that names too many
// secrets, ends it with its Failure. Any other
// error ends the attempt, so the retry policy and then the failure route
// decide.
func (f *ProjectDeployFlow) prepareBranch(ctx dex.Context, s Snapshot, in livePreparationInput, round func(context.Context) (LivePreparationResult, error), report func(reason string)) (LivePreparationResult, error) {
	var decided error
	result, err := converge(ctx, func(call context.Context) (LivePreparationResult, error) {
		result, err := round(call)
		if errors.Is(err, errLeaseLost) || errors.Is(err, infra.ErrOwnershipConflict) || errors.Is(err, connections.ErrConnectionDeleted) || errors.Is(err, infra.ErrGrantTooLarge) {
			decided, err = err, nil
		}
		return result, err
	}, report)
	if err != nil {
		return LivePreparationResult{}, err
	}
	result.Branch, result.Round, result.Timing = in.Branch, in.Round, measure(in.Branch, ctx.FirstAttemptAt().UTC())
	switch {
	case errors.Is(decided, errLeaseLost):
		result.Cancelled = true
	case errors.Is(decided, connections.ErrConnectionDeleted):
		result.Failure = dexflow.FailureMessage("This version selects a TeamSpace connection that was deleted; select another connection and deploy again.", f.redactor.Redact(s.ProjectID, decided.Error()))
	case errors.Is(decided, infra.ErrGrantTooLarge):
		result.Failure = dexflow.FailureMessage("This version's configuration names more secrets than one workload grant can hold; remove secrets from the Live configuration and deploy again.", f.redactor.Redact(s.ProjectID, decided.Error()))
	case decided != nil:
		result.Failure = dexflow.FailureMessage(in.Branch+" cannot finish until an operator resolves its cause.", f.redactor.Redact(s.ProjectID, decided.Error()))
	}
	return result, nil
}

// prepareLiveTemporal converges the project's Live Temporal namespace and the
// credential that reaches it, on the pinned storage and the Project's
// accepted references: the first deployment creates them and later ones reuse
// them through queries. The round ends with the data-plane check, so a
// namespace that does not accept the credential yet is pending. Every
// reference the round observed goes to the Project before the round reports
// pending or an error, so a failed deployment leaves nothing without an owner.
func (f *ProjectDeployFlow) prepareLiveTemporal(ctx context.Context, s Snapshot) (LivePreparationResult, error) {
	project, err := f.ownedProject(ctx, s)
	if err != nil {
		return LivePreparationResult{}, err
	}
	execution, effectErr := f.execution.EnsureTemporal(ctx, s.ProjectID, s.Resources.Storage, project.LiveExecution)
	if err = f.recordLiveExecution(ctx, s, project.LiveExecution, execution); err != nil {
		return LivePreparationResult{}, err
	}
	if effectErr != nil {
		return LivePreparationResult{Execution: execution}, fmt.Errorf("prepare the Live Temporal of Project %s: %w", s.ProjectID, effectErr)
	}
	return LivePreparationResult{Execution: execution}, nil
}

// prepareLiveAccess converges what the Live Dex and the generation need on the
// pinned storage: the Live usages of the TeamSpace connections the version
// selects, so its applications may read their credentials, the access of Dex
// and the applications, the generation's Service, which exists before its
// Pods so that readiness and the Live route bind a stable identity, and the
// generation's workload grant. The grant is a storage grant that needs no
// Dex, and it is named by the generation, so a retry reissues it. The lease is
// read again right before the grant, so a generation the Project no longer
// leases is never issued after the round's earlier calls.
func (f *ProjectDeployFlow) prepareLiveAccess(ctx context.Context, s Snapshot) (LivePreparationResult, error) {
	project, err := f.ownedProject(ctx, s)
	if err != nil {
		return LivePreparationResult{}, err
	}
	scope := infra.ScopeRef{ProjectID: s.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: s.Resources.Storage}
	selections, err := f.configuration.SharedConnectionSelections(ctx, scope, s.Inputs.Configuration.Object)
	if err != nil {
		return LivePreparationResult{}, fmt.Errorf("read the connections configuration revision %d selects: %w", s.ConfigurationRevision, err)
	}
	selected := slices.Sorted(maps.Values(selections))
	if err = f.usages.EnsureConnectionUsages(ctx, project.TeamSpaceID, s.ProjectID, slices.Compact(selected)); err != nil {
		return LivePreparationResult{}, fmt.Errorf("record the Live connection usages of Project %s: %w", s.ProjectID, err)
	}
	// The running Live keeps its registrations until this version commits, which prunes them.
	serving, err := f.servingSelections(ctx, project, s)
	if err != nil {
		return LivePreparationResult{}, err
	}
	if err = f.usages.RetainTriggerRegistrations(ctx, s.ProjectID, infra.ScopeLive, selections, serving); err != nil {
		return LivePreparationResult{}, fmt.Errorf("retain the Live trigger registrations of Project %s: %w", s.ProjectID, err)
	}
	if err := f.execution.EnsureExecutionAccess(ctx, s.ProjectID, s.Resources.Storage); err != nil {
		return LivePreparationResult{}, fmt.Errorf("prepare the Live execution access of Project %s: %w", s.ProjectID, err)
	}
	service, err := f.serving.EnsureGenerationService(ctx, s)
	if err != nil {
		return LivePreparationResult{}, fmt.Errorf("reconcile the Service of generation %d: %w", s.Lease.Generation, err)
	}
	if _, err = f.ownedProject(ctx, s); err != nil {
		return LivePreparationResult{Service: service}, err
	}
	if err = f.grants.IssueWorkload(ctx, liveWorkload(s)); err != nil {
		return LivePreparationResult{Service: service}, fmt.Errorf("issue the workload grant of generation %d: %w", s.Lease.Generation, err)
	}
	grant := WorkloadGrant{ID: infra.WorkloadName(s.DeploymentID, s.Lease.Generation), ProjectID: s.ProjectID, DeploymentID: s.DeploymentID, Generation: s.Lease.Generation, ConfigurationRevision: s.ConfigurationRevision, Configuration: s.Inputs.Configuration.Object}
	return LivePreparationResult{Service: service, Grant: grant}, nil
}

// servingSelections are the TeamSpace connections that the configuration of
// the Project's current Live selects, none without another Live.
func (f *ProjectDeployFlow) servingSelections(ctx context.Context, project projects.Project, s Snapshot) (map[string]string, error) {
	live := project.CurrentLive
	if live == nil || live.DeploymentID == s.DeploymentID {
		return nil, nil
	}
	serving, err := f.deployments.readSnapshot(ctx, s.ProjectID, live.DeploymentID)
	if err != nil {
		return nil, fmt.Errorf("read the Live deployment %s: %w", live.DeploymentID, err)
	}
	scope := infra.ScopeRef{ProjectID: s.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: s.Resources.Storage}
	selections, err := f.configuration.SharedConnectionSelections(ctx, scope, serving.Inputs.Configuration.Object)
	if err != nil {
		return nil, fmt.Errorf("read the connections the Live deployment %s selects: %w", live.DeploymentID, err)
	}
	return selections, nil
}

// joinLivePreparation merges the round's two branch results into the phase
// result to commit: what the branches observed, their timings and, when a
// branch failed or lost the lease outside Project cleanup, the Failure that
// ends the round. A result of another round, an unknown branch or a second
// result of one branch is a defect.
func joinLivePreparation(s Snapshot, in livePreparationInput, results []LivePreparationResult) phaseResult {
	resources := s.Resources
	joined := phaseResult{Phase: phasePrepare, StartedAt: in.StartedAt, Resources: &resources}
	seen := map[string]bool{}
	var failures []string
	for _, result := range results {
		if result.Round != in.Round || seen[result.Branch] || (result.Branch != branchTemporal && result.Branch != branchAccess) {
			panic(fmt.Sprintf("deployments: round %d under %s received result %s of round %d", in.Round, in.Key, result.Branch, result.Round))
		}
		seen[result.Branch] = true
		joined.Timings = append(joined.Timings, result.Timing)
		if result.Execution != (infra.ExecutionRefs{}) {
			resources.Execution = result.Execution
		}
		if result.Service != (infra.ResourceRef{}) {
			resources.Service = result.Service
		}
		if result.Grant != (WorkloadGrant{}) {
			joined.Grant = &result.Grant
		}
		switch {
		case result.Failure != "":
			failures = append(failures, result.Failure)
		case result.Cancelled && !s.CleanupRequested:
			failures = append(failures, result.Branch+" stopped because the deployment no longer owns the Project's deployment lease.")
		}
	}
	if len(failures) > 0 {
		joined.Failure = deploymentFailure(phasePrepare, strings.Join(failures, " "))
	}
	return joined
}

// temporalWaitCause names why the Temporal branch passed its wait limit, from
// one bounded, read-only check of the data plane with the Project's accepted
// credential. It never fails: an error makes the cause unclassified.
func (f *ProjectDeployFlow) temporalWaitCause(ctx context.Context, s Snapshot) string {
	ctx, cancel := context.WithTimeout(ctx, temporalCheckTimeout)
	defer cancel()
	limit := minutes(prepareTemporalWaitLimit)
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err == nil && project.LiveExecution.TemporalCredential.UID == "" {
		return "The Temporal namespace was not ready within " + limit + "."
	}
	var answer infra.DataPlaneAnswer
	if err == nil {
		answer, err = f.execution.CheckTemporalDataPlane(ctx, s.ProjectID, s.Resources.Storage, project.LiveExecution)
	}
	switch {
	case err != nil:
		return "The Temporal data plane check is unclassified: " + dexflow.TruncateCause(f.redactor.Redact(s.ProjectID, err.Error()))
	case answer == infra.DataPlaneUnauthorized:
		return "The Temporal data plane denied the Project namespace within " + limit + "."
	case answer == infra.DataPlaneUnreachable:
		return "The Temporal data plane stayed unreachable for " + limit + "."
	default:
		return "The Temporal data plane became ready after the wait limit; retry the deployment."
	}
}

// minutes names a wait limit in whole minutes.
func minutes(limit time.Duration) string {
	return fmt.Sprintf("%d minutes", int(limit/time.Minute))
}

// startLiveDex starts the project's Live Dex on the pinned storage and the
// Project's accepted Temporal, hands its Service to the Project and returns
// the deployment's resources once the Dex is ready.
func (f *ProjectDeployFlow) startLiveDex(ctx context.Context, s Snapshot) (DeploymentResources, error) {
	project, err := f.ownedProject(ctx, s)
	if err != nil {
		return DeploymentResources{}, err
	}
	execution, effectErr := f.execution.StartProjectDex(ctx, s.ProjectID, s.Resources.Storage, project.LiveExecution)
	if err = f.recordLiveExecution(ctx, s, project.LiveExecution, execution); err != nil {
		return DeploymentResources{}, err
	}
	if effectErr != nil {
		return DeploymentResources{}, fmt.Errorf("start the Live Dex of Project %s: %w", s.ProjectID, effectErr)
	}
	resources := s.Resources
	resources.Execution = execution
	return resources, nil
}

// recordLiveExecution hands the execution references a round observed to the
// Project under the deployment's lease when they differ from the accepted
// ones.
func (f *ProjectDeployFlow) recordLiveExecution(ctx context.Context, s Snapshot, accepted, observed infra.ExecutionRefs) error {
	if observed == accepted {
		return nil
	}
	if err := f.projects.RecordLiveExecution(ctx, s.ProjectID, projects.LiveExecutionUpdate{Lease: s.Lease, Expected: accepted, Execution: observed}); err != nil {
		return fmt.Errorf("hand the Live execution references to Project %s: %w", s.ProjectID, err)
	}
	return nil
}

// applyApplication runs the generation with the environment of its grant,
// which it reissues only while the Project still leases the generation.
func (f *ProjectDeployFlow) applyApplication(ctx context.Context, s Snapshot) (ApplicationResources, error) {
	if _, err := f.ownedProject(ctx, s); err != nil {
		return ApplicationResources{}, err
	}
	environment, err := f.grants.GrantEnvironment(ctx, liveWorkload(s))
	if err != nil {
		return ApplicationResources{}, fmt.Errorf("reconcile the workload grant of generation %d: %w", s.Lease.Generation, err)
	}
	application, err := f.serving.ApplyApplication(ctx, s, environment)
	if err != nil {
		return ApplicationResources{}, fmt.Errorf("apply the application of generation %d: %w", s.Lease.Generation, err)
	}
	return application, nil
}

// leaseLostFailure is the result of a phase that found the Project's lease no
// longer this deployment's: it fails at once into the failure route, which a
// deleting Project's cleanup then continues, instead of retrying a grant that
// the Project's state already refuses.
func (f *ProjectDeployFlow) leaseLostFailure(s Snapshot, phase string, err error) phaseResult {
	reason := dexflow.FailureMessage(phase+" stopped because the deployment no longer owns the Project's deployment lease.", f.redactor.Redact(s.ProjectID, err.Error()))
	return phaseResult{Phase: phase, Failure: deploymentFailure(phase, reason)}
}

func (f *ProjectDeployFlow) verifyReadiness(ctx context.Context, s Snapshot) (Readiness, error) {
	if _, err := f.ownedProject(ctx, s); err != nil {
		return Readiness{}, err
	}
	observed, err := f.serving.VerifyReadiness(ctx, s)
	if err != nil {
		return Readiness{}, fmt.Errorf("verify the readiness of generation %d: %w", s.Lease.Generation, err)
	}
	observed.CheckedAt = time.Now().UTC()
	return observed, nil
}

// commitCurrentLive activates the ready generation and then commits it as the
// Project's authoritative Live pointer under the deployment lease.
func (f *ProjectDeployFlow) commitCurrentLive(ctx context.Context, s Snapshot) (Activation, error) {
	project, err := f.ownedProject(ctx, s)
	if err != nil {
		return Activation{}, err
	}
	activation, err := f.serving.Activate(ctx, s)
	if err != nil {
		return Activation{}, fmt.Errorf("activate generation %d: %w", s.Lease.Generation, err)
	}
	pointer := projects.LivePointer{BuildRevisionID: s.BuildRevisionID, FDGActivation: activation.Manifest, DeploymentID: s.DeploymentID, ReleaseID: s.ReleaseID, URL: activation.URL, ImageDigest: s.Inputs.ImageDigest, BundleDigest: s.Inputs.BundleDigest, Revision: s.Lease.Generation, Ready: true, Manifest: s.Inputs.Manifest}
	if project.CurrentLive == nil || *project.CurrentLive != pointer {
		if err = f.projects.CommitLiveDeployment(ctx, s.ProjectID, projects.DeploymentCommit{Lease: s.Lease, Pointer: pointer}); err != nil {
			return Activation{}, fmt.Errorf("commit generation %d as Live of Project %s: %w", s.Lease.Generation, s.ProjectID, err)
		}
	}
	// Live now runs this version, so only its trigger registrations stay.
	scope := infra.ScopeRef{ProjectID: s.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: s.Resources.Storage}
	selections, err := f.configuration.SharedConnectionSelections(ctx, scope, s.Inputs.Configuration.Object)
	if err != nil {
		return Activation{}, fmt.Errorf("read the connections configuration revision %d selects: %w", s.ConfigurationRevision, err)
	}
	if err = f.usages.RetainTriggerRegistrations(ctx, s.ProjectID, infra.ScopeLive, selections); err != nil {
		return Activation{}, fmt.Errorf("prune the Live trigger registrations of Project %s: %w", s.ProjectID, err)
	}
	return activation, nil
}

// cleanupCancelledDeployment reclaims a deployment of a deleting Project, or
// one a person cancelled, that is not the Project's current Live. Before any
// provider effect only the exact lease of this deployment is reconciled; it
// returns a lease that Acquire committed although its response was lost, so
// the existing lease release can run. Otherwise the workload grant is revoked
// before the generation's resources are removed, on the storage that
// VerifyReleaseAndConfiguration pinned with the Inputs. The current Live, its
// generation, the Project's Live execution environment and the connection
// usages are never touched.
func (f *ProjectDeployFlow) cleanupCancelledDeployment(ctx context.Context, s Snapshot) (*projects.DeploymentLease, error) {
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if project.CurrentLive != nil && project.CurrentLive.DeploymentID == s.DeploymentID {
		return nil, fmt.Errorf("deployment %s is the current Live deployment and cannot be cancelled", s.DeploymentID)
	}
	if deploymentBeforeProviderEffects(s) {
		if s.Cancellation == nil && (!s.CleanupRequested || project.ProjectStatus != projects.StatusDeleting) {
			return nil, fmt.Errorf("cleanup before provider effects requires the deleting Project fence or a cancellation, but Project %s is %s", s.ProjectID, project.ProjectStatus)
		}
		if s.Lease != (projects.DeploymentLease{}) && (s.Lease.DeploymentID != s.DeploymentID || s.Lease.Generation == 0 || s.Lease.ExpectedLiveRevision != s.ExpectedLiveRevision) {
			return nil, fmt.Errorf("the recorded lease %+v is not this deployment's", s.Lease)
		}
		lease := project.ActiveDeployment
		if lease == nil || lease.DeploymentID != s.DeploymentID {
			// DELETING and AcquireDeployment share the Project revision lock, and a cancelled
			// deployment runs no acquire after this read: none creates this attempt's lease later.
			return nil, nil
		}
		if lease.Generation == 0 || lease.ExpectedLiveRevision != s.ExpectedLiveRevision || (s.Lease != (projects.DeploymentLease{}) && s.Lease != *lease) {
			return nil, fmt.Errorf("the Project's active lease %+v differs from the recorded lease %+v", *lease, s.Lease)
		}
		return lease, nil
	}
	if err = f.serving.StopWorker(ctx, s); err != nil {
		return nil, fmt.Errorf("stop candidate Worker of generation %d before revoking its grant: %w", s.Lease.Generation, err)
	}
	if err = f.grants.RevokeWorkload(ctx, liveWorkload(s)); err != nil {
		return nil, fmt.Errorf("revoke the workload grant of generation %d: %w", s.Lease.Generation, err)
	}
	if err = f.serving.CleanupDeployment(ctx, s); err != nil {
		return nil, fmt.Errorf("clean up generation %d: %w", s.Lease.Generation, err)
	}
	// What a preempted ApplyApplication round still creates is removed by a later round.
	if c := s.Cancellation; c != nil && c.Preempted && time.Since(c.RequestedAt) < preemptedRoundBound {
		return nil, fmt.Errorf("%w: the preempted %s may still act on generation %d", infra.ErrPending, phaseApply, s.Lease.Generation)
	}
	return nil, nil
}

// retirementEligible reports whether a replaced generation retires: only one
// that advertised the Live Worker endpoint, whose open Flows its successor
// takes over, and that neither retires nor is cleaned up already.
func retirementEligible(s Snapshot) bool {
	return s.Resources.Worker != (infra.ResourceRef{}) && s.Status != Retiring && s.Status != Retired && s.Status != Removed && !s.CleanupRequested
}

// drainRemaining is how much of the drain window of an admitted retirement
// remains.
func (f *ProjectDeployFlow) drainRemaining(r Retirement) time.Duration {
	return time.Until(r.RequestedAt.Add(f.retirementDrain))
}

// errRetiringLive reports a retirement of the Project's current Live
// generation, which no request admits.
var errRetiringLive = errors.New("the generation to retire is the Project's current Live")

// retireGeneration revokes the workload grant of a replaced generation and
// then removes its objects, on the storage it pinned. The Project's Live
// pointer must name another deployment. A Project being deleted takes the
// generation with it as well, so its retirement goes on.
func (f *ProjectDeployFlow) retireGeneration(ctx context.Context, s Snapshot) error {
	project, err := f.projects.Read(ctx, s.ProjectID)
	if err != nil {
		return fmt.Errorf("read Project %s: %w", s.ProjectID, err)
	}
	if project.CurrentLive != nil && project.CurrentLive.DeploymentID == s.DeploymentID {
		return fmt.Errorf("%w: deployment %s", errRetiringLive, s.DeploymentID)
	}
	if err := f.serving.StopWorker(ctx, s); err != nil {
		return fmt.Errorf("confirm retired Worker exit: %w", err)
	}
	if err = f.grants.RevokeWorkload(ctx, liveWorkload(s)); err != nil {
		return fmt.Errorf("revoke the workload grant of generation %d: %w", s.Lease.Generation, err)
	}
	if err = f.serving.CleanupDeployment(ctx, s); err != nil {
		return fmt.Errorf("remove generation %d: %w", s.Lease.Generation, err)
	}
	return nil
}

// deploymentBeforeProviderEffects proves that no provider phase can have run:
// the phase precedes the first provider Step and every provider reference is
// still its initial zero. CommitDeploymentPhase pins Inputs and the storage
// atomically before scheduling the first provider Step, so a cleanup phase or
// an absent grant alone is not proof.
func deploymentBeforeProviderEffects(s Snapshot) bool {
	switch s.Providers.Phase {
	case phaseAdmit, phaseAcquire, phaseVerify, phaseCleanup, phaseReleaseLease:
	default:
		return false
	}
	// Cleanup's commit marks even an absent grant revoked. Preserve that proof
	// if lease release exhausts its retries and re-enters cleanup.
	emptyGrant := s.Grant == (WorkloadGrant{}) || (s.Providers.Phase == phaseReleaseLease && s.Grant == (WorkloadGrant{Revoked: true}))
	return s.Inputs == (Inputs{}) && s.Resources == (DeploymentResources{}) && emptyGrant && s.Readiness == (Readiness{}) && s.URL == "" &&
		s.Providers.Application == (infra.ResourceRef{})
}

// liveWorkload is the workload grant scope of the deployment's generation.
func liveWorkload(s Snapshot) infra.Workload {
	return infra.Workload{ProjectID: s.ProjectID, WorkloadID: s.DeploymentID, Scope: infra.Scope{Kind: infra.ScopeLive}, Generation: s.Lease.Generation, ConfigurationRevision: s.ConfigurationRevision, Storage: s.Resources.Storage, Configuration: s.Inputs.Configuration.Object, GrantID: s.Grant.ID, Revoked: s.Grant.Revoked}
}
