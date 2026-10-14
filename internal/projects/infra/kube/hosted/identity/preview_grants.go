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
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// A Preview has one IAM role per Sandbox principal with one Pod Identity
// association that never moves to another role. The EKS Pod Identity agent
// caches the credentials it issued, so a Pod kept the superseded role's
// credentials after an association moved to a new generation's role, and the
// new application failed with AccessDenied. A grant generation therefore
// rewrites the scope policy of the principal role in place: its statements
// carry their generation in their Sid, a new generation keeps the statements of
// the generation it supersedes until that one is revoked, and revoking the last
// generation leaves a deny-all policy. IAM evaluates the policy per request,
// so even cached credentials follow it once the change has propagated, which
// takes a few seconds. The role's tags record the highest generation issued and
// revoked: a late issuance of an older or revoked generation never widens the
// policy.

// previewRoleName names the role of a Preview's Sandbox principal, by the
// principal's namespace and service account UIDs.
func previewRoleName(k *kube.Infrastructure, s infra.Workload) string {
	return roleName(grantName(k, s), "principal/"+s.Principal.Boundary.UID+"/"+s.Principal.Identity.UID)
}

// generationSidPrefix starts the Sid of a Preview grant statement:
// Generation<generation>Statement<index>.
const generationSidPrefix = "Generation"

// generationSid is the Sid of statement index of a grant generation.
func generationSid(generation uint64, index int) string {
	return generationSidPrefix + strconv.FormatUint(generation, 10) + "Statement" + strconv.Itoa(index)
}

// sidGeneration is the grant generation a statement's Sid names; ok is false
// for a statement of no generation, such as the deny-all statement.
func sidGeneration(sid string) (generation uint64, ok bool) {
	rest, found := strings.CutPrefix(sid, generationSidPrefix)
	digits, _, found2 := strings.Cut(rest, "Statement")
	if !found || !found2 {
		return 0, false
	}
	generation, err := strconv.ParseUint(digits, 10, 64)
	return generation, err == nil
}

// tagGeneration reads a generation tag; an absent tag is generation 0.
func tagGeneration(tags map[string]string, key string) (uint64, error) {
	value, found := tags[key]
	if !found {
		return 0, nil
	}
	generation, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("decode the %s tag: %w", key, err)
	}
	return generation, nil
}

// issuePreview grants generation s.Generation through the principal role. The
// generation is recorded on the role before the policy changes, the policy
// keeps the statements of every other generation it holds and replaces this
// generation's, and the association is created once. The Preview's other roles,
// of an earlier principal or of a per-generation grant, are deleted afterwards.
// A generation whose statements alone exceed the role's inline policy limit is
// refused before any change.
func (h *Account) issuePreview(ctx context.Context, k *kube.Infrastructure, s infra.Workload, secretKeys []string) error {
	name := previewRoleName(k, s)
	var own []policyStatement
	for index, statement := range h.grantStatements(s, secretKeys) {
		statement.Sid = generationSid(s.Generation, index)
		own = append(own, statement)
	}
	if err := checkScopePolicySize(name, policyJSON(own...), 0); err != nil {
		return err
	}
	identityCtx, cancel := CloudDeadline(ctx)
	defer cancel()
	tags, owner := grantTags(k, s), ownerTags(k, s)
	namespace, sa := roleServiceAccount(k, s)
	role, err := h.ensureRoleIdentity(identityCtx, k, tags, name, namespace, sa)
	if err != nil {
		return err
	}
	roleTags := iamTagMap(role.Tags)
	issued, err := tagGeneration(roleTags, tagGrantGeneration)
	if err != nil {
		return err
	}
	revoked, err := tagGeneration(roleTags, tagRevokedGeneration)
	if err != nil {
		return err
	}
	switch {
	case s.Generation <= revoked:
		return fmt.Errorf("the Preview grant generation %d of IAM role %s is revoked", s.Generation, name)
	case s.Generation < issued:
		return fmt.Errorf("the Preview grant generation %d is older than generation %d of IAM role %s", s.Generation, issued, name)
	case s.Generation > issued:
		if err = h.tagRole(identityCtx, name, owner, tagGrantGeneration, strconv.FormatUint(s.Generation, 10)); err != nil {
			return err
		}
	}
	current, statements, err := h.scopeStatements(identityCtx, name)
	if err != nil {
		return err
	}
	document := policyJSON(append(withoutGeneration(statements, s.Generation), own...)...)
	if err = checkScopePolicySize(name, document, 0); err != nil {
		// The superseded generation keeps its statements until the running
		// application no longer needs them; a Preview restart revokes it.
		return fmt.Errorf("%w, together with the statements of the generation it supersedes; stop and start Preview, or name fewer secrets", err)
	}
	if err = h.awaitScopePolicy(identityCtx, name, owner, roleTags[tagScopeWritten], s.Generation, current, document); err != nil {
		return err
	}
	cancel()
	arn := aws.ToString(role.Arn)
	if err = h.ensureAssociation(ctx, tags, owner, namespace, sa, arn, h.roleARN(k, grantName(k, s)+"-")); err != nil {
		return err
	}
	return h.deleteOtherPreviewRoles(ctx, k, s, name)
}

// revokePreview revokes generation s.Generation of the principal role: the
// revoked generation is recorded first, then the policy loses that
// generation's statements and denies everything once no generation remains.
// A missing role is created deny-only, so a lost issuance can never create an
// enabled one later. The association stays until the Preview's data is
// deleted, and the Preview's other roles are deleted.
func (h *Account) revokePreview(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	namespace, sa := roleServiceAccount(k, s)
	name := previewRoleName(k, s)
	owner := ownerTags(k, s)
	generation := strconv.FormatUint(s.Generation, 10)
	result, err := h.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: &name})
	if platform.HasAWSErrorCode(err, "NoSuchEntity") {
		tags := grantTags(k, s)
		tags[tagGrantGeneration], tags[tagRevokedGeneration] = generation, generation
		if err = h.createScopedRole(ctx, k, tags, name, namespace, sa, nil); err != nil {
			return err
		}
		return fmt.Errorf("%w: IAM role %s was created revoked", infra.ErrPending, name)
	}
	if err != nil {
		return err
	}
	roleTags := iamTagMap(result.Role.Tags)
	if err = VerifyTags(owner, roleTags); err != nil {
		return err
	}
	revoked, err := tagGeneration(roleTags, tagRevokedGeneration)
	if err != nil {
		return err
	}
	if s.Generation > revoked {
		if err = h.tagRole(ctx, name, owner, tagRevokedGeneration, generation); err != nil {
			return err
		}
	}
	current, statements, err := h.scopeStatements(ctx, name)
	if err != nil {
		return err
	}
	document := denyAllPolicy
	if remaining := withoutGeneration(statements, s.Generation); len(remaining) > 0 {
		document = policyJSON(remaining...)
	}
	if err = h.putScopePolicy(ctx, name, current, document); err != nil {
		return err
	}
	return h.deleteOtherPreviewRoles(ctx, k, s, name)
}

// scopeStatements reads the scope policy of a role and returns it with its
// statements of a grant generation; a role without the policy has none.
func (h *Account) scopeStatements(ctx context.Context, role string) (*string, []policyStatement, error) {
	result, err := h.IAM.GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: &role, PolicyName: aws.String(scopePolicyName)})
	if platform.HasAWSErrorCode(err, "NoSuchEntity") {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read the %s policy of IAM role %s: %w", scopePolicyName, role, err)
	}
	decoded, err := url.QueryUnescape(aws.ToString(result.PolicyDocument))
	if err != nil {
		return nil, nil, fmt.Errorf("decode the %s policy of IAM role %s: %w", scopePolicyName, role, err)
	}
	var document policyDocument
	if err = json.Unmarshal([]byte(decoded), &document); err != nil {
		return nil, nil, fmt.Errorf("decode the %s policy of IAM role %s: %w", scopePolicyName, role, err)
	}
	var statements []policyStatement
	for _, statement := range document.Statement {
		if _, ok := sidGeneration(statement.Sid); ok {
			statements = append(statements, statement)
		}
	}
	return result.PolicyDocument, statements, nil
}

// withoutGeneration are the statements of every grant generation but one.
func withoutGeneration(statements []policyStatement, generation uint64) []policyStatement {
	var kept []policyStatement
	for _, statement := range statements {
		if g, _ := sidGeneration(statement.Sid); g != generation {
			kept = append(kept, statement)
		}
	}
	return kept
}

// policyPropagation is how long an issuance stays pending after it wrote the
// scope policy of a principal role. IAM applies a policy change everywhere
// only after a delay that AWS does not bound; a Preview application started
// earlier could get AccessDenied on its first reads. It is an upper bound
// above the few seconds AWS describes, to be measured on AWS test once the
// owner approves that verification.
const policyPropagation = 15 * time.Second

// awaitScopePolicy writes the issuance's scope policy and stays pending until
// policyPropagation has passed since the write of this generation's document.
// The scope-written tag (<generation>@<time>) records that write after the
// policy. Only a write starts a window:
//   - the document changed: it is written, the tag is set and the window starts;
//   - it is unchanged and the tag records another generation, no generation or
//     an unreadable time: an earlier attempt of this generation wrote it and
//     lost its tag, so the tag is set and the window starts;
//   - it is unchanged and the tag records this generation: no window starts,
//     and the issuance waits only for what remains of the recorded one, which
//     has passed for every issuance after the first.
func (h *Account) awaitScopePolicy(ctx context.Context, role string, owner map[string]string, written string, generation uint64, current *string, document string) error {
	err := h.putScopePolicy(ctx, role, current, document)
	if err != nil && !errors.Is(err, infra.ErrPending) {
		return err
	}
	recordedGeneration, at, recorded := strings.Cut(written, "@")
	writtenAt, parseErr := time.Parse(time.RFC3339, at)
	if err != nil || !recorded || parseErr != nil || recordedGeneration != strconv.FormatUint(generation, 10) {
		now := time.Now().UTC().Format(time.RFC3339)
		if tagErr := h.tagRole(ctx, role, owner, tagScopeWritten, strconv.FormatUint(generation, 10)+"@"+now); tagErr != nil {
			return tagErr
		}
		return fmt.Errorf("%w: the %s policy of IAM role %s is propagating", infra.ErrPending, scopePolicyName, role)
	}
	if time.Since(writtenAt) < policyPropagation {
		return fmt.Errorf("%w: the %s policy of IAM role %s is propagating", infra.ErrPending, scopePolicyName, role)
	}
	return nil
}

// putScopePolicy writes the scope policy of a role unless current, the
// policy read before, is already the document, and stays pending while a new
// policy propagates.
func (h *Account) putScopePolicy(ctx context.Context, role string, current *string, document string) error {
	if current != nil {
		matches, err := policyMatches(current, document)
		if err != nil {
			return fmt.Errorf("decode the %s policy of IAM role %s: %w", scopePolicyName, role, err)
		}
		if matches {
			return nil
		}
	}
	if _, err := h.IAM.PutRolePolicy(ctx, &iam.PutRolePolicyInput{RoleName: &role, PolicyName: aws.String(scopePolicyName), PolicyDocument: &document}); err != nil {
		return fmt.Errorf("put the %s policy of IAM role %s: %w", scopePolicyName, role, err)
	}
	return fmt.Errorf("%w: the %s policy of IAM role %s is propagating", infra.ErrPending, scopePolicyName, role)
}

// deleteOtherPreviewRoles deletes the Preview's roles other than keep, after
// a check of the owner's tags, one per call: roles of an earlier Sandbox
// principal, which no issuance can reach because it verifies the current
// principal first, and per-generation roles of earlier releases.
func (h *Account) deleteOtherPreviewRoles(ctx context.Context, k *kube.Infrastructure, s infra.Workload, keep string) error {
	ctx, cancel := CloudDeadline(ctx)
	defer cancel()
	owner := ownerTags(k, s)
	return h.eachRole(ctx, k, grantName(k, s), func(role string, tags map[string]string) (bool, error) {
		if role == keep {
			return false, nil
		}
		if err := VerifyTags(owner, tags); err != nil {
			return true, err
		}
		if err := h.deleteRole(ctx, role); err != nil {
			return true, err
		}
		return true, fmt.Errorf("%w: IAM role %s of an earlier Preview grant is being deleted", infra.ErrPending, role)
	})
}
