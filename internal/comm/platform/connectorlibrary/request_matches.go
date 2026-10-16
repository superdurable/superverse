// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
)

const (
	// maximumRequestMatches bounds the connectors one request's matches list.
	maximumRequestMatches = 6
	// maximumRequestMatchesBytes bounds the JSON of one request's matches.
	maximumRequestMatchesBytes = 8 << 10
	// maximumMatchedRequestBytes bounds the request text that is searched.
	maximumMatchedRequestBytes = 32 << 10
)

// catalogAlias is another name people use for a released connector, which
// applies only while the catalog lists the connector.
type catalogAlias struct{ name, connectorID string }

var catalogAliases = []catalogAlias{
	{"Sheets", "google-sheets"}, {"Google Mail", "gmail"}, {"Outlook", "outlook-mail"}, {"Teams", "microsoft-teams"},
	{"Excel", "microsoft-excel"}, {"SharePoint", "microsoft-onedrive"}, {"Entra", "microsoft-entra-id"}, {"S3", "amazon-s3"},
	{"Postgres", "postgresql"}, {"Zendesk", "zendesk-support"}, {"Twilio", "twilio-messaging"}, {"Hacker News", "hacker-news-daily"},
}

// commonWords are connector names that are also everyday words. Such a
// display name or alias names its connector only as the catalog or the alias
// table capitalizes it, such as Front, Linear or LLM, and such an ID or
// directory leaf never does.
var commonWords = map[string]bool{
	"calendar": true, "crm": true, "desk": true, "docs": true, "drive": true, "email": true, "excel": true, "forms": true,
	"front": true, "gemini": true, "linear": true, "llm": true, "messaging": true, "monday": true, "notion": true,
	"outlook": true, "sheets": true, "snowflake": true, "spreadsheet": true, "support": true, "teams": true, "webhook": true,
	"workspace admin": true, "zoom": true,
}

// catalogForm is one way a request names a catalog entry: a display name,
// the ID with hyphens read as spaces, the directory's last element or an
// alias. An exact form matches only as written.
type catalogForm struct {
	text  string
	exact bool
	entry int
}

// RequestMatches finds the released connectors that request names, by whole
// words and in the order they first appear, in the copy of the catalog
// without any network call. It answers the CatalogMatches of at most six
// connectors as compact JSON of at most 8 KiB, or "" when no fetch has
// succeeded yet or nothing matches.
func (published *PublishedCatalog) RequestMatches(request string) string {
	catalog, warm := published.warm()
	if !warm {
		return ""
	}
	request = strings.Join(strings.Fields(comm.TruncateUTF8(request, maximumMatchedRequestBytes)), " ")
	order := namedEntries(request, catalogForms(catalog.Entries))
	if len(order) == 0 {
		return ""
	}
	matches := CatalogMatches{Source: PublishedCatalogURL, SHA256: catalog.SHA256, ObservedAt: catalog.ObservedAt, Total: len(order)}
	for _, index := range order[:min(len(order), maximumRequestMatches)] {
		matches.Matches = append(matches.Matches, catalog.Entries[index].Match())
	}
	text, _ := matches.FitJSON(maximumRequestMatchesBytes)
	return text
}

// catalogForms lists the forms of every entry. A common word is exact as a
// display name or alias and left out as an ID or a directory leaf.
func catalogForms(entries []CatalogEntry) []catalogForm {
	var forms []catalogForm
	add := func(text string, entry int, isName bool) {
		text = strings.Join(strings.Fields(strings.ReplaceAll(text, "-", " ")), " ")
		isCommon := commonWords[strings.ToLower(text)]
		if text != "" && (isName || !isCommon) {
			forms = append(forms, catalogForm{text: text, exact: isCommon, entry: entry})
		}
	}
	indexes := map[string]int{}
	for index, entry := range entries {
		indexes[entry.ID] = index
		add(entry.Name, index, true)
		add(entry.ID, index, false)
		add(path.Base(entry.Directory), index, false)
	}
	for _, alias := range catalogAliases {
		if index, listed := indexes[alias.connectorID]; listed {
			add(alias.name, index, true)
		}
	}
	return forms
}

// namedEntries returns the entries that request names, in the order of their
// first appearance. Overlapping forms resolve to the leftmost, then the
// longest, so "Outlook Calendar" names the calendar and not the mail alias.
func namedEntries(request string, forms []catalogForm) []int {
	type span struct{ start, end, entry int }
	lowered := asciiLower(request)
	var spans []span
	for _, form := range forms {
		haystack, needle := lowered, asciiLower(form.text)
		if form.exact {
			haystack, needle = request, form.text
		}
		for offset := 0; ; {
			found := strings.Index(haystack[offset:], needle)
			if found < 0 {
				break
			}
			start := offset + found
			end := start + len(needle)
			if isWordBoundary(request, start) && isWordBoundary(request, end) {
				spans = append(spans, span{start, end, form.entry})
			}
			offset = start + 1
		}
	}
	slices.SortFunc(spans, func(a, b span) int { return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end)) })
	var order []int
	covered := 0
	for _, found := range spans {
		if found.start < covered {
			continue
		}
		covered = found.end
		if !slices.Contains(order, found.entry) {
			order = append(order, found.entry)
		}
	}
	return order
}

// isWordBoundary reports whether offset of text lies between a letter or
// digit and something else, or at either end.
func isWordBoundary(text string, offset int) bool {
	if offset == 0 || offset == len(text) {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(text[:offset])
	after, _ := utf8.DecodeRuneInString(text[offset:])
	return !isWordRune(before) || !isWordRune(after)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// asciiLower lowercases the ASCII letters of text only, so every byte offset
// stays where it was; the catalog's names are ASCII.
func asciiLower(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, text)
}
