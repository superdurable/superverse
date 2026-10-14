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
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"golang.org/x/mod/semver"
)

// The published catalog lists each released connector at its latest
// release, never a release candidate. A TeamSpace creates connections from
// it; a connector the catalog does not list yet is configured from a Project
// that declares it.

const (
	// catalogLifetime is how long a fetched catalog serves before the next
	// fetch.
	catalogLifetime = 5 * time.Minute
	// catalogLimit bounds the catalog document.
	catalogLimit = 1 << 20
	// maxCatalogEntries bounds the connectors one catalog lists.
	maxCatalogEntries = 512
)

var (
	catalogDirectory = regexp.MustCompile(`^connectors/[a-z0-9-]+(/[a-z0-9-]+)*$`)
	catalogVersion   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

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

type catalogDocument struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Connectors []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Directory   string `json:"directory"`
	} `json:"connectors"`
}

// Catalog fetches the published catalog through its HTTP client, which the
// caller owns, and keeps one copy for catalogLifetime. Concurrent misses may
// fetch twice; nobody waits behind another's fetch.
type Catalog struct {
	httpClient *http.Client
	url        string
	// sourceURL is where the library's source is read at a release tag.
	sourceURL string
	mutex     sync.Mutex
	entries   []CatalogEntry
	fetchedAt time.Time
	// logos holds each release's logo by CatalogEntry.release, empty for none.
	logos map[string]string
}

// NewCatalog reads connectorlibrary.PublishedCatalogURL, and the logos of
// connectorlibrary.PublishedSourceURL, with httpClient.
func NewCatalog(httpClient *http.Client) *Catalog {
	return &Catalog{httpClient: httpClient, url: connectorlibrary.PublishedCatalogURL, sourceURL: connectorlibrary.PublishedSourceURL, logos: map[string]string{}}
}

// Entries returns the released connectors.
func (catalog *Catalog) Entries(ctx context.Context) ([]CatalogEntry, error) {
	catalog.mutex.Lock()
	entries, fetchedAt := catalog.entries, catalog.fetchedAt
	catalog.mutex.Unlock()
	if entries != nil && time.Since(fetchedAt) < catalogLifetime {
		return entries, nil
	}
	entries, err := catalog.fetch(ctx)
	if err != nil {
		return nil, err
	}
	catalog.mutex.Lock()
	catalog.entries, catalog.fetchedAt = entries, time.Now()
	catalog.mutex.Unlock()
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

func (catalog *Catalog) fetch(ctx context.Context) ([]CatalogEntry, error) {
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, catalog.url, nil))
	response, err := catalog.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch the connector catalog: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	contents, err := readResponseBody(response.Body, catalogLimit)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch the connector catalog: HTTP %d", response.StatusCode)
	}
	if err != nil {
		return nil, fmt.Errorf("read the connector catalog: %w", err)
	}
	var document catalogDocument
	if err = yaml.Unmarshal(contents, &document); err != nil || document.APIVersion != "connectors.dex.dev/catalog/v1alpha1" || document.Kind != "ConnectorCatalog" ||
		len(document.Connectors) == 0 || len(document.Connectors) > maxCatalogEntries {
		return nil, errors.New("the connector catalog is not a supported catalog document")
	}
	entries := make([]CatalogEntry, 0, len(document.Connectors))
	for _, connector := range document.Connectors {
		if !connectorReleaseIDPattern.MatchString(connector.ID) || !catalogDirectory.MatchString(connector.Directory) || !catalogVersion.MatchString(connector.Version) {
			return nil, errors.New("the connector catalog lists an invalid release identity")
		}
		entries = append(entries, CatalogEntry{
			ConnectorID: connector.ID, DisplayName: strings.TrimSpace(connector.Name), Description: strings.TrimSpace(connector.Description),
			ModulePath: connectorModulePath(connector.Directory, connector.Version), Version: connector.Version, VersionLine: VersionLine(connector.Version),
			directory: connector.Directory,
		})
	}
	return entries, nil
}

// connectorModulePath is the Go module of the connector in directory at
// version: from v2 on, Go module paths end in the major version.
func connectorModulePath(directory, version string) string {
	path := connectorlibrary.ModulePath + "/" + directory
	if major := VersionLine(version); major != "v0" && major != "v1" {
		path += "/" + major
	}
	return path
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
