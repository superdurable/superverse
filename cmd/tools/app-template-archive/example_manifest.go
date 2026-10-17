// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// maximumAgentInstructions is what the Agent's repository context inlines of
// AGENTS.md (maximumAgentInstructions in repository_context.go of the
// workspace Git package).
const maximumAgentInstructions = 6144

// examplePattern is an example ID, its folder's name.
var examplePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// exampleManifest is app-template/examples/<id>/example.json: what the start
// page shows, how the example composes, and its requirements card. The
// requirements and design keep the confirm_requirements field names.
type exampleManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Category      string   `json:"category"`
	Apps          []string `json:"apps"`
	Decider       string   `json:"decider"`
	Description   string   `json:"description"`
	// Extends names the example whose composed tree this one starts from; the
	// shared base when empty. Removes lists the files of that tree it drops.
	Extends      string          `json:"extends,omitempty"`
	Removes      []string        `json:"removes,omitempty"`
	Requirements requirements    `json:"requirements"`
	Design       json.RawMessage `json:"design"`
}

// requirements is the example's card with the trace of each feature.
type requirements struct {
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Features    []feature       `json:"features"`
	Connections json.RawMessage `json:"connections"`
	UserSetup   json.RawMessage `json:"user_setup"`
	Acceptance  json.RawMessage `json:"acceptance"`
}

// feature is one requirement with the source that implements it and either
// the tests that prove it or why none does.
type feature struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	Evidence       []string `json:"evidence"`
	Tests          []string `json:"tests,omitempty"`
	UntestedReason string   `json:"untested_reason,omitempty"`
}

// card is the confirm_requirements input of a manifest, in the schema's
// field order.
type card struct {
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Features    []cardFeature   `json:"features"`
	Connections json.RawMessage `json:"connections"`
	UserSetup   json.RawMessage `json:"user_setup"`
	Acceptance  json.RawMessage `json:"acceptance"`
	Design      json.RawMessage `json:"design,omitempty"`
}

type cardFeature struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func decodeManifest(contents []byte) (exampleManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest exampleManifest
	if err := decoder.Decode(&manifest); err != nil {
		return exampleManifest{}, err
	}
	if manifest.SchemaVersion != 1 {
		return exampleManifest{}, fmt.Errorf("schemaVersion %d is not 1", manifest.SchemaVersion)
	}
	return manifest, nil
}

// cardOf is the confirm_requirements input of a manifest.
func cardOf(manifest exampleManifest) ([]byte, error) {
	requirements := manifest.Requirements
	encoded := card{Title: requirements.Title, Summary: requirements.Summary, Features: []cardFeature{}, Connections: requirements.Connections,
		UserSetup: requirements.UserSetup, Acceptance: requirements.Acceptance, Design: manifest.Design}
	for _, feature := range requirements.Features {
		encoded.Features = append(encoded.Features, cardFeature{ID: feature.ID, Text: feature.Text})
	}
	return json.Marshal(encoded)
}

// validateExample checks a composed example: its manifest's fields, the
// composed tree's template manifest and AGENTS.md, and its card, which the
// Session's own card code must accept, with every feature traced to existing
// evidence and either existing API tests or a reason.
func validateExample(example composedExample) error {
	manifest := example.manifest
	_, known := apptemplate.ParseCategory(manifest.Category)
	switch {
	case !examplePattern.MatchString(manifest.ID):
		return errors.New("the ID must be 3 to 64 lower-case letters, digits and hyphens")
	case !known:
		return fmt.Errorf("the category %q is not a start page category", manifest.Category)
	case !text(manifest.Title, 60) || !text(manifest.Decider, 80) || !text(manifest.Description, 800):
		return errors.New("the title, decider and description are 1 to 60, 80 and 800 characters")
	case len(manifest.Apps) == 0 || len(manifest.Apps) > 6:
		return errors.New("an example names 1 to 6 apps")
	}
	for _, name := range []string{".superverse/template.json", "dex-app.yaml", "openapi/openapi.yaml"} {
		if _, found := fileOf(example.files, name); !found {
			return fmt.Errorf("the composed tree has no %s", name)
		}
	}
	instructions, found := fileOf(example.files, "AGENTS.md")
	if !found || len(instructions.contents) > maximumAgentInstructions {
		return fmt.Errorf("the composed AGENTS.md must exist within the %d bytes the Agent's repository context inlines", maximumAgentInstructions)
	}
	encoded, err := cardOf(manifest)
	if err != nil {
		return err
	}
	call := conversation.ToolCall{ID: "example-card", Name: conversation.ToolConfirmRequirements, Arguments: conversation.JSONObject(encoded)}
	if _, _, err = agentloop.RequestedRequirements(call, nil); err != nil {
		return fmt.Errorf("the requirements card: %w", err)
	}
	tests := testNames(example.files)
	for _, feature := range manifest.Requirements.Features {
		if (len(feature.Tests) == 0) == (feature.UntestedReason == "") {
			return fmt.Errorf("%s needs either tests or an untested_reason", feature.ID)
		}
		for _, test := range feature.Tests {
			if !tests[test] {
				return fmt.Errorf("%s names the test %s, which internal/apptest does not declare", feature.ID, test)
			}
		}
		for _, evidence := range feature.Evidence {
			if _, held := fileOf(example.files, evidence); !held {
				return fmt.Errorf("%s names the evidence %s, which the composed tree does not hold", feature.ID, evidence)
			}
		}
	}
	return nil
}

// catalogEntry is the catalog entry of a validated example.
func catalogEntry(example composedExample) (apptemplate.CatalogEntry, error) {
	encoded, err := cardOf(example.manifest)
	if err != nil {
		return apptemplate.CatalogEntry{}, err
	}
	connectors, err := connectorIDs(example.files)
	if err != nil {
		return apptemplate.CatalogEntry{}, err
	}
	manifest := example.manifest
	return apptemplate.CatalogEntry{ID: manifest.ID, Title: manifest.Title, Category: manifest.Category, Apps: manifest.Apps, Decider: manifest.Decider,
		Description: manifest.Description, ConnectorIDs: connectors, Card: encoded}, nil
}

func text(value string, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= 1 && length <= maximum
}
