// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapi

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// Object is a Kubernetes resource, or any JSON object nested in one, as the API
// serves it. Accessors are read-only: they never add a missing key, so reading
// an API response cannot change it. Code that builds a resource writes through
// EnsureChild.
type Object = map[string]any

// Child returns the object at key, or nil when it is absent. A nil Object reads
// as empty.
func Child(o Object, key string) Object {
	v, _ := o[key].(map[string]any)
	return v
}

// EnsureChild returns the object at key, adding an empty one when it is absent.
func EnsureChild(o Object, key string) Object {
	if v, ok := o[key].(map[string]any); ok {
		return v
	}
	v := Object{}
	o[key] = v
	return v
}

func String(o Object, key string) string { v, _ := o[key].(string); return v }

func Items(o Object, key string) []any { v, _ := o[key].([]any); return v }

// Ref returns the identity in the object's metadata.
func Ref(o Object) (namespace, name, uid string) {
	m := Child(o, "metadata")
	return String(m, "namespace"), String(m, "name"), String(m, "uid")
}

func UID(o Object) string { return String(Child(o, "metadata"), "uid") }

func Labels(o Object) Object { return Child(Child(o, "metadata"), "labels") }

// Deleting reports whether the object has been marked for deletion.
func Deleting(o Object) bool { return Child(o, "metadata")["deletionTimestamp"] != nil }

// ControlledBy reports whether the object of kind with uid is the controller
// of o.
func ControlledBy(o Object, kind, uid string) bool {
	for _, item := range Items(Child(o, "metadata"), "ownerReferences") {
		reference, _ := item.(map[string]any)
		if String(reference, "kind") == kind && String(reference, "uid") == uid && reference["controller"] == true {
			return true
		}
	}
	return false
}

// Matches reports whether found, as the API serves it, is the object desired
// describes: the same labels and annotations, and every field of desired's
// spec with the same value. A list matches element by element and must be as
// long as desired's; a label selector (selector, matchLabels) must equal
// desired's, because a key it gained selects other Pods. Fields that only
// found has, such as the ones the API server defaults, are ignored, so a
// desired field that a newer spec removed goes unnoticed.
func Matches(found, desired Object) bool {
	// desired holds Go numbers, which the API's JSON decodes as float64.
	var normal Object
	comm.MustDo(json.Unmarshal(comm.Must(json.Marshal(desired)), &normal))
	foundMetadata, desiredMetadata := Child(found, "metadata"), Child(normal, "metadata")
	return sameMap(Child(foundMetadata, "labels"), Child(desiredMetadata, "labels")) &&
		sameMap(Child(foundMetadata, "annotations"), Child(desiredMetadata, "annotations")) &&
		covers(found["spec"], normal["spec"], "spec")
}

// covers reports whether found holds every field of desired, the value at key.
func covers(found, desired any, key string) bool {
	switch want := desired.(type) {
	case map[string]any:
		have, isMap := found.(map[string]any)
		if !isMap {
			return false
		}
		if key == fieldSelector || key == fieldMatchLabels {
			return sameMap(have, want)
		}
		for field, value := range want {
			if !covers(have[field], value, field) {
				return false
			}
		}
		return true
	case []any:
		have, isList := found.([]any)
		if !isList || len(have) != len(want) {
			return false
		}
		for i := range want {
			if !covers(have[i], want[i], key) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(found, desired)
	}
}

// The label selector fields of a spec, which select Pods by equality.
const (
	fieldSelector    = "selector"
	fieldMatchLabels = "matchLabels"
)

// sameMap compares two maps of strings, an absent one reading as empty.
func sameMap(a, b map[string]any) bool { return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) }

// ObservedReady reports whether the controller of o observed its current
// generation as Ready.
func ObservedReady(o Object) bool {
	for _, item := range Items(Child(o, "status"), "conditions") {
		condition, _ := item.(map[string]any)
		if String(condition, "type") == "Ready" && String(condition, "status") == "True" && condition["observedGeneration"] == Child(o, "metadata")["generation"] {
			return true
		}
	}
	return false
}

// ServiceHost is the cluster DNS name of a Service.
func ServiceHost(namespace, service string) string {
	return service + "." + namespace + ".svc.cluster.local"
}

// ServiceAddress is the cluster DNS host and port of a Service port.
func ServiceAddress(namespace, service string, port int) string {
	return net.JoinHostPort(ServiceHost(namespace, service), strconv.Itoa(port))
}

// The kinds of the objects the providers manage. The values are the API's own
// spellings, which the objects carry as strings.
const (
	KindNamespace             = "Namespace"
	KindServiceAccount        = "ServiceAccount"
	KindRoleBinding           = "RoleBinding"
	KindResourceQuota         = "ResourceQuota"
	KindLimitRange            = "LimitRange"
	KindNetworkPolicy         = "NetworkPolicy"
	kindSecret                = "Secret"
	kindConfigMap             = "ConfigMap"
	KindPersistentVolumeClaim = "PersistentVolumeClaim"
	KindService               = "Service"
	kindHTTPRoute             = "HTTPRoute"
	kindDeployment            = "Deployment"
	KindSandbox               = "Sandbox"
)

// collections names the API collection of every kind the providers manage.
var collections = map[string]string{
	KindNamespace: "namespaces", KindServiceAccount: "serviceaccounts", KindRoleBinding: "rolebindings",
	KindResourceQuota: "resourcequotas", KindLimitRange: "limitranges", KindNetworkPolicy: "networkpolicies",
	kindSecret: "secrets", kindConfigMap: "configmaps", KindPersistentVolumeClaim: "persistentvolumeclaims",
	KindService: "services", kindHTTPRoute: "httproutes", kindDeployment: "deployments", KindSandbox: "sandboxes",
}

// CollectionPath is the API path of the collection that holds o. Providers build
// o from a kind they manage, so another kind, or a namespaced object without a
// namespace, is a programming error. A provider that reads kinds from data
// admits them before building the path.
func CollectionPath(o Object) string {
	kind, version := String(o, "kind"), String(o, "apiVersion")
	plural, ok := collections[kind]
	if !ok {
		panic(fmt.Sprintf("kubeapi: unmanaged kind %q", kind))
	}
	path := "/api/" + version
	if strings.Contains(version, "/") {
		path = "/apis/" + version
	}
	if kind != KindNamespace {
		namespace := String(Child(o, "metadata"), "namespace")
		if namespace == "" {
			panic("kubeapi: " + kind + " has no namespace")
		}
		path += "/namespaces/" + namespace
	}
	return path + "/" + plural
}
