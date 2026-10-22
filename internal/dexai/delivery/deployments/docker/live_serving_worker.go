// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

func applicationRuntimeArchive() []byte {
	var contents bytes.Buffer
	archive := tar.NewWriter(&contents)
	runtime := deployments.ApplicationRuntime()
	comm.MustDo(archive.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "superverse-app/", Mode: 0755}))
	comm.MustDo(archive.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "superverse-app/runtime.yaml", Mode: 0444, Size: int64(len(runtime))}))
	comm.Must(archive.Write(runtime))
	comm.MustDo(archive.Close())
	return contents.Bytes()
}

func (l *LiveServing) workerContainer(ctx context.Context, s deployments.Snapshot) (*dockerapi.Container, error) {
	name, labels := l.generation(s)
	worker, err := l.OwnedContainer(ctx, name+"-worker", labels)
	if err != nil || worker == nil {
		return worker, err
	}
	if s.Resources.Worker.UID != "" && s.Resources.Worker.UID != worker.ID {
		return nil, fmt.Errorf("%w: Worker container identity changed", infra.ErrOwnershipConflict)
	}
	return worker, nil
}

func (l *LiveServing) probeWorker(ctx context.Context, s deployments.Snapshot) error {
	worker, err := l.workerContainer(ctx, s)
	if err != nil {
		return err
	}
	if worker == nil || !worker.State.Running || worker.State.Restarting {
		return fmt.Errorf("%w: Worker container is not running", infra.ErrPending)
	}
	if worker.Config.Image != s.Inputs.ImageReference {
		return fmt.Errorf("Worker container has another image")
	}
	address, err := worker.PublishedAddress(deployments.WorkerReadinessPort)
	if err != nil {
		return fmt.Errorf("%w: Worker readiness port is not published yet: %w", infra.ErrPending, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return fmt.Errorf("%w: Worker readiness: %w", infra.ErrPending, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: Worker registration readiness returned %d", infra.ErrPending, response.StatusCode)
	}
	return nil
}

func (l *LiveServing) workerIdentity(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	if err := l.probeWorker(ctx, s); err != nil {
		return infra.ResourceRef{}, err
	}
	worker, err := l.workerContainer(ctx, s)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	if worker == nil {
		return infra.ResourceRef{}, fmt.Errorf("%w: Worker disappeared", infra.ErrPending)
	}
	name, _ := l.generation(s)
	return infra.ResourceRef{Boundary: l.ProjectNetwork(s.ProjectID), Name: name + "-worker", UID: worker.ID, Generation: s.Lease.Generation}, nil
}

// StopWorker reconciles exact ownership before every uncertain stop or removal.
func (l *LiveServing) StopWorker(ctx context.Context, s deployments.Snapshot) error {
	worker, err := l.workerContainer(ctx, s)
	if err != nil || worker == nil {
		return err
	}
	if worker.State.Running {
		if err := l.API().StopContainer(ctx, worker.ID, deployments.WorkerStopGrace); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("%w: Worker container is draining: %w", infra.ErrPending, err)
			}
			return err
		}
		return fmt.Errorf("%w: Worker exit needs confirmation", infra.ErrPending)
	}
	if err := l.API().RemoveContainer(ctx, worker.ID); err != nil {
		return err
	}
	return fmt.Errorf("%w: Worker removal needs confirmation", infra.ErrPending)
}
