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
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// ApplyApplication runs the generation's application from its immutable image
// in the namespace that the deployment accepted. The application reads its
// configuration through the SDK; environment holds the grant's storage and
// project references, which may be secret, so they reach the Pod only through
// an immutable generation Secret.
func (l *LiveServing) ApplyApplication(ctx context.Context, s deployments.Snapshot, environment map[string]string) (infra.ResourceRef, error) {
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation)
	if s.Resources.Storage.Boundary.UID == "" || s.Resources.Service.Name != name || s.Resources.Service.UID == "" {
		return infra.ResourceRef{}, fmt.Errorf("the application infrastructure of %s has no admitted namespace and Service identity", name)
	}
	if _, err := l.CurrentNamespace(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID); err != nil {
		return infra.ResourceRef{}, err
	}
	if _, err := l.Ensure(ctx, l.applicationPolicy(s, name), s.ProjectID, infrakube.ComponentApplication); err != nil {
		return infra.ResourceRef{}, err
	}
	secretName := name + "-environment"
	var env []any
	for _, setting := range deployments.ApplicationSettings(s, l.WorkerAddress(s.ProjectID), l.Routes.URL(infra.ScopeLive, s.ProjectID)) {
		env = append(env, kubeapi.EnvVar(setting[0], setting[1]))
	}
	secretData := kubeapi.Object{}
	for _, key := range slices.Sorted(maps.Keys(environment)) {
		value := environment[key]
		if strings.ContainsRune(value, '\x00') || len(value) > 32768 {
			// The value may be a credential; only its name is reported.
			return infra.ResourceRef{}, fmt.Errorf("application environment value %s holds a NUL byte or exceeds 32768 bytes", key)
		}
		secretData[key] = base64.StdEncoding.EncodeToString([]byte(value))
		env = append(env, kubeapi.SecretEnvVar(key, secretName, key))
	}
	secret := kubeapi.ImmutableSecret(l.generationMetadata(s, secretName, infrakube.ComponentApplication), secretData)
	infrakube.MarkSpec(secret)
	if _, err := l.Ensure(ctx, secret, s.ProjectID, infrakube.ComponentApplication); err != nil {
		return infra.ResourceRef{}, err
	}
	deployment := kubeapi.SinglePodDeployment(l.generationMetadata(s, name, infrakube.ComponentApplication), l.generationLabels(s, infrakube.ComponentApplication), l.applicationPod(s, env))
	infrakube.MarkSpec(deployment)
	found, err := l.Ensure(ctx, deployment, s.ProjectID, infrakube.ComponentApplication)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	ref := infrakube.ResourceReference(found)
	ref.Generation = s.Lease.Generation
	return ref, nil
}

// applicationPolicy admits traffic only to the generation's application port,
// from the peer the Backend names.
func (l *LiveServing) applicationPolicy(s deployments.Snapshot, name string) kubeapi.Object {
	ingress := kubeapi.AllowFrom(l.Backend.ApplicationIngressPeer(l.Infrastructure), kubeapi.TCPPort(s.Inputs.ApplicationPort))
	policy := kubeapi.New("networking.k8s.io/v1", kubeapi.KindNetworkPolicy, l.generationMetadata(s, name, infrakube.ComponentApplication), kubeapi.Object{
		"podSelector": kubeapi.MatchLabels(l.generationLabels(s, infrakube.ComponentApplication)), "policyTypes": []string{"Ingress"}, "ingress": []any{ingress},
	})
	infrakube.MarkSpec(policy)
	return policy
}

// applicationPod runs the image as an unprivileged, read-only container that
// cannot obtain a service-account token, in the runtime class of the Backend.
func (l *LiveServing) applicationPod(s deployments.Snapshot, env []any) kubeapi.Object {
	security := kubeapi.Object{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": kubeapi.Object{"drop": []string{"ALL"}}, "runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532}
	container := kubeapi.Object{"name": "application", "image": s.Inputs.ImageReference, "imagePullPolicy": "IfNotPresent", "env": env, "securityContext": security,
		"ports":          []any{kubeapi.ContainerPort("http", s.Inputs.ApplicationPort), kubeapi.ContainerPort("worker", infra.LiveWorkerPort)},
		"volumeMounts":   []any{kubeapi.VolumeMount("tmp", "/tmp")},
		"readinessProbe": kubeapi.Object{"httpGet": kubeapi.Object{"path": s.Inputs.HealthPath, "port": s.Inputs.ApplicationPort}, "periodSeconds": 2, "failureThreshold": 30},
		"resources":      kubeapi.Resources("100m", "128Mi", "2", "1Gi")}
	pod := kubeapi.Object{"serviceAccountName": l.WorkloadServiceAccount(infra.Workload{ProjectID: s.ProjectID, WorkloadID: s.DeploymentID, Generation: s.Lease.Generation}), "automountServiceAccountToken": false,
		"securityContext": kubeapi.Object{"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532, "fsGroup": 65532, "seccompProfile": kubeapi.Object{"type": "RuntimeDefault"}},
		"volumes":         []any{kubeapi.EmptyDirVolume("tmp", "1Gi")}, "containers": []any{container}}
	if class := l.Backend.ApplicationRuntimeClass(); class != "" {
		pod["runtimeClassName"] = class
	}
	return pod
}

// VerifyReadiness reports the generation ready when its published
// definitions, its Deployment of the accepted image and its Service are the
// accepted ones, the application answers its health path and its Worker
// accepts connections. A Deployment
// still rolling out or an unhealthy application is infra.ErrPending with the
// reason.
func (l *LiveServing) VerifyReadiness(ctx context.Context, s deployments.Snapshot) (deployments.Readiness, error) {
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation)
	if _, err := l.CurrentNamespace(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID); err != nil {
		return deployments.Readiness{}, err
	}
	if _, err := deployments.ReadPublication(ctx, l.Infrastructure, s); err != nil {
		return deployments.Readiness{}, err
	}
	deployment, ready, err := l.DeploymentReady(ctx, s.ProjectID, name, infrakube.ComponentApplication, s.Providers.Application.UID)
	if err != nil {
		return deployments.Readiness{}, err
	}
	if deployment == nil {
		return deployments.Readiness{}, fmt.Errorf("application Deployment %s is absent", name)
	}
	if !ready {
		return deployments.Readiness{}, fmt.Errorf("%w: application Deployment %s has no ready replica of its generation yet", infra.ErrPending, name)
	}
	containers := kubeapi.Items(kubeapi.Child(kubeapi.Child(kubeapi.Child(deployment, "spec"), "template"), "spec"), "containers")
	if len(containers) != 1 {
		return deployments.Readiness{}, fmt.Errorf("application Deployment %s has %d containers, not 1", name, len(containers))
	}
	if container, _ := containers[0].(map[string]any); kubeapi.String(container, "image") != s.Inputs.ImageReference {
		return deployments.Readiness{}, fmt.Errorf("application Deployment %s runs %s, not the release image %s", name, kubeapi.String(container, "image"), s.Inputs.ImageReference)
	}
	service, _, err := l.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+l.ProjectNamespace(s.ProjectID)+"/services/"+name, nil)
	if err != nil {
		return deployments.Readiness{}, err
	}
	if uid := infrakube.ResourceReference(service).UID; uid != s.Resources.Service.UID {
		return deployments.Readiness{}, fmt.Errorf("application Service %s has UID %q, not the admitted %s", name, uid, s.Resources.Service.UID)
	}
	if err = probeHealth(ctx, l.ServiceHTTP(), "http://"+kubeapi.ServiceAddress(l.ProjectNamespace(s.ProjectID), name, 8080)+s.Inputs.HealthPath, ""); err != nil {
		return deployments.Readiness{}, fmt.Errorf("%w: application health probe of %s: %w", infra.ErrPending, name, err)
	}
	if err = l.probeWorker(ctx, s); err != nil {
		return deployments.Readiness{}, err
	}
	return deployments.Readiness{Ready: true, ImageDigest: s.Inputs.ImageDigest, BundleDigest: s.Inputs.BundleDigest}, nil
}

// probeHealth requires HTTP 200 from a health path within three seconds,
// without following redirects. A non-empty host overrides the request host.
func probeHealth(ctx context.Context, base *http.Client, target, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	if host != "" {
		request.Host = host
	}
	client := *base
	client.Timeout = 3 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("application probe returned %d", response.StatusCode)
	}
	return nil
}
