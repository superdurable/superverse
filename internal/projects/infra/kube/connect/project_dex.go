// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connect

import (
	"context"

	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
	"google.golang.org/grpc"
)

// Connections opens the private connections of the platform server to the
// workloads that run in the Kubernetes cluster. It implements
// infra.Connections.
type Connections struct{ Infrastructure *kube.Infrastructure }

var _ infra.Connections = (*Connections)(nil)

// WorkerAddress is the address at which the project's Dex reaches the Worker
// of the current Live generation.
func (c *Connections) WorkerAddress(project string) string {
	return c.Infrastructure.WorkerAddress(project)
}

// DialProjectDex opens one private gRPC connection to the project's Live Dex
// after verifying the accepted namespace and Service UIDs and Dex readiness.
// Authentication and business authorization stay with the caller. The
// connection belongs to the caller's request: the returned function closes it,
// and so does the end of ctx.
func (c *Connections) DialProjectDex(ctx context.Context, project string, refs infra.ScopeRefs) (*grpc.ClientConn, func() error, error) {
	k := c.Infrastructure
	if err := k.VerifyProjectDex(ctx, project, refs); err != nil {
		return nil, nil, err
	}
	connection, closeConnection, err := k.DialPrivate(ctx, kube.DexAddress(refs.Execution))
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = closeConnection() })
	return connection, func() error { stop(); return closeConnection() }, nil
}
