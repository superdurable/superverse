// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connect_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	sandboxkube "github.com/superdurable/superversev2/internal/agents/sandbox/kube"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/connect"
)

// Requires the isolated Local Kind cluster, the installed coding profile whose
// Sandbox exposes the Preview Dex port, and kubectl. It provisions one real
// Sandbox as the platform server, in which no Preview engine runs yet, and
// probes its Dex through a port-forward of its Service: an engine that does
// not answer is pending, never an authorization failure, and a replaced
// namespace, Service or instance is an ownership conflict.
func TestKindSandboxDexIdentity(t *testing.T) {
	profile := os.Getenv("SUPERVERSE_TEST_KIND_PROFILE_NAME")
	if profile == "" {
		t.Fatal("requires the installed immutable V2 Sandbox profile name")
	}
	cluster := kubeapitest.KindPlatform(t)
	k := &kube.Infrastructure{API: cluster.ServerAPI(t), Dial: cluster.ServiceForwards(t), Environment: deploymentnames.EnvironmentTest}
	provisioner := &sandboxkube.InstanceProvisioner{Infrastructure: k, ProfileName: profile}
	s := sandbox.Instance{Identity: sandbox.Identity{ProjectID: flowid.RandomID(8)}, Generation: 1, WorkspaceRequestID: "kind-dex-" + flowid.RandomID(8)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, remove := range []func(context.Context, sandbox.Instance) (bool, error){provisioner.DeletePod, provisioner.DeleteVolume, provisioner.DeleteNamespace} {
			if err := poll(ctx, func() (bool, error) { return remove(ctx, s) }); err != nil {
				t.Errorf("clean up the Sandbox of %s: %v", s.Identity.ProjectID, err)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if err := poll(ctx, func() (bool, error) {
		resources, ready, err := provisioner.Provision(ctx, s)
		s.Resources = resources
		return ready, err
	}); err != nil {
		t.Fatalf("Sandbox not ready: %+v (%v)", s.Resources, err)
	}
	namespace := s.Resources.Boundary.Name
	service, _, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+namespace+"/services/"+s.Resources.ServiceName, nil)
	if err != nil {
		t.Fatal(err)
	}
	connections := &connect.Connections{Infrastructure: k}
	target := infra.SandboxDexTarget{
		ProjectID: s.Identity.ProjectID, WorkspaceRequestID: s.WorkspaceRequestID, Generation: s.Generation,
		Namespace: infra.ResourceRef{Name: namespace, UID: s.Resources.Boundary.UID},
		Sandbox:   infra.ResourceRef{Boundary: namespace, Name: s.Resources.Instance.Name, UID: s.Resources.Instance.UID},
		Service:   infra.ResourceRef{Boundary: namespace, Name: s.Resources.ServiceName, UID: kubeapi.UID(service)},
	}
	if err = connections.ProbeSandboxDex(ctx, target); !errors.Is(err, infra.ErrPending) || errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a Sandbox without a running Preview engine was not pending: %v", err)
	}
	for name, replace := range map[string]func(*infra.SandboxDexTarget){
		"namespace UID":        func(d *infra.SandboxDexTarget) { d.Namespace.UID = "replaced-namespace" },
		"Service UID":          func(d *infra.SandboxDexTarget) { d.Service.UID = "replaced-service" },
		"Sandbox UID":          func(d *infra.SandboxDexTarget) { d.Sandbox.UID = "replaced-sandbox" },
		"workspace request ID": func(d *infra.SandboxDexTarget) { d.WorkspaceRequestID = "another-request" },
	} {
		replaced := target
		replace(&replaced)
		if err = connections.ProbeSandboxDex(ctx, replaced); !errors.Is(err, infra.ErrOwnershipConflict) {
			t.Fatalf("a replaced %s was not an ownership conflict: %v", name, err)
		}
	}
	t.Logf("verified Sandbox Dex identity checks for %s, Sandbox UID %s, Service UID %s", namespace, target.Sandbox.UID, target.Service.UID)
}

// poll calls done every second until it reports true, and returns done's
// error or the end of ctx.
func poll(ctx context.Context, done func() (bool, error)) error {
	for {
		finished, err := done()
		if err != nil || finished {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
