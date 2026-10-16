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

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// temporalCloudOps is the Temporal Cloud Operations API endpoint and version in
// the Dex server configuration.
type temporalCloudOps struct {
	APIVersion string `json:"apiVersion"`
	HostPort   string `json:"hostPort"`
}

// DexSpec is the hosted project Dex: Temporal Cloud with the project's API
// key, Flow blobs in the project's bucket for the Temporal retention, and the
// platform node pool.
func (h *CloudAccount) DexSpec(_ context.Context, _ *kube.Infrastructure, _ string, storage infra.StorageRefs, access infra.TemporalAccess) (kube.ProjectDexSpec, error) {
	return kube.ProjectDexSpec{
		Temporal: kubeapi.Object{
			"hostPort": access.HostPort, "namespace": access.Namespace, "cloudAPIKey": access.APIKey,
			"cloudOps": temporalCloudOps{APIVersion: "v0.22.0", HostPort: "saas-api.tmprl.cloud:443"},
		},
		Storage:              kubeapi.Object{"s3Bucket": storage.StorageBucket, "s3Region": h.config.Region},
		HistoryRetentionDays: h.config.TemporalRetentionDays,
		Env:                  []any{kubeapi.EnvVar("AWS_REGION", h.config.Region)},
		NodeSelector:         kubeapi.Object{"superdurable.io/pool": "platform"},
		TmpSizeLimit:         "1Gi",
	}, nil
}
