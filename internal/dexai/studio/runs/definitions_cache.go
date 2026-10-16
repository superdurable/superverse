// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package runs

import (
	"container/list"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
)

// The definitions cache of one replica: parsed bundles keyed by the immutable,
// digest-verified object they came from, so an entry is never stale and its
// TTL only releases memory.
const (
	definitionsCacheEntries = 256
	definitionsCacheBytes   = 64 << 20
	definitionsCacheTTL     = 10 * time.Minute
)

// definitionsCache is a bounded LRU of parsed serving definitions.
type definitionsCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	// order holds *cachedDefinitions, the most recently used first.
	order *list.List
	bytes int
}

type cachedDefinitions struct {
	key    string
	bundle *fdg.Bundle
	bytes  int
	loaded time.Time
}

func newDefinitionsCache() *definitionsCache {
	return &definitionsCache{entries: map[string]*list.Element{}, order: list.New()}
}

// load returns the bundle cached under key, or reads, parses and caches it.
// An empty key, which a reference without a content digest yields, is never
// cached; neither is an error.
func (c *definitionsCache) load(key string, read func() ([]appmanifest.FlowDefinition, error)) (*fdg.Bundle, error) {
	if bundle := c.get(key); bundle != nil {
		return bundle, nil
	}
	definitions, err := read()
	if err != nil {
		return nil, err
	}
	bundle, err := servingDefinitions(definitions)
	if err != nil {
		return nil, err
	}
	size := 0
	for _, definition := range definitions {
		size += len(definition.Graph)
	}
	c.put(key, bundle, size)
	return bundle, nil
}

func (c *definitionsCache) get(key string) *fdg.Bundle {
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, found := c.entries[key]
	if !found {
		return nil
	}
	entry := element.Value.(*cachedDefinitions)
	if time.Since(entry.loaded) > definitionsCacheTTL {
		c.remove(element)
		return nil
	}
	c.order.MoveToFront(element)
	return entry.bundle
}

func (c *definitionsCache) put(key string, bundle *fdg.Bundle, size int) {
	if key == "" || size > definitionsCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, found := c.entries[key]; found {
		c.remove(element)
	}
	c.entries[key] = c.order.PushFront(&cachedDefinitions{key: key, bundle: bundle, bytes: size, loaded: time.Now()})
	c.bytes += size
	for c.order.Len() > definitionsCacheEntries || c.bytes > definitionsCacheBytes {
		c.remove(c.order.Back())
	}
}

func (c *definitionsCache) remove(element *list.Element) {
	entry := c.order.Remove(element).(*cachedDefinitions)
	delete(c.entries, entry.key)
	c.bytes -= entry.bytes
}
