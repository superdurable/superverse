// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package engineindex

// Name is a search index the Dex engine keeps for every Flow beside the
// application's own; no application Attribute owns one.
type Name string

// The engine's search indexes.
const (
	FlowType     Name = "FlowType"
	ParentFlowID Name = "DexParentFlowID"
	// ActiveStepTypes lists an open run's active Step types, for a Flow that indexes them.
	ActiveStepTypes Name = "ActiveStepTypes"
	// WorkQueuePermissions lists the work queues a run's Actions may use: the Dex
	// SDK's dex.WorkQueuePermissionsIndexKey, spelled here because this package
	// imports only the standard library.
	WorkQueuePermissions Name = "DexWorkQueuePermissions"
)

// Is reports whether name is one of the engine's search indexes.
func Is(name string) bool {
	switch Name(name) {
	case FlowType, ParentFlowID, ActiveStepTypes, WorkQueuePermissions:
		return true
	}
	return false
}
