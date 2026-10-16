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
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
)

// The project Gateway's authority on the real Local Kind cluster with the
// deployed chart. Only the Envoy Gateway controller reads Secrets cluster-wide,
// and it writes none outside its own namespace. The proxies' service account
// mounts no Kubernetes API token and has no permission at all. The gateway
// namespace holds only the controller and its proxies, behind ClusterIP
// Services. It needs SUPERVERSE_TEST_KIND_KUBECONFIG and the Gateway
// "projects" programmed.
func TestKindProjectGatewayAuthority(t *testing.T) {
	cluster := kubeapitest.KindPlatform(t)
	namespace := (&Infrastructure{Environment: deploymentnames.EnvironmentTest}).GatewayNamespace()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	kubectl := func(arguments ...string) (string, error) {
		command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", cluster.KubeconfigForTest, "--context", "kind-superverse-v2"}, arguments...)...) //nolint:gosec // kubectl with the test's own arguments
		output, err := command.CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}
	// can reports kubectl auth can-i, whose exit status is 1 for "no".
	can := func(user string, arguments ...string) bool {
		t.Helper()
		answer, _ := kubectl(append([]string{"auth", "can-i", "--as", user}, arguments...)...)
		if answer != "yes" && answer != "no" {
			t.Fatalf("kubectl auth can-i %v as %s: %s", arguments, user, answer)
		}
		return answer == "yes"
	}
	read := func(arguments ...string) kubeapi.Object {
		t.Helper()
		output, err := kubectl(append([]string{"get", "-o", "json"}, arguments...)...)
		if err != nil {
			t.Fatalf("kubectl get %v: %v: %s", arguments, err, output)
		}
		var object kubeapi.Object
		if err = json.Unmarshal([]byte(output), &object); err != nil {
			t.Fatal(err)
		}
		return object
	}

	controller := "system:serviceaccount:" + namespace + ":envoy-gateway"
	platform := (&Infrastructure{Environment: deploymentnames.EnvironmentTest}).Names().PlatformNamespace
	if !can(controller, "list", "secrets", "--all-namespaces") {
		t.Fatal("the controller cannot list Secrets: the route translation would not start")
	}
	for _, write := range [][]string{{"create", "secrets", "-n", platform}, {"update", "secrets", "-n", platform}, {"delete", "secrets", "-n", platform}, {"create", "pods", "-n", platform}, {"create", "pods/exec", "-n", platform}} {
		if can(controller, write...) {
			t.Fatalf("the controller may %v", write)
		}
	}

	pods := kubeapi.Items(read("pods", "-n", namespace), "items")
	if len(pods) == 0 {
		t.Fatalf("no Pod runs in %s", namespace)
	}
	proxies := 0
	for _, item := range pods {
		pod, _ := item.(map[string]any)
		labels := kubeapi.Labels(pod)
		spec := kubeapi.Child(pod, "spec")
		switch {
		case kubeapi.String(labels, "control-plane") == "envoy-gateway":
		case kubeapi.String(labels, "app.kubernetes.io/name") == "envoy" && kubeapi.String(labels, "app.kubernetes.io/managed-by") == "envoy-gateway":
			proxies++
			account := "system:serviceaccount:" + namespace + ":" + kubeapi.String(spec, "serviceAccountName")
			if automount, isSet := spec["automountServiceAccountToken"].(bool); !isSet || automount {
				t.Fatalf("the proxy %s mounts its Kubernetes API token", kubeapi.String(kubeapi.Child(pod, "metadata"), "name"))
			}
			for _, volume := range kubeapi.Items(spec, "volumes") {
				for _, source := range kubeapi.Items(kubeapi.Child(volume.(map[string]any), "projected"), "sources") {
					token := kubeapi.Child(source.(map[string]any), "serviceAccountToken")
					if token != nil && (kubeapi.String(token, "audience") == "" || strings.Contains(kubeapi.String(token, "audience"), "kubernetes.default")) {
						t.Fatalf("the proxy's projected token %v is valid for the Kubernetes API", token)
					}
				}
			}
			for _, access := range [][]string{{"get", "secrets", "--all-namespaces"}, {"get", "secrets", "-n", namespace}, {"list", "pods", "--all-namespaces"}, {"get", "services", "-n", namespace}, {"*", "*", "--all-namespaces"}} {
				if can(account, access...) {
					t.Fatalf("the proxies' service account %s may %v", account, access)
				}
			}
		case kubeapi.String(labels, "job-name") != "":
			// The chart's certificate Job, which ends after its run.
		default:
			t.Fatalf("the gateway namespace runs another workload: %v", labels)
		}
	}
	if proxies == 0 {
		t.Fatalf("no proxy of the project Gateway runs in %s", namespace)
	}
	for _, item := range kubeapi.Items(read("services", "-n", namespace), "items") {
		service, _ := item.(map[string]any)
		if serviceType := kubeapi.String(kubeapi.Child(service, "spec"), "type"); serviceType != "ClusterIP" {
			t.Fatalf("the gateway Service %s is %s", kubeapi.String(kubeapi.Child(service, "metadata"), "name"), serviceType)
		}
	}
	if !slices.ContainsFunc(pods, func(item any) bool {
		pod, _ := item.(map[string]any)
		return kubeapi.String(kubeapi.Labels(pod), "control-plane") == "envoy-gateway"
	}) {
		t.Fatalf("the controller does not run in %s", namespace)
	}
}
