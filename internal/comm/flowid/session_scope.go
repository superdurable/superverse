// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import "fmt"

// NamespaceNames names the Kubernetes namespace of a Project's Sandbox, the
// isolation boundary of its Session's workspace; deploymentnames.Names
// implements it for each environment.
type NamespaceNames interface {
	SessionNamespace(project string) string
}

// ValidateSessionScope checks a Project ID before a Sandbox instance or a
// Preview is admitted under it: lowercase letters and digits, and a Sandbox
// namespace within the 63-byte Kubernetes limit. An instance admitted past
// this check could never start its Sandbox.
func ValidateSessionScope(names NamespaceNames, projectID string) error {
	if !IsLowerAlphanumeric(projectID) || len(names.SessionNamespace(projectID)) > 63 {
		return fmt.Errorf("%w: the project ID must contain lowercase letters or digits, and its Sandbox namespace at most 63", ErrInvalidIdentity)
	}
	return nil
}
