// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapitest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// forwarding is the line in which kubectl names the local end of a forward.
var forwarding = regexp.MustCompile(`^Forwarding from (127\.0\.0\.1:[0-9]+) -> [0-9]+$`)

// ServiceForwards reaches the cluster's Services from the test process, which
// runs on the host: each address <service>.<namespace>.svc.cluster.local:<port>
// is dialed through a kubectl port-forward of that Service with the operator's
// kubeconfig. A forward that stopped is opened again once. The test's cleanup
// stops every forward. The platform server, which runs in the cluster, dials
// the same addresses itself.
func (cluster KindCluster) ServiceForwards(t testing.TB) func(ctx context.Context, address string) (net.Conn, error) {
	t.Helper()
	var mu sync.Mutex
	forwards := map[string]string{}
	var processes []*exec.Cmd
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, process := range processes {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	})
	open := func(address string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if local, open := forwards[address]; open {
			return local, nil
		}
		process, local, err := cluster.forward(address)
		if err != nil {
			return "", err
		}
		processes = append(processes, process)
		forwards[address] = local
		return local, nil
	}
	return func(ctx context.Context, address string) (net.Conn, error) {
		var dialer net.Dialer
		for attempt := 0; ; attempt++ {
			local, err := open(address)
			if err != nil {
				return nil, err
			}
			connection, err := dialer.DialContext(ctx, "tcp", local)
			if err == nil || attempt == 1 {
				return connection, err
			}
			mu.Lock()
			delete(forwards, address)
			mu.Unlock()
		}
	}
}

// forward starts the port-forward of the Service at address and returns the
// process and its local address once kubectl announces it.
func (cluster KindCluster) forward(address string) (*exec.Cmd, string, error) {
	host, port, err := net.SplitHostPort(address)
	service, namespace, inCluster := strings.Cut(strings.TrimSuffix(host, ".svc.cluster.local"), ".")
	if err != nil || !inCluster || strings.Contains(namespace, ".") {
		return nil, "", fmt.Errorf("%s is not the address of a Service in the cluster", address)
	}
	process := exec.Command("kubectl", "--kubeconfig", cluster.KubeconfigForTest, "--context", "kind-superverse-v2", "--request-timeout=15s", //nolint:gosec,noctx // the forward lives until the test's cleanup kills it
		"-n", namespace, "port-forward", "service/"+service, ":"+port, "--address", "127.0.0.1")
	output, err := process.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err = process.Start(); err != nil {
		return nil, "", err
	}
	announced := make(chan string, 1)
	go func() {
		// Every line is read, so kubectl never blocks on its output.
		lines := bufio.NewScanner(output)
		for lines.Scan() {
			if match := forwarding.FindStringSubmatch(lines.Text()); match != nil {
				select {
				case announced <- match[1]:
				default:
				}
			}
		}
	}()
	select {
	case local := <-announced:
		return process, local, nil
	case <-time.After(30 * time.Second):
		_ = process.Process.Kill()
		_ = process.Wait()
		return nil, "", errors.New("kubectl did not forward " + address)
	}
}
