// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
)

// queue is the in-memory eventqueue.Queue of one process: a server restart
// drops what it holds.
type queue struct {
	limits eventqueue.Limits
	logger *slog.Logger
	mutex  sync.Mutex
	// waiting holds each key's items in offer order; ready lists the keys
	// with waiting items and none being consumed, in the order they became
	// ready.
	waiting     map[string][]eventqueue.Item
	ready       []string
	consuming   map[string]bool
	items, size int
	wake        chan struct{}
}

// New returns an empty queue with limits.
func New(limits eventqueue.Limits, logger *slog.Logger) eventqueue.Queue {
	return &queue{limits: limits, logger: logger, waiting: map[string][]eventqueue.Item{}, consuming: map[string]bool{}, wake: make(chan struct{}, 1)}
}

// Offer queues item, or refuses it over a limit.
func (q *queue) Offer(item eventqueue.Item) error {
	if item.Size > q.limits.MaxItemBytes {
		return eventqueue.ErrTooLarge
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	switch {
	case q.items+1 > q.limits.MaxItems || q.size+item.Size > q.limits.MaxBytes:
		return eventqueue.ErrFull
	case len(q.waiting[item.Key]) >= q.limits.MaxKeyItems:
		return eventqueue.ErrKeyFull
	}
	q.items, q.size = q.items+1, q.size+item.Size
	if len(q.waiting[item.Key]) == 0 && !q.consuming[item.Key] {
		q.ready = append(q.ready, item.Key)
	}
	q.waiting[item.Key] = append(q.waiting[item.Key], item)
	q.signal()
	return nil
}

// Consume calls consume with each item until ctx ends.
func (q *queue) Consume(ctx context.Context, consume func(context.Context, any)) {
	var running sync.WaitGroup
	defer running.Wait()
	slots := make(chan struct{}, q.limits.Concurrency)
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		item, found := q.next()
		for !found {
			select {
			case <-q.wake:
			case <-ctx.Done():
				return
			}
			item, found = q.next()
		}
		running.Go(func() {
			defer func() { <-slots }()
			// A consumer that panics loses its item but neither its key's turn nor
			// the counted totals.
			defer q.done(item)
			defer func() {
				if recovered := recover(); recovered != nil {
					q.logger.Error("event queue consumer panicked; the item is dropped", "key", item.Key, "panic", fmt.Sprint(recovered))
				}
			}()
			consume(ctx, item.Value)
		})
	}
}

// next takes the first item of the first ready key.
func (q *queue) next() (eventqueue.Item, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if len(q.ready) == 0 {
		return eventqueue.Item{}, false
	}
	key := q.ready[0]
	q.ready = q.ready[1:]
	item := q.waiting[key][0]
	if q.waiting[key] = q.waiting[key][1:]; len(q.waiting[key]) == 0 {
		delete(q.waiting, key)
	}
	q.consuming[key] = true
	return item, true
}

// done releases a consumed item and makes its key ready again when more of
// its items wait.
func (q *queue) done(item eventqueue.Item) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.items, q.size = q.items-1, q.size-item.Size
	delete(q.consuming, item.Key)
	if len(q.waiting[item.Key]) > 0 {
		q.ready = append(q.ready, item.Key)
		q.signal()
	}
}

// signal wakes Consume without blocking; one pending wake-up suffices,
// because Consume takes every ready key before it waits again.
func (q *queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
