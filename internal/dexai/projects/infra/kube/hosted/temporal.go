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
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TemporalNamespace names the Project's Live namespace on the shared cluster.
func (*CloudAccount) TemporalNamespace(k *kube.Infrastructure, project string) string {
	return identity.LiveName(k, project)
}

// findNamespace admits only the exact scope owner, including its Kubernetes UID.
func (h *CloudAccount) findNamespace(ctx context.Context, k *kube.Infrastructure, project, uid string) (*workflowservice.DescribeNamespaceResponse, error) {
	out, err := workflowservice.NewWorkflowServiceClient(h.temporal).DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: h.TemporalNamespace(k, project)})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("describe Project Temporal namespace: %w", err)
	}
	if out.GetNamespaceInfo().GetName() != h.TemporalNamespace(k, project) {
		return nil, errors.New("project Temporal namespace identity is invalid")
	}
	if err = identity.VerifyTags(identity.LiveTags(k, project, uid), out.GetNamespaceInfo().GetData()); err != nil {
		return nil, err
	}
	return out, nil
}

// EnsureTemporal queries before registration and reconciles a racing or uncertain create.
func (h *CloudAccount) EnsureTemporal(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, _ string, accepted infra.ResourceRef) (infra.TemporalAccess, infra.ResourceRef, error) {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	n, err := h.findNamespace(ctx, k, project, storage.Boundary.UID)
	if err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	if n == nil {
		if accepted.UID != "" {
			return infra.TemporalAccess{}, infra.ResourceRef{}, fmt.Errorf("%w: accepted Project Temporal namespace is absent", infra.ErrOwnershipConflict)
		}
		_, err = workflowservice.NewWorkflowServiceClient(h.temporal).RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
			Namespace: h.TemporalNamespace(k, project), WorkflowExecutionRetentionPeriod: durationpb.New(time.Duration(h.config.TemporalRetentionDays) * 24 * time.Hour),
			Data: identity.LiveTags(k, project, storage.Boundary.UID),
		})
		if err != nil && status.Code(err) != codes.AlreadyExists {
			return infra.TemporalAccess{}, infra.ResourceRef{}, fmt.Errorf("register Project Temporal namespace: %w", err)
		}
		n, err = h.findNamespace(ctx, k, project, storage.Boundary.UID)
		if err != nil {
			return infra.TemporalAccess{}, infra.ResourceRef{}, err
		}
		if n == nil {
			return infra.TemporalAccess{}, infra.ResourceRef{}, fmt.Errorf("%w: Project Temporal namespace registration is not visible", infra.ErrPending)
		}
	}
	if accepted.UID != "" && (accepted.Name != h.TemporalNamespace(k, project) || accepted.UID != n.GetNamespaceInfo().GetId()) {
		return infra.TemporalAccess{}, infra.ResourceRef{}, fmt.Errorf("%w: Project Temporal namespace was replaced", infra.ErrOwnershipConflict)
	}
	if err = h.verifyNamespace(n); err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	return h.namespaceAccess(k, project), infra.ResourceRef{Boundary: k.ProjectNamespace(project), Name: h.TemporalNamespace(k, project), UID: n.GetNamespaceInfo().GetId()}, nil
}

// ReadTemporalAccess verifies the namespace without storing or reading credentials.
func (h *CloudAccount) ReadTemporalAccess(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, _ string, accepted infra.ResourceRef) (infra.TemporalAccess, error) {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	n, err := h.findNamespace(ctx, k, project, storage.Boundary.UID)
	if err != nil {
		return infra.TemporalAccess{}, err
	}
	if n == nil {
		return infra.TemporalAccess{}, errors.New("project Temporal namespace is absent")
	}
	if err = h.verifyNamespace(n); err != nil {
		return infra.TemporalAccess{}, err
	}
	if accepted.Name != h.TemporalNamespace(k, project) || accepted.UID != n.GetNamespaceInfo().GetId() {
		return infra.TemporalAccess{}, fmt.Errorf("%w: Project Temporal namespace was replaced", infra.ErrOwnershipConflict)
	}
	return h.namespaceAccess(k, project), nil
}

func (h *CloudAccount) verifyNamespace(n *workflowservice.DescribeNamespaceResponse) error {
	if n.GetNamespaceInfo().GetState() != enumspb.NAMESPACE_STATE_REGISTERED {
		return fmt.Errorf("%w: Project Temporal namespace is not registered", infra.ErrPending)
	}
	if n.GetConfig().GetWorkflowExecutionRetentionTtl().AsDuration() != time.Duration(h.config.TemporalRetentionDays)*24*time.Hour {
		return errors.New("project Temporal namespace retention changed")
	}
	return nil
}

func (h *CloudAccount) namespaceAccess(k *kube.Infrastructure, project string) infra.TemporalAccess {
	return infra.TemporalAccess{HostPort: h.config.TemporalAddress, Namespace: h.TemporalNamespace(k, project)}
}

// destroyTemporal verifies ownership before delete; a lost response is reconciled by name.
func (h *CloudAccount) destroyTemporal(ctx context.Context, k *kube.Infrastructure, project, uid string, accepted infra.ResourceRef) error {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	n, err := h.findNamespace(ctx, k, project, uid)
	if err != nil || n == nil {
		return err
	}
	if accepted.UID != "" && (accepted.Name != h.TemporalNamespace(k, project) || accepted.UID != n.GetNamespaceInfo().GetId()) {
		return fmt.Errorf("%w: accepted Project Temporal namespace was replaced", infra.ErrOwnershipConflict)
	}
	if n.GetNamespaceInfo().GetState() == enumspb.NAMESPACE_STATE_DELETED {
		return nil
	}
	_, err = operatorservice.NewOperatorServiceClient(h.temporal).DeleteNamespace(ctx, &operatorservice.DeleteNamespaceRequest{NamespaceId: n.GetNamespaceInfo().GetId()})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete Project Temporal namespace: %w", err)
	}
	return nil
}
