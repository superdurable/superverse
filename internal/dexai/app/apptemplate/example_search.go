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

	"github.com/superdurable/superversev2/internal/comm"
)

// SearchIndexFile is the generated example search index in the embedded
// examples directory. Studio bundles the same bytes
// (web/studio/lib/generated/example-catalog/example-search.json) and ranks
// its example grid with the same search (web/studio/lib/examples).
const SearchIndexFile = "examples/example-search.json"

// maximumSearchedRunes bounds the text a search or a match reads: its first
// 8,000 code points.
const maximumSearchedRunes = 8000

// minimumWordLength is the length below which a word is no search term.
const minimumWordLength = 2

// minimumStemLength is the length an ending cut leaves at least.
const minimumStemLength = 4

// stemEndings are cut from a word, longest first, until none applies, so that
// "approve", "approved", "approvals" and "approving" meet; "ies" becomes "y".
// web/studio/lib/examples/example-search.ts declares the same list.
var stemEndings = []string{"ing", "ies", "ed", "es", "al", "s", "e"}

// SearchExample is one example as the start page shows it, in catalog order.
type SearchExample struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Apps        []string `json:"apps"`
	Decider     string   `json:"decider"`
	Description string   `json:"description"`
}

// synonymGroup is one group of the shared synonym list: each occurrence of
// one of its phrases, as stems, also counts as Term, the stems of its first
// phrase.
type synonymGroup struct {
	Term    string     `json:"term"`
	Phrases [][]string `json:"phrases"`
}

// SearchVocabulary is what turns a text into search terms besides the stemmer:
// the stop words and the synonym groups.
type SearchVocabulary struct {
	StopWords []string       `json:"stopWords"`
	Synonyms  []synonymGroup `json:"synonyms"`
}

// SearchIndex is the generated index of the examples: the vocabulary and, for
// each term, its postings, [example position, score in thousandths] in
// position order. The generator scores every posting with BM25 over the
// weighted fields, so a search only adds integers and Go and TypeScript rank
// alike.
type SearchIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	Examples      []SearchExample `json:"examples"`
	SearchVocabulary
	Postings map[string][][2]int `json:"postings"`
}

// NewSearchVocabulary compiles the shared stop words and synonym groups, each
// group a list of phrases whose first names the group.
func NewSearchVocabulary(stopWords []string, synonyms [][]string) SearchVocabulary {
	vocabulary := SearchVocabulary{StopWords: slices.Sorted(slices.Values(stopWords)), Synonyms: []synonymGroup{}}
	for _, group := range synonyms {
		compiled := synonymGroup{}
		for _, phrase := range group {
			compiled.Phrases = append(compiled.Phrases, stems(searchWords(phrase)))
		}
		compiled.Term = strings.Join(compiled.Phrases[0], " ")
		vocabulary.Synonyms = append(vocabulary.Synonyms, compiled)
	}
	return vocabulary
}

// searchWords are the ASCII letter and digit runs of the first
// maximumSearchedRunes code points of text, lower-cased; every other
// character, a non-ASCII letter included, separates words.
func searchWords(text string) []string {
	var words []string
	var word strings.Builder
	count := 0
	for _, r := range text {
		if count++; count > maximumSearchedRunes {
			break
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			word.WriteRune(r)
			continue
		}
		if word.Len() > 0 {
			words, word = append(words, word.String()), strings.Builder{}
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

// stem cuts endings from a word until none applies with minimumStemLength
// letters left.
func stem(word string) string {
	for cut := true; cut; {
		cut = false
		for _, ending := range stemEndings {
			if strings.HasSuffix(word, ending) && len(word)-len(ending) >= minimumStemLength {
				word, cut = strings.TrimSuffix(word, ending), true
				if ending == "ies" {
					word += "y"
				}
				break
			}
		}
	}
	return word
}

func stems(words []string) []string {
	out := make([]string, len(words))
	for index, word := range words {
		out[index] = stem(word)
	}
	return out
}

// Terms are the search terms of text in order, repeated as they occur: the
// stem of every word of at least minimumWordLength letters that is not a stop
// word, and the term of a synonym group for each occurrence of one of its
// phrases that is not that term itself.
func (vocabulary SearchVocabulary) Terms(text string) []string {
	words := searchWords(text)
	stemmed := stems(words)
	var terms []string
	for index, word := range words {
		if _, stop := slices.BinarySearch(vocabulary.StopWords, word); len(word) >= minimumWordLength && !stop {
			terms = append(terms, stemmed[index])
		}
		for _, group := range vocabulary.Synonyms {
			for _, phrase := range group.Phrases {
				if (len(phrase) != 1 || phrase[0] != group.Term) && index+len(phrase) <= len(stemmed) && slices.Equal(stemmed[index:index+len(phrase)], phrase) {
					terms = append(terms, group.Term)
				}
			}
		}
	}
	return terms
}

// Closest returns the positions of the examples closest to text, best first
// and at most limit: the sum of the postings of the text's distinct terms,
// ties in catalog order. An example that shares no term is not among them.
func (index *SearchIndex) Closest(text string, limit int) []int {
	scores := make([]int, len(index.Examples))
	var positions []int
	seen := map[string]bool{}
	for _, term := range index.Terms(text) {
		if seen[term] {
			continue
		}
		seen[term] = true
		for _, posting := range index.Postings[term] {
			if scores[posting[0]] == 0 {
				positions = append(positions, posting[0])
			}
			scores[posting[0]] += posting[1]
		}
	}
	slices.SortFunc(positions, func(left, right int) int {
		if scores[left] != scores[right] {
			return scores[right] - scores[left]
		}
		return left - right
	})
	return positions[:min(limit, len(positions))]
}

// DecodeSearchIndex reads a search index the generator wrote.
func DecodeSearchIndex(data []byte) (*SearchIndex, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var index SearchIndex
	if err := decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("read the example search index: %w", err)
	}
	if index.SchemaVersion != 1 {
		return nil, fmt.Errorf("the example search index has schemaVersion %d, not 1", index.SchemaVersion)
	}
	for term, postings := range index.Postings {
		for _, posting := range postings {
			if posting[0] < 0 || posting[0] >= len(index.Examples) || posting[1] <= 0 {
				return nil, fmt.Errorf("the example search index has an invalid posting of %q", term)
			}
		}
	}
	return &index, nil
}

var searchIndex = sync.OnceValue(func() *SearchIndex {
	return comm.Must(DecodeSearchIndex(comm.Must(exampleFiles.ReadFile(SearchIndexFile))))
})
