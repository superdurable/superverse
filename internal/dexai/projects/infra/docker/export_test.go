// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"

	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// ComponentNameForTest is componentName for the tests.
func (r *Infrastructure) ComponentNameForTest(project string, component infra.Component) string {
	return r.componentName(project, component)
}

// PublishedForTest is published for the tests.
func (r *Infrastructure) PublishedForTest(ctx context.Context, name string, labels map[string]string, port int) (string, error) {
	return r.published(ctx, name, labels, port)
}

// StorageEndpointForTest is storageEndpoint for the tests.
func (r *Infrastructure) StorageEndpointForTest(ctx context.Context, project string, storage infra.StorageRefs) (string, error) {
	return r.storageEndpoint(ctx, project, storage)
}
