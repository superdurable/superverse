// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// DestroyIdentities deletes every Pod Identity association in namespace, when
// one is given, and then the IAM roles below the cloud name, one per call,
// after verifying owner's tags on each, and returns infra.ErrPending after
// each deletion. The owner calls it only after the workloads stopped: AWS
// applies permission changes asynchronously, so they prove no revocation. No
// role or permission is ever adopted from outside this scoped path.
func (h *Account) DestroyIdentities(ctx context.Context, k *kube.Infrastructure, name, namespace string, owner map[string]string) error {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	if namespace != "" {
		if err := h.deleteNamespaceAssociations(ctx, namespace, owner); err != nil {
			return err
		}
	}
	return h.eachRole(ctx, k, name, func(role string, tags map[string]string) (bool, error) {
		if err := VerifyTags(owner, tags); err != nil {
			return true, err
		}
		if err := h.deleteRole(ctx, role); err != nil {
			return true, err
		}
		return true, fmt.Errorf("%w: IAM role %s is being deleted", infra.ErrPending, role)
	})
}

// deleteNamespaceAssociations deletes every Pod Identity association in
// namespace across all result pages after verifying owner's tags on each, and
// returns infra.ErrPending when it deleted any. An association deleted since
// the listing is skipped, so a retry is safe.
func (h *Account) deleteNamespaceAssociations(ctx context.Context, namespace string, owner map[string]string) error {
	deleted := 0
	pages := eks.NewListPodIdentityAssociationsPaginator(h.EKS, &eks.ListPodIdentityAssociationsInput{ClusterName: &h.ClusterName, Namespace: &namespace, MaxResults: aws.Int32(100)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, listed := range page.Associations {
			observed, err := h.EKS.DescribePodIdentityAssociation(ctx, &eks.DescribePodIdentityAssociationInput{ClusterName: &h.ClusterName, AssociationId: listed.AssociationId})
			if platform.HasAWSErrorCode(err, "ResourceNotFoundException") {
				continue
			}
			if err != nil {
				return err
			}
			if observed.Association == nil {
				return errors.New("project association has no observed identity")
			}
			if err = VerifyTags(owner, observed.Association.Tags); err != nil {
				return err
			}
			_, err = h.EKS.DeletePodIdentityAssociation(ctx, &eks.DeletePodIdentityAssociationInput{ClusterName: &h.ClusterName, AssociationId: listed.AssociationId})
			if err != nil && !platform.HasAWSErrorCode(err, "ResourceNotFoundException") {
				return fmt.Errorf("delete Pod Identity association %s: %w", aws.ToString(listed.AssociationId), err)
			}
			deleted++
		}
	}
	if deleted > 0 {
		return fmt.Errorf("%w: deleted %d Pod Identity associations of namespace %s", infra.ErrPending, deleted, namespace)
	}
	return nil
}
