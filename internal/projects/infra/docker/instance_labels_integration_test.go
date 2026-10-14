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
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/projects/infra/docker/dockertest"
)

// Requires the local Docker Engine. The default instance owns the objects
// created before instances existed, which carry no instance label: it adopts
// and removes such a volume, and it still refuses a volume of another
// instance.
func TestDockerDefaultInstanceOwnsUnlabeledObjects(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	project := flowid.RandomID(4)
	labels := k.PreviewLabels(project)
	if labels[docker.NativeInstanceLabel] != docker.DefaultNativeInstance {
		t.Fatalf("the test infrastructure is not the default instance: %v", labels)
	}

	legacy := maps.Clone(labels)
	delete(legacy, docker.NativeInstanceLabel)
	unlabeled := "sv2-test-" + project + "-unlabeled"
	if _, err := k.API().CreateVolume(ctx, unlabeled, legacy); err != nil {
		t.Fatalf("create the unlabeled volume: %v", err)
	}
	t.Cleanup(func() { _ = k.API().RemoveVolume(context.Background(), unlabeled) })
	if _, err := k.EnsureVolume(ctx, unlabeled, labels); err != nil {
		t.Fatalf("the default instance refused its unlabeled volume: %v", err)
	}
	if gone, err := k.RemoveVolume(ctx, unlabeled, labels); err != nil || !gone {
		t.Fatalf("the default instance did not remove its unlabeled volume: gone %v, %v", gone, err)
	}

	other := maps.Clone(labels)
	other[docker.NativeInstanceLabel] = "other"
	foreign := "sv2-test-" + project + "-other"
	if _, err := k.API().CreateVolume(ctx, foreign, other); err != nil {
		t.Fatalf("create the other instance's volume: %v", err)
	}
	t.Cleanup(func() { _ = k.API().RemoveVolume(context.Background(), foreign) })
	if _, err := k.EnsureVolume(ctx, foreign, labels); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("the default instance adopted another instance's volume: %v", err)
	}
}
