// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/comm"
	"golang.org/x/mod/semver"
)

// The published catalog (PublishedCatalogURL) lists each released connector
// at its latest release, never a release candidate. This file owns its
// document: every reader decodes it with DecodeCatalog and answers a search
// with CatalogMatches, whether it fetched the document on the server or in a
// Sandbox.

const (
	// MaximumCatalogBytes bounds the catalog document.
	MaximumCatalogBytes = 1 << 20
	// MaximumCatalogEntries bounds the connectors one catalog lists.
	MaximumCatalogEntries = 512
	// CatalogLifetime is how long a fetched catalog serves before the next
	// fetch.
	CatalogLifetime = 5 * time.Minute
	// catalogAPIVersion and catalogKind identify the supported document.
	catalogAPIVersion = "connectors.dex.dev/catalog/v1alpha1"
	catalogKind       = "ConnectorCatalog"
)

var (
	catalogIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	catalogDirectoryPattern = regexp.MustCompile(`^connectors/[a-z0-9-]+(/[a-z0-9-]+)*$`)
	catalogVersionPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

// errInvalidCatalog marks a document that is not a supported catalog or that
// lists an invalid release identity.
var errInvalidCatalog = errors.New("the published connector catalog is not a supported catalog document")

// CatalogOperation is an operation or a trigger of a released connector.
type CatalogOperation struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description"`
}

// CatalogEntry is one released connector as the catalog lists it; its JSON
// form is the one the Agent reads.
type CatalogEntry struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Directory   string             `json:"directory"`
	Operations  []CatalogOperation `json:"operations"`
	Triggers    []CatalogOperation `json:"triggers"`
}

// Catalog is one fetched catalog document: its entries, the SHA-256 of its
// bytes and when it was fetched.
type Catalog struct {
	Entries    []CatalogEntry
	SHA256     string
	ObservedAt time.Time
}

// DecodeCatalog reads a catalog document fetched at observedAt. A document
// above MaximumCatalogBytes, of another version or kind, with no connector or
// more than 512, or with an invalid identity, directory or version, is
// errInvalidCatalog.
func DecodeCatalog(contents []byte, observedAt time.Time) (Catalog, error) {
	var document struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Connectors []CatalogEntry `json:"connectors"`
	}
	if len(contents) > MaximumCatalogBytes || yaml.Unmarshal(contents, &document) != nil || document.APIVersion != catalogAPIVersion ||
		document.Kind != catalogKind || len(document.Connectors) == 0 || len(document.Connectors) > MaximumCatalogEntries {
		return Catalog{}, errInvalidCatalog
	}
	for _, entry := range document.Connectors {
		if !catalogIDPattern.MatchString(entry.ID) || !catalogDirectoryPattern.MatchString(entry.Directory) || !catalogVersionPattern.MatchString(entry.Version) {
			return Catalog{}, fmt.Errorf("%w: it lists an invalid release identity", errInvalidCatalog)
		}
	}
	digest := sha256.Sum256(contents)
	return Catalog{Entries: document.Connectors, SHA256: hex.EncodeToString(digest[:]), ObservedAt: observedAt.UTC()}, nil
}

// CatalogModulePath is the Go module of the connector in directory at
// version: from v2 on, Go module paths end in the major version.
func CatalogModulePath(directory, version string) string {
	path := ModulePath + "/" + directory
	if major := semver.Major(version); major != "v0" && major != "v1" {
		path += "/" + major
	}
	return path
}

// CatalogMatch is a catalog entry with what the Agent needs to use it: its
// exact Go module, the command that adds the release to go.mod, and the
// manifest and generated factory in the module's directory, which go list -m
// -f '{{.Dir}}' <module> names once the release is added.
type CatalogMatch struct {
	CatalogEntry
	Module         string `json:"module"`
	InstallCommand string `json:"install_command"`
	ManifestPath   string `json:"manifest_path"`
	FactoryPath    string `json:"factory_path"`
}

// Match is the entry as a search answers it.
func (entry CatalogEntry) Match() CatalogMatch {
	module := CatalogModulePath(entry.Directory, entry.Version)
	return CatalogMatch{CatalogEntry: entry, Module: module, InstallCommand: "go get " + module + "@" + entry.Version,
		ManifestPath: "connector.yaml", FactoryPath: "zz_generated_connector.go"}
}

// CatalogMatches is the answer of a catalog search: the catalog's source,
// digest and fetch time, the matching entries in order, how many matched in
// all and whether entries were left out.
type CatalogMatches struct {
	Source     string         `json:"source"`
	SHA256     string         `json:"sha256"`
	ObservedAt time.Time      `json:"observed_at"`
	Matches    []CatalogMatch `json:"matches"`
	Total      int            `json:"total_matches"`
	Truncated  bool           `json:"truncated"`
}

// FitJSON is the answer as compact JSON of at most limit bytes: it leaves out
// matches from the end, marking the answer truncated, until it fits. False
// when even one match does not fit.
func (matches CatalogMatches) FitJSON(limit int) (string, bool) {
	for {
		matches.Truncated = matches.Total > len(matches.Matches)
		encoded := comm.Must(json.Marshal(matches))
		if len(encoded) <= limit {
			return string(encoded), true
		}
		if len(matches.Matches) <= 1 {
			return "", false
		}
		matches.Matches = matches.Matches[:len(matches.Matches)-1]
	}
}
