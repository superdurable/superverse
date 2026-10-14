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
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// associationPropagation is how long a new Pod Identity association stays pending.
// Its webhook injects credentials only into Pods admitted after it observes the
// association, and a Pod admitted earlier never receives them, even after its
// containers restart. A Preview Dex Pod admitted 3 s after its association had
// no credentials and could not reach S3.
const associationPropagation = 30 * time.Second

// roleMaxSession is the MaxSessionDuration of a workload role, also IAM's
// default for a role created without one.
const roleMaxSession = time.Hour

// podIdentityCredentialLifetime is how long the credentials EKS Pod Identity
// issues for a role stay valid, whatever the role's MaxSessionDuration. The
// EKS user guide (https://docs.aws.amazon.com/eks/latest/userguide/pod-id-how-it-works.html)
// states that the node's agent serves temporary credentials but gives no
// lifetime. Observed: EKS Auth assumes the role with durationSeconds 21600, as
// the AssumeRole events invoked by pods.eks.amazonaws.com in CloudTrail show
// (test, 2026-10-12). The agent (github.com/aws/eks-pod-identity-agent,
// cmd/server.go) renews a cached credential after at most 3 hours by default,
// its --max-credential-retention-before-renewal flag, and never past expiry.
const podIdentityCredentialLifetime = 6 * time.Hour

// The names of the inline policies of a workload role: the scope policy grants
// the role's access, and the revoked policy denies everything once the role is
// revoked.
const (
	scopePolicyName   = "scope"
	revokedPolicyName = "revoked"
)

// roleName names the role of identity below a cloud name.
func roleName(name, identity string) string {
	return name + "-" + OperationID(identity)[:12]
}

// roleTrust lets only the named service account of this cluster assume a role,
// through Pod Identity with session tags.
func (h *Account) roleTrust(namespace, serviceAccount string) string {
	c := h
	return policyJSON(policyStatement{
		Effect:    effectAllow,
		Principal: map[string]string{"Service": "pods.eks.amazonaws.com"},
		Action:    []policyAction{actionSTSAssumeRole, actionSTSTagSession},
		Condition: policyCondition{operatorStringEquals: {
			"aws:RequestTag/kubernetes-namespace":       namespace,
			"aws:RequestTag/kubernetes-service-account": serviceAccount,
			"aws:RequestTag/eks-cluster-arn":            "arn:aws:eks:" + c.Region + ":" + c.AccountID + ":cluster/" + c.ClusterName,
		}},
	})
}

// ensureRole converges a role with exactly the given tags and scope policy and
// returns its ARN. A role created by a revocation is a fence and is never
// enabled.
func (h *Account) ensureRole(ctx context.Context, k *kube.Infrastructure, tags map[string]string, name, namespace, serviceAccount, policy string) (string, error) {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	role, err := h.ensureRoleIdentity(ctx, k, tags, name, namespace, serviceAccount)
	if err != nil {
		return "", err
	}
	if _, err = h.IAM.GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: &name, PolicyName: aws.String(revokedPolicyName)}); err == nil {
		return "", errors.New("workload role is permanently revoked")
	} else if !platform.HasAWSErrorCode(err, "NoSuchEntity") {
		return "", err
	}
	if err = h.ensureInlinePolicy(ctx, name, scopePolicyName, policy); err != nil {
		return "", err
	}
	return aws.ToString(role.Arn), nil
}

// ensureRoleIdentity creates a missing role and returns it once its tags, its
// trust in exactly the service account and its permissions boundary are
// verified; its policies are the caller's.
func (h *Account) ensureRoleIdentity(ctx context.Context, k *kube.Infrastructure, tags map[string]string, name, namespace, serviceAccount string) (*iamtypes.Role, error) {
	result, err := h.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: &name})
	if platform.HasAWSErrorCode(err, "NoSuchEntity") {
		if err = h.createScopedRole(ctx, k, tags, name, namespace, serviceAccount, aws.Int32(int32(roleMaxSession/time.Second))); err != nil {
			return nil, err
		}
		result, err = h.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: &name})
		if platform.HasAWSErrorCode(err, "NoSuchEntity") {
			return nil, fmt.Errorf("%w: IAM role %s is propagating", infra.ErrPending, name)
		}
	}
	if err != nil {
		return nil, err
	}
	if result.Role == nil {
		return nil, errors.New("project role identity is missing")
	}
	role := result.Role
	if err = VerifyTags(tags, iamTagMap(role.Tags)); err != nil {
		return nil, err
	}
	trusted, err := policyMatches(role.AssumeRolePolicyDocument, h.roleTrust(namespace, serviceAccount))
	if err != nil {
		return nil, fmt.Errorf("decode the trust policy of IAM role %s: %w", name, err)
	}
	if !trusted || role.PermissionsBoundary == nil || aws.ToString(role.PermissionsBoundary.PermissionsBoundaryArn) != h.PermissionsBoundaryARN {
		return nil, errors.New("project IAM role trust or boundary changed")
	}
	return role, nil
}

// ensureInlinePolicy puts the named inline policy on a role, staying pending
// while a new policy propagates, and fails when the role already holds a
// different document under that name.
func (h *Account) ensureInlinePolicy(ctx context.Context, role, policyName, document string) error {
	current, err := h.IAM.GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: &role, PolicyName: &policyName})
	if platform.HasAWSErrorCode(err, "NoSuchEntity") {
		if _, err = h.IAM.PutRolePolicy(ctx, &iam.PutRolePolicyInput{RoleName: &role, PolicyName: &policyName, PolicyDocument: &document}); err != nil {
			return fmt.Errorf("put the %s policy of IAM role %s: %w", policyName, role, err)
		}
		return fmt.Errorf("%w: the %s policy of IAM role %s is propagating", infra.ErrPending, policyName, role)
	}
	if err != nil {
		return fmt.Errorf("read the %s policy of IAM role %s: %w", policyName, role, err)
	}
	matches, err := policyMatches(current.PolicyDocument, document)
	if err != nil {
		return fmt.Errorf("decode the %s policy of IAM role %s: %w", policyName, role, err)
	}
	if !matches {
		return fmt.Errorf("the %s policy of IAM role %s changed", policyName, role)
	}
	return nil
}

// policyMatches reports whether a URL-encoded policy document IAM returned is
// the JSON document want.
func policyMatches(encoded *string, want string) (bool, error) {
	decoded, err := url.QueryUnescape(aws.ToString(encoded))
	if err != nil {
		return false, err
	}
	return infra.EqualJSON([]byte(decoded), []byte(want)), nil
}

// createScopedRole creates a role with tags inside the project permissions
// boundary. Only the named service account can assume it.
func (h *Account) createScopedRole(ctx context.Context, k *kube.Infrastructure, tags map[string]string, name, namespace, serviceAccount string, maxSessionSeconds *int32) error {
	_, err := h.IAM.CreateRole(ctx, &iam.CreateRoleInput{RoleName: &name, Path: aws.String(k.Names().IAMWorkloadPath), AssumeRolePolicyDocument: aws.String(h.roleTrust(namespace, serviceAccount)), PermissionsBoundary: &h.PermissionsBoundaryARN, Tags: iamTags(tags), MaxSessionDuration: maxSessionSeconds})
	if err != nil && !platform.HasAWSErrorCode(err, "EntityAlreadyExists") {
		return err
	}
	return nil
}

// eachRole calls visit with every workload role below the cloud name and its
// tags, in bounded pages, until visit reports done or fails. A role deleted
// since the listing is skipped.
func (h *Account) eachRole(ctx context.Context, k *kube.Infrastructure, name string, visit func(role string, tags map[string]string) (bool, error)) error {
	marker := ""
	for range 100 {
		input := &iam.ListRolesInput{PathPrefix: aws.String(k.Names().IAMWorkloadPath), MaxItems: aws.Int32(100)}
		if marker != "" {
			input.Marker = &marker
		}
		page, err := h.IAM.ListRoles(ctx, input)
		if err != nil {
			return err
		}
		for _, candidate := range page.Roles {
			role := aws.ToString(candidate.RoleName)
			if !strings.HasPrefix(role, name+"-") {
				continue
			}
			result, err := h.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: &role})
			if platform.HasAWSErrorCode(err, "NoSuchEntity") {
				continue
			}
			if err != nil {
				return err
			}
			if result.Role == nil {
				return errors.New("project role has no observed identity")
			}
			if done, err := visit(role, iamTagMap(result.Role.Tags)); err != nil || done {
				return err
			}
		}
		if !page.IsTruncated {
			return nil
		}
		marker = aws.ToString(page.Marker)
	}
	return errors.New("project IAM role lookup exceeded bounded pages")
}

// deleteRole deletes a role and its inline policies; an absent one is gone.
func (h *Account) deleteRole(ctx context.Context, role string) error {
	for _, policy := range []string{scopePolicyName, revokedPolicyName} {
		_, err := h.IAM.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{RoleName: &role, PolicyName: &policy})
		if err != nil && !platform.HasAWSErrorCode(err, "NoSuchEntity") {
			return fmt.Errorf("delete the %s policy of IAM role %s: %w", policy, role, err)
		}
	}
	_, err := h.IAM.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: &role})
	if err != nil && !platform.HasAWSErrorCode(err, "NoSuchEntity") {
		return fmt.Errorf("delete IAM role %s: %w", role, err)
	}
	return nil
}

// deleteAssociation deletes one Pod Identity association and returns
// infra.ErrPending; one deleted meanwhile is gone, which the next call
// confirms.
func (h *Account) deleteAssociation(ctx context.Context, id *string) error {
	_, err := h.EKS.DeletePodIdentityAssociation(ctx, &eks.DeletePodIdentityAssociationInput{ClusterName: &h.ClusterName, AssociationId: id})
	if err != nil && !platform.HasAWSErrorCode(err, "ResourceNotFoundException") {
		return fmt.Errorf("delete Pod Identity association %s: %w", aws.ToString(id), err)
	}
	return fmt.Errorf("%w: Pod Identity association %s is being deleted", infra.ErrPending, aws.ToString(id))
}

// association returns the one Pod Identity association of a service account,
// or nil when it has none.
func (h *Account) association(ctx context.Context, namespace, serviceAccount string) (*ekstypes.PodIdentityAssociation, error) {
	list, err := h.EKS.ListPodIdentityAssociations(ctx, &eks.ListPodIdentityAssociationsInput{ClusterName: &h.ClusterName, Namespace: &namespace, ServiceAccount: &serviceAccount, MaxResults: aws.Int32(2)})
	if err != nil {
		return nil, err
	}
	if len(list.Associations) == 0 {
		return nil, nil
	}
	if len(list.Associations) != 1 || aws.ToString(list.NextToken) != "" {
		return nil, errors.New("ambiguous project Pod Identity association")
	}
	result, err := h.EKS.DescribePodIdentityAssociation(ctx, &eks.DescribePodIdentityAssociationInput{ClusterName: &h.ClusterName, AssociationId: list.Associations[0].AssociationId})
	if err != nil {
		return nil, err
	}
	return result.Association, nil
}

// ensureAssociation binds a service account to a role with tags and stays
// pending until the association has propagated. An association never moves to
// another role: one whose role ARN starts with replaces, another role of the
// same owner, is deleted after a check of the owner's tags, and the next call
// creates it for the role; an empty replaces deletes none.
func (h *Account) ensureAssociation(ctx context.Context, tags, owner map[string]string, namespace, serviceAccount, arn, replaces string) error {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	current, err := h.association(ctx, namespace, serviceAccount)
	if err != nil {
		return err
	}
	if current == nil {
		_, err = h.EKS.CreatePodIdentityAssociation(ctx, &eks.CreatePodIdentityAssociationInput{ClusterName: &h.ClusterName, Namespace: &namespace, ServiceAccount: &serviceAccount, RoleArn: &arn, Tags: tags, ClientRequestToken: aws.String(OperationID(h.ClusterName, tags[tagNamespaceUID], namespace, serviceAccount, arn)), DisableSessionTags: aws.Bool(false)})
		// An association that exists already, such as one whose create response
		// was lost, is verified by the next call like any other.
		if err != nil && !platform.HasAWSErrorCode(err, "ResourceInUseException") {
			return fmt.Errorf("create the Pod Identity association of %s/%s: %w", namespace, serviceAccount, err)
		}
		return fmt.Errorf("%w: the Pod Identity association of %s/%s is propagating", infra.ErrPending, namespace, serviceAccount)
	}
	if role := aws.ToString(current.RoleArn); role != arn && replaces != "" && strings.HasPrefix(role, replaces) {
		if err = VerifyTags(owner, current.Tags); err != nil {
			return err
		}
		return h.deleteAssociation(ctx, current.AssociationId)
	}
	if err = VerifyTags(tags, current.Tags); err != nil {
		return err
	}
	if aws.ToString(current.RoleArn) != arn || aws.ToString(current.Namespace) != namespace || aws.ToString(current.ServiceAccount) != serviceAccount || aws.ToBool(current.DisableSessionTags) || strings.TrimSpace(aws.ToString(current.TargetRoleArn)) != "" || strings.TrimSpace(aws.ToString(current.Policy)) != "" {
		return errors.New("project Pod Identity association changed")
	}
	// Pod Identity serves an association only once it has propagated.
	changed := aws.ToTime(current.CreatedAt)
	if modified := aws.ToTime(current.ModifiedAt); modified.After(changed) {
		changed = modified
	}
	if time.Since(changed) < associationPropagation {
		return fmt.Errorf("%w: the Pod Identity association of %s/%s is propagating", infra.ErrPending, namespace, serviceAccount)
	}
	return nil
}
