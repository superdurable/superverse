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
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// A project application reaches the platform server's credential read
// endpoint, and a Pod of any other namespace does not, under the chart's
// credential-read policy and the project's allow-application policy on the
// real Local Kind cluster with the deployed chart. The other Pod still reaches
// the server's health port, which the Local Kind companion policy keeps open.
// Each probe sends one HTTP request from a Pod of the project's Dex image and
// reports the status line, so a reachable endpoint answers 401 to a request
// without a scope token.
func TestKindCredentialReadReachability(t *testing.T) {
	fixture := newKindProjectFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	live := fixture.prepareStorage(ctx)
	fixture.awaitReady(ctx, "execution access", func() error { return fixture.k.EnsureExecutionAccess(ctx, fixture.project, live.storage) })
	server := fixture.k.Names().Product + "-server." + fixture.k.Names().PlatformNamespace + ".svc.cluster.local"

	application := fixture.probe(ctx, live.storage.Boundary.Name, kubeapi.Object{kubeapi.ComponentLabel: infrakube.ComponentApplication.String()}, server, infra.CredentialReadPort)
	if !strings.HasPrefix(application, "HTTP/1.1 401") {
		t.Fatalf("a project application did not reach the credential read endpoint: %q", application)
	}

	other := "sv2-probe-" + flowid.RandomID(6)
	fixture.kubectlCreate(ctx, kubeapi.New("v1", kubeapi.KindNamespace, kubeapi.Metadata("", other, nil), nil))
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer done()
		if err := fixture.kubectl(cleanup, "delete", "namespace", other, "--wait=false").Run(); err != nil {
			t.Errorf("delete the probe namespace %s: %v", other, err)
		}
	})
	if answer := fixture.probe(ctx, other, nil, server, infra.CredentialReadPort); answer != probeUnreachable {
		t.Fatalf("a Pod of another namespace reached the credential read endpoint: %q", answer)
	}
	if answer := fixture.probe(ctx, other, nil, server, 8082); !strings.HasPrefix(answer, "HTTP/1.1 ") {
		t.Fatalf("a Pod of another namespace no longer reaches the server's health port: %q", answer)
	}
}

// probeUnreachable is a probe's answer when its connection did not open.
const probeUnreachable = "unreachable"

// probe runs one Pod in namespace with labels that posts an empty request to
// host:port and returns the response's status line, or probeUnreachable when
// the connection does not open within five seconds.
func (f *kindProjectFixture) probe(ctx context.Context, namespace string, labels kubeapi.Object, host string, port int) string {
	f.test.Helper()
	name := "sv2-probe-" + flowid.RandomID(6)
	// The inner script is single-quoted, so the request is double-quoted for printf.
	request := `printf "POST /internal/connection-credentials/read HTTP/1.1\r\nHost: probe\r\nContent-Length: 0\r\nConnection: close\r\n\r\n" >&3`
	script := "if timeout 5 bash -c 'exec 3<>/dev/tcp/" + host + "/" + strconv.Itoa(port) + " && " + request + " && head -n 1 <&3'; then :; else echo " + probeUnreachable + "; fi"
	restricted := kubeapi.Object{"allowPrivilegeEscalation": false, "capabilities": kubeapi.Object{"drop": []string{"ALL"}}, "runAsNonRoot": true, "runAsUser": 65532,
		"seccompProfile": kubeapi.Object{"type": "RuntimeDefault"}}
	f.kubectlCreate(ctx, kubeapi.New("v1", "Pod", kubeapi.Metadata(namespace, name, labels), kubeapi.Object{
		"restartPolicy": "Never", "automountServiceAccountToken": false,
		"containers": []any{kubeapi.Object{"name": "probe", "image": f.k.DexImage, "command": []string{"bash", "-c", script}, "securityContext": restricted}},
	}))
	defer func() {
		if err := f.kubectl(context.WithoutCancel(ctx), "-n", namespace, "delete", "pod", name, "--wait=false").Run(); err != nil {
			f.test.Errorf("delete the probe Pod %s: %v", name, err)
		}
	}()
	if output, err := f.kubectl(ctx, "-n", namespace, "wait", "pod/"+name, "--for=jsonpath={.status.phase}=Succeeded", "--timeout=120s").CombinedOutput(); err != nil {
		f.test.Fatalf("the probe Pod %s did not finish: %v: %s", name, err, output)
	}
	output, err := f.kubectl(ctx, "-n", namespace, "logs", name).Output()
	if err != nil {
		f.test.Fatalf("read the probe Pod %s: %v", name, err)
	}
	return strings.TrimSpace(strings.TrimSuffix(string(output), "\r\n"))
}

// kubectl is a kubectl command with the test kubeconfig.
func (f *kindProjectFixture) kubectl(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2"}, args...)...)
}
