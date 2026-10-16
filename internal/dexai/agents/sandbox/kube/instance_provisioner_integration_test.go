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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/local"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// This opt-in test uses the real V2 controller and API with the platform ServiceAccount.
// It verifies provider provisioning, its repetition, resource discovery, UID
// cleanup and the repetition of every deletion after a lost answer, not the
// application E2E journey.
func TestKindSandboxProvisionAndCleanup(t *testing.T) {
	profileName := os.Getenv("SUPERVERSE_TEST_KIND_PROFILE_NAME")
	if profileName == "" {
		t.Fatal("requires the installed immutable V2 Sandbox profile name")
	}
	k, _ := kindProvisioner(t, profileName)
	id := sandbox.Identity{ProjectID: flowid.RandomID(8)}
	s := sandbox.Instance{Identity: id, Generation: 1}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		removals := []func(context.Context, sandbox.Instance) (bool, error){k.DeletePod, k.DeleteVolume, k.DeleteNamespace}
		for _, remove := range removals {
			if e := pollUntil(ctx, time.Second, func() (bool, error) { return remove(ctx, s) }); e != nil {
				t.Errorf("cleanup retained namespace %s: %v", k.namespace(id), e)
				return
			}
		}
		// A Step whose answer was lost deletes again with the same recorded
		// UIDs; absence is success, never an error.
		for index, remove := range removals {
			if done, e := remove(ctx, s); e != nil || !done {
				t.Errorf("repeated deletion %d of absent resources: done=%v (%v)", index, done, e)
			}
		}
		if found, e := k.DiscoverResources(ctx, sandbox.Instance{Identity: id, Generation: 1}); e != nil || found != (sandbox.Resources{}) {
			t.Errorf("deleted resources of namespace %s are still discovered: %+v (%v)", k.namespace(id), found, e)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	if err := pollUntil(ctx, time.Second, func() (bool, error) {
		resources, ready, provisionErr := k.Provision(ctx, s)
		s.Resources = resources
		return ready, provisionErr
	}); err != nil {
		t.Fatalf("Sandbox not ready: %+v (%v)", s.Resources, err)
	}
	r := s.Resources
	if r.Boundary.UID == "" || r.Instance.UID == "" || r.Volume.UID == "" || r.ServiceName == "" || !strings.Contains(r.ImageDigest, "@sha256:") {
		t.Fatalf("incomplete authoritative provider identities: %+v", r)
	}
	// A repeated provisioning, after a lost answer or by a new attempt, adopts
	// every existing owned object instead of failing on AlreadyExists.
	if again, ready, e := k.Provision(ctx, s); e != nil || !ready || again != r {
		t.Fatalf("repeated provisioning changed or failed: %+v ready=%v (%v)", again, ready, e)
	}
	// Reclamation without references finds every owned object by its name.
	found, e := k.DiscoverResources(ctx, sandbox.Instance{Identity: id, Generation: 1})
	if e != nil || found.Boundary != r.Boundary || found.Volume != r.Volume || found.Instance != r.Instance || found.ServiceName != r.ServiceName {
		t.Fatalf("discovered resources %+v (%v) differ from the provisioned %+v", found, e, r)
	}
	t.Logf("verified real Kind Sandbox %s generation=1 immutableImage=%s", k.namespace(id), r.ImageDigest)
}

// kindProvisioner provisions as the platform server of the isolated Local Kind
// cluster, which it also returns for the operator's own staging.
func kindProvisioner(t *testing.T, profileName string) (*InstanceProvisioner, kubeapitest.KindCluster) {
	t.Helper()
	cluster := kubeapitest.KindPlatform(t)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cluster.CAForTest) {
		t.Fatal("invalid Kind CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	api := &kubeapi.Client{ServerURL: cluster.ServerForTest, TokenFile: cluster.TokenFileForTest, HTTP: &http.Client{Transport: transport}}
	return &InstanceProvisioner{Infrastructure: &infrakube.Infrastructure{API: api, Environment: deploymentnames.EnvironmentTest}, ProfileName: profileName}, cluster
}

// This opt-in test uses the real V2 controller and API with the platform
// ServiceAccount, and the operator's kubectl only to stage what the platform
// may not create: a foreign Pod that mounts the workspace volume and a volume
// label of the earlier profile. One Session's volume moves from instance ws-1
// to instance ws-2:
//   - ws-1 records the coding-v4 identities, and its Sandbox and Pod carry
//     its workspace-request label;
//   - ws-2 is refused while ws-1's Pod runs, and DeletePod of ws-1 reports
//     done only once that Pod is gone;
//   - ws-2 is refused while a foreign Pod mounts the volume, waits without
//     creating its Sandbox while that Pod terminates, then becomes Ready on
//     the same volume;
//   - a volume labelled for coding-v3 is refused.
func TestKindSandboxVolumeHandover(t *testing.T) {
	profileName := os.Getenv("SUPERVERSE_TEST_KIND_PROFILE_NAME")
	if profileName == "" {
		t.Fatal("requires the installed immutable V2 Sandbox profile name")
	}
	k, cluster := kindProvisioner(t, profileName)
	id := sandbox.Identity{ProjectID: flowid.RandomID(8)}
	namespace := k.namespace(id)
	kubectl := func(ctx context.Context, input string, arguments ...string) error {
		command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", cluster.KubeconfigForTest, "--context", "kind-superverse-v2", "-n", namespace}, arguments...)...) //nolint:gosec // kubectl with the test's own arguments
		command.Stdin = strings.NewReader(input)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("kubectl %s: %w: %s", strings.Join(arguments, " "), err, output)
		}
		return nil
	}
	operator := func(input string, arguments ...string) {
		t.Helper()
		if err := kubectl(t.Context(), input, arguments...); err != nil {
			t.Fatal(err)
		}
	}
	first := sandbox.Instance{Identity: id, Generation: 1, WorkspaceRequestID: "ws-1"}
	second := sandbox.Instance{Identity: id, Generation: 1, WorkspaceRequestID: "ws-2"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		// The foreign Pod may be left with its finalizer by a failed step.
		_ = kubectl(ctx, "", "delete", "pod", "foreign-holder", "--ignore-not-found", "--wait=false")
		_ = kubectl(ctx, "", "patch", "pod", "foreign-holder", "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		owner := second
		if owner.Resources.Boundary.UID == "" {
			owner = first
		}
		for _, remove := range []func(context.Context, sandbox.Instance) (bool, error){k.DeletePod, k.DeleteVolume, k.DeleteNamespace} {
			if err := pollUntil(ctx, time.Second, func() (bool, error) { return remove(ctx, owner) }); err != nil {
				t.Errorf("clean up namespace %s: %v", namespace, err)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	provisionUntilReady := func(s *sandbox.Instance) {
		t.Helper()
		if err := pollUntil(ctx, time.Second, func() (bool, error) {
			resources, ready, err := k.Provision(ctx, *s)
			s.Resources = resources
			return ready, err
		}); err != nil {
			t.Fatalf("Sandbox of %s not ready: %+v (%v)", s.WorkspaceRequestID, s.Resources, err)
		}
	}
	get := func(path string) kubeapi.Object {
		t.Helper()
		o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
		if err != nil || code == http.StatusNotFound {
			t.Fatalf("read %s: %d (%v)", path, code, err)
		}
		return o
	}
	podsOf := func(sandboxUID string) []kubeapi.Object {
		t.Helper()
		var owned []kubeapi.Object
		for _, item := range kubeapi.Items(get("/api/v1/namespaces/"+namespace+"/pods"), "items") {
			if pod, _ := item.(map[string]any); kubeapi.ControlledBy(pod, "Sandbox", sandboxUID) {
				owned = append(owned, pod)
			}
		}
		return owned
	}

	provisionUntilReady(&first)
	r := first.Resources
	service := get("/api/v1/namespaces/" + namespace + "/services/" + r.ServiceName)
	ports := fmt.Sprint(kubeapi.Child(service, "spec")["ports"])
	if r.Identity.UID == "" || r.ServiceUID != kubeapi.UID(service) || r.Profile != infra.CodingSandboxProfile || !strings.Contains(ports, "8801") {
		t.Fatalf("incomplete coding-v4 identities %+v of a Service with ports %s", r, ports)
	}
	sandboxObject := get("/apis/agents.x-k8s.io/v1beta1/namespaces/" + namespace + "/sandboxes/" + r.Instance.Name)
	if label := kubeapi.String(kubeapi.Labels(sandboxObject), kubeapi.WorkspaceRequestLabel); label != first.WorkspaceRequestID {
		t.Fatalf("the Sandbox's workspace-request label is %q", label)
	}
	if err := pollUntil(ctx, time.Second, func() (bool, error) { return len(podsOf(r.Instance.UID)) == 1, nil }); err != nil {
		t.Fatal("the Sandbox runs no Pod:", err)
	}
	if label := kubeapi.String(kubeapi.Labels(podsOf(r.Instance.UID)[0]), kubeapi.WorkspaceRequestLabel); label != first.WorkspaceRequestID {
		t.Fatalf("the Pod's workspace-request label is %q", label)
	}

	second.Resources = sandbox.Resources{Boundary: r.Boundary, Volume: r.Volume, Profile: r.Profile}
	if _, _, err := k.Provision(ctx, second); !errors.Is(err, sandbox.ErrVolumeInUse) {
		t.Fatalf("a second instance beside the running Pod was not refused: %v", err)
	}
	if err := pollUntil(ctx, time.Second, func() (bool, error) { return k.DeletePod(ctx, first) }); err != nil {
		t.Fatal("DeletePod did not converge:", err)
	}
	if remaining := podsOf(r.Instance.UID); len(remaining) != 0 {
		t.Fatalf("DeletePod reported done while %d Pods of the Sandbox remain", len(remaining))
	}

	holderMetadata := kubeapi.Metadata("", "foreign-holder", nil)
	holderMetadata["finalizers"] = []string{"superdurable.io/test-hold"}
	holder := kubeapi.New("v1", "Pod", holderMetadata, kubeapi.Object{
		// Unschedulable: naming the claim in its spec is enough.
		"nodeSelector":    kubeapi.Object{"superdurable.io/test-unschedulable": "true"},
		"securityContext": kubeapi.Object{"runAsNonRoot": true, "runAsUser": 1000, "seccompProfile": kubeapi.Object{"type": "RuntimeDefault"}},
		"containers": []any{kubeapi.Object{"name": "holder", "image": r.ImageDigest, "command": []string{"sleep", "3600"},
			"securityContext": kubeapi.Object{"allowPrivilegeEscalation": false, "capabilities": kubeapi.Object{"drop": []string{"ALL"}}}}},
		"volumes": []any{kubeapi.ClaimVolume("workspace", r.Volume.Name)},
	})
	operator(string(comm.Must(json.Marshal(holder))), "apply", "-f", "-")
	if _, _, err := k.Provision(ctx, second); !errors.Is(err, sandbox.ErrVolumeInUse) {
		t.Fatalf("an instance beside a foreign Pod was not refused: %v", err)
	}
	operator("", "delete", "pod", "foreign-holder", "--wait=false")
	if _, ready, err := k.Provision(ctx, second); err != nil || ready {
		t.Fatalf("an instance beside a terminating Pod did not wait: ready=%v (%v)", ready, err)
	}
	if _, code, err := k.API.Call(ctx, http.MethodGet, "/apis/agents.x-k8s.io/v1beta1/namespaces/"+namespace+"/sandboxes/"+r.Instance.Name, nil); err != nil || code != http.StatusNotFound {
		t.Fatalf("the waiting instance created its Sandbox: %d (%v)", code, err)
	}
	operator("", "patch", "pod", "foreign-holder", "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
	provisionUntilReady(&second)
	if second.Resources.Volume != r.Volume || second.Resources.Instance.UID == r.Instance.UID {
		t.Fatalf("the second instance %+v did not adopt the volume with a new Sandbox", second.Resources)
	}

	operator("", "label", "pvc", r.Volume.Name, kubeapi.SandboxProfileLabel+"=coding-v3", "--overwrite")
	if _, _, err := k.Provision(ctx, second); !errors.Is(err, sandbox.ErrProfileMismatch) {
		t.Fatalf("a coding-v3 volume was not refused: %v", err)
	}
	t.Logf("verified the volume handover of %s from Sandbox UID %s to %s", namespace, r.Instance.UID, second.Resources.Instance.UID)
}

// This opt-in test uses the real V2 API with the platform ServiceAccount, and
// the operator's kubectl only to change route objects behind the platform's
// back. The Project's Preview route follows the platform's spec: an expose
// creates its Service, NetworkPolicy and HTTPRoute under the Project's host
// and keeps the ones that match; a policy and an HTTPRoute whose spec changed
// are replaced under the same names and host while the matching Service
// stays; an HTTPRoute that claims another Project's host is refused at
// admission, whoever writes it; a stop removes all three, so a stopped
// Preview admits nothing to the application's port and holds no route; and
// the next expose brings them back under the same host. No application runs,
// so the gateway never answers through the route.
func TestKindPreviewRouteLifecycle(t *testing.T) {
	profileName := os.Getenv("SUPERVERSE_TEST_KIND_PROFILE_NAME")
	if profileName == "" {
		t.Fatal("requires the installed immutable V2 Sandbox profile name")
	}
	k, cluster := kindProvisioner(t, profileName)
	k.Backend = &local.Backend{}
	k.Routes = infra.PublicRoutes{Environment: deploymentnames.EnvironmentTest, Local: true, LocalDomain: "localhost", LocalPort: 23080, LocalGatewayURL: "http://127.0.0.1:23080"}
	id := sandbox.Identity{ProjectID: flowid.RandomID(8)}
	namespace := k.namespace(id)
	s := sandbox.Instance{Identity: id, Generation: 1, WorkspaceRequestID: "ws-1"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		for _, remove := range []func(context.Context, sandbox.Instance) (bool, error){k.ReleasePreviewRoute, k.DeletePod, k.DeleteVolume, k.DeleteNamespace} {
			if err := pollUntil(ctx, time.Second, func() (bool, error) { return remove(ctx, s) }); err != nil {
				t.Errorf("clean up namespace %s: %v", namespace, err)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	if err := pollUntil(ctx, time.Second, func() (bool, error) {
		resources, ready, err := k.Provision(ctx, s)
		s.Resources = resources
		return ready, err
	}); err != nil {
		t.Fatalf("Sandbox not ready: %+v (%v)", s.Resources, err)
	}
	operator := func(arguments ...string) {
		t.Helper()
		command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", cluster.KubeconfigForTest, "--context", "kind-superverse-v2", "-n", namespace}, arguments...)...) //nolint:gosec // kubectl with the test's own arguments
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("kubectl %s: %v: %s", strings.Join(arguments, " "), err, output)
		}
	}
	route := k.previewRoute(s)
	// read returns the route object desired names, nil when it is absent.
	read := func(desired kubeapi.Object) kubeapi.Object {
		t.Helper()
		found, code, err := k.API.Call(ctx, http.MethodGet, kubeapi.CollectionPath(desired)+"/"+kubeapi.String(kubeapi.Child(desired, "metadata"), "name"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if code == http.StatusNotFound {
			return nil
		}
		return found
	}
	// exposeUntilCurrent exposes the Preview until every route object matches
	// the platform's spec and returns their UIDs in the route's order.
	exposeUntilCurrent := func(what string) []string {
		t.Helper()
		var uids []string
		if err := pollUntil(ctx, time.Second, func() (bool, error) {
			if _, _, err := k.ExposePreview(ctx, s); err != nil {
				return false, err
			}
			uids = uids[:0]
			for _, desired := range route {
				found := read(desired)
				if found == nil || kubeapi.Deleting(found) || !kubeapi.Matches(found, desired) {
					return false, nil
				}
				uids = append(uids, kubeapi.UID(found))
			}
			return true, nil
		}); err != nil {
			t.Fatalf("%s: the route of %s does not match the platform's spec: %v", what, namespace, err)
		}
		return slices.Clone(uids)
	}
	host := k.Routes.Host(infra.ScopePreview, id.ProjectID)
	hostOf := func() string {
		t.Helper()
		hostnames := kubeapi.Items(kubeapi.Child(read(route[2]), "spec"), "hostnames")
		if len(hostnames) != 1 {
			t.Fatalf("the HTTPRoute has %d hostnames", len(hostnames))
		}
		hostname, _ := hostnames[0].(string)
		return hostname
	}

	created := exposeUntilCurrent("the first start")
	if hostOf() != host {
		t.Fatalf("the HTTPRoute serves %s, not the Project's host %s", hostOf(), host)
	}
	// The cluster's administrator, not only the platform, is refused a route
	// that claims another Project's host from this namespace.
	foreign := kubeapi.HTTPRoute(kubeapi.Metadata(namespace, "foreign-host", nil), k.RouteGateway(), k.Routes.Host(infra.ScopePreview, flowid.RandomID(8)), previewAppName, 3000)
	claim := exec.CommandContext(ctx, "kubectl", "--kubeconfig", cluster.KubeconfigForTest, "--context", "kind-superverse-v2", "create", "-f", "-") //nolint:gosec // kubectl with the test's own arguments
	claim.Stdin = strings.NewReader(string(comm.Must(json.Marshal(foreign))))
	if output, err := claim.CombinedOutput(); err == nil || !strings.Contains(string(output), "denied") {
		t.Fatalf("an HTTPRoute for another Project's host was not refused at admission: %v: %s", err, output)
	}
	// A platform release that changes the route's spec reaches a Project
	// whose route exists: the changed objects are replaced, the matching one
	// is kept.
	operator("annotate", "httproute", previewAppName, "superdurable.io/test-drift=1")
	operator("patch", "networkpolicy", previewPolicyName, "--type=json", "-p", `[{"op":"replace","path":"/spec/ingress/0/ports/0/port","value":3001}]`)
	replaced := exposeUntilCurrent("a changed spec")
	if replaced[0] != created[0] || replaced[1] == created[1] || replaced[2] == created[2] || hostOf() != host {
		t.Fatalf("the reconciliation replaced %v of %v, or moved the host to %s", replaced, created, hostOf())
	}
	// A stop removes the route before it stops the application; an instance
	// without a Sandbox reference has no supervisor to ask.
	stopping := s
	stopping.Resources.Instance = sandbox.ResourceRef{}
	if err := pollUntil(ctx, time.Second, func() (bool, error) { return k.StopPreview(ctx, stopping, "stop-preview") }); err != nil {
		t.Fatalf("the stop did not converge: %v", err)
	}
	for _, desired := range route {
		if found := read(desired); found != nil {
			t.Fatalf("the stop left the %s of the route", kubeapi.String(desired, "kind"))
		}
	}
	// The gateway stops serving the stopped Preview's host: it answers 404,
	// as for a host it never knew.
	gatewayStatus := func() (int, error) {
		request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, k.Routes.LocalGatewayURL+"/", nil))
		request.Host = host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return 0, err
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode, nil
	}
	if err := pollUntil(ctx, time.Second, func() (bool, error) {
		code, err := gatewayStatus()
		return code == http.StatusNotFound, err
	}); err != nil {
		t.Fatalf("the gateway still serves the stopped Preview's host %s: %v", host, err)
	}
	// The next start brings the route back under the same host.
	restored := exposeUntilCurrent("the next start")
	if slices.Contains(restored, replaced[0]) || hostOf() != host {
		t.Fatalf("the next start did not create the route again under %s: %v", host, restored)
	}
	t.Logf("verified the Preview route of %s: created %v, replaced %v, removed by the stop, created again %v", namespace, created, replaced, restored)
}

// pollUntil calls done every interval until it reports true, and returns
// done's error or the end of ctx.
func pollUntil(ctx context.Context, interval time.Duration, done func() (bool, error)) error {
	for {
		finished, err := done()
		if err != nil || finished {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
