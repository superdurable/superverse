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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	sandboxkube "github.com/superdurable/superversev2/internal/dexai/agents/sandbox/kube"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	deploymentskube "github.com/superdurable/superversev2/internal/dexai/delivery/deployments/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	infraconnect "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/connect"
	infrahosted "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted"
)

// This file is the one place where the server touches Kubernetes: it builds the
// Kubernetes implementations of the project infrastructure, the Live serving
// and the Sandbox instances, and hands the rest of the composition the
// interfaces they implement. Nothing else in this package imports kubeapi.

// projectProviders are the runtime implementations that composition hands each
// consumer, as the interface the consumer uses. A role not used by the Studio
// Runs or a Flow never reaches it.
type projectProviders struct {
	storage     infra.Storage
	execution   infra.Execution
	grants      infra.Grants
	cleanup     infra.Cleanup
	redactor    infra.Redactor
	connections infra.Connections
	serving     deployments.Serving
	// sandboxAddresses resolves a Sandbox's sandboxd for the workspace
	// transport.
	sandboxAddresses workspace.SandboxAddresses
	// instances builds the Sandbox instance provider on the pooled workspace
	// transport, which exists only once the Session bootstrap built it.
	instances func(*workspace.Capabilities) sandbox.InstanceProvider
	// routes serve a gateway the public routes, where the runtime keeps them
	// itself; nil with Kubernetes.
	routes http.Handler
}

const serviceAccountDirectory = "/var/run/secrets/kubernetes.io/serviceaccount"

// newKubernetesProviders serves project Dex, storage and workloads, the Live
// applications and the Sandbox instances on the cluster, with a MinIO and a
// Temporal of their own per project in Local Kind and with the project cloud
// account elsewhere, whose credentials must belong to the configured account.
// The account is released through owned. The Backend is never a typed nil.
func newKubernetesProviders(ctx context.Context, config serverConfig, owned *resources) (*projectProviders, error) {
	api, err := newKubernetesAPI(inClusterKubernetesURL())
	if err != nil {
		return nil, err
	}
	routes := config.routes
	backend, err := newBackend(ctx, config, owned)
	if err != nil {
		return nil, err
	}
	infrastructure := &infrakube.Infrastructure{API: api, Environment: config.environment, DexImage: config.liveDexImage, Backend: backend}
	connections := &infraconnect.Connections{Infrastructure: infrastructure}
	serving := &deploymentskube.LiveServing{Infrastructure: infrastructure, Routes: routes}
	return &projectProviders{
		storage: infrastructure, execution: infrastructure, grants: infrastructure, cleanup: infrastructure, redactor: infrastructure,
		connections: connections, serving: serving,
		// A Sandbox's sandboxd is behind its headless Service in its namespace.
		sandboxAddresses: func(_ context.Context, binding workspace.Binding) (string, string, error) {
			host := kubeapi.ServiceHost(binding.SandboxBoundary, binding.SandboxServiceName)
			return net.JoinHostPort(host, strconv.Itoa(workspace.SandboxdProcessPort)), net.JoinHostPort(host, strconv.Itoa(workspace.SandboxdFilesystemPort)), nil
		},
		instances: func(workspaces *workspace.Capabilities) sandbox.InstanceProvider {
			return &sandboxkube.InstanceProvisioner{Infrastructure: infrastructure, ProfileName: config.sandboxProfileName, Routes: routes, Workspace: workspaces, Connections: connections}
		},
	}, nil
}

// newBackend selects the environment of the project infrastructure: the
// project's own MinIO and Temporal in Local Kind, the project cloud account
// elsewhere. It never returns a typed nil.
func newBackend(ctx context.Context, config serverConfig, owned *resources) (infrakube.Backend, error) {
	if config.localKind {
		backend := config.localImages
		return &backend, nil
	}
	if err := verifyAWSAccount(ctx, config.aws, config.hostedProjects.AccountID); err != nil {
		return nil, err
	}
	account, err := infrahosted.New(config.aws, config.hostedProjects)
	if err != nil {
		return nil, err
	}
	owned.add(account.Close)
	return account, nil
}

// verifyAWSAccount refuses to manage project cloud resources with credentials
// of any account other than the configured one.
func verifyAWSAccount(ctx context.Context, config aws.Config, accountID string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	caller, err := sts.NewFromConfig(config).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("verify hosted AWS account identity: %w", err)
	}
	if actual := aws.ToString(caller.Account); actual != accountID {
		return fmt.Errorf("hosted AWS credentials belong to account %s, not the configured %s", actual, accountID)
	}
	return nil
}

// inClusterKubernetesURL is the cluster API server that the kubelet announces
// to every Pod through its service environment; it is empty outside a Pod.
// It is the platform's injection, not a server setting.
func inClusterKubernetesURL() string {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	if host == "" {
		return ""
	}
	port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = "443"
	}
	return "https://" + net.JoinHostPort(host, port)
}

// newKubernetesAPI reaches the cluster API at serverURL from inside a Pod with
// its projected service account.
func newKubernetesAPI(serverURL string) (*kubeapi.Client, error) {
	if serverURL == "" {
		return nil, errors.New("runtime kubernetes requires an in-cluster KUBERNETES_SERVICE_HOST")
	}
	// kubeapi rereads the projected token for every call; it must exist now.
	tokenFile := serviceAccountDirectory + "/token"
	if _, err := os.ReadFile(tokenFile); err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	ca, err := os.ReadFile(serviceAccountDirectory + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read service account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid service account CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}}
	return &kubeapi.Client{ServerURL: serverURL, TokenFile: tokenFile, HTTP: &http.Client{Transport: transport}}, nil
}
