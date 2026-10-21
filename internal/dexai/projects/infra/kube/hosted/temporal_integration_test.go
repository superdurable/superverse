// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestHostedTemporalNamespaceIntegration uses an isolated real Temporal frontend.
func TestHostedTemporalNamespaceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	version, err := exec.CommandContext(ctx, "temporal", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("Temporal CLI required: %v: %s", err, version)
	}
	t.Logf("Temporal CLI: %s; frontend: %s", version, address)
	log, err := os.Create(filepath.Join(t.TempDir(), "temporal.log"))
	if err != nil {
		t.Fatal(err)
	}
	process := exec.CommandContext(ctx, "temporal", "server", "start-dev", "--ip", "127.0.0.1", "--port", port, "--headless")
	process.Stdout, process.Stderr = log, log
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = process.Wait(); _ = log.Close() }()
	connection, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := workflowservice.NewWorkflowServiceClient(connection)
	ready := time.Now().Add(30 * time.Second)
	for {
		probe, stop := context.WithTimeout(ctx, time.Second)
		_, err = client.GetSystemInfo(probe, &workflowservice.GetSystemInfoRequest{})
		stop()
		if err == nil {
			break
		}
		if time.Now().After(ready) {
			t.Fatalf("Temporal frontend did not start: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	h := &CloudAccount{config: Config{TemporalAddress: address, TemporalRetentionDays: 30}, temporal: connection}
	k := &kube.Infrastructure{Environment: deploymentnames.EnvironmentTest}
	project := uuid.NewString()
	storage := infra.StorageRefs{Boundary: infra.ResourceRef{UID: uuid.NewString()}}
	ensure := func() error {
		for {
			access, credential, err := h.EnsureTemporal(ctx, k, project, storage, "", infra.ResourceRef{})
			if errors.Is(err, infra.ErrPending) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			if err != nil {
				return err
			}
			if access.HostPort != address || access.Namespace != h.TemporalNamespace(k, project) || credential.Name != access.Namespace || credential.UID == "" {
				return errors.New("unexpected namespace access or credential")
			}
			return nil
		}
	}
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			if err := ensure(); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if t.Failed() {
		return
	}
	_, accepted, err := h.EnsureTemporal(ctx, k, project, storage, "", infra.ResourceRef{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.ReadTemporalAccess(ctx, k, project, storage, "", accepted); err != nil {
		t.Fatal(err)
	}
	foreign := storage
	foreign.Boundary.UID = uuid.NewString()
	if _, _, err = h.EnsureTemporal(ctx, k, project, foreign, "", infra.ResourceRef{}); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("foreign ownership admitted: %v", err)
	}
	if err = h.destroyTemporal(ctx, k, project, foreign.Boundary.UID, accepted); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("foreign delete admitted: %v", err)
	}
	wrong := accepted
	wrong.UID = uuid.NewString()
	if _, _, err = h.EnsureTemporal(ctx, k, project, storage, "", wrong); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("replacement provision admitted: %v", err)
	}
	if _, err = h.ReadTemporalAccess(ctx, k, project, storage, "", wrong); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("replacement read admitted: %v", err)
	}
	if err = h.destroyTemporal(ctx, k, project, storage.Boundary.UID, wrong); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("replacement delete admitted: %v", err)
	}
	if err = h.destroyTemporal(ctx, k, project, storage.Boundary.UID, accepted); err != nil {
		t.Fatal(err)
	}
	if err = h.destroyTemporal(ctx, k, project, storage.Boundary.UID, accepted); err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.EnsureTemporal(ctx, k, project, storage, "", accepted); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("missing accepted namespace recreated: %v", err)
	}
	if _, err = h.ReadTemporalAccess(ctx, k, project, storage, "", infra.ResourceRef{}); err == nil {
		t.Fatal("deleted namespace is readable")
	}
}
