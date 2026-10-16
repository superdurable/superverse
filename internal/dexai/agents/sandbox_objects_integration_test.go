// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// sandboxObjects reads and changes what a Project's Sandbox holds in the
// runtime of the local lane that SUPERVERSE_TEST_LANE names, with the
// operator's tools: kubectl with the operator's kubeconfig
// (SUPERVERSE_TEST_KIND_KUBECONFIG) on Local Kind, and the Docker CLI of the
// engine the operator's shell uses on Local Native. The scenarios read the
// runtime only to observe what the Flows did; they hold no runtime state of
// their own.
type sandboxObjects struct {
	// released reports whether nothing of the Sandbox is left: its namespace,
	// with everything in it, on Kind; every container, network and volume
	// labeled with the Project's Preview scope on Docker. boundary is the
	// Sandbox's boundary name, the same on both runtimes.
	released func(ctx context.Context, projectID, boundary string) bool
	// removeInstance removes the instance of r behind the Session's back, as a
	// lost node does, and instanceGone reports whether it is gone.
	removeInstance func(ctx context.Context, r sandbox.Resources)
	instanceGone   func(ctx context.Context, r sandbox.Resources) bool
	// volumeKept reports whether the workspace volume of r is still the
	// recorded one.
	volumeKept func(ctx context.Context, r sandbox.Resources) bool
	// ownVolume gives the workspace volume of r the Project label project, as
	// another owner that takes the volume does and as its owner takes it back.
	ownVolume func(ctx context.Context, r sandbox.Resources, project string)
}

// newSandboxObjects reaches the runtime of the test's local lane.
func newSandboxObjects(t *testing.T) sandboxObjects {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		return kindSandboxObjects(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG"))
	}
	return dockerSandboxObjects(t)
}

// sandboxResource is the kubectl resource of a Sandbox instance.
const sandboxResource = "sandboxes.agents.x-k8s.io"

// kindSandboxObjects reads the Sandbox's namespace, Sandbox object and PVC
// through kubectl.
func kindSandboxObjects(t *testing.T, kubeconfig string) sandboxObjects {
	kubectl := func(ctx context.Context, args ...string) string {
		return operatorCommand(t, ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig, "--context", "kind-superverse-v2", "--request-timeout=15s"}, args...)...)
	}
	return sandboxObjects{
		released: func(ctx context.Context, _, boundary string) bool {
			return kubectl(ctx, "get", "namespace", boundary, "--ignore-not-found", "-o", "name") == ""
		},
		removeInstance: func(ctx context.Context, r sandbox.Resources) {
			kubectl(ctx, "-n", r.Boundary.Name, "delete", sandboxResource, r.Instance.Name, "--wait=false")
			// A lost node takes its Pod at once: no graceful termination keeps sandboxd
			// answering, as `docker rm --force` on Native.
			kubectl(ctx, "-n", r.Boundary.Name, "delete", "pod", r.Instance.Name, "--grace-period=0", "--force", "--ignore-not-found", "--wait=false")
		},
		instanceGone: func(ctx context.Context, r sandbox.Resources) bool {
			if kubectl(ctx, "-n", r.Boundary.Name, "get", sandboxResource, r.Instance.Name, "--ignore-not-found", "-o", "name") != "" ||
				kubectl(ctx, "-n", r.Boundary.Name, "get", "pod", r.Instance.Name, "--ignore-not-found", "-o", "name") != "" {
				return false
			}
			// A Pod deleted by force leaves the API at once while its containers still
			// answer on the node; a lost node answers no more. Kind's nodes are Docker
			// containers named as the nodes.
			for _, node := range strings.Fields(kubectl(ctx, "get", "nodes", "-o", "jsonpath={.items[*].metadata.name}")) {
				if operatorCommand(t, ctx, "docker", "exec", node, "crictl", "ps", "--quiet", "--label", "io.kubernetes.pod.namespace="+r.Boundary.Name,
					"--label", "io.kubernetes.pod.name="+r.Instance.Name) != "" {
					return false
				}
			}
			return true
		},
		volumeKept: func(ctx context.Context, r sandbox.Resources) bool {
			return kubectl(ctx, "-n", r.Boundary.Name, "get", "pvc", r.Volume.Name, "-o", "jsonpath={.metadata.uid}") == r.Volume.UID
		},
		ownVolume: func(ctx context.Context, r sandbox.Resources, project string) {
			kubectl(ctx, "-n", r.Boundary.Name, "label", "pvc", r.Volume.Name, deploymentnames.ProjectLabel+"="+project, "--overwrite")
		},
	}
}

// dockerSandboxObjects reads the Sandbox's network, container and workspace
// volumes through the Docker CLI, by their labels: the workspace is one volume
// per subPath, all with the workspace UID label, and a volume's labels cannot
// change, so another owner takes one by creating it again under its name.
func dockerSandboxObjects(t *testing.T) sandboxObjects {
	docker := func(ctx context.Context, args ...string) string { return operatorCommand(t, ctx, "docker", args...) }
	// labeled lists the IDs of the objects of kind ("container", "network" or
	// "volume") that carry every label of labels.
	labeled := func(ctx context.Context, kind string, labels map[string]string) []string {
		args := []string{kind, "ls", "--quiet"}
		if kind == "container" {
			args = append(args, "--all")
		}
		for key, value := range labels {
			args = append(args, "--filter", "label="+key+"="+value)
		}
		return strings.Fields(docker(ctx, args...))
	}
	workspace := func(r sandbox.Resources) map[string]string {
		return map[string]string{infradocker.WorkspaceUIDLabel: r.Volume.UID}
	}
	return sandboxObjects{
		released: func(ctx context.Context, projectID, _ string) bool {
			scope := map[string]string{deploymentnames.ProjectLabel: projectID, deploymentnames.ScopeLabel: infra.ScopePreview.String()}
			return len(labeled(ctx, "container", scope))+len(labeled(ctx, "network", scope))+len(labeled(ctx, "volume", scope)) == 0
		},
		removeInstance: func(ctx context.Context, r sandbox.Resources) {
			for _, id := range labeled(ctx, "container", map[string]string{infradocker.InstanceUIDLabel: r.Instance.UID}) {
				docker(ctx, "container", "rm", "--force", "--volumes", id)
			}
		},
		instanceGone: func(ctx context.Context, r sandbox.Resources) bool {
			return len(labeled(ctx, "container", map[string]string{infradocker.InstanceUIDLabel: r.Instance.UID})) == 0
		},
		volumeKept: func(ctx context.Context, r sandbox.Resources) bool {
			return len(labeled(ctx, "volume", workspace(r))) == len(infradocker.WorkspaceSubPaths)
		},
		ownVolume: func(ctx context.Context, r sandbox.Resources, project string) {
			for _, name := range labeled(ctx, "volume", workspace(r)) {
				labels := map[string]string{}
				if err := json.Unmarshal([]byte(docker(ctx, "volume", "inspect", name, "--format", "{{json .Labels}}")), &labels); err != nil {
					t.Fatalf("the labels of the volume %s are unreadable (%T)", name, err)
				}
				labels[deploymentnames.ProjectLabel] = project
				create := []string{"volume", "create"}
				for key, value := range labels {
					create = append(create, "--label", key+"="+value)
				}
				docker(ctx, "volume", "rm", name)
				docker(ctx, append(create, name)...)
			}
		},
	}
}

// operatorCommand runs one bounded operator command and returns its trimmed
// output; a failure fails the test.
func operatorCommand(t *testing.T, ctx context.Context, name string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %s", name, strings.Join(args, " "), out)
	}
	return strings.TrimSpace(string(out))
}
