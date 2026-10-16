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
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	cloud "go.temporal.io/cloud-sdk/api/cloudservice/v1"
	cloudidentity "go.temporal.io/cloud-sdk/api/identity/v1"
	namespace "go.temporal.io/cloud-sdk/api/namespace/v1"
	operationpb "go.temporal.io/cloud-sdk/api/operation/v1"
	resource "go.temporal.io/cloud-sdk/api/resource/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// temporalKeyLifetime is the lifetime of a project's Temporal Cloud API key, a
// platform choice below the provider's two-year maximum.
const temporalKeyLifetime = 89 * 24 * time.Hour

// temporalIdentitySecret records the ID of the scope's Temporal Cloud service
// account, so a call reads the account by ID instead of listing the account.
const temporalIdentitySecret = "temporal-identity"

// TemporalNamespace names the scope's Temporal Cloud namespace.
func (h *CloudAccount) TemporalNamespace(k *kube.Infrastructure, project string) string {
	return identity.LiveName(k, project) + "." + h.config.TemporalAccountID
}

// findNamespace returns the scope's live Temporal Cloud namespace, or nil.
func (h *CloudAccount) findNamespace(ctx context.Context, k *kube.Infrastructure, project, uid string) (*namespace.Namespace, error) {
	out, err := h.temporal.CloudService().GetNamespace(ctx, &cloud.GetNamespaceRequest{Namespace: h.TemporalNamespace(k, project)})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n := out.Namespace
	if n != nil && n.State == resource.ResourceState_RESOURCE_STATE_DELETED {
		return nil, nil
	}
	if n == nil || n.Namespace != h.TemporalNamespace(k, project) {
		return nil, errors.New("project Temporal namespace identity is invalid")
	}
	if err = identity.VerifyTags(identity.LiveTags(k, project, uid), n.Tags); err != nil {
		return nil, err
	}
	return n, nil
}

// EnsureTemporal converges the project's Temporal Cloud namespace, its admin
// service account and the API key of the accepted credential generation, or
// of the next one once the accepted key is inside its renewal window, and
// returns the access to the namespace with the reference of the Secret that
// holds the key. The reference is returned as soon as the Secret exists, also
// while the key becomes active.
func (h *CloudAccount) EnsureTemporal(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, _ string, accepted infra.ResourceRef) (infra.TemporalAccess, infra.ResourceRef, error) {
	uid := storage.Boundary.UID
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	n, err := h.findNamespace(ctx, k, project, uid)
	if err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	if n == nil {
		_, err = h.temporal.CloudService().CreateNamespace(ctx, &cloud.CreateNamespaceRequest{Spec: &namespace.NamespaceSpec{Name: identity.LiveName(k, project), Description: "Superverse V2 " + k.Environment.String() + " " + project + " live " + uid, Replicas: []*namespace.ReplicaSpec{{Region: h.config.TemporalRegion}}, RetentionDays: h.config.TemporalRetentionDays, ApiKeyAuth: &namespace.ApiKeyAuthSpec{Enabled: true}, SearchAttributes: cloudSearchAttributes()}, Tags: identity.LiveTags(k, project, uid), AsyncOperationId: identity.OperationID(uid, "temporal-create")})
		if err != nil && status.Code(err) != codes.AlreadyExists {
			return infra.TemporalAccess{}, infra.ResourceRef{}, err
		}
		return infra.TemporalAccess{}, infra.ResourceRef{}, fmt.Errorf("%w: Temporal namespace %s is being created", infra.ErrPending, identity.LiveName(k, project))
	}
	if err = h.verifyNamespace(n); err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	sa, err := h.ensureTemporalServiceAccount(ctx, k, project, uid)
	if err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	// The previous generation's key stays while the project Dex may still run it.
	dexGeneration, ready, found, err := k.ProjectDexCredentialGeneration(ctx, project)
	if err != nil {
		return infra.TemporalAccess{}, infra.ResourceRef{}, err
	}
	holder := temporalKeyHolder{k: k, project: project, uid: uid, name: identity.LiveName(k, project), account: sa}
	token, credential, err := h.temporalCredential(ctx, holder, accepted, found && (!ready || dexGeneration != accepted.Generation))
	if err != nil {
		return infra.TemporalAccess{}, credential, err
	}
	access, err := namespaceAccess(n, token)
	return access, credential, err
}

// ReadTemporalAccess reads the access to the project's accepted Temporal
// Cloud namespace and credential without changing anything: the namespace must
// be active and unchanged, and the credential Secret the accepted one of its
// generation.
func (h *CloudAccount) ReadTemporalAccess(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, _ string, credential infra.ResourceRef) (infra.TemporalAccess, error) {
	uid := storage.Boundary.UID
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	n, err := h.findNamespace(ctx, k, project, uid)
	if err != nil {
		return infra.TemporalAccess{}, err
	}
	if n == nil {
		return infra.TemporalAccess{}, errors.New("project Temporal namespace is absent")
	}
	if err = h.verifyNamespace(n); err != nil {
		return infra.TemporalAccess{}, err
	}
	if credential.Name != credentialSecretName(credential.Generation) {
		return infra.TemporalAccess{}, fmt.Errorf("%w: the accepted credential Secret name does not match its generation", infra.ErrTemporalCredential)
	}
	stored, err := k.GetOwned(ctx, k.NamespacePath(project)+"/secrets/"+credential.Name, project, kube.ComponentDex)
	if err != nil {
		return infra.TemporalAccess{}, err
	}
	if stored == nil || kubeapi.UID(stored) != credential.UID {
		return infra.TemporalAccess{}, fmt.Errorf("%w: the accepted temporal-credential Secret is absent or replaced", infra.ErrTemporalCredential)
	}
	token, err := kube.SecretValue(stored, "api-key")
	if err != nil {
		return infra.TemporalAccess{}, err
	}
	return namespaceAccess(n, token)
}

// verifyNamespace checks that the project's namespace is active with the
// configuration this account creates.
func (h *CloudAccount) verifyNamespace(n *namespace.Namespace) error {
	if n.State != resource.ResourceState_RESOURCE_STATE_ACTIVE {
		return fmt.Errorf("%w: Temporal namespace %s is %s", infra.ErrPending, n.Namespace, n.State)
	}
	if n.Spec == nil || !n.Spec.GetApiKeyAuth().GetEnabled() || n.Spec.RetentionDays != h.config.TemporalRetentionDays || len(n.Spec.Replicas) != 1 || n.Spec.Replicas[0].Region != h.config.TemporalRegion {
		return errors.New("project Temporal namespace configuration changed")
	}
	for key, want := range cloudSearchAttributes() {
		if n.Spec.SearchAttributes[key] != want {
			return errors.New("project Temporal search attributes changed")
		}
	}
	return nil
}

// namespaceAccess is the access to a verified namespace with an API key.
func namespaceAccess(n *namespace.Namespace, token string) (infra.TemporalAccess, error) {
	endpoint := n.GetEndpoints().GetGrpcAddress()
	if endpoint == "" {
		return infra.TemporalAccess{}, fmt.Errorf("%w: Temporal namespace %s has no gRPC endpoint yet", infra.ErrPending, n.Namespace)
	}
	return infra.TemporalAccess{HostPort: endpoint, Namespace: n.Namespace, APIKey: token}, nil
}

// cloudSearchAttributes are infra.ProjectSearchAttributes in Temporal Cloud types.
func cloudSearchAttributes() map[string]namespace.NamespaceSpec_SearchAttributeType {
	attributes := make(map[string]namespace.NamespaceSpec_SearchAttributeType, len(infra.ProjectSearchAttributes))
	for _, attribute := range infra.ProjectSearchAttributes {
		attributes[attribute.Name] = namespace.NamespaceSpec_SEARCH_ATTRIBUTE_TYPE_KEYWORD
		if attribute.KeywordList {
			attributes[attribute.Name] = namespace.NamespaceSpec_SEARCH_ATTRIBUTE_TYPE_KEYWORD_LIST
		}
	}
	return attributes
}

// ensureTemporalServiceAccount converges the scope's namespace-admin service
// account and returns it once active. A create records the ID its response
// returns; a lost response is found by name on the next call.
func (h *CloudAccount) ensureTemporalServiceAccount(ctx context.Context, k *kube.Infrastructure, project, uid string) (*cloudidentity.ServiceAccount, error) {
	recorded, sa, err := h.temporalServiceAccount(ctx, k, project, uid, true)
	if err != nil {
		return nil, err
	}
	if sa == nil && recorded {
		return nil, fmt.Errorf("%w: the recorded Temporal service account is not listed", infra.ErrPending)
	}
	if sa == nil {
		out, err := h.temporal.CloudService().CreateServiceAccount(ctx, &cloud.CreateServiceAccountRequest{Spec: &cloudidentity.ServiceAccountSpec{Name: identity.LiveName(k, project), Description: k.Names().Product + "/" + uid, NamespaceScopedAccess: &cloudidentity.NamespaceScopedAccess{Namespace: h.TemporalNamespace(k, project), Access: &cloudidentity.NamespaceAccess{Permission: cloudidentity.NamespaceAccess_PERMISSION_ADMIN}}}, AsyncOperationId: identity.OperationID(uid, "temporal-account")})
		switch {
		case err == nil && out.ServiceAccountId != "":
			if err = recordServiceAccount(ctx, k, project, out.ServiceAccountId); err != nil {
				return nil, err
			}
		case err != nil && status.Code(err) != codes.AlreadyExists:
			return nil, err
		}
		return nil, fmt.Errorf("%w: Temporal service account %s is being created", infra.ErrPending, identity.LiveName(k, project))
	}
	if sa.State != resource.ResourceState_RESOURCE_STATE_ACTIVE {
		return nil, fmt.Errorf("%w: Temporal service account %s is %s", infra.ErrPending, sa.Id, sa.State)
	}
	return sa, nil
}

// temporalServiceAccount returns the scope's Temporal Cloud service account,
// or nil, and whether its ID is recorded. A recorded account is read by its ID.
// Without a record, before a create's response was recorded or for a scope
// created before the record existed, it is found by name in one bounded list
// of the account, and record stores the ID it finds. It admits the account
// only with the scope's owner marker and namespace-admin access.
func (h *CloudAccount) temporalServiceAccount(ctx context.Context, k *kube.Infrastructure, project, uid string, record bool) (bool, *cloudidentity.ServiceAccount, error) {
	stored, err := k.GetOwned(ctx, k.NamespacePath(project)+"/secrets/"+temporalIdentitySecret, project, kube.ComponentDex)
	if err != nil {
		return false, nil, err
	}
	var sa *cloudidentity.ServiceAccount
	if stored != nil {
		if sa, err = h.readTemporalServiceAccount(ctx, stored); err != nil || sa == nil {
			return true, nil, err
		}
	} else if sa, err = h.listTemporalServiceAccount(ctx, k, project); err != nil || sa == nil {
		return false, nil, err
	}
	if err = h.verifyServiceAccountScope(k, project, uid, sa); err != nil {
		return false, nil, err
	}
	if stored == nil && record {
		if err = recordServiceAccount(ctx, k, project, sa.Id); err != nil {
			return false, nil, err
		}
	}
	return stored != nil, sa, nil
}

// verifyServiceAccountScope admits a service account only with the scope's
// name, owner marker and namespace-admin access.
func (h *CloudAccount) verifyServiceAccountScope(k *kube.Infrastructure, project, uid string, sa *cloudidentity.ServiceAccount) error {
	spec := sa.Spec
	scope := spec.GetNamespaceScopedAccess()
	if spec.GetName() != identity.LiveName(k, project) || spec.Description != k.Names().Product+"/"+uid || spec.Access != nil || spec.ProjectScopedAccess != nil || scope.GetNamespace() != h.TemporalNamespace(k, project) || scope.GetAccess().GetPermission() != cloudidentity.NamespaceAccess_PERMISSION_ADMIN {
		return errors.New("project Temporal identity scope changed")
	}
	return nil
}

// readTemporalServiceAccount reads the recorded service account by ID, or nil
// once it is gone.
func (h *CloudAccount) readTemporalServiceAccount(ctx context.Context, stored kubeapi.Object) (*cloudidentity.ServiceAccount, error) {
	id, err := kube.SecretValue(stored, "service-account-id")
	if err != nil {
		return nil, err
	}
	out, err := h.temporal.CloudService().GetServiceAccount(ctx, &cloud.GetServiceAccountRequest{ServiceAccountId: id})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sa := out.GetServiceAccount()
	if sa == nil || sa.Id != id {
		return nil, errors.New("project Temporal identity is invalid")
	}
	if sa.State == resource.ResourceState_RESOURCE_STATE_DELETED {
		return nil, nil
	}
	return sa, nil
}

// listTemporalServiceAccount finds the scope's undeleted service account by
// name in at most 100 pages of the account's service accounts.
func (h *CloudAccount) listTemporalServiceAccount(ctx context.Context, k *kube.Infrastructure, project string) (*cloudidentity.ServiceAccount, error) {
	var found *cloudidentity.ServiceAccount
	page := ""
	for i := 0; i < 100; i++ {
		out, err := h.temporal.CloudService().GetServiceAccounts(ctx, &cloud.GetServiceAccountsRequest{PageSize: 1000, PageToken: page})
		if err != nil {
			return nil, err
		}
		for _, sa := range out.ServiceAccount {
			if sa.State == resource.ResourceState_RESOURCE_STATE_DELETED || sa.GetSpec().GetName() != identity.LiveName(k, project) {
				continue
			}
			if found != nil {
				return nil, errors.New("ambiguous project Temporal identity")
			}
			found = sa
		}
		if out.NextPageToken == "" {
			return found, nil
		}
		page = out.NextPageToken
	}
	return nil, errors.New("project Temporal identity lookup exceeded bounded pages")
}

// recordServiceAccount stores the service account ID in the scope's
// temporal-identity Secret; another recorded ID is a conflict.
func recordServiceAccount(ctx context.Context, k *kube.Infrastructure, project, id string) error {
	stored, err := k.Ensure(ctx, dexSecret(k, project, temporalIdentitySecret, map[string]string{"service-account-id": id}), project, kube.ComponentDex)
	if err != nil {
		return err
	}
	if recorded, err := kube.SecretValue(stored, "service-account-id"); err != nil || recorded != id {
		return fmt.Errorf("%w: another Temporal service account is recorded", infra.ErrOwnershipConflict)
	}
	return nil
}

// temporalKeyHolder is a service account whose API keys rotate, with the
// project namespace whose Secrets keep their one-time tokens. Each credential
// generation has its own key display name and Secrets, so the next key
// overlaps the accepted one until no Dex runs the accepted one.
type temporalKeyHolder struct {
	k       *kube.Infrastructure
	project string
	// uid marks descriptions and operation IDs with the owner.
	uid string
	// name is the display name of generation 0's key.
	name    string
	account *cloudidentity.ServiceAccount
}

// keyName is the deterministic display name of a generation's key.
func (t temporalKeyHolder) keyName(generation uint64) string {
	return generationName(t.name, generation)
}

// stored returns the Secret that holds a generation's key, or nil.
func (t temporalKeyHolder) stored(ctx context.Context, generation uint64) (kubeapi.Object, error) {
	return t.k.GetOwned(ctx, t.k.NamespacePath(t.project)+"/secrets/"+credentialSecretName(generation), t.project, kube.ComponentDex)
}

// credentialSecretName names the Secret that holds a generation's key and
// token; generation 0 keeps the name of the keys created before rotation.
func credentialSecretName(generation uint64) string {
	return generationName("temporal-credential", generation)
}

// keyAttemptSecretName names the Secret that persists a generation's create
// attempt before the provider call.
func keyAttemptSecretName(generation uint64) string {
	return generationName("temporal-key-attempt", generation)
}

func generationName(base string, generation uint64) string {
	if generation == 0 {
		return base
	}
	return base + "-" + strconv.FormatUint(generation, 10)
}

// credentialRef is the reference of a generation's credential Secret, or the
// zero reference without one.
func credentialRef(stored kubeapi.Object, generation uint64) infra.ResourceRef {
	if stored == nil {
		return infra.ResourceRef{}
	}
	ref := kube.ResourceReference(stored)
	ref.Generation = generation
	return ref
}

// temporalCredential returns the token of the holder's accepted key with the
// reference of the Secret that holds it. Once the key is inside its renewal
// window it moves to the next generation: it creates that key and returns it
// once active, so the caller's data-plane check verifies it before any Dex
// switches. Keys of no current generation are revoked; the previous one's
// stays while previousInUse. A one-time token exists only in its Secret, so a
// key whose token was lost is revoked and replaced, never guessed. A Secret
// the owning Flow accepted is never replaced: once it is gone or replaced,
// only an operator can repair the credential.
func (h *CloudAccount) temporalCredential(ctx context.Context, t temporalKeyHolder, accepted infra.ResourceRef, previousInUse bool) (string, infra.ResourceRef, error) {
	generation := accepted.Generation
	stored, err := t.stored(ctx, generation)
	if err != nil {
		return "", infra.ResourceRef{}, err
	}
	credential := credentialRef(stored, generation)
	if accepted.UID != "" && credential.UID != accepted.UID {
		return "", infra.ResourceRef{}, fmt.Errorf("%w: the accepted temporal-credential Secret is absent or replaced", infra.ErrTemporalCredential)
	}
	keys, err := h.temporalKeys(ctx, t.account)
	if err != nil {
		return "", credential, err
	}
	if stored == nil {
		// Nothing is accepted yet, so every listed key is an orphan.
		return "", infra.ResourceRef{}, h.createTemporalKey(ctx, t, generation, keys)
	}
	current, token, err := t.acceptedKey(stored, keys, generation)
	if err != nil {
		return "", credential, err
	}
	if err = h.retireTemporalKeys(ctx, t, generation, current, keys, previousInUse); err != nil {
		return "", credential, err
	}
	if time.Until(current.Spec.ExpiryTime.AsTime()) >= h.config.TemporalKeyRenewWindow {
		if current.State != resource.ResourceState_RESOURCE_STATE_ACTIVE {
			return "", credential, fmt.Errorf("%w: Temporal API key %s is %s", infra.ErrPending, current.Id, current.State)
		}
		return token, credential, nil
	}
	next := generation + 1
	if stored, err = t.stored(ctx, next); err != nil {
		return "", credential, err
	}
	if stored == nil {
		var lost []*cloudidentity.ApiKey
		for _, key := range keys {
			if key.GetSpec().GetDisplayName() == t.keyName(next) {
				lost = append(lost, key)
			}
		}
		return "", credential, h.createTemporalKey(ctx, t, next, lost)
	}
	rotated, token, err := t.acceptedKey(stored, keys, next)
	if errors.Is(err, infra.ErrTemporalCredential) {
		// The next generation is not accepted yet, so a broken one is replaced.
		return "", credential, h.discardTemporalKey(ctx, t, next, keys, errors.Is(err, errKeyUnlisted))
	}
	if err != nil {
		return "", credential, err
	}
	if rotated.State != resource.ResourceState_RESOURCE_STATE_ACTIVE {
		return "", credential, fmt.Errorf("%w: rotated Temporal API key %s is %s", infra.ErrPending, rotated.Id, rotated.State)
	}
	return token, credentialRef(stored, next), nil
}

// acceptedKey returns the listed key a generation's Secret holds and its
// token; a key that is unlisted, disabled, owned by another identity or named
// for another generation is ErrTemporalCredential.
func (t temporalKeyHolder) acceptedKey(stored kubeapi.Object, keys []*cloudidentity.ApiKey, generation uint64) (*cloudidentity.ApiKey, string, error) {
	id, err := kube.SecretValue(stored, "key-id")
	if err != nil {
		return nil, "", err
	}
	token, err := kube.SecretValue(stored, "api-key")
	if err != nil {
		return nil, "", err
	}
	for _, key := range keys {
		if key.Id != id {
			continue
		}
		if key.Spec.OwnerId != t.account.Id || key.Spec.Disabled || key.Spec.DisplayName != t.keyName(generation) || key.Spec.ExpiryTime == nil {
			return nil, "", infra.ErrTemporalCredential
		}
		return key, token, nil
	}
	return nil, "", fmt.Errorf("%w: %w: generation %d", infra.ErrTemporalCredential, errKeyUnlisted, generation)
}

// errKeyUnlisted reports a stored API key that the key list does not show.
var errKeyUnlisted = errors.New("the stored API key is not listed")

// discardTemporalKey replaces a broken generation that is not accepted yet:
// it revokes the generation's keys and then removes its Secrets, so the next
// call creates it again. A key that is only unlisted waits while its create
// operation still runs.
func (h *CloudAccount) discardTemporalKey(ctx context.Context, t temporalKeyHolder, generation uint64, keys []*cloudidentity.ApiKey, unlisted bool) error {
	secrets := t.k.NamespacePath(t.project) + "/secrets/"
	if unlisted {
		attempt, err := t.k.GetOwned(ctx, secrets+keyAttemptSecretName(generation), t.project, kube.ComponentDex)
		if err != nil {
			return err
		}
		if attempt != nil {
			operation, err := kube.SecretValue(attempt, "operation-id")
			if err != nil {
				return err
			}
			observed, err := h.temporal.CloudService().GetAsyncOperation(ctx, &cloud.GetAsyncOperationRequest{AsyncOperationId: operation})
			if err != nil && status.Code(err) != codes.NotFound {
				return err
			}
			if state := observed.GetAsyncOperation().GetState(); state == operationpb.AsyncOperation_STATE_PENDING || state == operationpb.AsyncOperation_STATE_IN_PROGRESS {
				return fmt.Errorf("%w: rotated Temporal API key creation is %s", infra.ErrPending, state)
			}
		}
	}
	var broken []*cloudidentity.ApiKey
	for _, key := range keys {
		if key.GetSpec().GetDisplayName() == t.keyName(generation) {
			broken = append(broken, key)
		}
	}
	if err := h.deleteTemporalKeys(ctx, broken, t.uid); err != nil {
		return err
	}
	for _, name := range []string{credentialSecretName(generation), keyAttemptSecretName(generation)} {
		if err := t.k.RemoveDexSecret(ctx, t.project, name); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: the broken Temporal API key of generation %d is being replaced", infra.ErrPending, generation)
}

// retireTemporalKeys revokes every key but the accepted one, the next
// generation's and, while previousInUse, the previous generation's. Once the
// previous generation is out of use its Secrets go too. Revoking does not
// hold back the accepted key.
func (h *CloudAccount) retireTemporalKeys(ctx context.Context, t temporalKeyHolder, generation uint64, current *cloudidentity.ApiKey, keys []*cloudidentity.ApiKey, previousInUse bool) error {
	retired := make([]*cloudidentity.ApiKey, 0, len(keys))
	for _, key := range keys {
		name := key.GetSpec().GetDisplayName()
		if key.Id == current.Id || name == t.keyName(generation+1) || generation > 0 && previousInUse && name == t.keyName(generation-1) {
			continue
		}
		retired = append(retired, key)
	}
	if err := h.deleteTemporalKeys(ctx, retired, t.uid); err != nil && !errors.Is(err, infra.ErrPending) {
		return err
	}
	if generation == 0 || previousInUse {
		return nil
	}
	for _, name := range []string{credentialSecretName(generation - 1), keyAttemptSecretName(generation - 1)} {
		if err := t.k.RemoveDexSecret(ctx, t.project, name); err != nil {
			return err
		}
	}
	return nil
}

// temporalKeys lists the undeleted API keys of the scope's service account.
func (h *CloudAccount) temporalKeys(ctx context.Context, sa *cloudidentity.ServiceAccount) ([]*cloudidentity.ApiKey, error) {
	out, err := h.temporal.CloudService().GetApiKeys(ctx, &cloud.GetApiKeysRequest{OwnerId: sa.Id, OwnerType: cloudidentity.OwnerType_OWNER_TYPE_SERVICE_ACCOUNT, PageSize: 1000})
	if err != nil {
		return nil, err
	}
	if out.NextPageToken != "" {
		return nil, errors.New("project Temporal credential list exceeded budget")
	}
	keys := make([]*cloudidentity.ApiKey, 0, len(out.ApiKeys))
	for _, key := range out.ApiKeys {
		if key.State != resource.ResourceState_RESOURCE_STATE_DELETED {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// deleteTemporalKeys revokes keys and returns infra.ErrPending while any of them
// remains listed.
func (h *CloudAccount) deleteTemporalKeys(ctx context.Context, keys []*cloudidentity.ApiKey, uid string) error {
	for _, key := range keys {
		if key.State == resource.ResourceState_RESOURCE_STATE_DELETING {
			continue
		}
		_, err := h.temporal.CloudService().DeleteApiKey(ctx, &cloud.DeleteApiKeyRequest{KeyId: key.Id, ResourceVersion: key.ResourceVersion, AsyncOperationId: identity.OperationID(uid, "delete-key", key.Id, key.ResourceVersion)})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
	}
	if len(keys) > 0 {
		return fmt.Errorf("%w: %d Temporal API keys are being deleted", infra.ErrPending, len(keys))
	}
	return nil
}

// dexSecret is an immutable Secret of the project Dex component. The Kubernetes
// API takes its values base64-encoded.
func dexSecret(k *kube.Infrastructure, project, name string, values map[string]string) kubeapi.Object {
	data := make(kubeapi.Object, len(values))
	for key, value := range values {
		data[key] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	return kubeapi.ImmutableSecret(k.Metadata(project, name, kube.ComponentDex), data)
}

// createTemporalKey creates a generation's API key and stores its one-time
// token in the generation's credential Secret; the next call accepts the key,
// so it returns infra.ErrPending. The keys lost holds, whose tokens were lost
// before their Secret existed, are revoked first. The attempt is persisted
// before the provider call, so a retry repeats the same asynchronous operation
// even before its key is listed.
func (h *CloudAccount) createTemporalKey(ctx context.Context, t temporalKeyHolder, generation uint64, lost []*cloudidentity.ApiKey) error {
	if err := h.deleteTemporalKeys(ctx, lost, t.uid); err != nil {
		return err
	}
	k, project := t.k, t.project
	attempt, err := k.Ensure(ctx, dexSecret(k, project, keyAttemptSecretName(generation), map[string]string{"operation-id": uuid.NewString(), "expires-at": time.Now().UTC().Add(temporalKeyLifetime).Format(time.RFC3339)}), project, kube.ComponentDex)
	if err != nil {
		return err
	}
	operation, err := kube.SecretValue(attempt, "operation-id")
	if err != nil {
		return err
	}
	expires, err := kube.SecretValue(attempt, "expires-at")
	if err != nil {
		return err
	}
	expiration, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return errors.New("invalid Temporal credential attempt expiry")
	}
	// A finished operation cannot return its one-time token again. The lost
	// key revocation before this call precedes replacing the exact attempt.
	retryAttempt := func() error {
		if removeErr := k.Remove(ctx, k.NamespacePath(project)+"/secrets", kube.ResourceReference(attempt), project, kube.ComponentDex); removeErr != nil {
			return removeErr
		}
		return fmt.Errorf("%w: the Temporal API key attempt is being replaced", infra.ErrPending)
	}
	c := h.temporal.CloudService()
	observed, err := c.GetAsyncOperation(ctx, &cloud.GetAsyncOperationRequest{AsyncOperationId: operation})
	switch {
	case err == nil:
		if state := observed.GetAsyncOperation().GetState(); state == operationpb.AsyncOperation_STATE_PENDING || state == operationpb.AsyncOperation_STATE_IN_PROGRESS {
			return fmt.Errorf("%w: Temporal API key creation is %s", infra.ErrPending, state)
		}
		return retryAttempt()
	case status.Code(err) != codes.NotFound:
		return err
	}
	result, err := c.CreateApiKey(ctx, &cloud.CreateApiKeyRequest{Spec: &cloudidentity.ApiKeySpec{OwnerId: t.account.Id, OwnerType: cloudidentity.OwnerType_OWNER_TYPE_SERVICE_ACCOUNT, DisplayName: t.keyName(generation), Description: k.Names().Product + "/" + t.uid, ExpiryTime: timestamppb.New(expiration)}, AsyncOperationId: operation})
	if err != nil {
		return err
	}
	if result.KeyId == "" || result.Token == "" {
		return retryAttempt()
	}
	_, err = k.Ensure(ctx, dexSecret(k, project, credentialSecretName(generation), map[string]string{"key-id": result.KeyId, "api-key": result.Token}), project, kube.ComponentDex)
	if err != nil {
		return fmt.Errorf("project Temporal credential persistence is unconfirmed: %w", err)
	}
	return fmt.Errorf("%w: Temporal API key %s was created", infra.ErrPending, result.KeyId)
}

// destroyTemporal deletes the scope's API keys, service account and namespace,
// one step per call, and returns infra.ErrPending until all are gone. Without
// the namespace, whose Secret records the service account ID, it finds the
// account by name.
func (h *CloudAccount) destroyTemporal(ctx context.Context, k *kube.Infrastructure, project, uid string, namespacePresent bool) error {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	c := h.temporal.CloudService()
	var sa *cloudidentity.ServiceAccount
	var err error
	if namespacePresent {
		_, sa, err = h.temporalServiceAccount(ctx, k, project, uid, false)
	} else if sa, err = h.listTemporalServiceAccount(ctx, k, project); err == nil && sa != nil {
		err = h.verifyServiceAccountScope(k, project, uid, sa)
	}
	if err != nil {
		return err
	}
	if sa != nil {
		var keys []*cloudidentity.ApiKey
		if keys, err = h.temporalKeys(ctx, sa); err != nil {
			return err
		}
		if err = h.deleteTemporalKeys(ctx, keys, uid); err != nil {
			return err
		}
		if sa.State == resource.ResourceState_RESOURCE_STATE_DELETING {
			return fmt.Errorf("%w: Temporal service account %s is being deleted", infra.ErrPending, sa.Id)
		}
		_, err = c.DeleteServiceAccount(ctx, &cloud.DeleteServiceAccountRequest{ServiceAccountId: sa.Id, ResourceVersion: sa.ResourceVersion, AsyncOperationId: identity.OperationID(uid, "delete-account", sa.Id, sa.ResourceVersion)})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		return fmt.Errorf("%w: Temporal service account %s is being deleted", infra.ErrPending, sa.Id)
	}
	n, err := h.findNamespace(ctx, k, project, uid)
	if err != nil {
		return err
	}
	if n == nil {
		return nil
	}
	if n.State == resource.ResourceState_RESOURCE_STATE_DELETING {
		return fmt.Errorf("%w: Temporal namespace %s is being deleted", infra.ErrPending, n.Namespace)
	}
	_, err = c.DeleteNamespace(ctx, &cloud.DeleteNamespaceRequest{Namespace: n.Namespace, ResourceVersion: n.ResourceVersion, AsyncOperationId: identity.OperationID(uid, "delete-namespace", n.ResourceVersion)})
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	return fmt.Errorf("%w: Temporal namespace %s is being deleted", infra.ErrPending, n.Namespace)
}
