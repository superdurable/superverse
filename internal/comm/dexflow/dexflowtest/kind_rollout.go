// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflowtest

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

type platformRolloutPod struct {
	Metadata struct {
		Name, UID                  string
		DeletionTimestamp          *time.Time
		DeletionGracePeriodSeconds *int64
	}
	Status struct {
		Conditions []struct {
			Type, Status       string
			LastTransitionTime time.Time
		}
	}
}

func platformPods(ctx context.Context, t testing.TB) []platformRolloutPod {
	t.Helper()
	out, err := kindRolloutCommand(ctx, t, "-n", "superverse-v2-system", "get", "pods", "-l", "app=superverse-v2-server", "-o", "json")
	if err != nil {
		t.Fatalf("read platform rollout Pods: %s", out)
	}
	var result struct{ Items []platformRolloutPod }
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	return result.Items
}

func platformReadyAt(pod platformRolloutPod) time.Time {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return condition.LastTransitionTime
		}
	}
	return time.Time{}
}

// rollKindServer requires actual registration readiness of the surge Pod before
// the old Pod enters termination, then observes the old process's bounded drain.
func rollKindServer(ctx context.Context, t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, podDeletionWait)
	defer cancel()
	old := platformPods(ctx, t)
	if len(old) != 1 || platformReadyAt(old[0]).IsZero() {
		t.Fatal("rollout requires one ready platform server")
	}
	if out, err := kindRolloutCommand(ctx, t, "-n", "superverse-v2-system", "rollout", "restart", "deployment/superverse-v2-server"); err != nil {
		t.Fatalf("platform rolling update: %s", out)
	}
	var ready time.Time
	var terminated *time.Time
	var replacement string
	for {
		present := false
		for _, pod := range platformPods(ctx, t) {
			if pod.Metadata.UID == old[0].Metadata.UID {
				present = true
				if pod.Metadata.DeletionTimestamp != nil {
					// Kubernetes records the end of the grace period, not the
					// start of termination, in deletionTimestamp.
					if pod.Metadata.DeletionGracePeriodSeconds == nil {
						t.Fatal("terminating Pod has no deletion grace period")
					}
					started := pod.Metadata.DeletionTimestamp.Add(-time.Duration(*pod.Metadata.DeletionGracePeriodSeconds) * time.Second)
					if terminated == nil || started.Before(*terminated) {
						terminated = &started
					}
				}
				continue
			}
			if observed := platformReadyAt(pod); !observed.IsZero() {
				ready = observed
				replacement = pod.Metadata.Name
			}
		}
		if terminated != nil && ready.IsZero() {
			t.Fatal("old Pod entered termination before any ready replacement")
		}
		if !present {
			if terminated == nil || ready.IsZero() || ready.After(*terminated) {
				t.Fatal("replacement registration readiness did not precede old Pod termination")
			}
			t.Logf("rollout old=%s replacement=%s ready=%s oldTermination=%s", old[0].Metadata.Name, replacement, ready.Format(time.RFC3339Nano), terminated.Format(time.RFC3339Nano))
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("platform rollout exceeded its three-minute bound")
		case <-time.After(time.Second):
		}
	}
}

func kindRolloutCommand(ctx context.Context, t testing.TB, args ...string) (string, error) {
	t.Helper()
	command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG"), "--context", "kind-superverse-v2", "--request-timeout=15s"}, args...)...) //nolint:gosec // the selected lane's owned cluster
	output, err := command.CombinedOutput()
	return string(output), err
}
