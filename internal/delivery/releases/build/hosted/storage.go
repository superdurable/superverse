// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	st "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

const hostedHeadSchema = "superverse.dev/hosted-build-admission/v1"

// hostedResultSchema is the runner's result, which v2 extends with the
// runner's measured phases.
const hostedResultSchema = "superverse.dev/hosted-build-result/v2"

// hostedPhase is where an admission of a hosted build stands, as its head
// records it.
type hostedPhase string

const (
	// hostedPreparing: the attempt is admitted and its source is being prepared.
	hostedPreparing hostedPhase = "PREPARING"
	// hostedAdmitted: the source is uploaded and the build may start.
	hostedAdmitted hostedPhase = "ADMITTED"
	hostedRunning  hostedPhase = "RUNNING"
	hostedReady    hostedPhase = "READY"
	hostedFailed   hostedPhase = "FAILED"
	// hostedCancelled: the attempt ended before or instead of its build.
	hostedCancelled hostedPhase = "CANCELLED"
	// hostedCleanup: the release's builds are being cleaned up; nothing starts.
	hostedCleanup hostedPhase = "CLEANUP"
)

// This record fences provider dispatch and cleanup, not Release business state.
// BuildID is the actual CodeBuild execution identity; no Release business status projection is kept.
type hostedHead struct {
	ExecutorDigest string                `json:"executorDigest"`
	HostImage      string                `json:"hostImage"`
	Schema         string                `json:"schemaVersion"`
	Request        build.BuildRequest    `json:"request"`
	Phase          hostedPhase           `json:"phase"`
	Source         objectstore.ObjectRef `json:"source"`
	BuildID        string                `json:"buildId,omitempty"`
	Result         objectstore.ObjectRef `json:"result"`
	AdmittedAt     time.Time             `json:"admittedAt,omitempty"`
	// Failure says why a CANCELLED or FAILED build ended, bounded by
	// boundedFailure.
	Failure string `json:"failure,omitempty"`
	// CodeBuild is CodeBuild's measurement of the phases before the runner,
	// kept once the build terminated because CodeBuild history expires.
	CodeBuild codeBuildPhases `json:"codeBuildPhases,omitzero"`
}
type hostedResult struct {
	ExecutorDigest string             `json:"executorDigest"`
	Schema         string             `json:"schemaVersion"`
	Request        build.BuildRequest `json:"request"`
	BuildID        string             `json:"buildId"`
	ImageReference string             `json:"artifactReference"`
	ImageDigest    string             `json:"imageId"`
	Payloads       build.Payloads     `json:"releaseArtifactPayloads"`
	// Timings are the runner's measured phases.
	Timings []build.BuildPhaseTiming `json:"phaseTimings"`
}

func (b *Builder) headKey(in build.BuildRequest) string {
	return b.config.Prefix + "/receipts/" + b.root(in) + ".json"
}
func decodeHosted(data []byte, target any) error {
	if err := comm.DecodeStrictJSON(data, target); err != nil {
		return fmt.Errorf("%w: decode the provider record: %w", errHostedConflict, err)
	}
	return nil
}
func (b *Builder) getObject(ctx context.Context, key, version string, limit int64) ([]byte, string, string, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(b.config.Bucket), ExpectedBucketOwner: &b.config.AccountID, Key: aws.String(key)}
	if version != "" {
		input.VersionId = &version
	}
	response, err := b.objects.GetObject(ctx, input)
	if err != nil {
		return nil, "", "", fmt.Errorf("read S3 object %s: %w", key, err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read S3 object %s: %w", key, err)
	}
	observedVersion := aws.ToString(response.VersionId)
	switch {
	case int64(len(data)) > limit || len(data) == 0:
		return nil, "", "", fmt.Errorf("S3 object %s: its size %d is outside 1..%d bytes", key, len(data), limit)
	case !objectstore.IsVersion(observedVersion) || (version != "" && observedVersion != version):
		return nil, "", "", fmt.Errorf("S3 object %s: version %q is not the requested version %q", key, observedVersion, version)
	case aws.ToString(response.ETag) == "":
		return nil, "", "", fmt.Errorf("S3 object %s: the response has no ETag", key)
	case response.ServerSideEncryption != st.ServerSideEncryptionAwsKms || aws.ToString(response.SSEKMSKeyId) != b.config.KMSKeyARN:
		return nil, "", "", fmt.Errorf("S3 object %s: encryption %q does not use the configured KMS key", key, response.ServerSideEncryption)
	}
	return data, aws.ToString(response.ETag), observedVersion, nil
}
func (b *Builder) readHead(ctx context.Context, in build.BuildRequest) (*hostedHead, string, error) {
	key := b.headKey(in)
	data, etag, _, err := b.getObject(ctx, key, "", 16384)
	if platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound") {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read the admission receipt: %w", err)
	}
	var head hostedHead
	if err = decodeHosted(data, &head); err != nil {
		return nil, "", fmt.Errorf("admission %s: %w", key, err)
	}
	if head.Schema != hostedHeadSchema || !flowid.IsSHA256Digest(head.ExecutorDigest) || !b.executorImage.MatchString(head.HostImage) || !sameHostedIdentity(head.Request, in) {
		return nil, "", fmt.Errorf("%w: admission %s has another schema, executor or request identity", errHostedConflict, key)
	}
	// The caller validated in, which the head's request equals but for its
	// attempt.
	if head.Request.Attempt < 1 || head.Request.Attempt > build.MaxAttempt {
		return nil, "", fmt.Errorf("%w: admission %s has attempt %d outside 1..%d", errHostedConflict, key, head.Request.Attempt, build.MaxAttempt)
	}
	if head.Phase == hostedAdmitted || head.Phase == hostedRunning || head.Phase == hostedReady || head.Phase == hostedFailed {
		if head.Source.Key != b.attemptPrefix(head.Request)+"source.zip" || !objectstore.IsVersion(head.Source.Version) || !flowid.IsSHA256Digest(head.Source.Digest) || head.Source.MediaType != objectstore.MediaTypeZip || head.AdmittedAt.IsZero() {
			return nil, "", fmt.Errorf("%w: %s admission %s has no exact source", errHostedConflict, head.Phase, key)
		}
	}
	if (head.Phase == hostedRunning || head.Phase == hostedReady || head.Phase == hostedFailed) && !strings.HasPrefix(head.BuildID, b.config.ProjectName+":") {
		return nil, "", fmt.Errorf("%w: %s admission %s names build %q of another project", errHostedConflict, head.Phase, key, head.BuildID)
	}
	if head.Phase == hostedReady && (head.Result.Key != b.attemptPrefix(head.Request)+"result.json" || !objectstore.IsVersion(head.Result.Version) || !flowid.IsSHA256Digest(head.Result.Digest) || head.Result.MediaType != objectstore.MediaTypeJSON) {
		return nil, "", fmt.Errorf("%w: READY admission %s has no exact result", errHostedConflict, key)
	}
	return &head, etag, nil
}
func (b *Builder) putObject(ctx context.Context, key, contentType string, data []byte, etag string) (objectstore.ObjectRef, error) {
	input := &s3.PutObjectInput{Bucket: aws.String(b.config.Bucket), ExpectedBucketOwner: &b.config.AccountID, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType, ServerSideEncryption: st.ServerSideEncryptionAwsKms, SSEKMSKeyId: &b.config.KMSKeyARN, Metadata: map[string]string{"sha256": strings.TrimPrefix(objectstore.Digest(data), "sha256:")}}
	if etag == "" {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = &etag
	}
	response, err := b.objects.PutObject(ctx, input)
	if platform.HasAWSErrorCode(err, "PreconditionFailed", "ConditionalRequestConflict") {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: conditional write of %s: %w", errHostedConflict, key, err)
	}
	if err != nil {
		return objectstore.ObjectRef{}, &platform.OutcomeUnknownError{Sentinel: errHostedUncertain, Message: "write of S3 object " + key + " is unconfirmed", Cause: err}
	}
	version := aws.ToString(response.VersionId)
	if !objectstore.IsVersion(version) {
		return objectstore.ObjectRef{}, fmt.Errorf("write S3 object %s: the bucket did not version the write", key)
	}
	return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(data), MediaType: contentType}, nil
}
func (b *Builder) writeHead(ctx context.Context, in build.BuildRequest, head *hostedHead, etag string) error {
	_, err := b.putObject(ctx, b.headKey(in), objectstore.MediaTypeJSON, comm.Must(json.Marshal(head)), etag)
	return err
}
func (b *Builder) readResult(ctx context.Context, in build.BuildRequest, ref objectstore.ObjectRef) (hostedResult, objectstore.ObjectRef, error) {
	key := b.attemptPrefix(in) + "result.json"
	if ref.Key != "" && (ref.Key != key || ref.Version == "" || !flowid.IsSHA256Digest(ref.Digest) || ref.MediaType != objectstore.MediaTypeJSON) {
		return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("%w: the accepted result reference is not the exact %s", errHostedConflict, key)
	}
	data, _, version, err := b.getObject(ctx, key, ref.Version, 4*objectstore.MaxObjectBytes+65536)
	if platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound") {
		return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("%w: %w", errHostedResultMissing, err)
	}
	if err != nil {
		return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("read the build result: %w", err)
	}
	if ref.Key != "" && objectstore.Digest(data) != ref.Digest {
		return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("%w: result %s differs from its accepted digest", errHostedConflict, key)
	}
	var result hostedResult
	if err = decodeHosted(data, &result); err != nil {
		return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("result %s: %w", key, err)
	}
	for name, payload := range map[string]string{"flow definitions": result.Payloads.FlowDefinitions, "connector contract": result.Payloads.ConnectorContract, "environment contract": result.Payloads.EnvironmentContract, "manifest": result.Payloads.Manifest} {
		if len(payload) == 0 || len(payload) > objectstore.MaxObjectBytes {
			return hostedResult{}, objectstore.ObjectRef{}, fmt.Errorf("build result %s: the %s payload size %d is outside 1..%d bytes", key, name, len(payload), objectstore.MaxObjectBytes)
		}
	}
	return result, objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(data), MediaType: objectstore.MediaTypeJSON}, nil
}

// finish records only an already observed provider termination, its exact
// immutable output, CodeBuild's measured phases and, for FAILED, its reason.
// This remains readable after CodeBuild history expires.
func (b *Builder) finish(ctx context.Context, h *hostedHead, phase hostedPhase, ref objectstore.ObjectRef, failure string) error {
	latest, etag, err := b.readHead(ctx, h.Request)
	if err != nil {
		return err
	}
	if latest == nil || latest.BuildID != h.BuildID || latest.Request != h.Request {
		return fmt.Errorf("%w: the admission of build %s changed before its %s outcome was recorded", errHostedConflict, h.BuildID, phase)
	}
	if latest.Phase == phase && latest.Result == ref {
		return nil
	}
	if latest.Phase != hostedRunning {
		return fmt.Errorf("%w: build %s is %s, not RUNNING, when its %s outcome is recorded", errHostedConflict, h.BuildID, latest.Phase, phase)
	}
	latest.Phase = phase
	latest.Result = ref
	latest.Failure = failure
	latest.CodeBuild = h.CodeBuild
	return b.writeHead(ctx, h.Request, latest, etag)
}
