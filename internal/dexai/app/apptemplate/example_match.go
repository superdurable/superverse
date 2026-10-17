// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package apptemplate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/superdurable/superversev2/internal/comm"
)

// MatchRulesFile is the generated data of the closest-example decision in the
// embedded examples directory: the facets with their terms, the negators, the
// names of the default example's source and each example's match block.
const MatchRulesFile = "examples/example-match.json"

// The decision's weights and bounds. An eligible example scores coreWeight
// per core facet and signalWeight per signal the request mentions, less
// surplusPenalty per connector of the example it neither mentions nor needs
// as a core facet, and less coreWeight per core facet of another eligible
// example its block lacks; it needs minimumScore, and it wins over the
// runner-up by winningMargin unless one extends the other. The search's best
// shortlistSize examples are the candidates. A fragment of at most
// maximumHeadingWords words before a colon whose negator ends it, such as
// "Not needed:", negates the next fragment.
const (
	coreWeight          = 3
	signalWeight        = 1
	surplusPenalty      = 2
	minimumScore        = 3
	winningMargin       = 2
	shortlistSize       = 5
	maximumHeadingWords = 4
)

// MatchTerm is a normalized phrase of a facet, a negator or a name of the
// default's source: lower-case ASCII words separated by one space. A Prefix
// term's last word matches any word that starts with it.
type MatchTerm struct {
	Text   string `json:"text"`
	Prefix bool   `json:"prefix,omitempty"`
}

// NewMatchTerm normalizes a phrase as written in the shared facets file; a
// trailing * makes it a prefix term.
func NewMatchTerm(phrase string) MatchTerm {
	prefix := strings.HasSuffix(phrase, "*")
	return MatchTerm{Text: matchText(strings.TrimSuffix(phrase, "*")), Prefix: prefix}
}

// MatchFacet is one idea a request can mention: its terms, its paired terms,
// which mention it only in a sentence that also holds one of its companions,
// the label the agent reads and the facets mentioning it implies.
type MatchFacet struct {
	ID         string      `json:"id"`
	Label      string      `json:"label"`
	Terms      []MatchTerm `json:"terms"`
	Paired     []MatchTerm `json:"paired,omitempty"`
	Companions []MatchTerm `json:"companions,omitempty"`
	Implies    []string    `json:"implies,omitempty"`
}

// ExampleRule is one example's match block with what the decision reads of
// the example: the connectors of its dex-app.yaml, the number of Steps of its
// design and the example it extends.
type ExampleRule struct {
	ID         string   `json:"id"`
	Core       []string `json:"core"`
	Signals    []string `json:"signals"`
	Conflicts  []string `json:"conflicts"`
	Connectors []string `json:"connectors"`
	Steps      int      `json:"steps"`
	Extends    string   `json:"extends,omitempty"`
}

// MatchRules is the decision's data, its examples in catalog order, as the
// search index lists them.
type MatchRules struct {
	SchemaVersion int           `json:"schemaVersion"`
	Negators      []MatchTerm   `json:"negators"`
	Facets        []MatchFacet  `json:"facets"`
	DefaultSource []MatchTerm   `json:"defaultSource"`
	Examples      []ExampleRule `json:"examples"`
}

// ExampleMatch is the example a request starts from: the facets of its match
// block the request mentions, in the block's order, and its score.
type ExampleMatch struct {
	ExampleID string
	Facets    []string
	Score     int
}

// matchText is text as terms are matched in it: its lower-cased ASCII letter
// and digit runs, separated by one space.
func matchText(text string) string { return strings.Join(searchWords(text), " ") }

// sentence is one fragment of a text between sentence ends, and whether a
// colon ends it.
type sentence struct {
	text      string
	endsColon bool
}

// sentences splits the first maximumSearchedRunes code points of text where
// a sentence ends: after ., !, ?, : or ; before white space or the end, and
// at a line break.
func sentences(text string) []sentence {
	runes := []rune(text)
	runes = runes[:min(len(runes), maximumSearchedRunes)]
	var out []sentence
	start := 0
	for index, r := range runes {
		switch {
		case r == '\n':
			out, start = append(out, sentence{text: string(runes[start:index])}), index+1
		case strings.ContainsRune(".!?:;", r) && (index+1 == len(runes) || strings.ContainsRune(" \t\r\n", runes[index+1])):
			out, start = append(out, sentence{text: string(runes[start : index+1]), endsColon: r == ':'}), index+1
		}
	}
	return append(out, sentence{text: string(runes[start:])})
}

// positions are where term starts in text, a matchText, at word boundaries.
func positions(text string, term MatchTerm) []int {
	var found []int
	for from := 0; from <= len(text); {
		at := strings.Index(text[from:], term.Text)
		if at < 0 || term.Text == "" {
			return found
		}
		at += from
		end := at + len(term.Text)
		if (at == 0 || text[at-1] == ' ') && (term.Prefix || end == len(text) || text[end] == ' ') {
			found = append(found, at)
		}
		from = at + 1
	}
	return found
}

// mentions reports whether text, a matchText, holds one of terms.
func mentions(text string, terms []MatchTerm) bool {
	for _, term := range terms {
		if len(positions(text, term)) > 0 {
			return true
		}
	}
	return false
}

// occurrence is where a facet occurs in a sentence's matchText.
type occurrence struct {
	facet string
	at    int
}

// occurrences are where the facets occur in text, a matchText of one
// sentence: at each of their terms, and at each paired term with a companion
// in the sentence, at the later of the paired term and the first companion.
func (rules *MatchRules) occurrences(text string) []occurrence {
	var found []occurrence
	for _, facet := range rules.Facets {
		for _, term := range facet.Terms {
			for _, at := range positions(text, term) {
				found = append(found, occurrence{facet: facet.ID, at: at})
			}
		}
		companion := -1
		for _, term := range facet.Companions {
			if at := positions(text, term); len(at) > 0 && (companion < 0 || at[0] < companion) {
				companion = at[0]
			}
		}
		if companion < 0 {
			continue
		}
		for _, term := range facet.Paired {
			for _, at := range positions(text, term) {
				found = append(found, occurrence{facet: facet.ID, at: max(at, companion)})
			}
		}
	}
	return found
}

// facetsOf finds the facets text mentions and those it negates. In a
// sentence, a facet after its first negator is negated; when no facet follows
// the sentence's last negator, as in "AI is not allowed", every facet of the
// sentence is; and a short heading such as "Not needed:" negates the next
// sentence. A negated facet is never mentioned, and a mentioned facet implies
// its implied facets unless they are negated.
func (rules *MatchRules) facetsOf(text string) (mentioned, negated map[string]bool) {
	mentioned, negated = map[string]bool{}, map[string]bool{}
	carried := false
	for _, fragment := range sentences(text) {
		normalized := matchText(fragment.text)
		var negations []int
		for _, negator := range rules.Negators {
			negations = append(negations, positions(normalized, negator)...)
		}
		slices.Sort(negations)
		found := rules.occurrences(normalized)
		trailing := len(negations) > 0 && !slices.ContainsFunc(found, func(at occurrence) bool { return at.at > negations[len(negations)-1] })
		for _, at := range found {
			if carried || trailing || len(negations) > 0 && at.at > negations[0] {
				negated[at.facet] = true
			} else {
				mentioned[at.facet] = true
			}
		}
		carried = fragment.endsColon && trailing && len(strings.Fields(normalized)) <= maximumHeadingWords
	}
	for id := range negated {
		delete(mentioned, id)
	}
	for _, facet := range rules.Facets {
		if mentioned[facet.ID] {
			for _, implied := range facet.Implies {
				if !negated[implied] {
					mentioned[implied] = true
				}
			}
		}
	}
	return mentioned, negated
}

// MentionedFacets are the facets text mentions, in facet order.
func (rules *MatchRules) MentionedFacets(text string) []string {
	mentioned, _ := rules.facetsOf(text)
	facets := []string{}
	for _, facet := range rules.Facets {
		if mentioned[facet.ID] {
			facets = append(facets, facet.ID)
		}
	}
	return facets
}

// candidate is one shortlisted example with its score.
type candidate struct {
	rule     ExampleRule
	position int
	score    int
	matched  []string
}

// evaluate scores rule against the facets; false when the example is not
// eligible: a core facet missing, a conflict mentioned, a facet of it
// negated or a score below minimumScore.
func evaluate(rule ExampleRule, mentioned, negated map[string]bool) (candidate, bool) {
	scored := candidate{rule: rule}
	for _, facet := range rule.Core {
		if !mentioned[facet] {
			return candidate{}, false
		}
		scored.score += coreWeight
		scored.matched = append(scored.matched, facet)
	}
	for _, facet := range rule.Signals {
		if mentioned[facet] {
			scored.score += signalWeight
			scored.matched = append(scored.matched, facet)
		}
	}
	for _, connector := range rule.Connectors {
		if !mentioned[connector] && !slices.Contains(rule.Core, connector) {
			scored.score -= surplusPenalty
		}
	}
	for _, facets := range [][]string{rule.Core, rule.Signals, rule.Connectors} {
		for _, facet := range facets {
			if negated[facet] {
				return candidate{}, false
			}
		}
	}
	for _, facet := range rule.Conflicts {
		if mentioned[facet] {
			return candidate{}, false
		}
	}
	return scored, scored.score >= minimumScore
}

// Pick decides which example text starts from, with index, the search index
// of the same examples: among the search's best shortlistSize, the eligible
// example with the highest score, then the fewest Steps, then catalog order,
// when it is the only one, extends the runner-up or is extended by it, or
// leads it by winningMargin. A text in another script than Latin, a text that
// names the default example's source, a win of the default and every other
// outcome start from the default, false.
func (rules *MatchRules) Pick(index *SearchIndex, text string) (ExampleMatch, bool) {
	if inOtherScript(text) || mentions(matchText(text), rules.DefaultSource) {
		return ExampleMatch{}, false
	}
	mentioned, negated := rules.facetsOf(text)
	var eligible []candidate
	for _, position := range index.Closest(text, shortlistSize) {
		if scored, isEligible := evaluate(rules.Examples[position], mentioned, negated); isEligible {
			scored.position = position
			eligible = append(eligible, scored)
		}
	}
	scores := make([]int, len(eligible))
	for at, scored := range eligible {
		scores[at] = scored.score - coreWeight*len(uncoveredCore(scored.rule, eligible))
	}
	for at := range eligible {
		eligible[at].score = scores[at]
	}
	eligible = slices.DeleteFunc(eligible, func(scored candidate) bool { return scored.score < minimumScore })
	if len(eligible) == 0 {
		return ExampleMatch{}, false
	}
	slices.SortFunc(eligible, func(left, right candidate) int {
		switch {
		case left.score != right.score:
			return right.score - left.score
		case left.rule.Steps != right.rule.Steps:
			return left.rule.Steps - right.rule.Steps
		}
		return left.position - right.position
	})
	top := eligible[0]
	if len(eligible) > 1 {
		next := eligible[1]
		if !related(top.rule, next.rule) && top.score-next.score < winningMargin {
			return ExampleMatch{}, false
		}
	}
	if top.rule.ID == DefaultExampleID {
		return ExampleMatch{}, false
	}
	return ExampleMatch{ExampleID: top.rule.ID, Facets: top.matched, Score: top.score}, true
}

// related reports whether one of two examples extends the other.
func related(left, right ExampleRule) bool {
	return left.Extends == right.ID || right.Extends == left.ID
}

// uncoveredCore are the core facets of the other eligible examples, each once,
// that rule's block neither needs nor counts, skipping an example it extends
// or that extends it: the request asks for them, and rule's source lacks them.
func uncoveredCore(rule ExampleRule, eligible []candidate) []string {
	var uncovered []string
	for _, other := range eligible {
		if other.rule.ID == rule.ID || related(rule, other.rule) {
			continue
		}
		for _, facet := range other.rule.Core {
			if !slices.Contains(rule.Core, facet) && !slices.Contains(rule.Signals, facet) && !slices.Contains(uncovered, facet) {
				uncovered = append(uncovered, facet)
			}
		}
	}
	return uncovered
}

// inOtherScript reports whether the first maximumSearchedRunes code points of
// text hold a letter of a script other than Latin, such as Chinese: the
// decision reads English, and the English words of such a text are not the
// whole request, its negations included.
func inOtherScript(text string) bool {
	count := 0
	for _, r := range text {
		if count++; count > maximumSearchedRunes {
			return false
		}
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

// DecodeMatchRules reads the decision data the generator wrote for the
// examples of index.
func DecodeMatchRules(data []byte, index *SearchIndex) (*MatchRules, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var rules MatchRules
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("read the example match rules: %w", err)
	}
	if rules.SchemaVersion != 1 {
		return nil, fmt.Errorf("the example match rules have schemaVersion %d, not 1", rules.SchemaVersion)
	}
	if len(rules.Examples) != len(index.Examples) {
		return nil, fmt.Errorf("the example match rules hold %d examples, the search index %d", len(rules.Examples), len(index.Examples))
	}
	for position, rule := range rules.Examples {
		if rule.ID != index.Examples[position].ID {
			return nil, fmt.Errorf("the example match rules list %s where the search index lists %s", rule.ID, index.Examples[position].ID)
		}
	}
	return &rules, nil
}

var matchRules = sync.OnceValue(func() *MatchRules {
	return comm.Must(DecodeMatchRules(comm.Must(exampleFiles.ReadFile(MatchRulesFile)), searchIndex()))
})

// ClosestExample decides which example of this server build a Project
// created from text starts from: false for the default. It reads no network
// and calls no model; the same text gives the same answer on every replica of
// one build.
func ClosestExample(text string) (ExampleMatch, bool) {
	return matchRules().Pick(searchIndex(), text)
}

// FacetLabels are the labels of facets of this server build, in order; an
// unknown facet keeps its ID.
func FacetLabels(facets []string) []string {
	labels := make([]string, 0, len(facets))
	for _, id := range facets {
		label := id
		for _, facet := range matchRules().Facets {
			if facet.ID == id {
				label = facet.Label
			}
		}
		labels = append(labels, label)
	}
	return labels
}
