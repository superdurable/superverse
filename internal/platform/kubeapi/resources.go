// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapi

import "maps"

// The builders below write the resources that the providers create, and their
// parts, in the form the API serves them, so the accessors read back what was
// built. Each writes exactly the fields it names; a caller adds any other.

// New is an object of apiVersion and kind with metadata and, unless it is nil,
// spec.
func New(apiVersion, kind string, metadata, spec Object) Object {
	object := typed(apiVersion, kind)
	object["metadata"] = metadata
	if spec != nil {
		object["spec"] = spec
	}
	return object
}

// Metadata names an object of namespace, empty for a cluster object, and
// labels it unless labels is nil.
func Metadata(namespace, name string, labels Object) Object {
	metadata := named(name, Object{})
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	if labels != nil {
		metadata["labels"] = labels
	}
	return metadata
}

// MatchLabels selects the objects that carry every one of labels.
func MatchLabels(labels Object) Object { return Object{fieldMatchLabels: labels} }

// DeleteOptions deletes exactly the object whose UID is uid.
func DeleteOptions(uid string) Object {
	options := typed("v1", "DeleteOptions")
	options["preconditions"] = Object{"uid": uid}
	return options
}

// ServiceAccount is a service account whose Pods get no API token.
func ServiceAccount(metadata Object) Object {
	account := New("v1", KindServiceAccount, metadata, nil)
	account["automountServiceAccountToken"] = false
	return account
}

// ImmutableSecret is a Secret that holds data, base64-encoded, and never
// changes.
func ImmutableSecret(metadata, data Object) Object {
	secret := New("v1", kindSecret, metadata, nil)
	secret["immutable"], secret["data"] = true, data
	return secret
}

// PersistentVolumeClaim claims one volume of size from storageClass, which a
// single node mounts read-write.
func PersistentVolumeClaim(metadata Object, storageClass, size string) Object {
	return New("v1", KindPersistentVolumeClaim, metadata, Object{
		"accessModes": []string{"ReadWriteOnce"}, "storageClassName": storageClass, "resources": Object{"requests": Object{"storage": size}},
	})
}

// rbacGroup is the API group of roles and their bindings, and clusterRoleKind
// the kind of the role that a binding grants. No provider creates a ClusterRole
// or a cluster-wide binding, so neither is a managed kind.
const (
	rbacGroup       = "rbac.authorization.k8s.io"
	clusterRoleKind = "ClusterRole"
)

// ClusterRoleForTest grants rules across the cluster. Tests create one to probe what a
// ServiceAccount may do.
func ClusterRoleForTest(name string, rules ...Object) Object {
	role := New(rbacGroup+"/v1", clusterRoleKind, Metadata("", name, nil), nil)
	role["rules"] = list(rules)
	return role
}

// BindClusterRole grants the ClusterRole named role to subjects: in the
// namespace of metadata as a binding of KindRoleBinding, or, in a test, across
// the cluster as a ClusterRoleBinding.
func BindClusterRole(kind string, metadata Object, role string, subjects ...Object) Object {
	binding := New(rbacGroup+"/v1", kind, metadata, nil)
	roleRef := ref(clusterRoleKind, role)
	roleRef["apiGroup"] = rbacGroup
	binding["roleRef"], binding["subjects"] = roleRef, list(subjects)
	return binding
}

// ServiceAccountSubject is the service account name of namespace as the
// subject of a binding.
func ServiceAccountSubject(namespace, name string) Object {
	subject := ref(KindServiceAccount, name)
	subject["namespace"] = namespace
	return subject
}

// Service is a ClusterIP Service that routes ports to the Pods that selector
// selects.
func Service(metadata, selector Object, ports ...Object) Object {
	return New("v1", KindService, metadata, Object{"type": "ClusterIP", fieldSelector: selector, "ports": list(ports)})
}

// ServicePort routes port of a Service, named name, to targetPort of its Pods.
func ServicePort(name string, port, targetPort int) Object {
	return named(name, Object{portField: port, "targetPort": targetPort})
}

// SinglePodDeployment runs one Pod of pod, labeled labels, and replaces it
// instead of rolling it, so two of them never run at once.
func SinglePodDeployment(metadata, labels, pod Object) Object {
	return New("apps/v1", kindDeployment, metadata, Object{
		"replicas": 1, "strategy": Object{"type": "Recreate"}, fieldSelector: MatchLabels(labels),
		"template": Object{"metadata": Object{"labels": maps.Clone(labels)}, "spec": pod},
	})
}

// ContainerPort declares port of a container, named name unless name is
// empty.
func ContainerPort(name string, port int) Object {
	declared := Object{"containerPort": port}
	if name != "" {
		declared = named(name, declared)
	}
	return declared
}

// Resources requests cpu and memory for a container and limits it to cpuLimit
// and memoryLimit.
func Resources(cpu, memory, cpuLimit, memoryLimit string) Object {
	return Object{"requests": Object{"cpu": cpu, "memory": memory}, "limits": Object{"cpu": cpuLimit, "memory": memoryLimit}}
}

// EnvVar sets the environment variable name to value.
func EnvVar(name, value string) Object { return named(name, Object{"value": value}) }

// SecretEnvVar sets the environment variable name to the value of key in the
// Secret named secret.
func SecretEnvVar(name, secret, key string) Object {
	return named(name, Object{"valueFrom": Object{"secretKeyRef": named(secret, Object{"key": key})}})
}

// ClaimVolume is the Pod volume name of the PersistentVolumeClaim named claim.
func ClaimVolume(name, claim string) Object {
	return named(name, Object{"persistentVolumeClaim": Object{"claimName": claim}})
}

// SecretVolume is the Pod volume name of the Secret named secret.
func SecretVolume(name, secret string) Object {
	return named(name, Object{"secret": Object{"secretName": secret}})
}

// EmptyDirVolume is the Pod volume name of an empty directory, of at most
// sizeLimit unless it is empty.
func EmptyDirVolume(name, sizeLimit string) Object {
	directory := Object{}
	if sizeLimit != "" {
		directory["sizeLimit"] = sizeLimit
	}
	return named(name, Object{"emptyDir": directory})
}

// VolumeMount mounts the Pod volume name at path.
func VolumeMount(name, path string) Object { return named(name, Object{"mountPath": path}) }

// NamespaceNamed are the labels of the namespace named name: the label that
// Kubernetes gives every namespace.
func NamespaceNamed(name string) Object { return Object{"kubernetes.io/metadata.name": name} }

// NamespacesPeer selects every Pod of the namespaces named names, for a
// NetworkPolicy rule.
func NamespacesPeer(names ...string) Object {
	return Object{"namespaceSelector": Object{"matchExpressions": []any{Object{"key": "kubernetes.io/metadata.name", "operator": "In", "values": names}}}}
}

// Peer selects the Pods that carry podLabels in the namespaces that carry
// namespaceLabels, for a NetworkPolicy rule. Without namespaceLabels it selects
// Pods of the policy's own namespace, and without podLabels every Pod of the
// selected namespaces.
func Peer(namespaceLabels, podLabels Object) Object {
	peer := Object{}
	if namespaceLabels != nil {
		peer["namespaceSelector"] = MatchLabels(namespaceLabels)
	}
	if podLabels != nil {
		peer["podSelector"] = MatchLabels(podLabels)
	}
	return peer
}

// AllowFrom is an ingress rule of a NetworkPolicy that admits traffic on ports
// from peer, or from anywhere when peer is nil.
func AllowFrom(peer Object, ports ...Object) Object { return policyRule("from", peer, ports) }

// AllowTo is an egress rule of a NetworkPolicy that admits traffic on ports to
// peer.
func AllowTo(peer Object, ports ...Object) Object { return policyRule("to", peer, ports) }

func policyRule(direction string, peer Object, ports []Object) Object {
	rule := Object{"ports": list(ports)}
	if peer != nil {
		rule[direction] = []any{peer}
	}
	return rule
}

// TCPPort and UDPPort are a port of a NetworkPolicy rule.
func TCPPort(port int) Object { return Object{"protocol": "TCP", portField: port} }

func UDPPort(port int) Object { return Object{"protocol": "UDP", portField: port} }

// GatewayAPIVersion is the Gateway API group and version of the routes and
// their parent Gateway.
const GatewayAPIVersion = "gateway.networking.k8s.io/v1"

// GatewayParent refers to the Gateway named name in namespace.
func GatewayParent(namespace, name string) Object {
	return Object{"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": namespace, "name": name}
}

// HTTPRoute routes every request for host that reaches the Gateway parent to
// port of the Service named service. Its request timeout is zero, which
// disables it: the idle timeouts of the load balancer and the proxy bound a
// request, as they bound a stream or a cold start.
func HTTPRoute(metadata, parent Object, host, service string, port int) Object {
	backend := named(service, Object{portField: port})
	return New(GatewayAPIVersion, kindHTTPRoute, metadata, Object{
		"parentRefs": []any{parent}, "hostnames": []any{host},
		"rules": []any{Object{"backendRefs": []any{backend}, "timeouts": Object{"request": "0s"}}},
	})
}

// portField holds the port number of a Service port, of a NetworkPolicy port
// and of a route's backend.
const portField = "port"

// typed is the type of an object or of a request body.
func typed(apiVersion, kind string) Object { return Object{"apiVersion": apiVersion, "kind": kind} }

// named gives fields the name that identifies them: in a list that the API
// merges by name, such as the containers, ports or volumes of a Pod, or in a
// reference to an object.
func named(name string, fields Object) Object {
	fields["name"] = name
	return fields
}

// ref refers to the object of kind named name.
func ref(kind, name string) Object { return named(name, Object{"kind": kind}) }

// list is objects as the list that a decoded JSON array is.
func list(objects []Object) []any {
	items := make([]any, len(objects))
	for i, o := range objects {
		items[i] = o
	}
	return items
}
