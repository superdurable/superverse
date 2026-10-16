// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockertest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// NetworkPrefixForTest is the IPv4 network the tests' networks take their
// subnets from; it is apart from the Native instances' ranges, which start at
// 10.233.0.0/16 and take the next /16 each.
var NetworkPrefixForTest = netip.MustParsePrefix("10.250.0.0/16")

// imagePins are the digest-pinned images of the repository's source lock.
type imagePins struct {
	dex, s3, temporal, gateway, registry string
}

// InfrastructureForTest is the Docker runtime of the local Docker Engine as the test
// environment: the real engine at SUPERVERSE_TEST_DOCKER_SOCKET (by default
// /var/run/docker.sock), the pinned images of the source lock, a private
// state directory of the test and a gateway of its own, which serves the
// routes from the test process exactly as the Native gateway serves them
// from the server. The test's cleanup removes the gateway; the test removes
// what its projects created. An unreachable engine fails the test.
func InfrastructureForTest(t testing.TB) *docker.Infrastructure {
	t.Helper()
	return InstanceInfrastructureForTest(t, "")
}

// InstanceInfrastructureForTest is InfrastructureForTest for the named Native
// instance; an empty name is the default instance.
func InstanceInfrastructureForTest(t testing.TB, instance string) *docker.Infrastructure {
	t.Helper()
	socket := os.Getenv("SUPERVERSE_TEST_DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	pins := readPins(t)
	var routes atomic.Pointer[docker.Routes]
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The gateway polls before the infrastructure exists.
		if current := routes.Load(); current != nil {
			current.ServeHTTP(w, r)
			return
		}
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	t.Cleanup(provider.Close)
	web, api := startGateway(t, socket, pins.gateway, provider.URL)
	webPort := comm.Must(netip.ParseAddrPort(web)).Port()
	k, err := docker.New(docker.Config{
		Socket: socket, Environment: deploymentnames.EnvironmentTest, DexImage: pins.dex, S3Image: pins.s3, TemporalImage: pins.temporal,
		StateDir: filepath.Join(t.TempDir(), "native"), Instance: instance, NetworkPrefix: NetworkPrefixForTest,
		Routes:     infra.PublicRoutes{Environment: deploymentnames.EnvironmentTest, Local: true, LocalDomain: "localhost", LocalPort: int(webPort)},
		GatewayURL: "http://" + web, GatewayAPI: "http://" + api,
	})
	if err != nil {
		t.Fatal(err)
	}
	routes.Store(k.Routes())
	t.Cleanup(func() { _ = k.Close() })
	return k
}

// readPins reads the images of the repository's source lock for the engine's
// architecture.
func readPins(t testing.TB) imagePins {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "infra-as-code/source-lock.json")) //nolint:gosec // the repository's source lock
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Dex struct {
			Server      string `json:"server"`
			ImageDigest string `json:"serverImageDigest"`
		} `json:"dex"`
		Local map[string]json.RawMessage `json:"localDependencies"`
	}
	if err = json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	pin := func(name string) map[string]string {
		var fields map[string]string
		if err := json.Unmarshal(lock.Local[name], &fields); err != nil {
			t.Fatalf("source lock pin %s: %v", name, err)
		}
		return fields
	}
	minio, temporal, gateway, registry := pin("minio"), pin("temporal"), pin("traefik"), pin("registry")
	return imagePins{
		dex:      "docker.io/superdurable/dex-server:v" + lock.Dex.Server + "@" + lock.Dex.ImageDigest,
		s3:       minio["repository"] + ":" + minio["release"] + "@" + minio[runtime.GOARCH],
		temporal: temporal["repository"] + ":" + temporal["version"] + "@" + temporal[runtime.GOARCH],
		gateway:  gateway["repository"] + ":" + gateway["version"] + "@" + gateway["index"],
		registry: registry["repository"] + ":" + registry["version"] + "@" + registry["index"],
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// testOwner marks the containers the test helpers run themselves, apart from
// a project's.
const testOwner = "superverse-v2-test"

// removeOnCleanup stops and removes the container id, named name, when the
// test ends, within a minute that outlives the test's own context.
func removeOnCleanup(ctx context.Context, t testing.TB, client *dockerapi.Client, name, id string) {
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if err := client.StopContainer(cleanup, id, time.Second); err != nil {
			t.Errorf("stop %s: %v", name, err)
		}
		if err := client.RemoveContainer(cleanup, id); err != nil {
			t.Errorf("remove %s: %v", name, err)
		}
	})
}

// The ports of the gateway container: its entry point and its API.
const (
	gatewayWeb = 8080
	gatewayAPI = 8081
)

// startGateway runs the pinned gateway image as the Native gateway runs, with
// its HTTP provider polling provider, and returns the loopback addresses of
// its entry point and its API once the API answers.
func startGateway(t testing.TB, socket, image, provider string) (web, api string) {
	t.Helper()
	client := dockerapi.New(socket)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, port, _ := strings.Cut(strings.TrimPrefix(provider, "http://"), ":")
	name := "sv2-test-gateway-" + flowid.RandomID(8)
	spec := dockerapi.ContainerSpec{Image: image, Labels: map[string]string{deploymentnames.ManagedByLabel: testOwner}, Cmd: []string{
		"--providers.http.endpoint=http://" + infra.HostFromContainers + ":" + port + "/routes", "--providers.http.pollInterval=1s",
		"--entrypoints." + docker.GatewayEntryPoint + ".address=:" + strconv.Itoa(gatewayWeb), "--entrypoints.traefik.address=:" + strconv.Itoa(gatewayAPI),
		"--api=true", "--api.insecure=true", "--log.level=ERROR",
	}, HostConfig: dockerapi.HostConfig{ReadonlyRootfs: true, CapDrop: []string{"ALL"}}}
	spec.Publish(gatewayWeb, gatewayAPI)
	if image, err := client.InspectImage(ctx, image); err != nil || image == nil {
		if err = client.PullImage(ctx, spec.Image); err != nil {
			t.Fatalf("pull the gateway image: %v", err)
		}
	}
	id, err := client.CreateContainer(ctx, name, spec)
	if err != nil {
		t.Fatalf("create the test gateway: %v", err)
	}
	removeOnCleanup(ctx, t, client, name, id)
	if err = client.StartContainer(ctx, id); err != nil {
		t.Fatalf("start the test gateway: %v", err)
	}
	container, err := client.InspectContainer(ctx, id)
	if err != nil || container == nil {
		t.Fatalf("read the test gateway: %v", err)
	}
	web, webErr := container.PublishedAddress(gatewayWeb)
	api, apiErr := container.PublishedAddress(gatewayAPI)
	if webErr != nil || apiErr != nil {
		t.Fatalf("the test gateway publishes no ports: %v %v", webErr, apiErr)
	}
	for {
		request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, "http://"+api+"/api/overview", nil)) //nolint:gosec // the API port the test gateway publishes on the loopback
		response, err := http.DefaultClient.Do(request)                                                           //nolint:gosec // the same request
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return web, api
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("the test gateway's API did not answer")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
