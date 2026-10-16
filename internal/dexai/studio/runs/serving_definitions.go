// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package runs

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
)

// servingDefinitions parses the definitions a serving Worker loaded. The
// owning Release or Preview read them in publication order and validated
// each definition's digest.
func servingDefinitions(definitions []appmanifest.FlowDefinition) (*fdg.Bundle, error) {
	graphs := make([]json.RawMessage, 0, len(definitions))
	for _, definition := range definitions {
		graphs = append(graphs, definition.Graph)
	}
	bundle, err := fdg.ParseBundle(graphs)
	if err != nil {
		return nil, definitionMismatch("are invalid: " + err.Error())
	}
	return bundle, nil
}

// previewDefinitions reads only the exact definitions the running Preview
// admitted: the Session's PreparedSource may already name an edit that this
// Worker has never loaded.
func (r *Runs) previewDefinitions(ctx context.Context, state preview.Snapshot) (*fdg.Bundle, error) {
	served := state.Preview
	definitions, _ := r.cached()
	key := definitionsKey(served.Definitions, "preview", state.Identity.ProjectID, served.SourceCommit, served.DefinitionRevision)
	return definitions.load(key, func() ([]appmanifest.FlowDefinition, error) {
		read, err := preview.ReadPreviewDefinitionBundle(ctx, r.Objects, state.Identity, served.Definitions, served.SourceCommit, served.DefinitionRevision)
		if errors.Is(err, preview.ErrDefinitionMismatch) {
			return nil, definitionMismatch("do not match the running Preview source")
		}
		return read, err
	})
}

func definitionMismatch(message string) error {
	return dexflow.Reject(dexflow.RejectionFailedPrecondition, "Serving Flow Definitions "+message)
}
