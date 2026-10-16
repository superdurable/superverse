// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"net/http"
	"strings"
	"sync"

	"google.golang.org/grpc"
	processv1 "sigs.k8s.io/agent-sandbox/packages/sandboxd/spec/process/v1"
)

// maximumProcessOutputBytes is the hard ceiling of one process's combined
// stdout and stderr. A start request may ask for less, never for more.
const maximumProcessOutputBytes int64 = 16 << 20

// SandboxClientConfig names one provisioned Sandbox: the sandboxd HTTP origin
// before /v1 and a connection to its process service.
type SandboxClientConfig struct {
	FilesystemBaseURL string
	HTTPClient        *http.Client // nil is http.DefaultClient
	ProcessConnection grpc.ClientConnInterface
	// ModuleRedirects are the lane's redirects of the tool environment.
	ModuleRedirects []ModuleRedirect
}

// SandboxClient speaks the sandboxd file and process protocols of exactly one
// Sandbox, and so of exactly one workspace.
type SandboxClient struct {
	baseURL    string
	httpClient *http.Client
	process    processv1.ProcessServiceClient
	redirects  []ModuleRedirect

	mu sync.Mutex
	// active reserves each running ExecutionID for the stream that owns it.
	active map[ExecutionID]*processStream
}

func NewSandboxClient(config SandboxClientConfig) *SandboxClient {
	httpClient := *http.DefaultClient
	if config.HTTPClient != nil {
		httpClient = *config.HTTPClient
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &SandboxClient{
		baseURL:    strings.TrimRight(config.FilesystemBaseURL, "/"),
		httpClient: &httpClient,
		process:    processv1.NewProcessServiceClient(config.ProcessConnection),
		active:     make(map[ExecutionID]*processStream),
		redirects:  config.ModuleRedirects,
	}
}
