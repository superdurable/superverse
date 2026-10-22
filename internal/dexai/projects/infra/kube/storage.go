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
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// EnsureStorage converges a project's configuration storage: its namespace,
// the platform server's delegated RoleBinding, the default-deny network policy
// and the storage backend, which is the project's own MinIO with its versioned
// application bucket in Local Kind and the project's S3 bucket when hosted. It
// returns ErrPending while a resource is still becoming ready. The owning Flow
// records the returned references even with an error, so partially
// provisioned resources stay owned for cleanup. A reference the call does not
// reach stays the accepted one: a returned reference without it would read as
// a replacement.
func (k *Infrastructure) EnsureStorage(ctx context.Context, project string, accepted infra.StorageRefs) (refs infra.StorageRefs, err error) {
	namespace, err := k.ensureScopeNamespace(ctx, project, accepted.Boundary.UID)
	if err != nil {
		return accepted, err
	}
	refs = accepted
	refs.Boundary, refs.StorageBucket, refs.BlobPrefix = namespace, k.bucket(project, namespace.UID), infra.ConfigurationPrefix
	delegated, err := k.ensureDelegation(ctx, project)
	if err != nil {
		return refs, err
	}
	// Kubernetes authorizers briefly lag a new RoleBinding. A denial within that
	// window is pending, so the owning Flow polls again; a lasting one fails.
	defer func() {
		if kubeapi.WithinAuthorizerLag(err, delegated, k.ProjectNamespace(project)) {
			err = fmt.Errorf("%w: the delegated RoleBinding is not authorized yet: %w", infra.ErrPending, err)
		}
	}()
	// Every component's traffic is denied until its own policy admits it.
	if _, err = k.Ensure(ctx, k.networkPolicy(project, "default-deny", kubeapi.Object{"podSelector": kubeapi.Object{}, "policyTypes": []string{"Ingress", "Egress"}}), project, ComponentInfrastructure); err != nil {
		return refs, err
	}
	service, err := k.Backend.EnsureStorage(ctx, k, project, namespace.UID, accepted.Storage.UID)
	if service.UID != "" {
		refs.Storage = service
	}
	return refs, err
}

// Complete reports whether every storage identity is recorded; where the
// Backend runs the storage itself, that includes its Service.
func (k *Infrastructure) Complete(r infra.StorageRefs) bool {
	return r.Boundary.UID != "" && r.StorageBucket != "" && r.BlobPrefix != "" && k.Backend.StorageComplete(r)
}

// bucket names the storage bucket of a scope.
func (k *Infrastructure) bucket(project, namespaceUID string) string {
	return k.Backend.Bucket(k, project, namespaceUID)
}

// S3 returns the verified bucket of a scope. Where the bucket name derives
// from the namespace UID, it first verifies the accepted namespace.
func (k *Infrastructure) S3(ctx context.Context, project, namespaceUID string) (*infra.ApplicationObjects, error) {
	if k.Backend.BucketBoundToNamespace() {
		if _, err := k.CurrentNamespace(ctx, project, namespaceUID); err != nil {
			return nil, err
		}
	}
	return k.Backend.Objects(ctx, k, project, namespaceUID)
}

// ensureScopeNamespace returns the project's active namespace. A namespace
// whose UID differs from the accepted one is a replacement and is never
// adopted.
func (k *Infrastructure) ensureScopeNamespace(ctx context.Context, project, acceptedUID string) (infra.ResourceRef, error) {
	namespace := kubeapi.New("v1", kubeapi.KindNamespace, kubeapi.Metadata("", k.ProjectNamespace(project), k.OwnerLabels(project, ComponentInfrastructure)), nil)
	found, err := k.Ensure(ctx, namespace, project, ComponentInfrastructure)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	ref := ResourceReference(found)
	if acceptedUID != "" && acceptedUID != ref.UID {
		return infra.ResourceRef{}, errors.New("project namespace UID changed")
	}
	if kubeapi.Deleting(found) {
		return infra.ResourceRef{}, errors.New("project namespace is terminating")
	}
	return ref, nil
}

// ensureDelegation binds the platform server to the project runtime role in the
// project's namespace and returns when the binding was created.
func (k *Infrastructure) ensureDelegation(ctx context.Context, project string) (time.Time, error) {
	role := k.Names().Product + "-project-runtime"
	server := kubeapi.ServiceAccountSubject(k.Names().PlatformNamespace, "superverse-server")
	binding, err := k.Ensure(ctx, kubeapi.BindClusterRole(kubeapi.KindRoleBinding, k.Metadata(project, role, ComponentInfrastructure), role, server), project, ComponentInfrastructure)
	if err != nil {
		return time.Time{}, err
	}
	created, _ := time.Parse(time.RFC3339, kubeapi.String(kubeapi.Child(binding, "metadata"), "creationTimestamp"))
	return created, nil
}

// EnsureServiceAccount creates the token-less service account of a component
// that runs in the project namespace.
func (k *Infrastructure) EnsureServiceAccount(ctx context.Context, project string, component Component) error {
	_, err := k.Ensure(ctx, kubeapi.ServiceAccount(k.Metadata(project, component.String(), component)), project, component)
	return err
}

// EnsureComponentTraffic converges the policy that admits one component's
// traffic. Component traffic changes between releases of this server, so the
// policies are updated in place.
func (k *Infrastructure) EnsureComponentTraffic(ctx context.Context, project string, component Component) error {
	ingress, egress := k.componentTraffic(project, component)
	policy := k.networkPolicy(project, "allow-"+component.String(), kubeapi.Object{"podSelector": kubeapi.MatchLabels(componentLabels(component)), "policyTypes": []string{"Ingress", "Egress"}, "ingress": ingress, "egress": egress})
	_, err := k.ensureInPlace(ctx, policy, project, ComponentInfrastructure, "")
	return err
}

func (k *Infrastructure) networkPolicy(project, name string, spec kubeapi.Object) kubeapi.Object {
	return kubeapi.New("networking.k8s.io/v1", kubeapi.KindNetworkPolicy, k.Metadata(project, name, ComponentInfrastructure), spec)
}

// componentTraffic is the traffic a project component may receive and send:
// DNS, the fixed ports between components and from the platform server, the
// applications' credential reads from the platform server, and public HTTPS
// outside private ranges. Storage also admits the project's
// Sandbox Pods, whose Preview reads its configuration there, and Temporal the
// platform server, which checks its data plane. Hosted Dex and applications
// also reach the Pod Identity agent, and hosted Dex reaches the private Temporal frontend.
func (k *Infrastructure) componentTraffic(project string, component Component) (ingress, egress []any) {
	names := k.Names()
	platform := kubeapi.Peer(kubeapi.NamespaceNamed(names.PlatformNamespace), nil)
	platformServer := kubeapi.Peer(kubeapi.NamespaceNamed(names.PlatformNamespace), kubeapi.Object{"app": names.Product + "-server"})
	sandboxes := kubeapi.Peer(kubeapi.Object{kubeapi.ProjectLabel: project, "superdurable.io/purpose": "coding-sandbox-sessions"}, kubeapi.Object{kubeapi.SandboxProfileLabel: infra.CodingSandboxProfile})
	dns := kubeapi.AllowTo(kubeapi.Peer(kubeapi.NamespaceNamed("kube-system"), kubeapi.Object{"k8s-app": "kube-dns"}), kubeapi.UDPPort(53), kubeapi.TCPPort(53))
	ingress, egress = []any{}, []any{dns}
	switch component {
	case ComponentApplication:
		// The generation's HTTP admission is installed with the application.
		egress = append(egress, EgressTo(componentPods(ComponentDex), infra.DexPort), EgressTo(componentPods(ComponentStorage), infra.StoragePort),
			EgressTo(platformServer, infra.CredentialReadPort), publicEgress(443, privateRanges))
	case ComponentDex:
		ingress = append(ingress, ingressFrom(componentPods(ComponentApplication), infra.DexPort), ingressFrom(platform, infra.DexPort))
		egress = append(egress, EgressTo(componentPods(ComponentTemporal), infra.TemporalPort), EgressTo(componentPods(ComponentStorage), infra.StoragePort), publicEgress(443, privateRanges))
	case ComponentTemporal:
		ingress = append(ingress, ingressFrom(componentPods(ComponentDex), infra.TemporalPort), ingressFrom(platformServer, infra.TemporalPort))
	case ComponentStorage:
		ingress = append(ingress, ingressFrom(componentPods(ComponentDex), infra.StoragePort), ingressFrom(componentPods(ComponentApplication), infra.StoragePort), ingressFrom(platform, infra.StoragePort), ingressFrom(sandboxes, infra.StoragePort))
	}
	egress = append(egress, k.Backend.ExtraEgress(k, component)...)
	return ingress, egress
}

// privateRanges are the cluster, link-local and loopback networks that public
// egress excludes.
var privateRanges = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8"}

// ingressFrom admits TCP traffic from peer to port, and EgressTo TCP traffic
// to port of peer.
func ingressFrom(peer kubeapi.Object, port int) kubeapi.Object {
	return kubeapi.AllowFrom(peer, kubeapi.TCPPort(port))
}

func EgressTo(peer kubeapi.Object, port int) kubeapi.Object {
	return kubeapi.AllowTo(peer, kubeapi.TCPPort(port))
}

// componentLabels select the Pods of a component.
func componentLabels(component Component) kubeapi.Object {
	return kubeapi.Object{kubeapi.ComponentLabel: component.String()}
}

func componentPods(component Component) kubeapi.Object {
	return kubeapi.Peer(nil, componentLabels(component))
}

func publicEgress(port int, except []string) kubeapi.Object {
	return EgressTo(kubeapi.Object{"ipBlock": kubeapi.Object{"cidr": "0.0.0.0/0", "except": except}}, port)
}
