// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// MinimumRedactedValueBytes is the shortest configured value that redaction
// replaces; shorter values cannot be credentials and would only blank out
// ordinary words.
const MinimumRedactedValueBytes = 8

// ConfiguredValues returns every value of at least MinimumRedactedValueBytes
// that an application running with the frozen configuration of scope can
// hold: the ordinary settings of its connections, trigger bindings and
// operations, its resolved application environment, secrets included, and the
// current credentials of the TeamSpace connections it selects, which a frozen
// configuration names only by ID. They are longest first, so a redaction that
// replaces them in order never leaves part of a longer value behind.
//
// The values are secret. Callers hold them in memory for one redaction and
// never return, log or store them.
func (c *Configuration) ConfiguredValues(ctx context.Context, scope infra.ScopeRef, frozen objectstore.ObjectRef) ([]string, error) {
	store, configuration, err := c.readFrozen(ctx, scope, frozen)
	if err != nil {
		return nil, err
	}
	values := configuredValues{}
	for _, connection := range configuration.Connections {
		values.addJSON(connection.Configuration)
		if connection.SharedConnectionID == "" {
			continue
		}
		credentials, readErr := c.Shared.Reader.StoredValues(ctx, connection.SharedConnectionID)
		if errors.Is(readErr, connections.ErrNotFound) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read the credentials of connection %s: %w", connection.ConnectionName, readErr)
		}
		for _, value := range credentials {
			values.addJSON(value)
		}
	}
	for _, binding := range configuration.TriggerBindings {
		values.addJSON(binding.Configuration)
	}
	for _, operation := range configuration.OperationConfigurations {
		values.addJSON(operation.Configuration)
	}
	environment, err := store.ResolveApplicationEnvironment(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("resolve the application environment: %w", err)
	}
	for name := range configuration.Environment {
		value, _ := environment.Lookup(name)
		values.add(value)
	}
	return values.sorted(), nil
}

// SharedConnectionIDs returns the TeamSpace connections that the frozen
// configuration of scope selects, each once.
func (c *Configuration) SharedConnectionIDs(ctx context.Context, scope infra.ScopeRef, frozen objectstore.ObjectRef) ([]string, error) {
	selected, err := c.SharedConnectionSelections(ctx, scope, frozen)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, id := range selected {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// SharedConnectionSelections returns the TeamSpace connection that the
// frozen configuration of scope selects for each connection name that
// selects one.
func (c *Configuration) SharedConnectionSelections(ctx context.Context, scope infra.ScopeRef, frozen objectstore.ObjectRef) (map[string]string, error) {
	_, configuration, err := c.readFrozen(ctx, scope, frozen)
	if err != nil {
		return nil, err
	}
	return confighost.SharedSelections(configuration), nil
}

// readFrozen reads the frozen configuration of scope with the store that
// resolves it.
func (c *Configuration) readFrozen(ctx context.Context, scope infra.ScopeRef, frozen objectstore.ObjectRef) (*connectorconfig.ConfigurationStore, connectorconfig.Configuration, error) {
	snapshot, err := infra.SnapshotRefFor(scope, frozen)
	if err != nil {
		return nil, connectorconfig.Configuration{}, err
	}
	objects, err := c.Storage.ConfigurationObjects(ctx, scope)
	if err != nil {
		return nil, connectorconfig.Configuration{}, err
	}
	connectorScope := connectorconfig.Scope{ProjectID: scope.ProjectID, Kind: scope.Scope.Kind.String()}
	// The store fails only for a scope that ConfigurationObjects accepted.
	store := comm.Must(connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: connectorScope}))
	configuration, err := store.ReadSnapshot(ctx, snapshot)
	if err != nil {
		return nil, connectorconfig.Configuration{}, fmt.Errorf("read the frozen configuration: %w", err)
	}
	return store, configuration, nil
}

// OutputRedaction replaces, in output shown outside the platform (build logs
// and the Agent's platform tools), the configured values an application of
// the Project can hold and the Project's internal resource names. Values are
// replaced first, longest first, so replacing a name inside a value never
// leaves the rest of that value behind. Its Values are secret: it is held for
// one redaction and never returned, logged or stored.
type OutputRedaction struct {
	Project        string
	Infrastructure infra.Redactor
	Values         []string
}

// Text is text with every value and internal name replaced.
func (r OutputRedaction) Text(text string) string {
	for _, value := range r.Values {
		text = strings.ReplaceAll(text, value, "[REDACTED]")
	}
	return r.Infrastructure.Redact(r.Project, text)
}

// configuredValues collects the distinct values a redaction replaces. A value
// of several lines also adds each line, so output read in pages of whole lines
// never shows a part of it.
type configuredValues map[string]bool

func (values configuredValues) add(value string) {
	if len(value) < MinimumRedactedValueBytes {
		return
	}
	values[value] = true
	if strings.Contains(value, "\n") {
		for line := range strings.Lines(value) {
			if line = strings.TrimRight(line, "\r\n"); len(line) >= MinimumRedactedValueBytes {
				values[line] = true
			}
		}
	}
}

// addJSON adds every string and number of a JSON document, or the raw text of
// a document that does not decode.
func (values configuredValues) addJSON(document json.RawMessage) {
	var decoded any
	if comm.DecodeStrictJSONNumbers(document, &decoded) != nil {
		values.add(string(document))
		return
	}
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case string:
			values.add(current)
		case json.Number:
			values.add(current.String())
		case map[string]any:
			for _, entry := range current {
				walk(entry)
			}
		case []any:
			for _, entry := range current {
				walk(entry)
			}
		}
	}
	walk(decoded)
}

// sorted returns the values longest first.
func (values configuredValues) sorted() []string {
	sorted := slices.Collect(maps.Keys(values))
	slices.SortFunc(sorted, func(left, right string) int { return len(right) - len(left) })
	return sorted
}
