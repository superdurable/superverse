// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package objectstore

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// The cleanup IDs below bound destructive prefixes: an empty or
// separator-bearing ID would widen the prefix to another scope, so each must be
// a non-empty run of lowercase letters and digits.

// DeleteProjectObjects performs bounded cleanup after the Project owner has fenced admissions and completed child cleanup.
// It never scans another project or an unscoped bucket. Call again until done.
func (s *Store) DeleteProjectObjects(ctx context.Context, projectID string) (bool, error) {
	if !flowid.IsLowerAlphanumeric(projectID) {
		return false, fmt.Errorf("%w: cleanup project ID %q is not lowercase alphanumeric", errInvalidObject, projectID)
	}
	// The last prefix holds the prepared Preview sources, which the Session's
	// release deletes first; infra.PreviewKeysOf derives it.
	return s.deletePrefixes(ctx, []string{projectID + "/", "projects/" + projectID + "/source/", "projects/" + projectID + "/preview-source/"})
}

// DeletePrefix removes the object versions under prefix, a key prefix below
// the store's environment prefix that ends with "/". Its owner derives the
// prefix, such as a Project's prepared Preview sources; an unclean one would
// widen the deletion, so it is a programming error.
func (s *Store) DeletePrefix(ctx context.Context, prefix string) (bool, error) {
	if !strings.HasSuffix(prefix, "/") || !validKey(strings.TrimSuffix(prefix, "/")) {
		panic("objectstore: invalid cleanup prefix " + strconv.Quote(prefix))
	}
	return s.deletePrefixes(ctx, []string{prefix})
}

// DeletePrefixExcept removes the object versions under prefix, as DeletePrefix
// does, except every version of the keys in keep, full keys such as Put
// returns or FullKey derives. A connection's credential write-back keeps its
// current and new credential this way and removes what others left.
func (s *Store) DeletePrefixExcept(ctx context.Context, prefix string, keep ...string) (bool, error) {
	if !strings.HasSuffix(prefix, "/") || !validKey(strings.TrimSuffix(prefix, "/")) {
		panic("objectstore: invalid cleanup prefix " + strconv.Quote(prefix))
	}
	for _, key := range keep {
		if !strings.HasPrefix(key, s.prefix+prefix) {
			panic("objectstore: kept key " + strconv.Quote(key) + " is outside " + strconv.Quote(prefix))
		}
	}
	return s.deletePrefixes(ctx, []string{prefix}, keep...)
}

// FullKey is the full key of key, which a reference of an object at key holds.
func (s *Store) FullKey(key string) string { return s.prefix + key }

// DeleteReleaseObjects removes only one commit's immutable source/build versions
// after Release admission and external providers have been fenced and settled.
func (s *Store) DeleteReleaseObjects(ctx context.Context, projectID, commit string) (bool, error) {
	if !flowid.IsLowerAlphanumeric(projectID) || !flowid.IsCommitSHA(commit) {
		return false, fmt.Errorf("%w: cleanup project %q or commit %q is invalid", errInvalidObject, projectID, commit)
	}
	return s.deletePrefixes(ctx, []string{projectID + "/releases/" + commit + "/"})
}

// deletePrefixes deletes at most a budget of the versions under prefixes, all
// but those of the full keys keep, and reports done once a call finds nothing
// to delete.
func (s *Store) deletePrefixes(ctx context.Context, prefixes []string, keep ...string) (bool, error) {
	const budget = 16
	remaining := budget
	done := true
	for _, relative := range prefixes {
		prefix := s.prefix + relative
		result, err := s.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &s.bucket, Prefix: &prefix, MaxKeys: aws.Int32(int32(remaining))})
		if err != nil {
			return false, fmt.Errorf("list versions under %s: %w", prefix, err)
		}
		if aws.ToBool(result.IsTruncated) {
			done = false
		}
		targets := make([]struct{ key, version *string }, 0, len(result.Versions)+len(result.DeleteMarkers))
		for _, v := range result.Versions {
			targets = append(targets, struct{ key, version *string }{v.Key, v.VersionId})
		}
		for _, v := range result.DeleteMarkers {
			targets = append(targets, struct{ key, version *string }{v.Key, v.VersionId})
		}
		for _, target := range targets {
			if !strings.HasPrefix(aws.ToString(target.key), prefix) || !validKey(aws.ToString(target.key)) || !IsVersion(aws.ToString(target.version)) {
				return false, fmt.Errorf("%w: listed version %q of %q is not an exact object under %s", errInvalidObject, aws.ToString(target.version), aws.ToString(target.key), prefix)
			}
			if slices.Contains(keep, aws.ToString(target.key)) {
				continue
			}
			if remaining == 0 {
				return false, nil
			}
			if _, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: target.key, VersionId: target.version}); err != nil {
				return false, fmt.Errorf("delete %s version %s: %w", aws.ToString(target.key), aws.ToString(target.version), err)
			}
			remaining--
			done = false
		}
		if remaining == 0 {
			return false, nil
		}
	}
	return done, nil
}
