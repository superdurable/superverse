// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// publishedConnectorCatalogURL is the official catalog the Sandbox fetches.
const publishedConnectorCatalogURL = connectorlibrary.PublishedCatalogURL

var (
	connectorDirectoryPattern = regexp.MustCompile(`^connectors/[a-z0-9-]+(/[a-z0-9-]+)*$`)
	connectorVersionPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

type connectorCatalogOperation struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description"`
}

type connectorCatalogEntry struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Version     string                      `json:"version"`
	Directory   string                      `json:"directory"`
	Operations  []connectorCatalogOperation `json:"operations"`
	Triggers    []connectorCatalogOperation `json:"triggers"`
}

type connectorCatalogDocument struct {
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Connectors []connectorCatalogEntry `json:"connectors"`
}

// publishedConnectorCatalog is one fetched catalog with its content digest and
// fetch time.
type publishedConnectorCatalog struct {
	document   connectorCatalogDocument
	digest     string
	observedAt time.Time
}

// One public document, at most 1 MiB, is retained for five minutes. No private
// scope or credential enters this cache, and it owns no background resources.
// Concurrent misses may read twice; no caller waits behind another's network IO.
type connectorCatalogCache struct {
	mutex   sync.Mutex
	catalog *publishedConnectorCatalog
}

type connectorCatalogMatch struct {
	connectorCatalogEntry
	// Module is the connector's exact Go module path. After InstallCommand adds
	// the release to go.mod, go list -m -f '{{.Dir}}' <module> names the
	// directory where the file tools read its manifest and factory.
	Module         string `json:"module"`
	InstallCommand string `json:"install_command"`
	ManifestPath   string `json:"manifest_path"`
	FactoryPath    string `json:"factory_path"`
}

type connectorCatalogPayload struct {
	Source     string                  `json:"source"`
	SHA256     string                  `json:"sha256"`
	ObservedAt time.Time               `json:"observed_at"`
	Matches    []connectorCatalogMatch `json:"matches"`
	Total      int                     `json:"total_matches"`
	Truncated  bool                    `json:"truncated"`
}

func (execution *Execution) connectorCatalog(ctx context.Context, call Call) (Result, error) {
	arguments, err := toolinput.ConnectorCatalog.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	terms := strings.Fields(strings.ToLower(arguments.Query))
	if len(terms) == 0 {
		return Result{}, invalidArguments(errors.New("query must contain a provider or capability term"))
	}
	catalog, err := execution.loadConnectorCatalog(ctx, call)
	if err != nil {
		return Result{}, err
	}
	matches := make([]connectorCatalogMatch, 0, arguments.Limit.Value)
	total := 0
	for _, entry := range catalog.document.Connectors {
		if !containsAllTerms(entry, terms) {
			continue
		}
		if arguments.Operation.Set {
			operations := []connectorCatalogOperation{}
			for _, operation := range entry.Operations {
				if operation.Name == arguments.Operation.Value {
					operations = append(operations, operation)
				}
			}
			if len(operations) == 0 {
				continue
			}
			entry.Operations = operations
		}
		total++
		if len(matches) < arguments.Limit.Value {
			module := "github.com/superdurable/dex-connectors-library/" + entry.Directory
			matches = append(matches, connectorCatalogMatch{connectorCatalogEntry: entry,
				Module: module, InstallCommand: "go get " + module + "@" + entry.Version,
				ManifestPath: "connector.yaml", FactoryPath: "zz_generated_connector.go"})
		}
	}
	for {
		result := jsonResult(connectorCatalogPayload{Source: publishedConnectorCatalogURL, SHA256: catalog.digest, ObservedAt: catalog.observedAt,
			Matches: matches, Total: total, Truncated: total > len(matches)}, false)
		if len(result.Content) <= 16<<10 {
			return result, nil
		}
		if len(matches) <= 1 {
			return Result{}, sandboxd.NewToolFailure(toolfailure.CatalogResultTooLarge, errors.New("select one exact operation to narrow this connector result"))
		}
		matches = matches[:len(matches)-1]
	}
}

// containsAllTerms reports whether every term occurs in the values of the
// entry: its identity and description and the name, kind and description of
// each operation and trigger. Field names never match.
func containsAllTerms(entry connectorCatalogEntry, terms []string) bool {
	values := []string{entry.ID, entry.Name, entry.Description, entry.Version, entry.Directory}
	for _, operation := range entry.Operations {
		values = append(values, operation.Name, operation.Kind, operation.Description)
	}
	for _, trigger := range entry.Triggers {
		values = append(values, trigger.Name, trigger.Kind, trigger.Description)
	}
	text := strings.ToLower(strings.Join(values, " "))
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// loadConnectorCatalog returns the cached document or fetches the official one
// inside the Sandbox; raw catalog bytes never become model or UI progress.
func (execution *Execution) loadConnectorCatalog(ctx context.Context, call Call) (*publishedConnectorCatalog, error) {
	cache := execution.catalog
	cache.mutex.Lock()
	cached := cache.catalog
	cache.mutex.Unlock()
	if cached != nil && time.Since(cached.observedAt) < 5*time.Minute {
		return cached, nil
	}
	output, err := execution.runPlatformProgram(ctx, "connector-catalog",
		[]string{"curl", "--fail", "--silent", "--show-error", "--proto", "=https", "--max-time", "15", "--max-filesize", "1048576", publishedConnectorCatalogURL},
		20*time.Second, 2<<20)
	if err != nil {
		return nil, err
	}
	if output.ExitCode != 0 || len(output.Stdout) > 1<<20 {
		return nil, sandboxd.NewToolFailure(toolfailure.CatalogUnavailable, fmt.Errorf("official catalog fetch failed (exit %d); retry after checking Sandbox connectivity", output.ExitCode))
	}
	var fetched connectorCatalogDocument
	if decodeErr := yaml.Unmarshal([]byte(output.Stdout), &fetched); decodeErr != nil || fetched.APIVersion != "connectors.dex.dev/catalog/v1alpha1" || fetched.Kind != "ConnectorCatalog" || len(fetched.Connectors) == 0 || len(fetched.Connectors) > 512 {
		return nil, sandboxd.NewToolFailure(toolfailure.CatalogInvalid, errors.New("published catalog has an invalid or unsupported document"))
	}
	for _, entry := range fetched.Connectors {
		if !connectorDirectoryPattern.MatchString(entry.Directory) || !connectorVersionPattern.MatchString(entry.Version) {
			return nil, sandboxd.NewToolFailure(toolfailure.CatalogInvalid, errors.New("published catalog contains an invalid release identity"))
		}
	}
	hash := sha256.Sum256([]byte(output.Stdout))
	catalog := &publishedConnectorCatalog{document: fetched, digest: hex.EncodeToString(hash[:]), observedAt: time.Now().UTC()}
	cache.mutex.Lock()
	cache.catalog = catalog
	cache.mutex.Unlock()
	return catalog, nil
}
