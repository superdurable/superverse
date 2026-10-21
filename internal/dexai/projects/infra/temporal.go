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
	"errors"
	"fmt"

	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TemporalAccess is how a project Dex and the data-plane check reach the
// project's Temporal namespace over the private network; it has no credential.
type TemporalAccess struct {
	HostPort, Namespace string
}

func (a TemporalAccess) String() string { return a.Namespace + " at " + a.HostPort }

func (a TemporalAccess) GoString() string { return a.String() }

// DataPlaneAnswer is how a project's Temporal data plane answered one
// DescribeNamespace with the project's credential.
type DataPlaneAnswer string

const (
	// DataPlaneReady: the namespace is registered under its name.
	DataPlaneReady DataPlaneAnswer = "ready"
	// DataPlaneUnauthorized: the data plane does not accept the credential.
	DataPlaneUnauthorized DataPlaneAnswer = "unauthorized"
	// DataPlaneUnreachable: the data plane or the namespace does not answer.
	DataPlaneUnreachable DataPlaneAnswer = "unreachable"
)

// DescribeTemporalNamespace asks a project's Temporal data plane once, over
// connection, to describe namespace. A registered namespace of that name is
// DataPlaneReady; PermissionDenied and Unauthenticated are
// DataPlaneUnauthorized; Unavailable, DeadlineExceeded and NotFound are
// DataPlaneUnreachable. Any other outcome is an error that carries only its
// gRPC code: a status message could carry provider detail about the
// credential.
func DescribeTemporalNamespace(ctx context.Context, connection *grpc.ClientConn, namespace string) (DataPlaneAnswer, error) {
	described, err := workflowservice.NewWorkflowServiceClient(connection).DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: namespace})
	switch code := status.Code(err); code {
	case codes.OK:
		info := described.GetNamespaceInfo()
		if info.GetName() != namespace {
			return "", errors.New("the Temporal data plane described another namespace")
		}
		if info.GetState() != enums.NAMESPACE_STATE_REGISTERED {
			return "", fmt.Errorf("the Temporal namespace is %s", info.GetState())
		}
		return DataPlaneReady, nil
	case codes.PermissionDenied, codes.Unauthenticated:
		return DataPlaneUnauthorized, nil
	case codes.Unavailable, codes.DeadlineExceeded, codes.NotFound:
		return DataPlaneUnreachable, nil
	default:
		return "", fmt.Errorf("describe the Temporal namespace: gRPC code %s", code)
	}
}

// projectSearchAttributes are the Temporal search attributes project Dex
// queries, in the order the local Temporal server registers them.
var projectSearchAttributes = []struct {
	Name        string
	KeywordList bool
}{{"FlowType", false}, {"DexParentFlowID", false}, {"ActiveStepTypes", true}, {"keyword1", false}, {"keyword2", false}, {"keyword3", false}}

// LocalTemporalNamespace is the Temporal namespace of a project whose own
// Temporal start-dev runs in its Live scope: app-<project>.
func LocalTemporalNamespace(project string) string { return "app-" + project }

// LocalTemporalAccess is how a project's Live Dex reaches the Temporal
// start-dev of its own scope; it needs no API key.
func LocalTemporalAccess(namespace string) TemporalAccess {
	return TemporalAccess{HostPort: ScopeAddress(ComponentTemporal, TemporalPort), Namespace: namespace}
}

// TemporalStartDevArgs are the arguments of a project's Temporal start-dev
// server: every address, the namespace its Dex uses with the
// projectSearchAttributes, and the database file it keeps on its volume.
func TemporalStartDevArgs(namespace, database string) []string {
	args := []string{"server", "start-dev", "--ip", "0.0.0.0", "--namespace", namespace, "--db-filename", database}
	for _, attribute := range projectSearchAttributes {
		valueType := "Keyword"
		if attribute.KeywordList {
			valueType = "KeywordList"
		}
		args = append(args, "--search-attribute", attribute.Name+"="+valueType)
	}
	return args
}
