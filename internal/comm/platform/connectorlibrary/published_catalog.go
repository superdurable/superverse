// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

const (
	// catalogFetchTimeout bounds one fetch of the catalog document.
	catalogFetchTimeout = 15 * time.Second
	// catalogRetryDelay is when Run fetches again after a failed fetch.
	catalogRetryDelay = time.Minute
)

// PublishedCatalog is this server replica's copy of the published catalog,
// one document of at most 1 MiB. Run keeps it warm in the background; Fresh
// serves the readers that may wait for a fetch, and RequestMatches the ones
// that must not. The copy holds only public data, and the last good copy
// stays until a newer one replaces it. Concurrent fetches may happen; none
// waits behind another.
type PublishedCatalog struct {
	httpClient *http.Client
	url        string
	mutex      sync.Mutex
	current    *Catalog
}

// NewPublishedCatalog reads PublishedCatalogURL with httpClient, which the
// caller owns.
func NewPublishedCatalog(httpClient *http.Client) *PublishedCatalog {
	return &PublishedCatalog{httpClient: httpClient, url: PublishedCatalogURL}
}

// Run keeps the copy warm until ctx ends: it fetches at once and then every
// CatalogLifetime, and a minute after a failed fetch, which it logs and which
// keeps the last copy. The caller owns the goroutine that runs it.
func (published *PublishedCatalog) Run(ctx context.Context, logger *slog.Logger) {
	for {
		wait := CatalogLifetime
		if _, err := published.fetch(ctx); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "the connector catalog refresh failed; the last copy stays", "error", err.Error())
			wait = catalogRetryDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Fresh returns the copy while it is younger than CatalogLifetime, and
// otherwise fetches the document now.
func (published *PublishedCatalog) Fresh(ctx context.Context) (Catalog, error) {
	if current, warm := published.warm(); warm && time.Since(current.ObservedAt) < CatalogLifetime {
		return current, nil
	}
	return published.fetch(ctx)
}

// warm returns the last good copy without a network call; false while no
// fetch has succeeded.
func (published *PublishedCatalog) warm() (Catalog, bool) {
	published.mutex.Lock()
	defer published.mutex.Unlock()
	if published.current == nil {
		return Catalog{}, false
	}
	return *published.current, true
}

// fetch reads the document within catalogFetchTimeout and, when it is a
// valid catalog, keeps it as the copy.
func (published *PublishedCatalog) fetch(ctx context.Context) (Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	response, err := published.httpClient.Do(comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, published.url, nil)))
	if err != nil {
		return Catalog{}, fmt.Errorf("fetch the connector catalog: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("fetch the connector catalog: HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, MaximumCatalogBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("read the connector catalog: %w", err)
	}
	catalog, err := DecodeCatalog(contents, time.Now())
	if err != nil {
		return Catalog{}, err
	}
	published.mutex.Lock()
	if published.current == nil || !published.current.ObservedAt.After(catalog.ObservedAt) {
		published.current = &catalog
	}
	published.mutex.Unlock()
	return catalog, nil
}
