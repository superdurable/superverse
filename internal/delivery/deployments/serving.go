// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// Serving runs each Live deployment generation as its own application and
// service in the project's Live scope and moves the project's public Live route
// to the generation it activates. It is the runtime of ProjectDeployFlow, and
// the Agent's diagnostics read the application's environment and log through it.
// The storage, the Temporal, the Dex and the workload grants of the same scope
// are the roles of package infra, which ProjectDeployFlow holds beside it.
// Package kube implements it for Kubernetes and package docker for Docker.
//
// Every effect verifies the exact references the deployment accepted before it
// acts. infra.ErrPending means a resource is still converging.
type Serving interface {
	// EnsureGenerationService reconciles the generation's application Service,
	// which exists before its Pods, so readiness and the Live route bind a
	// stable identity.
	EnsureGenerationService(ctx context.Context, s Snapshot) (infra.ResourceRef, error)
	// ApplyApplication runs the generation's application from its immutable
	// image in the namespace the deployment accepted. environment holds the
	// grant's storage and project references, which may be secret.
	ApplyApplication(ctx context.Context, s Snapshot, environment map[string]string) (infra.ResourceRef, error)
	// VerifyReadiness reports the generation ready when its published
	// definitions, its Deployment of the accepted image and its Service are the
	// accepted ones, the application answers its health path and its Worker
	// accepts connections.
	VerifyReadiness(ctx context.Context, s Snapshot) (Readiness, error)
	// Activate moves the project's Live Worker endpoint and then its Live route
	// to the ready generation, makes its published Flow Definitions the ones the
	// project Dex serves, and succeeds once the public route answers the health
	// path. Every Live Flow targets the Worker endpoint, so the generation takes
	// over the open Flows that earlier generations started.
	Activate(ctx context.Context, s Snapshot) (Activation, error)
	// CleanupDeployment removes a generation that is not Project.CurrentLive,
	// which its Deployment Flow has proved, and the Live route it may still own.
	CleanupDeployment(ctx context.Context, s Snapshot) error
	// RemoveServing removes the Project's current Live generation, its route and
	// its Live Worker endpoint, for Live removal or Project deletion.
	RemoveServing(ctx context.Context, p projects.Project) error

	// ReadApplication reads the deployment's application once, for the Agent's
	// diagnostics. ErrApplicationChanged means the application is no longer the
	// one the deployment recorded.
	ReadApplication(ctx context.Context, deployment Snapshot) (LiveApplication, error)
}

// LiveApplication is one read of a Live application: the environment that
// redaction needs and the log both come from the application it read.
type LiveApplication interface {
	// Environment are the values of the application's environment that come from
	// Secrets, such as the workload grant, for the redaction of what the Agent
	// reads.
	Environment(ctx context.Context) ([]string, error)
	// Log reads the last lines of the application's log, whole lines only and not
	// yet redacted, with the state of its container, which explains an empty or
	// restarting log.
	Log(ctx context.Context, tailLines int) (ApplicationLog, error)
}

// ApplicationSettings are the fixed settings of a Live application
// generation's process, in order, which every runtime sets before the grant's
// environment: its HTTP port, its project Dex, the address its Worker binds
// and the one the project Dex dials, which is the Live Worker endpoint that
// Dex resolves itself (headless), so it follows the current generation, its
// blob cache and its public URL.
func ApplicationSettings(s Snapshot, workerTarget, publicURL string) [][2]string {
	return [][2]string{
		{"PORT", strconv.Itoa(s.Inputs.ApplicationPort)}, {"DEX_FLOW_SERVICE_ADDRESS", infra.ScopeAddress(infra.ComponentDex, infra.DexPort)},
		{"DEX_WORKER_BIND_ADDRESS", net.JoinHostPort("0.0.0.0", strconv.Itoa(infra.LiveWorkerPort))}, {"DEX_WORKER_TARGET", workerTarget}, {"DEX_WORKER_HEADLESS", "true"},
		{"DEX_BLOB_CACHE_DIR", "/tmp/dex-blobs"}, {"PUBLIC_BASE_URL", publicURL},
	}
}

// Activation is the serving state that Activate committed: the public URL, the
// exact active Flow Definition manifest version and the Live Worker endpoint.
type Activation struct {
	URL      string
	Manifest objectstore.ObjectRef
	// Worker is the project's Live Worker endpoint, which now selects the
	// generation.
	Worker infra.ResourceRef
}

// ApplicationLog is the end of the Live application container's log, with the
// container's state, which explains an empty or restarting log. Its JSON is the
// document the Agent's tool returns.
type ApplicationLog struct {
	State           string `json:"state"`
	Restarts        int64  `json:"restarts"`
	LastTermination string `json:"last_termination,omitempty"`
	Log             string `json:"log"`
}

// ErrApplicationChanged: the Live application changed while it was read, so the
// read answers about an object the deployment never recorded.
var ErrApplicationChanged = errors.New("the Live application changed while it was read; read again")
