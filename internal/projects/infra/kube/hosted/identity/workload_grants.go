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
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// IssueWorkload gives a workload generation an IAM role scoped to its
// configuration, secrets and connections in the Live bucket, and binds the
// role to its service account through Pod Identity. A Live generation gets its
// own role and service account; a Preview's generations share the role of its
// Sandbox principal (issuePreview).
func (h *Account) IssueWorkloadWithObjects(ctx context.Context, k *kube.Infrastructure, s infra.Workload, objects *infra.ApplicationObjects) error {
	keys, err := k.AcceptedSecretKeys(ctx, s, objects)
	if err != nil {
		return err
	}
	if s.Preview() {
		return h.issuePreview(ctx, k, s, keys)
	}
	name := roleName(grantName(k, s), s.Name())
	// The revocation adds the deny-all policy beside the scope policy, so the
	// budget reserves it and the fence always fits.
	policy := policyJSON(h.grantStatements(s, keys)...)
	if err = checkScopePolicySize(name, policy, policySize(denyAllPolicy)); err != nil {
		return err
	}
	namespace, sa := roleServiceAccount(k, s)
	metadata := kubeapi.Metadata(k.ProjectNamespace(s.ProjectID), sa, k.WorkloadLabels(s))
	if _, err = k.Ensure(ctx, kubeapi.ServiceAccount(metadata), s.ProjectID, kube.ComponentApplication); err != nil {
		return err
	}
	tags := grantTags(k, s)
	if err = h.deleteExpiredLiveFences(ctx, k, s, name); err != nil {
		return err
	}
	arn, err := h.ensureRole(ctx, k, tags, name, namespace, sa, policy)
	if err != nil {
		return err
	}
	return h.ensureAssociation(ctx, tags, tags, namespace, sa, arn, "")
}

// fenceLifetime is how long a revoked Live generation's role stays as a deny
// fence. It outlives every credential the role issued, so no credential of
// a deleted fence is ever valid; the constant below fails to compile otherwise.
const fenceLifetime = 12 * time.Hour

const _ = uint64(fenceLifetime - max(podIdentityCredentialLifetime, roleMaxSession) - 1)

// deleteExpiredLiveFences deletes the project's Live grant roles whose fence
// lifetime has passed, after their tag check, one per call; issuing a Live
// generation runs it before the generation's own role, current, which it never
// deletes. A role without a revoked-at tag, the Dex role or a current grant,
// never expires. The listing matches only the project's Live roles: their
// names start with the Live scope's cloud name.
func (h *Account) deleteExpiredLiveFences(ctx context.Context, k *kube.Infrastructure, s infra.Workload, current string) error {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	owner := ownerTags(k, s)
	return h.eachRole(ctx, k, LiveName(k, s.ProjectID), func(role string, tags map[string]string) (bool, error) {
		if role == current || !fenceExpired(tags) {
			return false, nil
		}
		if err := VerifyTags(owner, tags); err != nil {
			return true, err
		}
		if err := h.deleteRole(ctx, role); err != nil {
			return true, err
		}
		return true, fmt.Errorf("%w: the expired fence IAM role %s is being deleted", infra.ErrPending, role)
	})
}

// fenceExpired reports whether a role's revocation is older than the fence
// lifetime. A role without a revocation date is a live grant or a fence still
// being dated; neither has expired.
func fenceExpired(tags map[string]string) bool {
	revoked, err := time.Parse(time.RFC3339, tags[tagRevokedAt])
	return err == nil && time.Since(revoked) >= fenceLifetime
}

// GrantEnvironment issues the grant and carries no credential: Pod Identity
// provides the role's, and the workload uses the project KMS key for its
// writes.
func (h *Account) GrantEnvironmentWithObjects(ctx context.Context, k *kube.Infrastructure, s infra.Workload, objects *infra.ApplicationObjects) (map[string]string, error) {
	if err := h.IssueWorkloadWithObjects(ctx, k, s, objects); err != nil {
		return nil, err
	}
	env := map[string]string{"AWS_REGION": h.Region, "AWS_EC2_METADATA_DISABLED": "true", "DEX_PROJECT_STORAGE_KMS_KEY_ARN": h.KMSKeyARN}
	if s.Preview() {
		// The Preview process runs in the Sandbox with the Pod Identity token
		// projected for its service account.
		env["AWS_CONTAINER_CREDENTIALS_FULL_URI"] = "http://169.254.170.23/v1/credentials"
		env["AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE"] = "/var/run/secrets/pods.eks.amazonaws.com/serviceaccount/eks-pod-identity-token"
	}
	return env, nil
}

// WorkloadServiceAccount is the account of the generation's own IAM role.
func (h *Account) WorkloadServiceAccount(k *kube.Infrastructure, s infra.Workload) string {
	_, name := roleServiceAccount(k, s)
	return name
}

// roleServiceAccount is the namespace and service account a workload role
// trusts: the Live generation's own, or the Preview's Sandbox principal.
func roleServiceAccount(k *kube.Infrastructure, s infra.Workload) (namespace, name string) {
	if s.Preview() {
		return s.Principal.Boundary.Name, s.Principal.Identity.Name
	}
	return k.ProjectNamespace(s.ProjectID), s.Name()
}

// grantStatements are the IAM policy statements of a workload's grant, which
// also uses the project KMS key only through S3 for the bucket and the granted
// objects.
func (h *Account) grantStatements(s infra.Workload, secretKeys []string) []policyStatement {
	bucket, _, reads, readWrites := s.GrantResources(secretKeys)
	return []policyStatement{
		{Effect: effectAllow, Action: actionS3GetBucketVersioning, Resource: bucket},
		{Effect: effectAllow, Action: []policyAction{actionS3GetObject, actionS3GetObjectVersion}, Resource: reads},
		{Effect: effectAllow, Action: []policyAction{actionS3GetObject, actionS3GetObjectVersion, actionS3PutObject}, Resource: readWrites},
		h.kmsThroughS3(bucket, slices.Concat(reads, readWrites)...),
	}
}

// roleARN is the ARN of the workload role name.
func (h *Account) roleARN(k *kube.Infrastructure, name string) string {
	return "arn:aws:iam::" + h.AccountID + ":role" + k.Names().IAMWorkloadPath + name
}

// RevokeWorkload revokes one workload generation. A Live generation's role
// gets a deny policy and a revoked-at tag and loses its Pod Identity
// association; the denied role stays as a fence against a late issuance for
// fenceLifetime, after which the project's next Live issuance deletes it, or
// until the scope is cleaned up. Live generations have their own service
// accounts, so new Pods get new credentials and the deny-all, which IAM
// evaluates per request, stops cached ones. A Preview generation leaves the policy of its
// principal role (revokePreview). AWS applies the denial asynchronously, so
// observing it does not prove that issued credentials fail.
func (h *Account) RevokeWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	if s.Storage.Boundary.UID == "" {
		// The cloud identities are tagged with the Live namespace UID. An
		// unrecorded UID comes from the current namespace; without it no role
		// can be proven the workload's own.
		ns, err := k.CleanupNamespace(ctx, s.ProjectID, "")
		if err != nil || ns == nil {
			return err
		}
		s.Storage.Boundary = kube.ResourceReference(ns)
	}
	if s.Preview() {
		return h.revokePreview(ctx, k, s)
	}
	namespace, sa := roleServiceAccount(k, s)
	name := roleName(grantName(k, s), s.Name())
	result, err := h.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: &name})
	if platform.HasAWSErrorCode(err, "NoSuchEntity") {
		// Create the role deny-only even when issuance lost its response, so a
		// stale issuance can never create an enabled identity later.
		if err = h.createScopedRole(ctx, k, grantTags(k, s), name, namespace, sa, nil); err != nil {
			return err
		}
		return fmt.Errorf("%w: IAM role %s was created as a revocation fence", infra.ErrPending, name)
	}
	if err != nil {
		return err
	}
	owner := ownerTags(k, s)
	if err = VerifyTags(owner, iamTagMap(result.Role.Tags)); err != nil {
		return err
	}
	if err = h.ensureInlinePolicy(ctx, name, revokedPolicyName, denyAllPolicy); err != nil {
		return err
	}
	// The first revocation dates the fence; a repeated one keeps that date.
	// The platform may tag only with its owner tags in the request.
	if _, dated := iamTagMap(result.Role.Tags)[tagRevokedAt]; !dated {
		if err = h.tagRole(ctx, name, owner, tagRevokedAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	association, err := h.association(ctx, namespace, sa)
	if err != nil || association == nil {
		return err
	}
	if err = VerifyTags(owner, association.Tags); err != nil {
		return err
	}
	if aws.ToString(association.RoleArn) != aws.ToString(result.Role.Arn) {
		return fmt.Errorf("%w: workload association changed before revocation", infra.ErrOwnershipConflict)
	}
	return h.deleteAssociation(ctx, association.AssociationId)
}

// tagRole sets one tag of a role. The platform may tag only with its owner
// tags in the request, so they are repeated.
func (h *Account) tagRole(ctx context.Context, role string, owner map[string]string, key, value string) error {
	tags := map[string]string{key: value, tagManagedBy: owner[tagManagedBy], tagEnvironment: owner[tagEnvironment]}
	if _, err := h.IAM.TagRole(ctx, &iam.TagRoleInput{RoleName: &role, Tags: iamTags(tags)}); err != nil {
		return fmt.Errorf("tag IAM role %s with %s: %w", role, key, err)
	}
	return nil
}

// DestroyPreviewGrants deletes the project Preview's Pod Identity
// associations in its principal's namespace and then its principal roles,
// one per call.
func (h *Account) DestroyPreviewGrants(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, namespace kubeapi.Object, principalNamespace string) error {
	// The grants outlive the Live namespace; their tags bind its UID.
	namespaceUID := storage.Boundary.UID
	if namespace != nil {
		namespaceUID = kubeapi.UID(namespace)
	}
	return h.DestroyIdentities(ctx, k, cloudName(k, project, infra.PreviewKeysOf(project).GrantIdentity), principalNamespace, previewTags(k, project, namespaceUID))
}
