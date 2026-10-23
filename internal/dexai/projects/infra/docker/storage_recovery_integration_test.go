// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package docker_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker/dockertest"
)

// Requires the real Docker Engine. Setup and Preview cleanup must recover
// storage stopped by a Native lane shutdown without replacing its identity.
func TestDockerStoppedProjectStorageRecovers(t *testing.T) {
	k := dockertest.InstanceInfrastructureForTest(t, "recovery")
	project := flowid.RandomID(4)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "initial storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	want := storage
	name := k.ComponentNameForTest(project, infra.ComponentStorage)
	labels := k.OwnerLabels(project, infra.ComponentStorage)
	original, err := k.OwnedContainer(ctx, name, labels)
	if err != nil || original == nil {
		t.Fatalf("inspect initial storage: %v", err)
	}
	workload := liveWorkload(ctx, t, k, project, storage)
	scope := infra.ScopeRef{ProjectID: project, Scope: workload.Scope, Storage: storage}
	configuration, err := k.ReadConfiguration(ctx, scope, workload.Configuration)
	if err != nil {
		t.Fatalf("read original configuration: %v", err)
	}

	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"setup", func() (err error) { storage, err = k.EnsureStorage(ctx, project, storage); return err }},
		{"preview grants", func() error { return k.DestroyPreviewGrants(ctx, project, storage, "") }},
		{"preview configuration", func() error { return k.DestroyPreviewConfiguration(ctx, project, storage) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := k.API().StopContainer(ctx, original.ID, time.Second); err != nil {
				t.Fatalf("stop storage: %v", err)
			}
			stopped, err := k.API().InspectContainer(ctx, original.ID)
			if err != nil || stopped == nil || stopped.State.Status != dockerapi.StateExited {
				t.Fatalf("storage did not exit: %v", err)
			}
			// A stale accepted identity must not start the stopped service.
			stale := storage
			stale.Storage.UID = "replaced-storage"
			if _, err = k.EnsureStorage(ctx, project, stale); !errors.Is(err, infra.ErrOwnershipConflict) {
				t.Fatalf("stale identity was accepted: %v", err)
			}
			stopped, err = k.API().InspectContainer(ctx, original.ID)
			if err != nil || stopped == nil || stopped.State.Running {
				t.Fatalf("stale identity restarted storage: %v", err)
			}
			if err = operation.run(); !errors.Is(err, infra.ErrPending) {
				t.Fatalf("restart must report pending: %v", err)
			}
			// The restart happens in the first reconcile, before readiness polls.
			restarted, err := k.OwnedContainer(ctx, name, labels)
			if err != nil || restarted == nil || !restarted.State.Running || restarted.ID != original.ID {
				t.Fatalf("reconcile did not restart the original storage container: %v", err)
			}
			dockertest.EventuallyForTest(ctx, t, operation.name+" after restart", operation.run)
			if storage != want {
				t.Fatalf("recovery changed accepted storage references")
			}
			if current, err := k.ReadConfiguration(ctx, scope, workload.Configuration); err != nil || !bytes.Equal(current, configuration) {
				t.Fatalf("recovery did not preserve the configuration object: %v", err)
			}
		})
	}
	t.Run("foreign owner", func(t *testing.T) {
		if _, err := k.RemoveContainer(ctx, name, labels, time.Second); err != nil {
			t.Fatal(err)
		}
		foreign := maps.Clone(original.Config.Labels)
		foreign[docker.NativeInstanceLabel] = "another"
		id, err := k.API().CreateContainer(ctx, name, dockerapi.ContainerSpec{Image: original.Config.Image, Labels: foreign})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := k.API().RemoveContainer(context.Background(), id); err != nil {
				t.Errorf("remove foreign container: %v", err)
			}
		})
		if err = k.DestroyPreviewConfiguration(ctx, project, storage); !errors.Is(err, infra.ErrOwnershipConflict) {
			t.Fatalf("foreign storage was accepted: %v", err)
		}
		container, err := k.API().InspectContainer(ctx, id)
		if err != nil || container == nil || container.State.Status != dockerapi.StateCreated {
			t.Fatalf("cleanup started another owner's storage: %v", err)
		}
	})
}
