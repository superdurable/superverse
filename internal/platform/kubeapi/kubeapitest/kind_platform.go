// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapitest

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"

	"github.com/superdurable/superversev2/internal/platform/kubeapi"
)

// tokenDuration bounds the platform ServiceAccount token of one test.
const tokenDuration = "15m"

// KindClusterRoleBindingForTest is the kind of the cluster-wide binding with which a
// test gives a probe ServiceAccount a ClusterRole. No provider manages one, so
// the test posts it to its collection path itself.
const KindClusterRoleBindingForTest = "ClusterRoleBinding"

// KindCluster is the isolated Local Kind cluster of a test, reached as the
// platform server.
type KindCluster struct {
	// KubeconfigForTest is the operator's kubeconfig file of the cluster.
	KubeconfigForTest string
	// ServerForTest is the URL of the Kubernetes API server.
	ServerForTest string
	// CAForTest is the PEM certificate authority of the API server.
	CAForTest []byte
	// TokenFileForTest holds a short-lived token of the platform server's
	// ServiceAccount, readable only by its owner.
	TokenFileForTest string
}

// KindPlatform reads the kubeconfig in SUPERVERSE_TEST_KIND_KUBECONFIG, which
// must name only the isolated kind-superverse-v2 cluster, and mints a token of
// the platform server's ServiceAccount into the test's temporary directory. A
// missing or foreign cluster fails the test.
func KindPlatform(t testing.TB) KindCluster {
	t.Helper()
	kubeconfig := os.Getenv("SUPERVERSE_TEST_KIND_KUBECONFIG")
	if kubeconfig == "" {
		t.Fatal("requires the isolated V2 Kind kubeconfig in SUPERVERSE_TEST_KIND_KUBECONFIG")
	}
	data, err := os.ReadFile(kubeconfig) //nolint:gosec // the operator-supplied test kubeconfig
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		CurrentContext string `json:"current-context"`
		Clusters       []struct {
			Cluster struct {
				Server string `json:"server"`
				CA     string `json:"certificate-authority-data"`
			} `json:"cluster"`
		} `json:"clusters"`
	}
	if err = yaml.Unmarshal(data, &config); err != nil || config.CurrentContext != "kind-superverse-v2" || len(config.Clusters) != 1 {
		t.Fatal("not an isolated V2 Kind kubeconfig")
	}
	ca, err := base64.StdEncoding.DecodeString(config.Clusters[0].Cluster.CA)
	if err != nil {
		t.Fatal(err)
	}
	token, err := exec.CommandContext(t.Context(), "kubectl", "--kubeconfig", kubeconfig, "--context", "kind-superverse-v2", //nolint:gosec // fixed kubectl arguments
		"-n", "superverse-v2-system", "create", "token", "superverse-server", "--duration="+tokenDuration).Output()
	if err != nil {
		t.Fatal("cannot obtain a token of the V2 platform ServiceAccount")
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err = os.WriteFile(tokenFile, token, 0o600); err != nil { //nolint:gosec // a file in the test's own temporary directory
		t.Fatal(err)
	}
	return KindCluster{KubeconfigForTest: kubeconfig, ServerForTest: config.Clusters[0].Cluster.Server, CAForTest: ca, TokenFileForTest: tokenFile}
}

// ServerAPI is the Kubernetes API of the cluster as the platform server: the
// cluster's server and CA with the platform server's token.
func (cluster KindCluster) ServerAPI(t testing.TB) *kubeapi.Client {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cluster.CAForTest) {
		t.Fatal("the Kind cluster's CA is not PEM")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}}
	return &kubeapi.Client{ServerURL: cluster.ServerForTest, TokenFile: cluster.TokenFileForTest, HTTP: &http.Client{Transport: transport}}
}
