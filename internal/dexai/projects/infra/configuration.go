// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
)

// ReadExactVersion reads the version ref pins and accepts it only with ref's
// digest.
func ReadExactVersion(ctx context.Context, objects *ApplicationObjects, ref objectstore.ObjectRef) ([]byte, error) {
	data, version, err := objects.Get(ctx, ref.Key, ref.Version)
	if err != nil {
		return nil, err
	}
	if version != ref.Version || objectstore.Digest(data) != ref.Digest {
		return nil, errors.New("configuration object integrity mismatch")
	}
	return data, nil
}

// IsConfigurationHead reports whether ref is one version of the scope's
// configuration head as the Connector SDK writes it. The version, media type
// and digest come from a provider response, so they are checked too.
func IsConfigurationHead(scope ScopeRef, ref objectstore.ObjectRef) bool {
	return scope.Storage.BlobPrefix == ConfigurationPrefix && ref.Key == configurationKey(scope.ProjectID, scope.Scope, "configuration/head") &&
		ref.Version != "" && ref.MediaType == objectstore.MediaTypeJSON && flowid.IsSHA256Digest(ref.Digest)
}

// ScopedObjectRef admits a configuration head the Connector SDK reported for
// the scope and returns its full bucket reference. Anything else is
// ErrAccessDenied.
func ScopedObjectRef(scope ScopeRef, ref connectorconfig.SnapshotRef) (objectstore.ObjectRef, error) {
	full := objectstore.ObjectRef{Key: ConfigurationPrefix + ref.Key, Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType}
	if !IsConfigurationHead(scope, full) {
		return objectstore.ObjectRef{}, ErrAccessDenied
	}
	return full, nil
}

// SnapshotRefFor is the inverse of ScopedObjectRef: it returns the
// scope-relative reference of a configuration head of the scope. Anything
// else is ErrAccessDenied.
func SnapshotRefFor(scope ScopeRef, ref objectstore.ObjectRef) (connectorconfig.SnapshotRef, error) {
	if !IsConfigurationHead(scope, ref) {
		return connectorconfig.SnapshotRef{}, ErrAccessDenied
	}
	return connectorconfig.SnapshotRef{Key: strings.TrimPrefix(ref.Key, ConfigurationPrefix), Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType}, nil
}

// credentialBrokerTokenName is the application secret name under which the
// Connector SDK stores a scope's credential broker token.
const credentialBrokerTokenName = "DEX_CREDENTIAL_BROKER_TOKEN"

// ApplicationSecretKeys returns the keys of the application secrets that an
// accepted configuration references, for a read grant: its environment's
// secrets and the scope token of its credential broker. It never reads a
// secret. ReadConfiguration already pinned data by scoped key, version and
// digest, and the Connector SDK validated its schema, scope and names when the
// platform wrote it, so only the boundary this grant owns is checked: every
// secret stays under the scope's app-secrets prefix, because the result
// becomes the grant policy.
func ApplicationSecretKeys(data []byte, scope ScopeRef) ([]string, error) {
	type secretRef struct {
		Key string `json:"key"`
	}
	var document struct {
		Environment map[string]struct {
			SecretRef *secretRef `json:"secretRef"`
		} `json:"environment"`
		CredentialBroker *struct {
			TokenSecretRef secretRef `json:"tokenSecretRef"`
		} `json:"credentialBroker"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("application configuration is invalid: %w", err)
	}
	secrets := map[string]string{}
	for name, entry := range document.Environment {
		if entry.SecretRef != nil {
			secrets[entry.SecretRef.Key] = name
		}
	}
	if document.CredentialBroker != nil {
		secrets[document.CredentialBroker.TokenSecretRef.Key] = credentialBrokerTokenName
	}
	keys := make([]string, 0, len(secrets))
	for key, name := range secrets {
		if !strings.HasPrefix(key, scopeKeyPrefix(scope.ProjectID, scope.Scope)+"app-secrets/"+name+"/") {
			return nil, errors.New("application secret is outside the accepted scope")
		}
		keys = append(keys, ConfigurationPrefix+key)
	}
	sort.Strings(keys)
	return keys, nil
}

// ConfigurationS3Client pins the hosted bucket owner on every request. The
// Connector SDK store has no per-call ExpectedBucketOwner, so a private clone
// of the client sets the header before signing.
func ConfigurationS3Client(objects *ApplicationObjects) *s3.Client {
	if objects.AccountID == "" {
		return objects.Client
	}
	options := objects.Options()
	options.APIOptions = append(append([]func(*middleware.Stack) error{}, options.APIOptions...), func(stack *middleware.Stack) error {
		return stack.Build.Add(middleware.BuildMiddlewareFunc("SuperverseConfigurationBucketOwner", func(ctx context.Context, input middleware.BuildInput, next middleware.BuildHandler) (middleware.BuildOutput, middleware.Metadata, error) {
			request, ok := input.Request.(*smithyhttp.Request)
			if !ok {
				panic("infra: S3 request is not HTTP")
			}
			request.Header.Set("X-Amz-Expected-Bucket-Owner", objects.AccountID)
			return next.HandleBuild(ctx, input)
		}), middleware.Before)
	})
	return s3.New(options)
}
