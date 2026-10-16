// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repohost

import (
	"regexp"
	"strings"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
)

// managedMarkerPrefix starts the description that marks a repository as owned
// by one Superverse Project setup. The host stores the description with the
// repository, so the marker format is fixed.
const managedMarkerPrefix = "superverse-managed:v2:"

// nameSlugLength is the most characters of a Project's name that its
// repository's name carries. The slug only lets a person recognize the Project
// in a list of repositories; the Project ID keeps the name unique. Eight
// characters keep the longest name (the longest prefix, the slug and a Project
// ID of at most 32 characters) at 53 of the 100 characters a repository name
// may have.
const nameSlugLength = 8

// outsideNameSlug matches a run of characters that the slug does not keep.
var outsideNameSlug = regexp.MustCompile(`[^a-z0-9]+`)

// SetupIdentity identifies one setup of a Project for the repository it
// creates. ProjectName is the Project's immutable name, which the repository's
// name carries as a slug.
type SetupIdentity struct {
	ProjectID, ProjectName, SetupID string
}

// ManagedName is the name of the private repository that a Project's setup
// creates: <prefix>-<name slug>-<projectID>, or <prefix>-<projectID> when the
// Project's name leaves no slug. The setup alone derives it from the Project's
// name; every later operation uses the repository name that the Project's
// binding recorded.
func ManagedName(prefix deploymentnames.RepositoryPrefix, projectName, projectID string) string {
	if slug := nameSlug(projectName); slug != "" {
		return prefix.String() + "-" + slug + "-" + projectID
	}
	return prefix.String() + "-" + projectID
}

// nameSlug is the lowercase Project name with every run of characters outside
// a-z and 0-9 turned into one '-', trimmed of '-' at both ends, cut to
// nameSlugLength characters and trimmed of a trailing '-' again.
func nameSlug(projectName string) string {
	slug := strings.Trim(outsideNameSlug.ReplaceAllString(strings.ToLower(projectName), "-"), "-")
	return strings.TrimRight(slug[:min(len(slug), nameSlugLength)], "-")
}

// ManagedMarker is the ownership marker in the description of the repository
// that one setup of a Project created. Archival checks it before it touches a
// repository.
func ManagedMarker(environment deploymentnames.Environment, projectID, setupID string) string {
	return managedMarkerPrefix + environment.String() + ":" + projectID + ":" + setupID
}
