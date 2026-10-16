// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deploymentnames

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/superdurable/superversev2/internal/comm"
)

//go:embed environments.json
var catalogJSON []byte

// Names holds the catalog entries Go reads. The catalog has more keys, which
// Terraform, Helm and the infra scripts read directly.
type Names struct {
	MaxProjectIDLength int    `json:"max_project_id_length"`
	Product            string `json:"product"`
	WorkloadPrefix     string `json:"workload_prefix"`
	PlatformNamespace  string `json:"platform_namespace"`
	Cluster            string `json:"cluster"`
	ECRPrefix          string `json:"ecr_prefix"`
	BuildProject       string `json:"build_project"`
	ArtifactPrefix     string `json:"artifact_prefix"`
	IAMWorkloadPath    string `json:"iam_workload_path"`
	DNSDomain          string `json:"dns_domain"`
}

// catalog holds the entry of every declared environment. The file and the
// declared environments must name the same set, so an entry of an undeclared
// environment or a declared environment without an entry panics at start.
var catalog = func() map[Environment]Names {
	var entries map[string]Names
	comm.MustDo(json.Unmarshal(catalogJSON, &entries))
	result := make(map[Environment]Names, len(entries))
	for name, names := range entries {
		environment, err := ParseEnvironment(name)
		if err != nil {
			panic(fmt.Errorf("environments.json: %w", err))
		}
		result[environment] = names
	}
	for _, environment := range environments {
		if _, isCatalogued := result[environment]; !isCatalogued {
			panic("environments.json has no entry for the " + environment.name + " environment")
		}
	}
	return result
}()

// For returns the names of an environment. Only the zero Environment has none.
func For(environment Environment) (Names, error) {
	names, isCatalogued := catalog[environment]
	if !isCatalogued {
		return Names{}, &unknownEnvironmentError{name: environment.name}
	}
	return names, nil
}

// MustFor is for components whose environment was already validated.
// An invalid internal composition fails closed; there is no legacy-name fallback.
func MustFor(environment Environment) Names { return comm.Must(For(environment)) }

// ProjectNamespace holds a project's Live Dex, storage and serving generations.
func (n Names) ProjectNamespace(project string) string {
	return n.WorkloadPrefix + "-project-" + project
}

// SessionNamespace holds the coding Sandbox of a Project's Session: the
// isolation boundary of its workspace.
func (n Names) SessionNamespace(project string) string {
	return n.WorkloadPrefix + "-session-" + project
}
