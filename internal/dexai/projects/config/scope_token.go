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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// scopeTokenObject is the secret object that holds a scope's credential
// broker token, as connectorconfig.ConfigurationStore.CreateCredentialBrokerToken
// writes it; the reference's digest pins its exact contents.
type scopeTokenObject struct {
	Scope connectorconfig.Scope `json:"scope"`
	Value string                `json:"value"`
}

// ReadScopeToken reads the token of a Project scope from the secret object
// that ref, the scope's stored connectorconfig.ApplicationSecretRef, names
// in the Project's configuration storage. The Slack event relay signs its
// deliveries with it. The token never reaches an error.
func (c *Configuration) ReadScopeToken(ctx context.Context, projectID, scope string, ref json.RawMessage) (string, error) {
	kind, known := infra.ParseScopeKind(scope)
	if !known {
		return "", fmt.Errorf("unknown scope %q", scope)
	}
	var reference connectorconfig.ApplicationSecretRef
	if err := json.Unmarshal(ref, &reference); err != nil {
		return "", fmt.Errorf("decode the scope token reference: %w", err)
	}
	project, err := c.Projects.Read(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("read the Project: %w", err)
	}
	objects, err := c.Storage.ConfigurationObjects(ctx, infra.ScopeRef{ProjectID: projectID, Scope: infra.Scope{Kind: kind}, Storage: project.Storage})
	if err != nil {
		return "", err
	}
	object, err := objects.ReadObject(ctx, reference.Key, reference.Version)
	if err != nil {
		return "", fmt.Errorf("read the scope token object: %w", err)
	}
	digest := sha256.Sum256(object.Contents)
	var token scopeTokenObject
	if "sha256:"+hex.EncodeToString(digest[:]) != reference.Digest || json.Unmarshal(object.Contents, &token) != nil ||
		token.Scope != (connectorconfig.Scope{ProjectID: projectID, Kind: kind.String()}) || token.Value == "" {
		return "", errors.New("the scope token object differs from its reference")
	}
	return token.Value, nil
}
