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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// publishedConnectorCatalogURL is the official catalog the Sandbox fetches.
const publishedConnectorCatalogURL = connectorlibrary.PublishedCatalogURL

// One public document, at most 1 MiB, is retained for five minutes. No private
// scope or credential enters this cache, and it owns no background resources.
// Concurrent misses may read twice; no caller waits behind another's network IO.
type connectorCatalogCache struct {
	mutex   sync.Mutex
	catalog *connectorlibrary.Catalog
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
	answer := connectorlibrary.CatalogMatches{Source: publishedConnectorCatalogURL, SHA256: catalog.SHA256, ObservedAt: catalog.ObservedAt,
		Matches: make([]connectorlibrary.CatalogMatch, 0, arguments.Limit.Value)}
	for _, entry := range catalog.Entries {
		if !containsAllTerms(entry, terms) {
			continue
		}
		if arguments.Operation.Set {
			operations := []connectorlibrary.CatalogOperation{}
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
		answer.Total++
		if len(answer.Matches) < arguments.Limit.Value {
			answer.Matches = append(answer.Matches, entry.Match())
		}
	}
	content, fits := answer.FitJSON(16 << 10)
	if !fits {
		return Result{}, sandboxd.NewToolFailure(toolfailure.CatalogResultTooLarge, errors.New("select one exact operation to narrow this connector result"))
	}
	return Result{Content: content}, nil
}

// containsAllTerms reports whether every term occurs in the values of the
// entry: its identity and description and the name, kind and description of
// each operation and trigger. Field names never match.
func containsAllTerms(entry connectorlibrary.CatalogEntry, terms []string) bool {
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
func (execution *Execution) loadConnectorCatalog(ctx context.Context, call Call) (*connectorlibrary.Catalog, error) {
	cache := execution.catalog
	cache.mutex.Lock()
	cached := cache.catalog
	cache.mutex.Unlock()
	if cached != nil && time.Since(cached.ObservedAt) < connectorlibrary.CatalogLifetime {
		return cached, nil
	}
	output, err := execution.runPlatformProgram(ctx, "connector-catalog",
		[]string{"curl", "--fail", "--silent", "--show-error", "--proto", "=https", "--max-time", "15", "--max-filesize", strconv.Itoa(connectorlibrary.MaximumCatalogBytes), publishedConnectorCatalogURL},
		20*time.Second, 2<<20)
	if err != nil {
		return nil, err
	}
	if output.ExitCode != 0 || len(output.Stdout) > connectorlibrary.MaximumCatalogBytes {
		return nil, sandboxd.NewToolFailure(toolfailure.CatalogUnavailable, fmt.Errorf("official catalog fetch failed (exit %d); retry after checking Sandbox connectivity", output.ExitCode))
	}
	catalog, err := connectorlibrary.DecodeCatalog([]byte(output.Stdout), time.Now())
	if err != nil {
		return nil, sandboxd.NewToolFailure(toolfailure.CatalogInvalid, err)
	}
	cache.mutex.Lock()
	cache.catalog = &catalog
	cache.mutex.Unlock()
	return &catalog, nil
}
