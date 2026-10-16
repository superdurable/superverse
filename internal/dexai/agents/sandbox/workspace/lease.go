// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
)

// Lease is the Sandbox capability a Session accepted. The Session persists it and
// replays it to the transport; no Flow is consulted while it is usable. It
// carries no expiry: the Session owns the instance and keeps the lease until it
// reclaims the instance.
type Lease struct {
	RequestID  string         `json:"requestId"` // Sandbox instance identity
	Binding    Binding        `json:"binding"`
	Repository git.Repository `json:"repository"`
}

// Validate checks a lease before the Session accepts it.
func (lease Lease) Validate() error {
	if lease.RequestID == "" {
		return errors.New("workspace lease request ID is missing")
	}
	return lease.Binding.validate()
}

var (
	dnsLabelPattern     = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	canonicalUIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
)

// Binding names one admitted Sandbox by provider identities only. Its
// addresses are resolved from them by the SandboxAddresses of the runtime the
// server was built with, so no caller can route tools to an arbitrary
// endpoint. FlowID is the FlowID of the Project's Session, the root of the
// execution IDs. Binding is part of the persisted Lease.
type Binding struct {
	ProjectID          string `json:"projectId"`
	FlowID             string `json:"flowId"`
	Generation         int64  `json:"generation"`
	SandboxBoundary    string `json:"sandboxNamespace"`
	SandboxServiceName string `json:"sandboxServiceName"`
	SandboxUID         string `json:"sandboxUid"`
	WorkspaceID        string `json:"workspaceId"`
}

// validate checks a persisted binding before any transport is dialed for it.
func (binding Binding) validate() error {
	if binding.Generation < 1 {
		return errors.New("sandbox generation must be positive")
	}
	for name, value := range map[string]string{
		"project ID":   binding.ProjectID,
		"Flow ID":      binding.FlowID,
		"Sandbox UID":  binding.SandboxUID,
		"workspace ID": binding.WorkspaceID,
	} {
		if !canonicalUIDPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	for name, value := range map[string]string{
		"Sandbox namespace": binding.SandboxBoundary,
		"Sandbox Service":   binding.SandboxServiceName,
	} {
		if len(value) > 63 || !dnsLabelPattern.MatchString(value) {
			return fmt.Errorf("%s is not a DNS label", name)
		}
	}
	return nil
}

// SandboxAddresses resolves the host:port addresses through which the server
// reaches an admitted Sandbox's sandboxd: its process service (gRPC, port
// SandboxdProcessPort) and its filesystem service (HTTP, port
// SandboxdFilesystemPort). The runtime that provisioned the Sandbox resolves
// them from the identities the Binding records, for every new connection, so
// no caller routes tools to an arbitrary endpoint. An error means the Sandbox
// cannot be reached now.
type SandboxAddresses func(ctx context.Context, binding Binding) (process, filesystem string, err error)

// The ports sandboxd serves in every Sandbox.
const (
	SandboxdFilesystemPort = 8080
	SandboxdProcessPort    = 9090
)
