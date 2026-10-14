// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package eventqueue

import (
	"context"
	"errors"
)

// Implementation names a Queue implementation; the server configuration
// selects one.
type Implementation string

// The implementations. Beta has only the in-memory one.
const (
	ImplementationMemory Implementation = "memory"
)

var (
	// ErrTooLarge is an item over the per-item size limit.
	ErrTooLarge = errors.New("the item is too large")
	// ErrFull is an item that would exceed the total item or byte limit.
	ErrFull = errors.New("the queue is full")
	// ErrKeyFull is an item whose key already has its limit of items.
	ErrKeyFull = errors.New("the key's queue is full")
)

// Limits bound a queue against memory exhaustion.
type Limits struct {
	// MaxItems and MaxBytes bound the items held in all keys, those being
	// consumed included.
	MaxItems, MaxBytes int
	// MaxItemBytes bounds one item.
	MaxItemBytes int
	// MaxKeyItems bounds the items waiting under one key.
	MaxKeyItems int
	// Concurrency bounds the items consumed at once across keys.
	Concurrency int
}

// Item is one queued value under its key, with the size its limits count.
// Value is the caller's own type, which Consume hands back.
type Item struct {
	Key   string
	Size  int
	Value any
}

// Queue holds items under keys, best effort: an item over a limit is refused
// and nothing is redelivered.
type Queue interface {
	// Offer queues an item without blocking, or returns ErrTooLarge, ErrFull
	// or ErrKeyFull.
	Offer(item Item) error
	// Consume calls consume with each item until ctx ends: one at a time and
	// in offer order per key, at most Limits.Concurrency at once across keys.
	// It returns once the calls in progress returned; items still queued are
	// dropped.
	Consume(ctx context.Context, consume func(context.Context, any))
}
