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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	st "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
)

// This provider receipt fences late dispatch and tracks bounded deletion progress.
// It is never used as Release business state or exposed through the resource API.
type hostedReleaseCleanup struct {
	Schema             string                      `json:"schemaVersion"`
	Environment        deploymentnames.Environment `json:"environment"`
	ProjectID          string                      `json:"projectId"`
	ReleaseID          string                      `json:"releaseId"`
	RepositoryFullName string                      `json:"repositoryFullName"`
	Cursor             string                      `json:"cursor,omitempty"`
	Complete           bool                        `json:"complete,omitempty"`
}

func (b *Builder) releaseReceiptPrefix(in build.BuildRequest) string {
	return b.config.Prefix + "/receipts/" + in.Environment.String() + "/" + in.ProjectID + "/" + in.ReleaseID + "/"
}
func (b *Builder) releaseFenceKey(in build.BuildRequest) string {
	return strings.TrimSuffix(b.releaseReceiptPrefix(in), "/") + ".cleanup.json"
}
func releaseCleanupIdentity(in build.BuildRequest) hostedReleaseCleanup {
	return hostedReleaseCleanup{Schema: "superverse.dev/hosted-release-cleanup/v1", Environment: in.Environment, ProjectID: in.ProjectID, ReleaseID: in.ReleaseID, RepositoryFullName: in.RepositoryFullName}
}
func (b *Builder) readReleaseFence(ctx context.Context, in build.BuildRequest) (*hostedReleaseCleanup, string, error) {
	data, etag, _, err := b.getObject(ctx, b.releaseFenceKey(in), "", 16384)
	if platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound") {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var fence hostedReleaseCleanup
	if err = decodeHosted(data, &fence); err != nil {
		return nil, "", fmt.Errorf("release cleanup fence: %w", err)
	}
	observed := fence
	observed.Cursor = ""
	observed.Complete = false
	if observed != releaseCleanupIdentity(in) || fence.Cursor != "" && !strings.HasPrefix(fence.Cursor, b.releaseReceiptPrefix(in)) {
		return nil, "", fmt.Errorf("%w: the release cleanup fence belongs to another Release or cursor", errHostedConflict)
	}
	return &fence, etag, nil
}
func (b *Builder) checkReleaseAdmission(ctx context.Context, in build.BuildRequest) error {
	fence, _, err := b.readReleaseFence(ctx, in)
	if err != nil {
		return err
	}
	if fence != nil {
		return errors.New("release build admission is permanently closed by cleanup")
	}
	return nil
}
func (b *Builder) saveReleaseFence(ctx context.Context, in build.BuildRequest, fence hostedReleaseCleanup, etag string) (string, error) {
	_, writeErr := b.putObject(ctx, b.releaseFenceKey(in), objectstore.MediaTypeJSON, comm.Must(json.Marshal(fence)), etag)
	actual, nextTag, err := b.readReleaseFence(ctx, in)
	if err == nil && actual != nil && *actual == fence {
		return nextTag, nil
	}
	return "", errors.Join(writeErr, err, errHostedUncertain)
}

// cleanupBatch bounds the build receipts one CleanupRelease call processes:
// each costs about a dozen S3, CodeBuild and ECR calls, and eight fit the
// call's 30-second budget inside the 40-second Release cleanup round.
const cleanupBatch = 8

// CleanupRelease processes at most cleanupBatch exact build receipts per
// invocation and returns ErrPending while receipts remain. Its persisted
// cursor avoids reloading an unbounded map or revisiting old builds.
func (b *Builder) CleanupRelease(ctx context.Context, in build.BuildRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := b.validate(in); err != nil {
		return err
	}
	fence, etag, err := b.readReleaseFence(ctx, in)
	if err != nil {
		return err
	}
	if fence == nil {
		value := releaseCleanupIdentity(in)
		etag, err = b.saveReleaseFence(ctx, in, value, "")
		if err != nil {
			return err
		}
		fence = &value
	}
	if fence.Complete {
		return nil
	}
	prefix := b.releaseReceiptPrefix(in)
	list := &s3.ListObjectsV2Input{Bucket: &b.config.Bucket, ExpectedBucketOwner: &b.config.AccountID, Prefix: &prefix, MaxKeys: aws.Int32(cleanupBatch)}
	if fence.Cursor != "" {
		list.StartAfter = &fence.Cursor
	}
	page, err := b.objects.ListObjectsV2(ctx, list)
	if err != nil {
		return fmt.Errorf("list the build receipts under %s: %w", prefix, err)
	}
	for _, item := range page.Contents {
		key := aws.ToString(item.Key)
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, ".json") || strings.Contains(strings.TrimPrefix(key, prefix), "/") {
			return fmt.Errorf("%w: listed receipt %q is not a build receipt of %s", errHostedConflict, key, prefix)
		}
		var data []byte
		if data, _, _, err = b.getObject(ctx, key, "", 16384); err != nil {
			return err
		}
		var head hostedHead
		if err = decodeHosted(data, &head); err != nil {
			return fmt.Errorf("receipt %s: %w", key, err)
		}
		if head.Schema != hostedHeadSchema || head.Request.Environment != in.Environment || head.Request.ProjectID != in.ProjectID || head.Request.ReleaseID != in.ReleaseID || head.Request.RepositoryFullName != in.RepositoryFullName || b.headKey(head.Request) != key {
			return fmt.Errorf("%w: receipt %s belongs to another Release", errHostedConflict, key)
		}
		if _, err = b.validate(head.Request); err != nil {
			return fmt.Errorf("%w: receipt %s: %w", errHostedConflict, key, err)
		}
		if err = b.cancelBuild(ctx, head.Request); err != nil {
			return err
		}
		if err = b.cleanupBuild(ctx, head.Request); err != nil {
			return err
		}
		fence.Cursor = key
		etag, err = b.saveReleaseFence(ctx, in, *fence, etag)
		if err != nil {
			return err
		}
	}
	if aws.ToBool(page.IsTruncated) {
		return fmt.Errorf("%w: more build receipts remain after %s", build.ErrPending, fence.Cursor)
	}
	fence.Complete = true
	_, err = b.saveReleaseFence(ctx, in, *fence, etag)
	return err
}

// cleanupBuild cleans up the build of the validated request in. It keeps the
// admission object forever: deleting a job directory must not allow a delayed
// Start to recreate a released build. Failed deletions are safe to retry
// because Inspect MISSING does not claim artifact cleanup completion.
func (b *Builder) cleanupBuild(ctx context.Context, in build.BuildRequest) error {
	h, etag, err := b.readHead(ctx, in)
	if err != nil {
		return err
	}
	if h != nil && in.Attempt < h.Request.Attempt {
		return fmt.Errorf("%w: cleanup attempt %d precedes the admitted attempt %d", errHostedConflict, in.Attempt, h.Request.Attempt)
	}
	if h == nil {
		h = b.newHead(in, hostedCleanup, "")
		if err = b.writeHead(ctx, in, h, ""); err != nil {
			return err
		}
	} else if h.Phase != hostedCleanup {
		if h.Phase == hostedPreparing {
			if _, err = b.inspectSource(ctx, h.Request); err != nil {
				return fmt.Errorf("%w: the source upload of a PREPARING build: %w", errHostedUncertain, err)
			}
		}
		result, readErr := b.inspectHead(ctx, in, h, etag)
		if readErr != nil {
			return readErr
		}
		// inspectHead may have committed a terminal provider observation. Match
		// that exact observed attempt and source, not any subsequently refreshed
		// terminal-looking head: a newer PREPARING owner may still upload source.
		proven := *h
		switch result.State {
		case build.BuildReady:
			proven.Phase = hostedReady
		case build.BuildFailed:
			if proven.Phase != hostedCancelled {
				proven.Phase = hostedFailed
			}
		case build.BuildMissing:
			if proven.Phase != hostedPreparing {
				return fmt.Errorf("%w: a %s build reads as missing", errHostedConflict, proven.Phase)
			}
		case build.BuildRunning:
			return fmt.Errorf("%w: hosted build %s must terminate before its cleanup", build.ErrPending, result.OperationID)
		case build.BuildUnknown:
			return fmt.Errorf("%w: hosted build %s has an unknown outcome and cannot be cleaned up", errHostedUncertain, result.OperationID)
		default:
			return fmt.Errorf("%w: build state %s before cleanup", errHostedConflict, result.State)
		}
		h, etag, err = b.readHead(ctx, in)
		if err != nil {
			return err
		}
		if h == nil || *h != proven {
			return fmt.Errorf("%w: the admission changed after its termination was proven", errHostedConflict)
		}
		h.Phase = hostedCleanup
		if err = b.writeHead(ctx, in, h, etag); err != nil {
			return err
		}
	}
	if err = b.deleteImage(ctx, h.Request); err != nil {
		return err
	}
	// Version deletion is confined to this Release's job prefix. Receipt versions
	// are deliberately outside it; no lifecycle rule may remove live fences.
	prefix := b.jobPrefix(in)
	versions, err := b.objects.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &b.config.Bucket, ExpectedBucketOwner: &b.config.AccountID, Prefix: &prefix, MaxKeys: aws.Int32(500)})
	if err != nil {
		return fmt.Errorf("list the job versions under %s: %w", prefix, err)
	}
	var objects []st.ObjectIdentifier
	for _, v := range versions.Versions {
		if !strings.HasPrefix(aws.ToString(v.Key), prefix) || aws.ToString(v.VersionId) == "" {
			return fmt.Errorf("%w: listed version of %q is outside %s or unversioned", errHostedConflict, aws.ToString(v.Key), prefix)
		}
		objects = append(objects, st.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
	}
	for _, v := range versions.DeleteMarkers {
		if !strings.HasPrefix(aws.ToString(v.Key), prefix) || aws.ToString(v.VersionId) == "" {
			return fmt.Errorf("%w: listed delete marker of %q is outside %s or unversioned", errHostedConflict, aws.ToString(v.Key), prefix)
		}
		objects = append(objects, st.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
	}
	if len(objects) > 0 {
		result, err := b.objects.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: &b.config.Bucket, ExpectedBucketOwner: &b.config.AccountID, Delete: &st.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if err != nil {
			return fmt.Errorf("delete the job versions under %s: %w", prefix, err)
		}
		if len(result.Errors) > 0 {
			first := result.Errors[0]
			return fmt.Errorf("delete the job versions under %s: %d of %d failed, first %s: %s %s", prefix, len(result.Errors), len(objects), aws.ToString(first.Key), aws.ToString(first.Code), aws.ToString(first.Message))
		}
	}
	if aws.ToBool(versions.IsTruncated) {
		return fmt.Errorf("%w: more job versions remain under %s", build.ErrPending, prefix)
	}
	return nil
}
