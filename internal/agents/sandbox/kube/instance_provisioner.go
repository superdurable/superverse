// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// InstanceProvisioner applies the Kubernetes resources of Sandbox instances and
// reaches their workspace. It queries exact resource identities before every
// effect and owns no lifecycle state. It implements sandbox.InstanceProvider.
type InstanceProvisioner struct {
	// Infrastructure supplies the cluster API, the names of the instance
	// namespaces and the product labels, and the Backend's answers about public
	// routes.
	*infrakube.Infrastructure
	// ProfileName is the coding profile ConfigMap in the platform namespace.
	ProfileName string
	Routes      infra.PublicRoutes
	Workspace   *workspace.Capabilities
	// Connections reaches the Preview Dex of an instance through its
	// Sandbox Service.
	Connections infra.Connections
}

func (k *InstanceProvisioner) namespace(id sandbox.Identity) string {
	return k.SessionBoundary(id.ProjectID)
}

// SessionBoundary is the namespace of the Project's Session.
func (k *InstanceProvisioner) SessionBoundary(projectID string) string {
	return k.Names().SessionNamespace(projectID)
}

// labels mark every object of the instance as the Project's Preview scope;
// verify reads the owner labels back before an object is adopted or deleted.
// The generation label carries the constant generation of every instance.
func (k *InstanceProvisioner) labels(s sandbox.Instance) kubeapi.Object {
	return kubeapi.Object{
		kubeapi.ManagedByLabel:      k.Names().Product,
		kubeapi.ProjectLabel:        s.Identity.ProjectID,
		kubeapi.ScopeLabel:          infra.ScopePreview.String(),
		kubeapi.SandboxProfileLabel: infra.CodingSandboxProfile,
		kubeapi.GenerationLabel:     strconv.FormatUint(s.Generation, 10),
	}
}

func resource(o kubeapi.Object) sandbox.ResourceRef {
	namespace, name, uid := kubeapi.Ref(o)
	return sandbox.ResourceRef{Boundary: namespace, Name: name, UID: uid}
}

// ensure creates desired unless it exists and verifies that the object found
// belongs to the Project's Sandbox.
func (k *InstanceProvisioner) ensure(ctx context.Context, s sandbox.Instance, desired kubeapi.Object) (kubeapi.Object, error) {
	found, err := k.API.GetOrCreate(ctx, desired)
	if err != nil {
		return nil, err
	}
	if err = k.verify(found, s.Identity); err != nil {
		return nil, err
	}
	return found, nil
}

// verify requires the owner labels of the Project's Sandbox on o.
func (k *InstanceProvisioner) verify(o kubeapi.Object, id sandbox.Identity) error {
	l := kubeapi.Labels(o)
	if kubeapi.UID(o) == "" || kubeapi.String(l, kubeapi.ManagedByLabel) != k.Names().Product || kubeapi.String(l, kubeapi.ProjectLabel) != id.ProjectID || kubeapi.String(l, kubeapi.ScopeLabel) != infra.ScopePreview.String() {
		namespace, name, _ := kubeapi.Ref(o)
		return fmt.Errorf("%w: %s %s/%s lacks the owner labels of the Sandbox of Project %s", infra.ErrOwnershipConflict, kubeapi.String(o, "kind"), namespace, name, id.ProjectID)
	}
	return nil
}

// inject places a profile object in the instance namespace and labels it. The
// Sandbox and its Pod template also carry the instance's WorkspaceRequestID,
// which outlives neither, so a Pod tells which instance it serves.
func (k *InstanceProvisioner) inject(o kubeapi.Object, s sandbox.Instance) {
	metadata := kubeapi.EnsureChild(o, "metadata")
	if kubeapi.String(o, "kind") == kubeapi.KindNamespace {
		metadata["name"] = k.namespace(s.Identity)
	} else {
		metadata["namespace"] = k.namespace(s.Identity)
	}
	labels := kubeapi.EnsureChild(metadata, "labels")
	for key, value := range k.labels(s) {
		labels[key] = value
	}
	if kubeapi.String(o, "kind") == kubeapi.KindSandbox {
		labels[sandbox.ImageContractLabel] = strconv.Itoa(sandbox.ImageContractRevision)
		labels[kubeapi.WorkspaceRequestLabel] = s.WorkspaceRequestID
		pod := kubeapi.EnsureChild(kubeapi.EnsureChild(o, "spec"), "podTemplate")
		podLabels := kubeapi.EnsureChild(kubeapi.EnsureChild(pod, "metadata"), "labels")
		for key, value := range k.labels(s) {
			podLabels[key] = value
		}
		podLabels[kubeapi.WorkspaceRequestLabel] = s.WorkspaceRequestID
	}
}

// profileKinds are the resource kinds a coding profile may declare.
var profileKinds = []string{kubeapi.KindNamespace, kubeapi.KindServiceAccount, kubeapi.KindRoleBinding, kubeapi.KindResourceQuota, kubeapi.KindLimitRange,
	kubeapi.KindNetworkPolicy, kubeapi.KindPersistentVolumeClaim, kubeapi.KindSandbox}

// profile reads the immutable coding profile and verifies its contract
// revision and the digests of its namespace baseline.
func (k *InstanceProvisioner) profile(ctx context.Context) (map[string]string, error) {
	o, _, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+k.Names().PlatformNamespace+"/configmaps/"+k.ProfileName, nil)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("coding profile %s/%s is missing", k.Names().PlatformNamespace, k.ProfileName)
	}
	if immutable, _ := o["immutable"].(bool); !immutable {
		return nil, fmt.Errorf("coding profile %s/%s must be immutable", k.Names().PlatformNamespace, k.ProfileName)
	}
	data := map[string]string{}
	for key, value := range kubeapi.Child(o, "data") {
		if text, ok := value.(string); ok {
			data[key] = text
		}
	}
	return data, sandbox.VerifyCodingProfile(k.ProfileName, data)
}

// provisionKeys are the coding profile objects of an instance, in apply order.
var provisionKeys = []string{
	"baseline-namespace.yaml", "baseline-session-manager-role-binding.yaml", "baseline-service-account.yaml",
	"baseline-resource-quota.yaml", "baseline-limit-range.yaml", "baseline-network-policy-default-deny.yaml",
	"baseline-network-policy-superverse-v2-ingress.yaml", "baseline-network-policy-sandbox-egress.yaml",
	"pvc.yaml", "sandbox.yaml",
}

// Provision applies the coding profile to the instance and returns its
// resources; ready reports that the Sandbox is Ready and serves its Service.
// A recorded UID that no longer matches is a failure, never adopted. The
// Sandbox is created only once the workspace volume is of this profile and no
// other Pod mounts it: ErrProfileMismatch and ErrVolumeInUse refuse it, and a
// terminating Pod of an earlier instance is waited for.
func (k *InstanceProvisioner) Provision(ctx context.Context, s sandbox.Instance) (result sandbox.Resources, ready bool, resultErr error) {
	r, id := s.Resources, s.Identity
	data, err := k.profile(ctx)
	if err != nil {
		return r, false, err
	}
	var delegationCreated time.Time
	defer func() {
		// The authorizer may briefly lag the exact RoleBinding create response.
		// The Step's next polling round asks again without losing resources.
		// Older, malformed, or unrelated denials remain conclusive failures.
		if kubeapi.WithinAuthorizerLag(resultErr, delegationCreated, k.namespace(id)) {
			ready, resultErr = false, nil
		}
	}()
	var actual kubeapi.Object
	for _, key := range provisionKeys {
		var desired kubeapi.Object
		if err = yaml.Unmarshal([]byte(data[key]), &desired); err != nil {
			return r, false, fmt.Errorf("decode coding profile entry %s: %w", key, err)
		}
		if desired == nil {
			return r, false, fmt.Errorf("coding profile %s has no %s", k.ProfileName, key)
		}
		if !slices.Contains(profileKinds, kubeapi.String(desired, "kind")) {
			return r, false, fmt.Errorf("coding profile entry %s declares unsupported kind %q", key, kubeapi.String(desired, "kind"))
		}
		k.inject(desired, s)
		if actual, err = k.ensure(ctx, s, desired); err != nil {
			return r, false, err
		}
		switch key {
		case "baseline-namespace.yaml":
			if r.Boundary, err = adopt(r.Boundary, resource(actual), "namespace"); err != nil {
				return r, false, err
			}
		case "baseline-session-manager-role-binding.yaml":
			delegationCreated, _ = time.Parse(time.RFC3339, kubeapi.String(kubeapi.Child(actual, "metadata"), "creationTimestamp"))
		case "baseline-service-account.yaml":
			if r.Identity, err = adopt(r.Identity, resource(actual), "service account"); err != nil {
				return r, false, err
			}
		case "pvc.yaml":
			if r.Volume, err = adopt(r.Volume, resource(actual), "workspace"); err != nil {
				return r, false, err
			}
			if r.Profile = kubeapi.String(kubeapi.Labels(actual), kubeapi.SandboxProfileLabel); r.Profile != infra.CodingSandboxProfile {
				return r, false, fmt.Errorf("%w: volume %s is %q", sandbox.ErrProfileMismatch, r.Volume.Name, r.Profile)
			}
			var free bool
			if free, err = k.volumeFree(ctx, s, r.Volume.Name); err != nil || !free {
				return r, false, err
			}
		case "sandbox.yaml":
			if r.Instance, err = adopt(r.Instance, resource(actual), "Sandbox"); err != nil {
				return r, false, err
			}
			containers := kubeapi.Items(kubeapi.Child(kubeapi.Child(kubeapi.Child(actual, "spec"), "podTemplate"), "spec"), "containers")
			if len(containers) > 0 {
				if container, ok := containers[0].(map[string]any); ok {
					r.ImageDigest = kubeapi.String(container, "image")
				}
			}
		}
	}
	ready = kubeapi.ObservedReady(actual)
	service, err := k.sandboxService(actual, id)
	if err != nil || service == "" {
		return r, false, err
	}
	o, code, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+k.namespace(id)+"/services/"+service, nil)
	if err != nil || code == http.StatusNotFound {
		return r, false, err
	}
	if r.ServiceUID != "" && r.ServiceUID != kubeapi.UID(o) {
		return r, false, fmt.Errorf("%w: Sandbox Service UID changed from %s to %s", infra.ErrOwnershipConflict, r.ServiceUID, kubeapi.UID(o))
	}
	r.ServiceName, r.ServiceUID = service, kubeapi.UID(o)
	return r, ready, nil
}

// volumeFree reports whether no Pod but the instance's own mounts the
// workspace volume. Two Pods on one node could both mount a ReadWriteOnce
// volume, and the Preview engine's state must have one writer. A Pod of an
// earlier instance that terminates is not free yet; one that nobody deletes is
// ErrVolumeInUse, because the earlier instance's reclamation already waited for
// its Pods to go.
func (k *InstanceProvisioner) volumeFree(ctx context.Context, s sandbox.Instance, volume string) (bool, error) {
	pods, _, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+k.namespace(s.Identity)+"/pods", nil)
	if err != nil {
		return false, err
	}
	free := true
	for _, item := range kubeapi.Items(pods, "items") {
		pod, _ := item.(map[string]any)
		if !mounts(pod, volume) || kubeapi.String(kubeapi.Labels(pod), kubeapi.WorkspaceRequestLabel) == s.WorkspaceRequestID {
			continue
		}
		if !kubeapi.Deleting(pod) {
			_, name, _ := kubeapi.Ref(pod)
			return false, fmt.Errorf("%w: Pod %s mounts volume %s", sandbox.ErrVolumeInUse, name, volume)
		}
		free = false
	}
	return free, nil
}

// mounts reports whether pod mounts the persistent volume claim volume.
func mounts(pod kubeapi.Object, volume string) bool {
	for _, item := range kubeapi.Items(kubeapi.Child(pod, "spec"), "volumes") {
		mounted, _ := item.(map[string]any)
		if kubeapi.String(kubeapi.Child(mounted, "persistentVolumeClaim"), "claimName") == volume {
			return true
		}
	}
	return false
}

// sandboxService is the Service the controller published for sandbox, or ""
// until it does, possibly before the Sandbox is Ready. Its FQDN names a
// Service of the instance namespace.
func (k *InstanceProvisioner) sandboxService(sandbox kubeapi.Object, id sandbox.Identity) (string, error) {
	fqdn := kubeapi.String(kubeapi.Child(sandbox, "status"), "serviceFQDN")
	service, published := strings.CutSuffix(fqdn, kubeapi.ServiceHost(k.namespace(id), ""))
	if !published {
		return "", nil
	}
	if service == "" || strings.Contains(service, ".") {
		return "", fmt.Errorf("invalid Sandbox Service FQDN %q", fqdn)
	}
	return service, nil
}

// adopt accepts found unless a different object was recorded before.
func adopt(recorded, found sandbox.ResourceRef, kind string) (sandbox.ResourceRef, error) {
	if recorded.UID != "" && recorded.UID != found.UID {
		return recorded, fmt.Errorf("%w: %s UID changed from %s to %s", infra.ErrOwnershipConflict, kind, recorded.UID, found.UID)
	}
	return found, nil
}

// DiscoverResources completes the references reclamation needs from the
// deterministic names of the instance's namespace, workspace volume and
// Sandbox: a cleanup instance receives no Sandbox reference, and provisioning
// whose attempts failed after creating objects recorded none of them. Every
// object found must carry the Sandbox's owner labels and any recorded UID; an
// absent object keeps the zero reference.
func (k *InstanceProvisioner) DiscoverResources(ctx context.Context, s sandbox.Instance) (sandbox.Resources, error) {
	r, namespace := s.Resources, k.namespace(s.Identity)
	found, err := k.discover(ctx, "/api/v1/namespaces/"+namespace, s.Identity)
	if err != nil || found == nil {
		return r, err
	}
	if r.Boundary, err = adopt(r.Boundary, resource(found), "namespace"); err != nil || kubeapi.Deleting(found) {
		// A terminating namespace takes its objects with it, and first the
		// RoleBinding that reads them.
		return r, err
	}
	if r.Volume.UID != "" && r.Instance.UID != "" {
		return r, nil
	}
	data, err := k.profile(ctx)
	if err != nil {
		return r, err
	}
	if r.Volume.UID == "" {
		volume, err := k.discoverProfileObject(ctx, s.Identity, data, "pvc.yaml", "/api/v1/namespaces/"+namespace+"/persistentvolumeclaims/")
		if err != nil {
			return r, err
		}
		if volume != nil {
			r.Volume = resource(volume)
		}
	}
	if r.Instance.UID == "" {
		sandbox, err := k.discoverProfileObject(ctx, s.Identity, data, "sandbox.yaml", "/apis/agents.x-k8s.io/v1beta1/namespaces/"+namespace+"/sandboxes/")
		if err != nil || sandbox == nil {
			return r, err
		}
		r.Instance = resource(sandbox)
		if r.ServiceName, err = k.sandboxService(sandbox, s.Identity); err != nil {
			return r, err
		}
	}
	return r, nil
}

// discoverProfileObject reads the object the coding profile declares under
// key, by its declared name below collection.
func (k *InstanceProvisioner) discoverProfileObject(ctx context.Context, id sandbox.Identity, data map[string]string, key, collection string) (kubeapi.Object, error) {
	var declared kubeapi.Object
	if err := yaml.Unmarshal([]byte(data[key]), &declared); err != nil {
		return nil, fmt.Errorf("decode coding profile entry %s: %w", key, err)
	}
	name := kubeapi.String(kubeapi.Child(declared, "metadata"), "name")
	if name == "" {
		return nil, fmt.Errorf("coding profile entry %s names no object", key)
	}
	return k.discover(ctx, collection+name, id)
}

// discover reads the object at path and verifies the Sandbox's owner labels on
// it. Absence is nil.
func (k *InstanceProvisioner) discover(ctx context.Context, path string, id sandbox.Identity) (kubeapi.Object, error) {
	o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil || code == http.StatusNotFound {
		return nil, err
	}
	if err = k.verify(o, id); err != nil {
		return nil, err
	}
	return o, nil
}
