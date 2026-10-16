// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"golang.org/x/mod/semver"
)

// The published catalog lists each released connector at its latest
// release, never a release candidate. A TeamSpace creates connections from
// it; a connector the catalog does not list yet is configured from a Project
// that declares it.

// CatalogEntry is one released connector of the catalog.
type CatalogEntry struct {
	ConnectorID, DisplayName, Description string
	// ModulePath is the connector's Go module at Version.
	ModulePath, Version, VersionLine string
	// LogoSVG is the logo the release publishes, which only WithLogos fills.
	LogoSVG string
	// directory is the connector's directory in the library.
	directory string
}

// release names the entry's release in the library.
func (entry CatalogEntry) release() string { return entry.directory + "@" + entry.Version }

// Module is the entry's release.
func (entry CatalogEntry) Module() Module {
	return Module{ConnectorID: entry.ConnectorID, ModulePath: entry.ModulePath, ModuleVersion: entry.Version}
}

// Catalog reads the released connectors from the server replica's copy of
// the published catalog, which the composition root keeps warm, and fetches
// release logos through its HTTP client, which the caller owns.
type Catalog struct {
	httpClient *http.Client
	published  *connectorlibrary.PublishedCatalog
	// sourceURL is where the library's source is read at a release tag.
	sourceURL string
	mutex     sync.Mutex
	// logos holds each release's logo by CatalogEntry.release, empty for none.
	logos map[string]string
}

// NewCatalog reads the catalog from published, and the logos of
// connectorlibrary.PublishedSourceURL with httpClient.
func NewCatalog(httpClient *http.Client, published *connectorlibrary.PublishedCatalog) *Catalog {
	return &Catalog{httpClient: httpClient, published: published, sourceURL: connectorlibrary.PublishedSourceURL, logos: map[string]string{}}
}

// Entries returns the released connectors; a copy older than
// connectorlibrary.CatalogLifetime is fetched again first.
func (catalog *Catalog) Entries(ctx context.Context) ([]CatalogEntry, error) {
	published, err := catalog.published.Fresh(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]CatalogEntry, 0, len(published.Entries))
	for _, connector := range published.Entries {
		entries = append(entries, CatalogEntry{
			ConnectorID: connector.ID, DisplayName: strings.TrimSpace(connector.Name), Description: strings.TrimSpace(connector.Description),
			ModulePath: connectorlibrary.CatalogModulePath(connector.Directory, connector.Version), Version: connector.Version, VersionLine: VersionLine(connector.Version),
			directory: connector.Directory,
		})
	}
	return entries, nil
}

// LatestRelease returns the catalog's release of a connector's version line.
func (catalog *Catalog) LatestRelease(ctx context.Context, connectorID, versionLine string) (CatalogEntry, bool, error) {
	entries, err := catalog.Entries(ctx)
	if err != nil {
		return CatalogEntry{}, false, err
	}
	for _, entry := range entries {
		if entry.ConnectorID == connectorID && entry.VersionLine == versionLine {
			return entry, true, nil
		}
	}
	return CatalogEntry{}, false, nil
}

// VersionLine is a release's or release candidate's major version, the line
// of a TeamSpace connection: v0.3.0-rc.2, v0.3.0 and v0.4.1 are all v0.
func VersionLine(version string) string { return semver.Major(version) }

// IsNewerVersion reports whether candidate replaces current as the version
// whose declarations refresh a connection of their line: a release replaces
// any older version, and a release candidate only a release candidate, while
// the line has no release.
func IsNewerVersion(current, candidate string) bool {
	if semver.Prerelease(candidate) != "" && semver.Prerelease(current) == "" {
		return false
	}
	return semver.Compare(candidate, current) > 0
}
