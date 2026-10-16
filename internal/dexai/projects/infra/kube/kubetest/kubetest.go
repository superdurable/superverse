// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubetest

import (
	"testing"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/local"
)

// InfrastructureForTest is the Kubernetes runtime of the isolated V2 Kind cluster as
// the test environment, with the Local Kind backend and without images. It
// calls the Kubernetes API with the platform server's own scoped credential
// and reaches the cluster's Services through port-forwards, which the test's
// cleanup stops. A missing or foreign cluster fails the test.
func InfrastructureForTest(t testing.TB) *kube.Infrastructure {
	t.Helper()
	cluster := kubeapitest.KindPlatform(t)
	return &kube.Infrastructure{API: cluster.ServerAPI(t), Dial: cluster.ServiceForwards(t), Environment: deploymentnames.EnvironmentTest, Backend: &local.Backend{}}
}
