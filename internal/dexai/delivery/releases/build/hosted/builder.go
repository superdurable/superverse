// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cb "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ec "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
)

// Config contains trusted deployment inputs, never browser overrides.
// Prefix has disjoint receipts/ (platform only) and jobs/ (build-role output) trees.
type Config struct {
	Environment deploymentnames.Environment

	ProjectName, Region, AccountID, RepositoryURI, Bucket, Prefix, KMSKeyARN, ToolImage, HostImage string
}

// Builder runs Release builds in the environment's dedicated CodeBuild
// project, pushes images to ECR and keeps its admission receipts in S3.
type Builder struct {
	config     Config
	profiles   *build.Profiles
	builds     *codebuild.Client
	logs       *cloudwatchlogs.Client
	images     *ecr.Client
	objects    *s3.Client
	repository string
	names      deploymentnames.Names
	// executorImage matches the dedicated host and tool images of this
	// account, region and environment.
	executorImage *regexp.Regexp
}

const (
	// findClockSkew widens the search past the admission time for clock skew
	// between the platform and CodeBuild.
	findClockSkew = 2 * time.Minute
	// findMaxPages bounds one search; only a burst of thousands of builds since
	// the admission exhausts it, and exhaustion stays UNKNOWN.
	findMaxPages = 20
)

// errHostedCapacity reports a dispatch that CodeBuild refused for the account's
// limits before creating an execution. The caller's retry rules apply.
var errHostedCapacity = errors.New("hosted build capacity is exhausted")

var errHostedConflict = errors.New("hosted build admission or immutable identity changed")
var errHostedUncertain = errors.New("hosted build provider outcome is unconfirmed")

// errHostedResultMissing reports a finished build attempt that wrote no result.
var errHostedResultMissing = errors.New("hosted build result does not exist")

var (
	awsRegionPattern  = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]$`)
	awsAccountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	s3BucketPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// New does not make network calls. AWS uses its default credential
// chain; neither repository credentials nor platform secrets enter CodeBuild.
// Each setting is checked against the deployment catalog of its environment,
// and a rejection names the setting without echoing its value.
func New(config aws.Config, input Config, profiles *build.Profiles) (*Builder, error) {
	names, err := deploymentnames.For(input.Environment)
	if err != nil {
		return nil, err
	}
	registry := input.AccountID + ".dkr.ecr." + input.Region + ".amazonaws.com/"
	executorImage := regexp.MustCompile(`^` + regexp.QuoteMeta(registry+names.ECRPrefix+"/") + `(build-host|build-tools)@sha256:[a-f0-9]{64}$`)
	for _, setting := range []struct {
		name  string
		valid bool
	}{
		{"aws.region", config.Region == input.Region && config.BaseEndpoint == nil && awsRegionPattern.MatchString(input.Region)},
		{"aws.accountId", awsAccountPattern.MatchString(input.AccountID)},
		{"builds.hosted.prefix", input.Prefix == names.ArtifactPrefix},
		{"builds.hosted.project", input.ProjectName == names.BuildProject},
		{"builds.hosted.repository", input.RepositoryURI == registry+names.ECRPrefix+"/applications"},
		{"builds.hosted.bucket", s3BucketPattern.MatchString(input.Bucket)},
		{"builds.hosted.kmsKeyARN", strings.HasPrefix(input.KMSKeyARN, "arn:aws:kms:"+input.Region+":"+input.AccountID+":key/")},
		{"builds.hosted.hostImage", executorImage.MatchString(input.HostImage)},
		{"builds.hosted.toolImage", executorImage.MatchString(input.ToolImage)},
	} {
		if !setting.valid {
			return nil, fmt.Errorf("hosted build setting %s is invalid", setting.name)
		}
	}
	// CAS and dispatch are never automatically retried. A lost response is resolved
	// by reading the durable receipt and the actual accepted CodeBuild execution.
	config.RetryMaxAttempts = 1
	return &Builder{config: input, profiles: profiles, builds: codebuild.NewFromConfig(config), logs: cloudwatchlogs.NewFromConfig(config), images: ecr.NewFromConfig(config), objects: s3.NewFromConfig(config), repository: strings.TrimPrefix(input.RepositoryURI, registry), names: names, executorImage: executorImage}, nil
}

// validate accepts a request for this builder's environment whose recipe is
// the registered profile it names.
func (b *Builder) validate(in build.BuildRequest) (build.Profile, error) {
	profile, err := b.profiles.Get(in.ProfileVersion)
	if err != nil {
		return build.Profile{}, err
	}
	switch {
	case in.Profile != profile.ID || in.ProfileDigest != profile.Digest:
		return build.Profile{}, fmt.Errorf("hosted build request recipe %s differs from the registered profile %s", in.Profile, in.ProfileVersion)
	case in.Environment != b.config.Environment:
		return build.Profile{}, fmt.Errorf("hosted build request environment %q is not this builder's %q", in.Environment, b.config.Environment)
	case len(in.ProjectID) > b.names.MaxProjectIDLength:
		return build.Profile{}, fmt.Errorf("hosted build project ID exceeds %d characters", b.names.MaxProjectIDLength)
	}
	if err = build.ValidateIdentity(in); err != nil {
		return build.Profile{}, err
	}
	return profile, nil
}

// Executor inputs are pinned separately from the unchanged imported build
// profile. One accepted attempt cannot adopt a new tool image or trusted runner.
func (b *Builder) executorDigest() string {
	return objectstore.Digest(comm.Must(json.Marshal(struct {
		HostImage      string `json:"HostImage"`
		ToolImage      string `json:"ToolImage"`
		RunnerDigest   string `json:"RunnerDigest"`
		ContractDigest string `json:"ContractDigest"`
		CategoryDigest string `json:"CategoryDigest"`
		Environment    string `json:"Environment"`
		Compute        string `json:"Compute"`
	}{b.config.HostImage, b.config.ToolImage, objectstore.Digest(hostedRunner), objectstore.Digest(hostedBuildContract), objectstore.Digest(comm.Must(json.Marshal(build.FailureCategoryContract()))), string(cb.EnvironmentTypeArmContainer), string(cb.ComputeTypeBuildGeneral1Large)})))
}

func sameHostedIdentity(a, c build.BuildRequest) bool { a.Attempt = 0; c.Attempt = 0; return a == c }
func (b *Builder) root(in build.BuildRequest) string {
	return in.Environment.String() + "/" + in.ProjectID + "/" + in.ReleaseID + "/" + in.BuildRevisionID
}
func (b *Builder) jobPrefix(in build.BuildRequest) string {
	return b.config.Prefix + "/jobs/" + b.root(in) + "/"
}
func (b *Builder) attemptPrefix(in build.BuildRequest) string {
	return b.jobPrefix(in) + "attempt-" + strconv.FormatUint(in.Attempt, 10) + "/"
}

// tag names one attempt's image in the environment's ECR repository.
func (b *Builder) tag(in build.BuildRequest) string {
	attempt := "-a" + strconv.FormatUint(in.Attempt, 10)
	if in.Environment == deploymentnames.EnvironmentStaging {
		// Staging omits the environment and compacts the revision UUID, so a
		// tag with its longest project ID stays within Docker's 128 characters.
		return b.names.WorkloadPrefix + "-" + in.ProjectID + "-" + in.SourceCommit + "-" + in.ProfileDigest[:16] + "-" + strings.ReplaceAll(strings.TrimPrefix(in.BuildRevisionID, "build-"), "-", "") + attempt
	}
	return b.names.WorkloadPrefix + "-" + in.Environment.String() + "-" + in.ProjectID + "-" + in.SourceCommit + "-" + in.ProfileDigest[:16] + "-" + in.BuildRevisionID + attempt
}
func (b *Builder) token(in build.BuildRequest) string {
	sum := sha256.Sum256([]byte(b.config.ProjectName + "\x00" + in.RequestID + "\x00" + strconv.FormatUint(in.Attempt, 10)))
	return hex.EncodeToString(sum[:])
}

func (b *Builder) Inspect(ctx context.Context, in build.BuildRequest) (build.BuildResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := b.validate(in); err != nil {
		return build.BuildResult{}, err
	}
	head, etag, err := b.readHead(ctx, in)
	if err != nil {
		return build.BuildResult{}, err
	}
	if head == nil {
		return build.BuildResult{State: build.BuildMissing}, nil
	}
	return b.inspectHead(ctx, in, head, etag)
}
func (b *Builder) inspectHead(ctx context.Context, in build.BuildRequest, h *hostedHead, etag string) (build.BuildResult, error) {
	result := build.BuildResult{Attempt: h.Request.Attempt, OperationID: h.BuildID}
	switch h.Phase {
	case hostedCleanup, hostedPreparing:
		result.State = build.BuildMissing
		return result, nil
	case hostedCancelled, hostedFailed:
		result.State = build.BuildFailed
		result.Failure = h.Failure
		result.FailureCategory = h.FailureCategory
		if result.FailureCategory == 0 {
			result.FailureCategory = build.FailureUnclassifiedPlatform
		}
		result.Timings = h.CodeBuild.timings(nil)
		return result, nil
	case hostedReady:
		return b.readyResult(ctx, h)
	case hostedAdmitted:
		execution, err := b.findExecution(ctx, h)
		if err != nil {
			return build.BuildResult{}, err
		}
		if execution == nil {
			result.State = build.BuildUnknown
			return result, nil
		}
		h.BuildID = aws.ToString(execution.Id)
		h.Phase = hostedRunning
		if err = b.writeHead(ctx, in, h, etag); err != nil {
			return build.BuildResult{}, err
		}
		return b.observe(ctx, h, *execution)
	case hostedRunning:
		response, err := b.builds.BatchGetBuilds(ctx, &codebuild.BatchGetBuildsInput{Ids: []string{h.BuildID}})
		if err != nil {
			return build.BuildResult{}, fmt.Errorf("%w: read CodeBuild build %s: %w", errHostedUncertain, h.BuildID, err)
		}
		if len(response.Builds) != 1 {
			result.State = build.BuildUnknown
			return result, nil
		}
		if !b.matches(response.Builds[0], h) {
			return build.BuildResult{}, fmt.Errorf("%w: CodeBuild build %s differs from its admission", errHostedConflict, h.BuildID)
		}
		return b.observe(ctx, h, response.Builds[0])
	default:
		return build.BuildResult{}, fmt.Errorf("%w: unknown admission phase %q", errHostedConflict, h.Phase)
	}
}

// Start admits attempt in.Attempt of the build, uploads its source and
// dispatches it once. A call that finds the attempt already admitted joins its
// operation instead.
func (b *Builder) Start(ctx context.Context, in build.BuildRequest, repositoryToken string) (build.BuildResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, err := b.validate(in)
	if err != nil {
		return build.BuildResult{}, err
	}
	if err = b.checkReleaseAdmission(ctx, in); err != nil {
		return build.BuildResult{}, err
	}
	// This read rejects inherited execution features before dispatch. Actual
	// Build identity is checked again because project configuration can change.
	if err = b.verifyProject(ctx); err != nil {
		return build.BuildResult{}, err
	}
	a, joined, err := b.admitPreparation(ctx, in)
	if err != nil {
		return build.BuildResult{}, err
	}
	if joined != nil {
		return *joined, nil
	}
	// Register this exact job before checking the commit fence again. A concurrent
	// cleanup either sees this PREPARING head, or this read stops source/effects.
	if err = b.checkReleaseAdmission(ctx, in); err != nil {
		if a.owned {
			err = b.abandonPreparation(ctx, in, a, err)
		}
		return build.BuildResult{}, err
	}
	source, err := b.inspectSource(ctx, in)
	if errors.Is(err, errHostedSourceMissing) && a.owned {
		source, err = b.prepareSource(ctx, in, repositoryToken)
		if errors.Is(err, errHostedPreparationRejected) {
			a.head.Phase, a.head.Failure = hostedCancelled, boundedFailure(err.Error())
			if writeErr := b.writeHead(ctx, in, a.head, a.etag); writeErr != nil {
				return build.BuildResult{}, writeErr
			}
			return build.BuildResult{}, err
		}
	}
	if errors.Is(err, errHostedSourceMissing) {
		// Only the owner uploads source; another caller cannot repeat a missing
		// upload, whose outcome stays unknown.
		return build.BuildResult{State: build.BuildUnknown, Attempt: in.Attempt}, nil
	}
	if err != nil {
		return build.BuildResult{}, err
	}
	input := b.startInput(in, profile, source)
	a.head.Source, a.head.Phase, a.head.AdmittedAt = source, hostedAdmitted, time.Now().UTC()
	if err = b.writeHead(ctx, in, a.head, a.etag); err != nil {
		return build.BuildResult{}, err
	}
	return b.dispatch(ctx, in, a.head, input)
}

// admission is the PREPARING head of one attempt with the ETag that guards its
// next write.
type admission struct {
	head *hostedHead
	etag string
	// owned says this call created the head, which alone permits the source
	// upload.
	owned bool
}

// newHead is a new admission of in under this builder's executor.
func (b *Builder) newHead(in build.BuildRequest, phase hostedPhase, failure string) *hostedHead {
	return &hostedHead{Schema: hostedHeadSchema, ExecutorDigest: b.executorDigest(), HostImage: b.config.HostImage, Request: in, Phase: phase, Failure: failure}
}

// admitPreparation returns the PREPARING admission of attempt in.Attempt,
// which it creates when none exists or a failed older attempt holds it. When
// an operation is already admitted for in, it returns that operation's result
// for the caller to join instead.
func (b *Builder) admitPreparation(ctx context.Context, in build.BuildRequest) (admission, *build.BuildResult, error) {
	h, etag, err := b.readHead(ctx, in)
	if err != nil {
		return admission{}, nil, err
	}
	owned := h == nil
	if h != nil {
		if h.Phase == hostedCleanup {
			return admission{}, nil, errors.New("hosted build permanently closed by cleanup")
		}
		var result build.BuildResult
		if result, err = b.inspectHead(ctx, in, h, etag); err != nil {
			return admission{}, nil, err
		}
		if result.State != build.BuildMissing && (result.State != build.BuildFailed || in.Attempt <= h.Request.Attempt) {
			return admission{}, &result, nil
		}
		if in.Attempt < h.Request.Attempt {
			return admission{}, nil, fmt.Errorf("%w: attempt %d precedes the admitted attempt %d", errHostedConflict, in.Attempt, h.Request.Attempt)
		}
		if result.State == build.BuildFailed {
			if etag, err = b.supersedeFailed(ctx, in); err != nil {
				return admission{}, nil, err
			}
			owned = true
		}
	}
	if owned {
		if err = b.writeHead(ctx, in, b.newHead(in, hostedPreparing, ""), etag); err != nil {
			return admission{}, nil, err
		}
		if h, etag, err = b.readHead(ctx, in); err != nil {
			return admission{}, nil, err
		}
	}
	if h == nil || h.Phase != hostedPreparing || h.Request.Attempt != in.Attempt || h.ExecutorDigest != b.executorDigest() {
		return admission{}, nil, fmt.Errorf("%w: another owner changed the PREPARING admission of attempt %d", errHostedConflict, in.Attempt)
	}
	return admission{head: h, etag: etag, owned: owned}, nil, nil
}

// supersedeFailed proves again that a FAILED or CANCELLED older attempt holds
// the admission of in and removes that attempt's image. It returns the ETag
// that the PREPARING admission of in must replace.
func (b *Builder) supersedeFailed(ctx context.Context, in build.BuildRequest) (string, error) {
	h, etag, err := b.readHead(ctx, in)
	if err != nil {
		return "", err
	}
	if h == nil || (h.Phase != hostedFailed && h.Phase != hostedCancelled) || in.Attempt <= h.Request.Attempt {
		return "", fmt.Errorf("%w: the failed admission changed before attempt %d", errHostedConflict, in.Attempt)
	}
	// The Release's explicit source-preparation admission supplies a new,
	// strictly higher attempt. Bind its current trusted executor only after
	// the prior operation is proven terminal. Versioned head/source/result
	// objects retain that old operation unchanged. Same-attempt recovery
	// and image builds keep their accepted executor fence.
	if h.ExecutorDigest != b.executorDigest() && !in.SourceOnly {
		return "", errors.New("accepted build executor changed; a new versioned profile is required")
	}
	if err = b.deleteImage(ctx, h.Request); err != nil {
		return "", err
	}
	return etag, nil
}

// abandonPreparation records the owned admission a as CANCELLED after cleanup
// closed the Release, which closed reports. The owner has not begun uploading
// source, and the record keeps a concurrent cleanup from waiting forever for
// that upload.
func (b *Builder) abandonPreparation(ctx context.Context, in build.BuildRequest, a admission, closed error) error {
	ctx, stop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer stop()
	cancelled := *a.head
	cancelled.Phase = hostedCancelled
	cancelled.Failure = boundedFailure("Release cleanup closed build admission: " + closed.Error())
	writeErr := b.writeHead(ctx, in, &cancelled, a.etag)
	actual, _, readErr := b.readHead(ctx, in)
	if readErr != nil || actual == nil || *actual != cancelled {
		return errors.Join(closed, writeErr, readErr, errHostedUncertain)
	}
	return closed
}

// dispatch starts the CodeBuild execution of the ADMITTED head h. Only the
// caller whose CAS wrote h may dispatch: later callers cannot replay the call,
// even after CodeBuild's five-minute idempotency-token window expires.
func (b *Builder) dispatch(ctx context.Context, in build.BuildRequest, h *hostedHead, input *codebuild.StartBuildInput) (build.BuildResult, error) {
	response, err := b.builds.StartBuild(ctx, input)
	var rejected *cb.InvalidInputException
	if errors.As(err, &rejected) {
		// A typed validation rejection proves this call created no execution.
		// Retain source and the dispatch fence; only a new accepted attempt may retry.
		latest, latestETag, readErr := b.readHead(ctx, in)
		if readErr != nil {
			return build.BuildResult{}, readErr
		}
		if latest == nil || latest.Phase != hostedAdmitted || latest.Request != in || latest.Source != h.Source {
			return build.BuildResult{}, fmt.Errorf("%w: the admission changed after CodeBuild rejected the dispatch: %w", errHostedConflict, err)
		}
		latest.Phase = hostedCancelled
		latest.Failure = boundedFailure("CodeBuild rejected the dispatch: " + err.Error())
		if writeErr := b.writeHead(ctx, in, latest, latestETag); writeErr != nil {
			return build.BuildResult{}, writeErr
		}
		return build.BuildResult{State: build.BuildFailed, Attempt: in.Attempt, Failure: latest.Failure, FailureCategory: build.FailureUnclassifiedPlatform}, nil
	}
	var limited *cb.AccountLimitExceededException
	if errors.As(err, &limited) {
		// A typed account-limit rejection also proves no execution exists. It is
		// capacity, not a verdict on the attempt: reopen PREPARING, whose source
		// stays, so the caller's retry dispatches again as the next CAS winner.
		latest, latestETag, readErr := b.readHead(ctx, in)
		if readErr != nil {
			return build.BuildResult{}, readErr
		}
		if latest == nil || latest.Phase != hostedAdmitted || latest.Request != in || latest.Source != h.Source {
			return build.BuildResult{}, fmt.Errorf("%w: the admission changed after CodeBuild reported its account limit: %w", errHostedConflict, err)
		}
		latest.Phase = hostedPreparing
		if writeErr := b.writeHead(ctx, in, latest, latestETag); writeErr != nil {
			return build.BuildResult{}, writeErr
		}
		return build.BuildResult{}, fmt.Errorf("%w: %w", errHostedCapacity, err)
	}
	// Any other failure may still have created the execution. The admitted head
	// stays the dispatch fence, and the caller's retry inspects it, which finds
	// the execution by its identity instead of dispatching again.
	if err != nil {
		return build.BuildResult{}, &platform.OutcomeUnknownError{Sentinel: errHostedUncertain, Message: "hosted build dispatch is unconfirmed", Cause: err}
	}
	if response.Build == nil {
		return build.BuildResult{}, fmt.Errorf("%w: StartBuild returned no build", errHostedUncertain)
	}
	if !b.matches(*response.Build, h) {
		return build.BuildResult{}, fmt.Errorf("%w: CodeBuild started build %s that differs from its admission", errHostedConflict, aws.ToString(response.Build.Id))
	}
	latest, latestETag, err := b.readHead(ctx, in)
	if err != nil {
		return build.BuildResult{}, err
	}
	if latest == nil || latest.Phase != hostedAdmitted || latest.Request.Attempt != in.Attempt {
		return build.BuildResult{}, fmt.Errorf("%w: the admission of started build %s changed", errHostedConflict, aws.ToString(response.Build.Id))
	}
	latest.BuildID = aws.ToString(response.Build.Id)
	latest.Phase = hostedRunning
	if err = b.writeHead(ctx, in, latest, latestETag); err != nil {
		return build.BuildResult{}, err
	}
	return b.observe(ctx, latest, *response.Build)
}

// findExecution finds the execution of the ADMITTED head h without scanning the
// project's history: builds list newest first and the execution was created
// after h.AdmittedAt, so the search ends at the first build older than that.
// Its cost is the builds created since the admission, and nil is absence only
// when the search reached that point. Exhaustion preserves UNKNOWN and never
// permits another provider dispatch.
func (b *Builder) findExecution(ctx context.Context, h *hostedHead) (*cb.Build, error) {
	var cursor *string
	var found *cb.Build
	horizon := h.AdmittedAt.Add(-findClockSkew)
	for page := 0; page < findMaxPages; page++ {
		list, err := b.builds.ListBuildsForProject(ctx, &codebuild.ListBuildsForProjectInput{ProjectName: aws.String(b.config.ProjectName), SortOrder: cb.SortOrderTypeDescending, NextToken: cursor})
		if err != nil {
			return nil, fmt.Errorf("%w: list CodeBuild builds: %w", errHostedUncertain, err)
		}
		reachedHorizon := false
		if len(list.Ids) > 0 {
			batch, err := b.builds.BatchGetBuilds(ctx, &codebuild.BatchGetBuildsInput{Ids: list.Ids})
			if err != nil {
				return nil, fmt.Errorf("%w: read CodeBuild builds: %w", errHostedUncertain, err)
			}
			for _, v := range batch.Builds {
				if v.StartTime != nil && v.StartTime.Before(horizon) {
					reachedHorizon = true
				}
				if b.matches(v, h) {
					if found != nil && aws.ToString(found.Id) != aws.ToString(v.Id) {
						return nil, fmt.Errorf("%w: CodeBuild builds %s and %s both match one admission", errHostedConflict, aws.ToString(found.Id), aws.ToString(v.Id))
					}
					match := v
					found = &match
				}
			}
		}
		cursor = list.NextToken
		if reachedHorizon || aws.ToString(cursor) == "" {
			return found, nil
		}
	}
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("%w: the dispatched build was not found within %d pages of builds created since its admission", errHostedUncertain, findMaxPages)
}
func (b *Builder) matches(v cb.Build, h *hostedHead) bool {
	if aws.ToString(v.ProjectName) != b.config.ProjectName || v.Source == nil || v.Source.Type != cb.SourceTypeS3 || aws.ToString(v.Source.Location) != b.config.Bucket+"/"+h.Source.Key || aws.ToString(v.SourceVersion) != h.Source.Version || v.Environment == nil || !hostedEnvironment(v.Environment, h.HostImage) || len(v.FileSystemLocations) != 0 || len(v.SecondarySources) != 0 || len(v.SecondarySourceVersions) != 0 || len(v.SecondaryArtifacts) != 0 || (v.Cache != nil && v.Cache.Type != cb.CacheTypeNoCache) || v.VpcConfig != nil {
		return false
	}
	values := map[string]string{}
	for _, value := range v.Environment.EnvironmentVariables {
		name := aws.ToString(value.Name)
		if value.Type != cb.EnvironmentVariableTypePlaintext || (name != "SV2_BUILD_IDENTITY" && name != "SV2_PROFILE_DIGEST" && name != "SV2_EXECUTOR_DIGEST" && name != "SV2_BUILD_CONFIG") {
			return false
		}
		if _, duplicate := values[name]; duplicate {
			return false
		}
		values[name] = aws.ToString(value.Value)
	}
	if len(values) != 4 {
		return false
	}
	return values["SV2_BUILD_IDENTITY"] == b.token(h.Request) && values["SV2_PROFILE_DIGEST"] == h.Request.ProfileDigest && values["SV2_EXECUTOR_DIGEST"] == h.ExecutorDigest
}
func (b *Builder) observe(ctx context.Context, h *hostedHead, v cb.Build) (build.BuildResult, error) {
	result := build.BuildResult{Attempt: h.Request.Attempt, OperationID: aws.ToString(v.Id)}
	switch v.BuildStatus {
	case cb.StatusTypeInProgress:
		result.State = build.BuildRunning
		return result, nil
	case cb.StatusTypeFailed, cb.StatusTypeFault, cb.StatusTypeStopped, cb.StatusTypeTimedOut, cb.StatusTypeSucceeded:
		// Like the result and the reason below, the measured phases join h,
		// which cleanup compares with the recorded head.
		h.CodeBuild = measureCodeBuild(v)
		_, ref, err := b.readResult(ctx, h.Request, objectstore.ObjectRef{})
		if errors.Is(err, errHostedResultMissing) && v.BuildStatus != cb.StatusTypeSucceeded {
			// A build without a result reports CodeBuild's phases and the
			// runner's fixed category, when its private record exists.
			category, categoryErr := b.readFailureCategory(ctx, h)
			if categoryErr != nil {
				return build.BuildResult{}, categoryErr
			}
			h.FailureCategory = category
			h.Failure = codeBuildFailure(v)
			result.Failure = h.Failure
			result.FailureCategory = category
			result.Timings = h.CodeBuild.timings(nil)
			if err = b.finish(ctx, h, hostedFailed, objectstore.ObjectRef{}, h.Failure); err != nil {
				return build.BuildResult{}, err
			}
			result.State = build.BuildFailed
			return result, nil
		}
		if err != nil {
			return build.BuildResult{}, err
		}
		// A result published before a lost response remains verifiable even if that
		// response made CodeBuild report FAILED. No app code runs after publication.
		h.Result = ref
		ready, err := b.readyResult(ctx, h)
		if err != nil {
			return build.BuildResult{}, err
		}
		if err = b.finish(ctx, h, hostedReady, ref, ""); err != nil {
			return build.BuildResult{}, err
		}
		return ready, nil
	default:
		result.State = build.BuildUnknown
		return result, nil
	}
}
func (b *Builder) readyResult(ctx context.Context, h *hostedHead) (build.BuildResult, error) {
	artifact, _, err := b.readResult(ctx, h.Request, h.Result)
	if err != nil {
		return build.BuildResult{}, err
	}
	if artifact.Schema != hostedResultSchema || artifact.ExecutorDigest != h.ExecutorDigest || artifact.Request != h.Request || artifact.BuildID != h.BuildID {
		return build.BuildResult{}, fmt.Errorf("%w: the result of build %s names another schema, executor, request or build", errHostedConflict, h.BuildID)
	}
	if h.Request.SourceOnly {
		if artifact.ImageDigest != "" || artifact.ImageReference != "" {
			return build.BuildResult{}, fmt.Errorf("%w: source preparation %s produced an image", errHostedConflict, h.BuildID)
		}
		return build.BuildResult{State: build.BuildReady, Attempt: h.Request.Attempt, OperationID: h.BuildID, Payloads: artifact.Payloads, Timings: h.CodeBuild.timings(artifact.Timings)}, nil
	}
	if !flowid.IsSHA256Digest(artifact.ImageDigest) || artifact.ImageReference != b.config.RepositoryURI+"@"+artifact.ImageDigest {
		return build.BuildResult{}, fmt.Errorf("%w: build %s reports image %q outside the application repository", errHostedConflict, h.BuildID, artifact.ImageReference)
	}
	tag := b.tag(h.Request)
	response, err := b.images.DescribeImages(ctx, &ecr.DescribeImagesInput{RegistryId: aws.String(b.config.AccountID), RepositoryName: aws.String(b.repository), ImageIds: []ec.ImageIdentifier{{ImageTag: aws.String(tag)}}})
	if err != nil {
		return build.BuildResult{}, fmt.Errorf("%w: describe ECR image %s: %w", errHostedUncertain, tag, err)
	}
	if len(response.ImageDetails) != 1 || aws.ToString(response.ImageDetails[0].ImageDigest) != artifact.ImageDigest {
		return build.BuildResult{}, fmt.Errorf("%w: ECR image %s is not the reported digest %s", errHostedUncertain, tag, artifact.ImageDigest)
	}
	return build.BuildResult{State: build.BuildReady, Attempt: h.Request.Attempt, OperationID: h.BuildID, ImageReference: artifact.ImageReference, ImageDigest: artifact.ImageDigest, Payloads: artifact.Payloads, Timings: h.CodeBuild.timings(artifact.Timings)}, nil
}

// ReadLog reads one page of the CloudWatch log stream of the CodeBuild
// execution that the build's admission receipt names, never one a cursor
// names: the cursor is the receipt's attempt and CloudWatch's
// nextForwardToken, so a cursor of an earlier attempt restarts at the new
// attempt's log and a forged one can only fail. A build that never ran, or
// whose execution or stream CodeBuild no longer keeps, has no log.
func (b *Builder) ReadLog(ctx context.Context, in build.BuildRequest, read build.LogRead) (build.LogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := b.validate(in); err != nil {
		return build.LogPage{}, err
	}
	attempt, token, err := parseLogCursor(read.Cursor)
	if err != nil {
		return build.LogPage{}, err
	}
	h, _, err := b.readHead(ctx, in)
	if err != nil {
		return build.LogPage{}, err
	}
	if h == nil || h.BuildID == "" {
		// Nothing runs without an execution, unless the admitted one is still found.
		return build.LogPage{Next: read.Cursor, Complete: h == nil || h.Phase != hostedAdmitted && h.Phase != hostedPreparing}, nil
	}
	if attempt != h.Request.Attempt {
		token = ""
	}
	response, err := b.builds.BatchGetBuilds(ctx, &codebuild.BatchGetBuildsInput{Ids: []string{h.BuildID}})
	if err != nil {
		return build.LogPage{}, logReadError(fmt.Errorf("read CodeBuild build %s: %w", h.BuildID, err))
	}
	if len(response.Builds) != 1 {
		return build.LogPage{Next: read.Cursor, Complete: true}, nil
	}
	if !b.matches(response.Builds[0], h) {
		return build.LogPage{}, fmt.Errorf("%w: CodeBuild build %s differs from its admission", errHostedConflict, h.BuildID)
	}
	execution := response.Builds[0]
	running := execution.BuildStatus == cb.StatusTypeInProgress
	if execution.Logs == nil || aws.ToString(execution.Logs.StreamName) == "" {
		return build.LogPage{Next: read.Cursor, Complete: !running}, nil
	}
	group, stream := aws.ToString(execution.Logs.GroupName), aws.ToString(execution.Logs.StreamName)
	if group != "/aws/codebuild/"+b.config.ProjectName {
		return build.LogPage{}, fmt.Errorf("%w: CodeBuild build %s logs to group %s", errHostedConflict, h.BuildID, group)
	}
	page, next, err := b.readLogEvents(ctx, group, stream, token, read)
	if platform.HasAWSErrorCode(err, "ResourceNotFoundException") {
		return build.LogPage{Next: read.Cursor, Complete: !running}, nil
	}
	if err != nil {
		return build.LogPage{}, logReadError(fmt.Errorf("read the CloudWatch log of CodeBuild build %s: %w", h.BuildID, err))
	}
	return build.LogPage{Text: page, Next: logCursor(h.Request.Attempt, next), Complete: !running && page == ""}, nil
}

// logReadError classifies a log read that an AWS rate limit refused as
// build.ErrLogThrottled.
func logReadError(err error) error {
	if platform.HasAWSErrorCode(err, "ThrottlingException", "TooManyRequestsException") {
		return fmt.Errorf("%w: %w", build.ErrLogThrottled, err)
	}
	return err
}

// logEventsLimit is the most CloudWatch events one log page asks for.
const logEventsLimit = 500

// readLogEvents reads the page after token, or the stream's end with
// read.Tail, and returns its text and the forward token after it. A page whose
// events exceed read.MaximumBytes is read again with as many events as fit,
// so the token never skips an event; one longer event is cut.
func (b *Builder) readLogEvents(ctx context.Context, group, stream, token string, read build.LogRead) (string, string, error) {
	input := &cloudwatchlogs.GetLogEventsInput{LogGroupName: &group, LogStreamName: &stream, StartFromHead: aws.Bool(!read.Tail), Limit: aws.Int32(logEventsLimit)}
	if token != "" && !read.Tail {
		input.NextToken = &token
	}
	events, err := b.logs.GetLogEvents(ctx, input)
	if err != nil {
		return "", "", err
	}
	if read.Tail {
		return comm.TailUTF8(logText(events.Events), read.MaximumBytes), aws.ToString(events.NextForwardToken), nil
	}
	fitting, size := 0, 0
	for _, event := range events.Events {
		if size += len(logLine(event)); size > read.MaximumBytes {
			break
		}
		fitting++
	}
	if fitting == len(events.Events) {
		return logText(events.Events), aws.ToString(events.NextForwardToken), nil
	}
	input.Limit = aws.Int32(int32(max(fitting, 1))) //nolint:gosec // at most logEventsLimit
	if events, err = b.logs.GetLogEvents(ctx, input); err != nil {
		return "", "", err
	}
	return comm.TruncateUTF8(logText(events.Events), read.MaximumBytes), aws.ToString(events.NextForwardToken), nil
}

// logText joins log events into lines.
func logText(events []cwtypes.OutputLogEvent) string {
	var text strings.Builder
	for _, event := range events {
		text.WriteString(logLine(event))
	}
	return text.String()
}

// logLine is an event's message as one line.
func logLine(event cwtypes.OutputLogEvent) string {
	message := aws.ToString(event.Message)
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	return message
}

// logCursor names the attempt and the forward token after a page.
func logCursor(attempt uint64, token string) string {
	if token == "" {
		return ""
	}
	return strconv.FormatUint(attempt, 10) + ":" + token
}

// parseLogCursor reads a cursor of logCursor; an empty one starts the log.
func parseLogCursor(cursor string) (uint64, string, error) {
	if cursor == "" {
		return 0, "", nil
	}
	attempt, token, found := strings.Cut(cursor, ":")
	number, err := strconv.ParseUint(attempt, 10, 64)
	if !found || err != nil || number < 1 || number > build.MaxAttempt || !strings.HasPrefix(token, "f/") || len(token) > 512 {
		return 0, "", fmt.Errorf("%w: %q", build.ErrInvalidLogCursor, cursor)
	}
	return number, token, nil
}

func (b *Builder) Cancel(ctx context.Context, in build.BuildRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := b.validate(in); err != nil {
		return err
	}
	return b.cancelBuild(ctx, in)
}

// cancelBuild cancels the build of the validated request in.
func (b *Builder) cancelBuild(ctx context.Context, in build.BuildRequest) error {
	h, etag, err := b.readHead(ctx, in)
	if err != nil {
		return err
	}
	if h == nil {
		return b.writeHead(ctx, in, b.newHead(in, hostedCancelled, "cancelled before admission"), "")
	}
	if in.Attempt < h.Request.Attempt {
		return fmt.Errorf("%w: cancellation attempt %d precedes the admitted attempt %d", errHostedConflict, in.Attempt, h.Request.Attempt)
	}
	if h.Phase == hostedPreparing {
		if _, err = b.inspectSource(ctx, h.Request); err != nil {
			return fmt.Errorf("%w: the source upload of a PREPARING build: %w", errHostedUncertain, err)
		}
		h.Phase = hostedCancelled
		h.Failure = "cancelled while its source was prepared"
		return b.writeHead(ctx, in, h, etag)
	}
	result, err := b.inspectHead(ctx, in, h, etag)
	if err != nil {
		return err
	}
	if result.State == build.BuildUnknown {
		return fmt.Errorf("%w: %w: the %s build to cancel has an unknown outcome", build.ErrCancelUnconfirmed, errHostedUncertain, h.Phase)
	}
	if result.State != build.BuildRunning {
		return nil
	}
	if _, err = b.builds.StopBuild(ctx, &codebuild.StopBuildInput{Id: aws.String(result.OperationID)}); err != nil {
		return fmt.Errorf("%w: stop CodeBuild build %s: %w", errHostedUncertain, result.OperationID, err)
	}
	// Stop acceptance is not termination. The durable caller must query again.
	return fmt.Errorf("%w: CodeBuild accepted the stop of build %s, whose termination is not yet observed", build.ErrPending, result.OperationID)
}
func (b *Builder) deleteImage(ctx context.Context, in build.BuildRequest) error {
	if in.SourceOnly {
		return nil
	}
	tag := b.tag(in)
	out, err := b.images.BatchDeleteImage(ctx, &ecr.BatchDeleteImageInput{RegistryId: aws.String(b.config.AccountID), RepositoryName: aws.String(b.repository), ImageIds: []ec.ImageIdentifier{{ImageTag: aws.String(tag)}}})
	if err != nil {
		return fmt.Errorf("%w: delete ECR image %s: %w", errHostedUncertain, tag, err)
	}
	for _, failure := range out.Failures {
		if failure.FailureCode != ec.ImageFailureCodeImageNotFound {
			return fmt.Errorf("%w: delete ECR image %s: %s %s", errHostedUncertain, tag, failure.FailureCode, aws.ToString(failure.FailureReason))
		}
	}
	return nil
}

// codeBuildFailure says how a finished build without a result ended: its
// status and the first unsuccessful phase with CodeBuild's context, which
// names the failed command, never its output.
func codeBuildFailure(v cb.Build) string {
	reason := fmt.Sprintf("CodeBuild build %s ended %s", aws.ToString(v.Id), v.BuildStatus)
	failed := slices.IndexFunc(v.Phases, func(phase cb.BuildPhase) bool {
		return phase.PhaseStatus != "" && phase.PhaseStatus != cb.StatusTypeSucceeded && phase.PhaseStatus != cb.StatusTypeInProgress
	})
	if failed < 0 {
		return boundedFailure(reason)
	}
	phase := v.Phases[failed]
	details := make([]string, len(phase.Contexts))
	for i, detail := range phase.Contexts {
		details[i] = aws.ToString(detail.StatusCode) + " " + aws.ToString(detail.Message)
	}
	return boundedFailure(fmt.Sprintf("%s in phase %s (%s): %s", reason, phase.PhaseType, phase.PhaseStatus, strings.Join(details, "; ")))
}

// codeBuildPhases are CodeBuild's measurements of the phases before the
// runner: QUEUE and CodeBuild's share of HOST_PREPARATION. A phase CodeBuild
// never ended stays zero.
type codeBuildPhases struct {
	Queue           build.BuildPhaseTiming `json:"queue,omitzero"`
	HostPreparation build.BuildPhaseTiming `json:"hostPreparation,omitzero"`
}

// measureCodeBuild sums the ended CodeBuild phases of QUEUE (SUBMITTED and
// QUEUED) and of the host's preparation (PROVISIONING, DOWNLOAD_SOURCE and
// INSTALL, which starts the host's Docker daemon).
func measureCodeBuild(v cb.Build) codeBuildPhases {
	sum := func(phase string, types ...cb.BuildPhaseType) build.BuildPhaseTiming {
		var timing build.BuildPhaseTiming
		for _, p := range v.Phases {
			if !slices.Contains(types, p.PhaseType) || p.StartTime == nil || p.EndTime == nil {
				continue
			}
			if timing.Phase == "" {
				timing = build.BuildPhaseTiming{Phase: phase, StartedAt: p.StartTime.UTC()}
			}
			timing.DurationMs += max(p.EndTime.Sub(*p.StartTime).Milliseconds(), 1)
		}
		return timing
	}
	return codeBuildPhases{
		Queue:           sum(build.PhaseQueue, cb.BuildPhaseTypeSubmitted, cb.BuildPhaseTypeQueued),
		HostPreparation: sum(build.PhaseHostPreparation, cb.BuildPhaseTypeProvisioning, cb.BuildPhaseTypeDownloadSource, cb.BuildPhaseTypeInstall),
	}
}

// timings are a build's phases: CodeBuild's, then the runner's, whose own
// host work extends HOST_PREPARATION.
func (c codeBuildPhases) timings(runner []build.BuildPhaseTiming) []build.BuildPhaseTiming {
	var timings []build.BuildPhaseTiming
	for _, timing := range append([]build.BuildPhaseTiming{c.Queue, c.HostPreparation}, runner...) {
		if i := slices.IndexFunc(timings, func(t build.BuildPhaseTiming) bool { return t.Phase == timing.Phase }); i >= 0 {
			timings[i].DurationMs += timing.DurationMs
		} else if timing.Phase != "" {
			timings = append(timings, timing)
		}
	}
	return timings
}

// boundedFailure keeps the first 512 bytes of a failure reason, which a
// Release build record carries.
func boundedFailure(reason string) string {
	if len(reason) > 512 {
		return strings.ToValidUTF8(reason[:512], "") + "..."
	}
	return reason
}

func hostedEnvironment(environment *cb.ProjectEnvironment, hostImage string) bool {
	return environment != nil && aws.ToString(environment.Image) == hostImage &&
		environment.Type == cb.EnvironmentTypeArmContainer && environment.ComputeType == cb.ComputeTypeBuildGeneral1Large &&
		aws.ToBool(environment.PrivilegedMode) && environment.ImagePullCredentialsType == cb.ImagePullCredentialsTypeServiceRole &&
		environment.Fleet == nil && environment.DockerServer == nil && environment.ComputeConfiguration == nil &&
		environment.RegistryCredential == nil && aws.ToString(environment.Certificate) == ""
}
func (b *Builder) verifyProject(ctx context.Context) error {
	response, err := b.builds.BatchGetProjects(ctx, &codebuild.BatchGetProjectsInput{Names: []string{b.config.ProjectName}})
	if err != nil {
		return fmt.Errorf("read CodeBuild project %s: %w", b.config.ProjectName, err)
	}
	if len(response.Projects) != 1 {
		return fmt.Errorf("read CodeBuild project %s: CodeBuild returned %d projects", b.config.ProjectName, len(response.Projects))
	}
	p := response.Projects[0]
	if aws.ToString(p.Name) != b.config.ProjectName || !hostedEnvironment(p.Environment, b.config.HostImage) ||
		len(p.Environment.EnvironmentVariables) != 0 || p.Source == nil || p.Source.Type != cb.SourceTypeNoSource ||
		p.Artifacts == nil || p.Artifacts.Type != cb.ArtifactsTypeNoArtifacts ||
		len(p.SecondarySources) != 0 || len(p.SecondarySourceVersions) != 0 || len(p.SecondaryArtifacts) != 0 ||
		len(p.FileSystemLocations) != 0 || p.BuildBatchConfig != nil || p.VpcConfig != nil || p.Webhook != nil ||
		(p.Cache != nil && p.Cache.Type != cb.CacheTypeNoCache) || aws.ToInt32(p.AutoRetryLimit) != 0 ||
		p.ProjectVisibility != cb.ProjectVisibilityTypePrivate {
		return fmt.Errorf("CodeBuild project %s has unsupported inherited settings", b.config.ProjectName)
	}
	return nil
}
