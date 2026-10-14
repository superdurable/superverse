// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
)

// Each catalog entry shows the logo its release publishes in the library's
// source at its release tag: logo.svg in the connector's directory, else the
// Studio icon ui/public/icon.svg. A release tag's files never change, so a
// logo, or the answer that a release publishes none, is kept for the life of
// the process. Studio shows a logo as an image, never as inline markup, so
// no script in it runs; one that holds a script or a foreign object counts
// as none all the same.

const (
	// logoLimit bounds one logo document.
	logoLimit = 32 << 10
	// logoBudget bounds the logo fetches of one catalog read; a logo it
	// misses is fetched again by the next read.
	logoBudget = 5 * time.Second
	// logoFetchers bounds the logo fetches of one catalog read at once.
	logoFetchers = 8
	// maxCatalogLogos bounds the releases whose logos the process keeps;
	// reaching it forgets them all, and the next reads fetch them again.
	maxCatalogLogos = 2 * maxCatalogEntries
)

// logoFiles are the files of a connector's directory that may hold its logo,
// in the order they are tried.
var logoFiles = []string{"logo.svg", "ui/public/icon.svg"}

// WithLogos returns the entries with the logos their releases publish, empty
// for none or for one that did not arrive within logoBudget.
func (catalog *Catalog) WithLogos(ctx context.Context, entries []CatalogEntry) []CatalogEntry {
	ctx, cancel := context.WithTimeout(ctx, logoBudget)
	defer cancel()
	entries = slices.Clone(entries)
	var fetches sync.WaitGroup
	slots := make(chan struct{}, logoFetchers)
	for index := range entries {
		entry := &entries[index]
		catalog.mutex.Lock()
		logo, known := catalog.logos[entry.release()]
		catalog.mutex.Unlock()
		if known {
			entry.LogoSVG = logo
			continue
		}
		fetches.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			logo, err := catalog.fetchLogo(ctx, *entry)
			if err != nil {
				return
			}
			catalog.mutex.Lock()
			if len(catalog.logos) >= maxCatalogLogos {
				clear(catalog.logos)
			}
			catalog.logos[entry.release()] = logo
			catalog.mutex.Unlock()
			entry.LogoSVG = logo
		})
	}
	fetches.Wait()
	return entries
}

// fetchLogo returns the logo of an entry's release, empty when it publishes
// none; an error is a failure the next read retries.
func (catalog *Catalog) fetchLogo(ctx context.Context, entry CatalogEntry) (string, error) {
	for _, file := range logoFiles {
		location := catalog.sourceURL + "/" + entry.directory + "/" + entry.Version + "/" + entry.directory + "/" + file
		response, err := catalog.httpClient.Do(comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, location, nil)))
		if err != nil {
			return "", fmt.Errorf("fetch the logo of %s %s: %w", entry.ConnectorID, entry.Version, err)
		}
		contents, err := readResponseBody(response.Body, logoLimit)
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusNotFound:
			continue
		case response.StatusCode != http.StatusOK:
			return "", fmt.Errorf("fetch the logo of %s %s: HTTP %d", entry.ConnectorID, entry.Version, response.StatusCode)
		case err != nil:
			return "", nil
		}
		return svgImage(contents), nil
	}
	return "", nil
}

// svgImage returns contents when they are an SVG document an image may show,
// and empty otherwise.
func svgImage(contents []byte) string {
	contents = bytes.TrimSpace(contents)
	lower := bytes.ToLower(contents)
	if !utf8.Valid(contents) || !bytes.HasPrefix(lower, []byte("<")) || !bytes.Contains(lower, []byte("<svg")) ||
		bytes.Contains(lower, []byte("<script")) || bytes.Contains(lower, []byte("<foreignobject")) || bytes.Contains(lower, []byte("javascript:")) {
		return ""
	}
	return string(contents)
}
