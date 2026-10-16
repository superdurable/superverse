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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sync"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Ensure creates want when it is absent and returns the object only when it is
// the component's own: owner labels, the same deployment generation and, for
// an immutable resource, the same spec digest.
func (k *Infrastructure) Ensure(ctx context.Context, want kubeapi.Object, project string, component Component) (kubeapi.Object, error) {
	found, err := k.API.GetOrCreate(ctx, want)
	if err != nil {
		return nil, err
	}
	if err = k.VerifyOwner(found, project, component); err != nil {
		return nil, err
	}
	expected, actual := kubeapi.Labels(want), kubeapi.Labels(found)
	for _, key := range []string{kubeapi.WorkloadIDLabel, kubeapi.GenerationLabel} {
		if v := kubeapi.String(expected, key); v != "" && v != kubeapi.String(actual, key) {
			return nil, errors.New("resource generation differs from the requested one")
		}
	}
	if digest := specDigest(want); digest != "" && digest != specDigest(found) {
		return nil, errors.New("immutable resource input conflict")
	}
	return found, nil
}

// specDigestAnnotation carries the digest of an immutable resource's inputs.
const specDigestAnnotation = "superdurable.io/spec-digest"

// MarkSpec records the digest of an immutable resource's inputs, so a
// same-name object with other inputs is a conflict instead of a match.
func MarkSpec(o kubeapi.Object) {
	sum := sha256.Sum256(comm.Must(json.Marshal(kubeapi.Object{"spec": o["spec"], "data": o["data"]})))
	kubeapi.Child(o, "metadata")["annotations"] = kubeapi.Object{specDigestAnnotation: hex.EncodeToString(sum[:])}
}

func specDigest(o kubeapi.Object) string {
	return kubeapi.String(kubeapi.Child(kubeapi.Child(o, "metadata"), "annotations"), specDigestAnnotation)
}

// replaceChangedSecret deletes the component's immutable Secret of want's name
// when it holds other inputs than want, which Ensure then creates again under
// the same name. The delete is bound to the UID it read, and a Secret still
// being deleted is infra.ErrPending, so a retried round converges.
func (k *Infrastructure) replaceChangedSecret(ctx context.Context, want kubeapi.Object, project string, component Component) error {
	path := kubeapi.CollectionPath(want) + "/" + kubeapi.String(kubeapi.Child(want, "metadata"), "name")
	current, err := k.GetOwned(ctx, path, project, component)
	if err != nil || current == nil || specDigest(current) == specDigest(want) {
		return err
	}
	if !kubeapi.Deleting(current) {
		if _, _, err = k.API.Call(ctx, http.MethodDelete, path, kubeapi.DeleteOptions(kubeapi.UID(current))); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: the changed %s is being replaced", infra.ErrPending, path)
}

// Remove deletes the accepted object at path/name and nothing else: an absent
// object is done, and a different owner or UID is an ownership conflict.
func (k *Infrastructure) Remove(ctx context.Context, path string, ref infra.ResourceRef, project string, component Component) error {
	o, err := k.GetOwned(ctx, path+"/"+ref.Name, project, component)
	if err != nil || o == nil {
		return err
	}
	if ref.UID == "" || kubeapi.UID(o) != ref.UID {
		return fmt.Errorf("%w: cleanup UID changed", infra.ErrOwnershipConflict)
	}
	options := kubeapi.DeleteOptions(ref.UID)
	options["propagationPolicy"] = "Foreground"
	_, _, err = k.API.Call(ctx, http.MethodDelete, path+"/"+ref.Name, options)
	return err
}

// CurrentNamespace returns the scope's namespace when it is still the accepted,
// active one; an empty uid accepts the current owned namespace.
func (k *Infrastructure) CurrentNamespace(ctx context.Context, project, uid string) (kubeapi.Object, error) {
	o, err := k.GetOwned(ctx, k.NamespacePath(project), project, ComponentInfrastructure)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("project namespace is absent")
	}
	if kubeapi.Deleting(o) || uid != "" && kubeapi.UID(o) != uid {
		return nil, errors.New("project namespace UID changed")
	}
	return o, nil
}

// DeploymentReady reports whether the component's named Deployment has rolled
// out its current generation with one ready replica, and returns the observed
// Deployment. A non-empty uid binds the accepted Deployment.
func (k *Infrastructure) DeploymentReady(ctx context.Context, project, name string, component Component, uid string) (kubeapi.Object, bool, error) {
	o, err := k.GetOwned(ctx, "/apis/apps/v1/namespaces/"+k.ProjectNamespace(project)+"/deployments/"+name, project, component)
	if err != nil || o == nil {
		return nil, false, err
	}
	if uid != "" && kubeapi.UID(o) != uid {
		return nil, false, errors.New("application Deployment UID changed")
	}
	status := kubeapi.Child(o, "status")
	generation, _ := kubeapi.Child(o, "metadata")["generation"].(float64)
	observed, _ := status["observedGeneration"].(float64)
	ready, _ := status["readyReplicas"].(float64)
	updated, _ := status["updatedReplicas"].(float64)
	return o, generation > 0 && observed >= generation && ready == 1 && updated == 1, nil
}

// Service is a ClusterIP Service selecting a component's Pods.
func (k *Infrastructure) Service(project, name string, component Component, selector kubeapi.Object, ports ...kubeapi.Object) kubeapi.Object {
	return kubeapi.Service(k.Metadata(project, name, component), selector, ports...)
}

// InfrastructureDeployment runs one scope component as a single restricted Pod
// that is replaced, never rolled, so it never runs twice against its volume.
func (k *Infrastructure) InfrastructureDeployment(project string, component Component, image string, args []string, port int, env, mounts, volumes []any) kubeapi.Object {
	name := component.String()
	container := kubeapi.Object{"name": name, "image": image, "args": args, "imagePullPolicy": "IfNotPresent", "ports": []any{kubeapi.ContainerPort("", port)}, "env": env, "volumeMounts": mounts, "resources": kubeapi.Resources("100m", "256Mi", "2", "2Gi"), "readinessProbe": kubeapi.Object{"tcpSocket": kubeapi.Object{"port": port}, "periodSeconds": 2, "failureThreshold": 30}, "securityContext": kubeapi.Object{"allowPrivilegeEscalation": false, "capabilities": kubeapi.Object{"drop": []string{"ALL"}}}}
	pod := kubeapi.Object{"serviceAccountName": name, "automountServiceAccountToken": false, "securityContext": kubeapi.Object{"seccompProfile": kubeapi.Object{"type": "RuntimeDefault"}}, "containers": []any{container}, "volumes": volumes}
	return kubeapi.SinglePodDeployment(k.Metadata(project, name, component), k.OwnerLabels(project, component), pod)
}

// ServiceHTTP is the client for HTTP requests to an in-cluster Service. URLs
// keep their cluster DNS name; Dial, when set, only changes where sockets
// connect.
func (k *Infrastructure) ServiceHTTP() *http.Client {
	if k.Dial == nil {
		return http.DefaultClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // net/http always installs a *Transport
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) { return k.Dial(ctx, address) }
	return &http.Client{Transport: transport}
}

// DialPrivate connects to an in-cluster gRPC address. The returned function
// closes the connection, once. The connection stays inside the private
// network the scope owns: no browser endpoint, credential or caller dial
// option is used.
func (k *Infrastructure) DialPrivate(ctx context.Context, address string) (*grpc.ClientConn, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy()}
	if k.Dial != nil {
		options = append(options, grpc.WithContextDialer(k.Dial))
	}
	connection, err := grpc.NewClient("passthrough:///"+address, options...)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	var closeErr error
	return connection, func() error {
		once.Do(func() { closeErr = connection.Close() })
		return closeErr
	}, nil
}

// SecretValue decodes one non-empty value of a Secret's data.
func SecretValue(o kubeapi.Object, key string) (string, error) {
	value, err := base64.StdEncoding.DecodeString(kubeapi.String(kubeapi.Child(o, "data"), key))
	if err != nil || len(value) == 0 {
		return "", errors.New("project credential object is invalid")
	}
	return string(value), nil
}

func CredentialPair(o kubeapi.Object) (access, secret string, err error) {
	if access, err = SecretValue(o, infra.AccessKeyIDVariable); err != nil {
		return "", "", err
	}
	secret, err = SecretValue(o, infra.SecretAccessKeyVariable)
	return access, secret, err
}

// ensureInPlace converges a scope resource whose desired spec changes between
// releases of this server: the project Dex Deployment and Service and the
// component traffic policies. It updates the existing object under its
// resourceVersion instead of replacing it, so an accepted UID survives. A
// conflicting or uncertain write returns to the owning Flow, whose retry reads
// the resource again before writing.
func (k *Infrastructure) ensureInPlace(ctx context.Context, want kubeapi.Object, project string, component Component, acceptedUID string) (kubeapi.Object, error) {
	path := kubeapi.CollectionPath(want) + "/" + kubeapi.String(kubeapi.Child(want, "metadata"), "name")
	current, err := k.GetOwned(ctx, path, project, component)
	if err != nil {
		return nil, err
	}
	if current == nil {
		if acceptedUID != "" {
			return nil, errors.New("accepted in-place resource disappeared")
		}
		return k.Ensure(ctx, want, project, component)
	}
	metadata := kubeapi.Child(current, "metadata")
	if kubeapi.String(metadata, "namespace") != k.ProjectNamespace(project) || kubeapi.Deleting(current) || acceptedUID != "" && kubeapi.UID(current) != acceptedUID {
		return nil, errors.New("in-place resource identity changed")
	}
	if !reflect.DeepEqual(kubeapi.Labels(current), kubeapi.Labels(want)) {
		return nil, errors.New("in-place resource labels changed")
	}
	desiredSpec := kubeapi.Child(want, "spec")
	if ownedSpecMatches(kubeapi.Child(current, "spec"), desiredSpec) {
		return current, nil
	}
	if kubeapi.String(want, "kind") == kubeapi.KindService {
		// The API server allocates the Service's cluster IPs; keep them.
		spec := kubeapi.Child(current, "spec")
		for _, field := range []string{"ports", "selector", "type"} {
			spec[field] = desiredSpec[field]
		}
		desiredSpec = spec
	}
	if kubeapi.String(metadata, "resourceVersion") == "" {
		return nil, errors.New("in-place resource lacks a revision")
	}
	current["spec"] = desiredSpec
	delete(current, "status")
	updated, _, err := k.API.Call(ctx, http.MethodPut, path, current)
	if err != nil {
		var status *kubeapi.StatusError
		if errors.As(err, &status) && status.Status == http.StatusConflict {
			return nil, fmt.Errorf("%w: %s changed during its update: %w", infra.ErrPending, path, err)
		}
		return nil, err
	}
	if kubeapi.UID(updated) != kubeapi.UID(current) {
		return nil, errors.New("in-place resource UID changed during its update")
	}
	return updated, nil
}

// ownedSpecMatches reports whether current holds every field of desired.
// Kubernetes adds defaults, so extra map keys are ignored, but lists must match
// exactly so that a removed port or environment entry is removed. Both sides
// are compared in their JSON form, which normalizes map types and numbers.
func ownedSpecMatches(current, desired kubeapi.Object) bool {
	normalize := func(input kubeapi.Object) any {
		var result any
		comm.MustDo(json.Unmarshal(comm.Must(json.Marshal(input)), &result))
		return result
	}
	actual, wanted := normalize(current), normalize(desired)
	if actual == nil || wanted == nil {
		return false
	}
	var contains func(any, any) bool
	contains = func(actual, wanted any) bool {
		switch value := wanted.(type) {
		case map[string]any:
			object, ok := actual.(map[string]any)
			if !ok {
				return false
			}
			for key, item := range value {
				if !contains(object[key], item) {
					return false
				}
			}
			return true
		case []any:
			list, ok := actual.([]any)
			if !ok || len(list) != len(value) {
				return false
			}
			for index, item := range value {
				if !contains(list[index], item) {
					return false
				}
			}
			return true
		default:
			return reflect.DeepEqual(actual, wanted)
		}
	}
	return contains(actual, wanted)
}
