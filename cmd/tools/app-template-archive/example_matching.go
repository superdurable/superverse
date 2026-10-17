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
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// The search's BM25 parameters: term-frequency saturation and length
// normalization.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// searchField is one field of an example the search finds it by, with its
// weight: the title and the apps count most.
type searchField struct {
	text   string
	weight int
}

// searchFields are what an example is found by, generated from its manifest:
// its title, apps, category, decider, description and the text of its card's
// features.
func searchFields(manifest exampleManifest) []searchField {
	var features []string
	for _, feature := range manifest.Requirements.Features {
		features = append(features, feature.Text)
	}
	return []searchField{
		{manifest.Title, 3}, {strings.Join(manifest.Apps, ", "), 3}, {manifest.Category, 1}, {manifest.Decider, 1},
		{manifest.Description, 1}, {strings.Join(features, "\n"), 1},
	}
}

// searchWordsSource is app-template/examples/search-words.json.
type searchWordsSource struct {
	StopWords []string   `json:"stopWords"`
	Synonyms  [][]string `json:"synonyms"`
}

// facetsSource is app-template/examples/facets.json.
type facetsSource struct {
	Negators []string `json:"negators"`
	Facets   []struct {
		ID         string   `json:"id"`
		Label      string   `json:"label"`
		Terms      []string `json:"terms"`
		Paired     []string `json:"paired,omitempty"`
		Companions []string `json:"companions,omitempty"`
		Implies    []string `json:"implies,omitempty"`
	} `json:"facets"`
}

// labelledKind is why a labelled request is in the set, a closed set.
type labelledKind string

// The kinds of labelled requests.
const (
	labelledDirect      labelledKind = "direct"
	labelledParaphrased labelledKind = "paraphrased"
	labelledNegated     labelledKind = "negated"
	labelledNearMiss    labelledKind = "near-miss"
	labelledUnrelated   labelledKind = "unrelated"
	labelledPlaceholder labelledKind = "placeholder"
	labelledTestPrompt  labelledKind = "test-prompt"
)

var labelledKinds = []labelledKind{labelledDirect, labelledParaphrased, labelledNegated, labelledNearMiss, labelledUnrelated, labelledPlaceholder, labelledTestPrompt}

// expectDefault is the expected answer of a request that starts from the
// default example.
const expectDefault = "default"

// labelledRequest is one request of app-template/examples/labelled-requests.json:
// its text, the example it starts from or default, and its kind.
type labelledRequest struct {
	Text   string       `json:"text"`
	Expect string       `json:"expect"`
	Kind   labelledKind `json:"kind"`
}

// labelledSearch is a labelled request with every example the Go search
// ranks for it, best first, which Studio's check compares its own search
// with (web/studio/scripts/check-example-search.mjs).
type labelledSearch struct {
	Text    string   `json:"text"`
	Expect  string   `json:"expect"`
	Ranking []string `json:"ranking"`
}

// readShared decodes a shared file of app-template/examples strictly.
func readShared(templateDirectory, name string, value any) error {
	contents, err := os.ReadFile(filepath.Join(templateDirectory, examplesDirectory, name))
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return fmt.Errorf("app-template/%s/%s: %w", examplesDirectory, name, err)
	}
	return nil
}

// buildSearchIndex scores every term of every example with BM25 over the
// weighted fields: a term's weighted frequency is the sum of its occurrences
// times its field's weight, and an example's length the weighted number of its
// terms.
func buildSearchIndex(examples []composedExample, vocabulary apptemplate.SearchVocabulary) apptemplate.SearchIndex {
	frequencies := make([]map[string]float64, len(examples))
	lengths := make([]float64, len(examples))
	total, documents := 0.0, map[string]int{}
	index := apptemplate.SearchIndex{SchemaVersion: 1, SearchVocabulary: vocabulary, Postings: map[string][][2]int{}}
	for position, example := range examples {
		manifest := example.manifest
		index.Examples = append(index.Examples, apptemplate.SearchExample{ID: manifest.ID, Title: manifest.Title, Category: manifest.Category,
			Apps: manifest.Apps, Decider: manifest.Decider, Description: manifest.Description})
		frequencies[position] = map[string]float64{}
		for _, field := range searchFields(manifest) {
			terms := vocabulary.Terms(field.text)
			lengths[position] += float64(field.weight * len(terms))
			for _, term := range terms {
				frequencies[position][term] += float64(field.weight)
			}
		}
		total += lengths[position]
		for term := range frequencies[position] {
			documents[term]++
		}
	}
	average, count := total/float64(len(examples)), float64(len(examples))
	for position := range examples {
		for term, frequency := range frequencies[position] {
			idf := math.Log(1 + (count-float64(documents[term])+0.5)/(float64(documents[term])+0.5))
			score := idf * frequency * (bm25K1 + 1) / (frequency + bm25K1*(1-bm25B+bm25B*lengths[position]/average))
			index.Postings[term] = append(index.Postings[term], [2]int{position, max(1, int(math.Round(score*1000)))})
		}
	}
	return index
}

var facetPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// buildMatchRules compiles the shared facets and negators with every
// example's match block and checks them: unique facets with a label and
// terms, each term or paired term in exactly one facet, paired terms only
// with companions, a prefix term of at least four letters, implied and
// referenced facets that exist, a facet in at most one list of a block, and a
// facet for every connector of every example.
func buildMatchRules(examples []composedExample, source facetsSource) (apptemplate.MatchRules, error) {
	rules := apptemplate.MatchRules{SchemaVersion: 1, Negators: []apptemplate.MatchTerm{}, Facets: []apptemplate.MatchFacet{}}
	for _, negator := range source.Negators {
		term := apptemplate.NewMatchTerm(negator)
		if term.Text == "" {
			return rules, fmt.Errorf("the negator %q has no word", negator)
		}
		rules.Negators = append(rules.Negators, term)
	}
	owner, known := map[string]string{}, map[string]bool{}
	for _, facet := range source.Facets {
		if !facetPattern.MatchString(facet.ID) || known[facet.ID] || facet.Label == "" || len(facet.Terms) == 0 {
			return rules, fmt.Errorf("the facet %q needs a unique lower-case ID, a label and terms", facet.ID)
		}
		known[facet.ID] = true
		if (len(facet.Paired) == 0) != (len(facet.Companions) == 0) {
			return rules, fmt.Errorf("the facet %s needs both paired terms and companions, or neither", facet.ID)
		}
		compiled := apptemplate.MatchFacet{ID: facet.ID, Label: facet.Label, Implies: facet.Implies}
		for _, list := range []struct {
			raw   []string
			into  *[]apptemplate.MatchTerm
			owned bool
		}{{facet.Terms, &compiled.Terms, true}, {facet.Paired, &compiled.Paired, true}, {facet.Companions, &compiled.Companions, false}} {
			for _, raw := range list.raw {
				term := apptemplate.NewMatchTerm(raw)
				switch {
				case len(term.Text) < 2 || term.Prefix && len(term.Text) < 4:
					return rules, fmt.Errorf("the term %q of %s is shorter than 2 letters, or 4 for a prefix", raw, facet.ID)
				case list.owned && owner[term.Text] != "":
					return rules, fmt.Errorf("the term %q is a term of both %s and %s", raw, owner[term.Text], facet.ID)
				}
				if list.owned {
					owner[term.Text] = facet.ID
				}
				*list.into = append(*list.into, term)
			}
		}
		rules.Facets = append(rules.Facets, compiled)
	}
	for _, facet := range rules.Facets {
		for _, implied := range facet.Implies {
			if !known[implied] {
				return rules, fmt.Errorf("the facet %s implies the unknown facet %s", facet.ID, implied)
			}
		}
	}
	for _, example := range examples {
		rule, err := exampleRule(example, known)
		if err != nil {
			return rules, fmt.Errorf("example %s: %w", example.manifest.ID, err)
		}
		rules.Examples = append(rules.Examples, rule)
		if example.manifest.ID == apptemplate.DefaultExampleID {
			if rules.DefaultSource, err = defaultSourceNames(example); err != nil {
				return rules, err
			}
		}
	}
	return rules, nil
}

// exampleRule is an example's match block with its connectors, the Steps of
// its design and what it extends.
func exampleRule(example composedExample, known map[string]bool) (apptemplate.ExampleRule, error) {
	block := example.manifest.Match
	connectors, err := connectorIDs(example.files)
	if err != nil {
		return apptemplate.ExampleRule{}, err
	}
	if len(block.Core) == 0 {
		return apptemplate.ExampleRule{}, errors.New("its match block names no core facet")
	}
	listed := map[string]bool{}
	for _, facets := range [][]string{block.Core, block.Signals, block.Conflicts, connectors} {
		for _, facet := range facets {
			if !known[facet] {
				return apptemplate.ExampleRule{}, fmt.Errorf("its match block or a connector names %q, which facets.json does not declare", facet)
			}
		}
	}
	for _, facets := range [][]string{block.Core, block.Signals, block.Conflicts} {
		for _, facet := range facets {
			if listed[facet] {
				return apptemplate.ExampleRule{}, fmt.Errorf("its match block names %s twice", facet)
			}
			listed[facet] = true
		}
	}
	design, err := designNames(example.manifest.Design)
	if err != nil {
		return apptemplate.ExampleRule{}, err
	}
	return apptemplate.ExampleRule{ID: example.manifest.ID, Core: block.Core, Signals: orEmpty(block.Signals), Conflicts: orEmpty(block.Conflicts),
		Connectors: connectors, Steps: len(design.types) - design.flows, Extends: example.manifest.Extends}, nil
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// designNames reads the Flow and Step type names of an example's design.
func designNames(design json.RawMessage) (designedTypes, error) {
	var decoded struct {
		Flows []struct {
			FlowType string `json:"flow_type"`
			Steps    []struct {
				StepType string `json:"step_type"`
			} `json:"steps"`
		} `json:"flows"`
	}
	if err := json.Unmarshal(design, &decoded); err != nil {
		return designedTypes{}, fmt.Errorf("read the design: %w", err)
	}
	var names designedTypes
	for _, flow := range decoded.Flows {
		names.types = append(names.types, flow.FlowType)
		names.flows++
	}
	for _, flow := range decoded.Flows {
		for _, step := range flow.Steps {
			names.types = append(names.types, step.StepType)
		}
	}
	return names, nil
}

// designedTypes are the Flow and Step type names of a design, its Flows
// first, and how many of them are Flows.
type designedTypes struct {
	types []string
	flows int
}

// defaultSourceNames are the names of the default example's own source: the
// Flow and Step types of its design and the file names of its Flow
// definitions. A request that names one is a change to the default.
func defaultSourceNames(example composedExample) ([]apptemplate.MatchTerm, error) {
	names, err := designNames(example.manifest.Design)
	if err != nil {
		return nil, err
	}
	manifest, found := fileOf(example.files, "dex-app.yaml")
	if !found {
		return nil, errors.New("the default example has no dex-app.yaml")
	}
	var document struct {
		FlowDefinitions []struct {
			SourcePath string `json:"sourcePath"`
		} `json:"flowDefinitions"`
	}
	if err = json.Unmarshal(manifest.contents, &document); err != nil {
		return nil, fmt.Errorf("read the default example's dex-app.yaml: %w", err)
	}
	words := slices.Clone(names.types)
	for _, definition := range document.FlowDefinitions {
		words = append(words, path.Base(definition.SourcePath))
	}
	terms := []apptemplate.MatchTerm{}
	for _, word := range words {
		terms = append(terms, apptemplate.NewMatchTerm(word))
	}
	return terms, nil
}

// checkLabelled decides every labelled request with the generated index and
// rules and fails when a pick differs from its expected answer; it returns
// every request with the Go search's ranking.
func checkLabelled(requests []labelledRequest, examples []composedExample, index *apptemplate.SearchIndex, rules *apptemplate.MatchRules) ([]labelledSearch, error) {
	ids := map[string]bool{}
	for _, example := range examples {
		ids[example.manifest.ID] = true
	}
	if ids[expectDefault] {
		return nil, fmt.Errorf("no example may be named %s, the answer of a request that starts from the default", expectDefault)
	}
	var wrong []string
	searches := make([]labelledSearch, 0, len(requests))
	for _, request := range requests {
		if !slices.Contains(labelledKinds, request.Kind) || strings.TrimSpace(request.Text) == "" || request.Expect != expectDefault && !ids[request.Expect] {
			return nil, fmt.Errorf("the labelled request %q needs a text, a known kind and an expected example or %s", request.Text, expectDefault)
		}
		picked := expectDefault
		if match, isMatch := rules.Pick(index, request.Text); isMatch {
			picked = match.ExampleID
		}
		if picked != request.Expect {
			wrong = append(wrong, fmt.Sprintf("  %s, expected %s: %s", picked, request.Expect, request.Text))
		}
		search := labelledSearch{Text: request.Text, Expect: request.Expect, Ranking: []string{}}
		for _, position := range index.Closest(request.Text, len(index.Examples)) {
			search.Ranking = append(search.Ranking, index.Examples[position].ID)
		}
		searches = append(searches, search)
	}
	if len(wrong) > 0 {
		return nil, fmt.Errorf("%d of %d labelled requests start from another example than expected:\n%s", len(wrong), len(requests), strings.Join(wrong, "\n"))
	}
	return searches, nil
}

// proposeMatch prints a match block for an example for its author to review:
// the facets of its title as the core, the facets its apps, trigger (its first
// feature), decider and description mention as signals, and no conflicts.
func proposeMatch(example composedExample, rules *apptemplate.MatchRules) error {
	manifest := example.manifest
	block := matchBlock{Core: rules.MentionedFacets(manifest.Title), Signals: []string{}, Conflicts: []string{}}
	connectors, err := connectorIDs(example.files)
	if err != nil {
		return err
	}
	described := []string{strings.Join(manifest.Apps, ", "), manifest.Decider, manifest.Description}
	if len(manifest.Requirements.Features) > 0 {
		described = append(described, manifest.Requirements.Features[0].Text)
	}
	signals := map[string]bool{}
	for _, facet := range append(rules.MentionedFacets(strings.Join(described, "\n")), connectors...) {
		if !slices.Contains(block.Core, facet) && !signals[facet] {
			signals[facet] = true
			block.Signals = append(block.Signals, facet)
		}
	}
	sort.Strings(block.Signals)
	encoded, err := json.MarshalIndent(struct {
		Match matchBlock `json:"match"`
	}{block}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
