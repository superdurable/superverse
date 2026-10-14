// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package postgres

// AttributeStore is a platform Dex Attribute Store of the read model: the
// store that one Flow type selects where it starts, which writes the Flow's
// synced Attributes into the row of the table that its row key splits from
// the FlowID. Every environment's Dex configuration names the same stores
// (infra-as-code/scripts/local_platform.py for the local lanes).
type AttributeStore struct{ name string }

// The read model's stores. ProjectFlow's and ProjectSetupFlow's write
// different columns of one projects row.
var (
	TeamSpaceStore    = AttributeStore{name: "team_spaces"}
	ProjectStore      = AttributeStore{name: "projects"}
	ProjectSetupStore = AttributeStore{name: "project_setups"}
	ReleaseStore      = AttributeStore{name: "releases"}
	DeploymentStore   = AttributeStore{name: "deployments"}
)

// Names returns the AttributeStoreNames that select the store where its
// Flow starts. Dex refuses a start that names a store it does not configure.
func (s AttributeStore) Names() []string { return []string{s.name} }
