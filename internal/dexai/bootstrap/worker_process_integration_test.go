// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// TestPlatformPushWorkerProcess hosts the real production assembly for a transport-gated Session test.
func TestPlatformPushWorkerProcess(t *testing.T) {
	port := os.Getenv("SUPERVERSE_TEST_PUSH_WORKER_PORT")
	if port == "" {
		t.Skip("helper process for the held-Preview race; launched by TestSessionRemoteHeldPreviewBranchHTTPDexIntegration")
	}
	paths := strings.Split(os.Getenv("SUPERVERSE_CONFIG_FILE"), ",")
	settings, err := serverconfig.Load(paths...)
	if err != nil {
		t.Fatal(err)
	}
	if err = settings.Validate(); err != nil {
		t.Fatal(err)
	}
	if settings.Native.Instance != os.Getenv("SUPERVERSE_TEST_NATIVE_INSTANCE") || settings.Native.RunID != os.Getenv("SUPERVERSE_TEST_NATIVE_RUN_ID") || settings.Dex.ServerAddress != os.Getenv("SUPERVERSE_TEST_DEX_ADDRESS") {
		t.Fatal("push Worker effective config does not belong to the selected Native lane and run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker, closeResources, err := ComposeWorkerForTest(ctx, settings, dex.WorkerOptions{BindAddress: net.JoinHostPort("0.0.0.0", port), WorkerTarget: dex.WorkerTarget{Address: net.JoinHostPort("host.docker.internal", port)}}, filepath.Join(settings.Native.StateDir, "runs", settings.Native.RunID, "superverse-server"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := closeResources(); err != nil {
			t.Error(err)
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- worker.Start() }()
	startup := time.Now().Add(2 * time.Minute)
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(startup) {
			t.Fatal("test-process platform push Worker did not listen")
		}
		select {
		case err := <-exited:
			t.Fatalf("test-process Worker exited: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	fmt.Println("PULLPLATFORM_PUSH_WORKER_READY")
	// The parent owns stdin; EOF drains before process cleanup and releases the owner lock.
	_, err = io.Copy(io.Discard, os.Stdin)
	if err != nil {
		t.Error(err)
	}
	drain, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := worker.Stop(drain); err != nil {
		t.Error(err)
	}
	cancel()
}

// TestPlatformPushWorkerCompositionIntegration invokes actual TeamSpace handlers
// through the same production assembly that the held-Preview child process uses.
// It needs no generated application or LLM connector.
func TestPlatformPushWorkerCompositionIntegration(t *testing.T) {
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		t.Fatal("host production assembly requires the Native lane")
	}
	settings, err := serverconfig.Load(strings.Split(os.Getenv("SUPERVERSE_CONFIG_FILE"), ",")...)
	if err != nil {
		t.Fatal(err)
	}
	if err = settings.Validate(); err != nil {
		t.Fatal(err)
	}
	if settings.Native.Instance != os.Getenv("SUPERVERSE_TEST_NATIVE_INSTANCE") || settings.Native.RunID != os.Getenv("SUPERVERSE_TEST_NATIVE_RUN_ID") || settings.Dex.ServerAddress != dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS") {
		t.Fatal("composition config must belong to the selected Native instance and run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	port := dexflowtest.FreePortForTest(ctx, t)
	options := dex.WorkerOptions{BindAddress: net.JoinHostPort("0.0.0.0", port), WorkerTarget: dex.WorkerTarget{Address: net.JoinHostPort("host.docker.internal", port)}}
	worker, closeResources, err := ComposeWorkerForTest(ctx, settings, options, filepath.Join(settings.Native.StateDir, "runs", settings.Native.RunID, "superverse-server"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeResources(); err != nil {
			t.Error(err)
		}
	})
	exited := make(chan error, 1)
	go func() { exited <- worker.Start() }()
	t.Cleanup(func() {
		drain, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := worker.Stop(drain); err != nil {
			t.Error(err)
		}
		select {
		case err := <-exited:
			if err != nil {
				t.Error(err)
			}
		case <-drain.Done():
			t.Error("composed push Worker did not stop")
		}
	})
	for {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second)
		if err == nil {
			_ = connection.Close()
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("composed Worker exited before listening: %v", err)
		case <-ctx.Done():
			t.Fatal("composed Worker failed to listen")
		case <-time.After(20 * time.Millisecond):
		}
	}
	flow := &teamspaces.TeamSpaceFlow{}
	client := &teamspaces.TeamSpaceFlowClient{Dex: dexflowtest.ClientForTest(t, settings.Dex.ServerAddress, worker.WorkerTarget(), flow), Flow: flow}
	id := flowid.RandomID(8)
	actor := teamspaces.Actor{AccountID: "pullplatform-" + id, PlatformAdmin: true}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		if _, err := client.RequestDeletion(cleanup, id, teamspaces.DeletionRequest{Actor: actor, ID: "composition-cleanup"}); err != nil {
			t.Errorf("delete composed TeamSpace: %s", dexflowtest.ErrorCategory(err))
			return
		}
		if _, err := client.Dex.WaitForFlow(cleanup, flowid.TeamSpaceFlowID(id), dex.WaitForFlowOptions{}); err != nil {
			t.Errorf("complete composed TeamSpace: %s", dexflowtest.ErrorCategory(err))
		}
	})
	start := teamspaces.Start{Name: "Real composed Worker", CreatorAccountID: actor.AccountID}
	start.Identity.RequestID = flowid.Fingerprint(start)
	start.Identity.Fingerprint, start.Identity.ActorAccountID = start.Identity.RequestID, actor.AccountID
	if err := client.Create(ctx, id, start); err != nil {
		t.Fatalf("start composed TeamSpace: %s", dexflowtest.ErrorCategory(err))
	}
	renamed, err := client.Rename(ctx, id, teamspaces.Renaming{Actor: actor, ExpectedRevision: 1, Name: "Real composed Worker renamed"})
	if err != nil {
		t.Fatalf("invoke composed Worker: %s", dexflowtest.ErrorCategory(err))
	}
	if renamed.Name != "Real composed Worker renamed" || renamed.Revision != 2 {
		t.Fatal("composed handler did not persist its authoritative TeamSpace update")
	}
	t.Logf("FlowID=%s invoked the actual production assembly through an isolated push target and reached revision %d", flowid.TeamSpaceFlowID(id), renamed.Revision)
}
