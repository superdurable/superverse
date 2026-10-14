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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// EventuallyForTest calls converge every second until it returns nil, failing the
// test on an error other than infra.ErrPending or when ctx ends.
func EventuallyForTest(ctx context.Context, t testing.TB, what string, converge func() error) {
	t.Helper()
	for {
		err := converge()
		if err == nil {
			return
		}
		if !errors.Is(err, infra.ErrPending) {
			t.Fatalf("%s: %v", what, err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s did not converge: %v", what, err)
		case <-time.After(time.Second):
		}
	}
}

// DestroyProjectForTest removes the project's Live scope as project deletion does
// and requires that nothing of the project is left: no container, network or
// volume with its label, and no credential file.
func DestroyProjectForTest(t testing.TB, k *docker.Infrastructure, project string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	EventuallyForTest(ctx, t, "the project's deletion", func() error { return k.DestroyScope(ctx, project, infra.ScopeRefs{}) })
	requireNothingLeft(ctx, t, k, project)
}

// requireNothingLeft fails the test while any container, network or volume of
// this Native instance carries the project's label or a credential file of the
// project remains. The default instance's objects may carry no instance label.
func requireNothingLeft(ctx context.Context, t testing.TB, k *docker.Infrastructure, project string) {
	t.Helper()
	instance := k.OwnerLabels(project, infra.ComponentInfrastructure)[docker.NativeInstanceLabel]
	selector := dockerapi.Labels(docker.SelectorLabels(map[string]string{deploymentnames.ProjectLabel: project, docker.NativeInstanceLabel: instance}))
	containers, err := k.API().ListContainers(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	networks, err := k.API().ListNetworks(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := k.API().ListVolumes(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	left := 0
	for _, c := range containers {
		left += ofInstance(c.Labels, instance)
	}
	for _, n := range networks {
		left += ofInstance(n.Labels, instance)
	}
	for _, v := range volumes {
		left += ofInstance(v.Labels, instance)
	}
	if left != 0 {
		t.Fatalf("project %s left %d of its containers, networks and volumes", project, left)
	}
	if _, err = os.Stat(k.ProjectCredentials(project)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("project %s left its credential files: %v", project, err)
	}
}

// ofInstance is 1 when labels mark an object of instance; an object without
// the instance label belongs to the default instance.
func ofInstance(labels map[string]string, instance string) int {
	owner, present := labels[docker.NativeInstanceLabel]
	if !present {
		owner = docker.DefaultNativeInstance
	}
	if owner == instance {
		return 1
	}
	return 0
}

// CredentialFilesForTest are the credential files of the project.
func CredentialFilesForTest(t testing.TB, k *docker.Infrastructure, project string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(k.ProjectCredentials(project), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// WebPortForTest and WebPathForTest are the port and the path at which a WebContainerForTest
// answers 200.
const (
	WebPortForTest = 8080
	WebPathForTest = "/ping"
)

// WebContainerForTest runs a small web server of the project in the pinned gateway
// image, publishing WebPortForTest on the loopback, and returns its ID. The test's
// cleanup removes it.
func WebContainerForTest(ctx context.Context, t testing.TB, k *docker.Infrastructure, project string) string {
	t.Helper()
	pins := readPins(t)
	spec := dockerapi.ContainerSpec{Image: pins.gateway, Labels: map[string]string{deploymentnames.ProjectLabel: project, deploymentnames.ManagedByLabel: testOwner},
		Cmd:        []string{"--entrypoints.web.address=:" + strconv.Itoa(WebPortForTest), "--ping=true", "--ping.entrypoint=web", "--log.level=ERROR"},
		HostConfig: dockerapi.HostConfig{ReadonlyRootfs: true, CapDrop: []string{"ALL"}}}
	spec.Publish(WebPortForTest)
	if err := k.EnsureImage(ctx, spec.Image); err != nil {
		t.Fatal(err)
	}
	name := "sv2-test-web-" + flowid.RandomID(8)
	id, err := k.API().CreateContainer(ctx, name, spec)
	if err != nil {
		t.Fatal(err)
	}
	removeOnCleanup(ctx, t, k.API(), name, id)
	if err = k.API().StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	return id
}
