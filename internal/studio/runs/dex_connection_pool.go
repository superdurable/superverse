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
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

const (
	// dexVerification is how long a pooled connection serves after its dial
	// verified the recorded identity of its Dex.
	dexVerification = 30 * time.Second
	// dexDialTimeout bounds one dial with its identity checks.
	dexDialTimeout = 30 * time.Second
)

// errPoolClosed refuses a request that arrives after Runs.Close.
var errPoolClosed = errors.New("the Studio Runs connection pool is closed")

// dial opens a connection bound to ctx: the end of ctx closes it, and so does
// the returned function.
type dial func(context.Context) (*grpc.ClientConn, func() error, error)

// dexConnectionPool holds the verified Dex connections of one replica, keyed by
// the recorded identity their dial verified. It owns every connection: an
// entry retires on expiry, LRU eviction, a failed transport or Close, and
// closes once no request holds it.
type dexConnectionPool struct {
	limit  int
	logger *slog.Logger
	dials  atomic.Uint64

	mu      sync.Mutex
	closed  bool
	entries map[string]*list.Element
	// order holds *pooledConnection, the most recently used first.
	order *list.List
	// closable holds the retired entries that no request holds any more,
	// which unlock closes outside mu.
	closable []*pooledConnection
}

// pooledConnection is one dial of one key. ready closes when the dial ends;
// conn, closeConn and err are set before.
type pooledConnection struct {
	key       string
	ready     chan struct{}
	conn      *grpc.ClientConn
	closeConn func() error
	err       error
	verified  time.Time
	// leases counts the requests that hold the connection; a retired entry
	// closes when the last one releases it.
	leases  int
	retired bool
}

func newDexConnectionPool(limit int, logger *slog.Logger) *dexConnectionPool {
	return &dexConnectionPool{limit: limit, logger: logger, entries: map[string]*list.Element{}, order: list.New()}
}

// acquire returns a verified connection for key, dialing it when no fresh one
// is pooled, and the function that releases it. Requests for one key share a
// dial; each waits with its own ctx.
func (p *dexConnectionPool) acquire(ctx context.Context, key string, open dial) (*grpc.ClientConn, func(), error) {
	p.mu.Lock()
	if p.closed {
		p.unlock()
		return nil, nil, errPoolClosed
	}
	entry := p.current(key)
	if entry == nil {
		entry = &pooledConnection{key: key, ready: make(chan struct{})}
		p.entries[key] = p.order.PushFront(entry)
		for p.order.Len() > p.limit {
			p.retire(p.order.Back().Value.(*pooledConnection))
		}
		p.dials.Add(1)
		go p.dial(entry, open)
	}
	entry.leases++
	p.unlock()

	select {
	case <-entry.ready:
	case <-ctx.Done():
		p.release(entry)
		return nil, nil, ctx.Err()
	}
	if entry.err != nil {
		p.release(entry)
		return nil, nil, entry.err
	}
	return entry.conn, func() { p.release(entry) }, nil
}

// current returns the pooled entry of key while it serves, and retires an
// expired or failed one. The caller holds mu.
func (p *dexConnectionPool) current(key string) *pooledConnection {
	element, found := p.entries[key]
	if !found {
		return nil
	}
	entry := element.Value.(*pooledConnection)
	select {
	case <-entry.ready:
		if entry.err != nil || time.Since(entry.verified) > dexVerification {
			p.retire(entry)
			return nil
		}
	default:
		// The dial is in flight; share it.
	}
	p.order.MoveToFront(element)
	return entry
}

// dial runs one entry's dial under a context the pool owns, bounded by
// dexDialTimeout, so the connection outlives the request that asked for it.
func (p *dexConnectionPool) dial(entry *pooledConnection, open dial) {
	ctx, cancel := context.WithCancel(context.Background())
	expired := time.AfterFunc(dexDialTimeout, cancel)
	conn, closeConn, err := open(ctx)
	if !expired.Stop() && err == nil {
		// The bound passed as the dial returned: its context closed the connection.
		err = errors.Join(context.DeadlineExceeded, closeConn())
	}
	p.mu.Lock()
	if err != nil {
		cancel()
		entry.err = err
	} else {
		entry.conn, entry.verified = conn, time.Now()
		entry.closeConn = func() error { defer cancel(); return closeConn() }
	}
	close(entry.ready)
	if err != nil {
		p.retire(entry)
	}
	p.closeIfUnused(entry)
	p.unlock()
}

// release returns a request's lease. A connection that the request left
// failed retires, so the next request verifies its Dex again.
func (p *dexConnectionPool) release(entry *pooledConnection) {
	p.mu.Lock()
	entry.leases--
	select {
	case <-entry.ready:
		if entry.conn != nil {
			if state := entry.conn.GetState(); state == connectivity.TransientFailure || state == connectivity.Shutdown {
				p.retire(entry)
			}
		}
	default:
	}
	p.closeIfUnused(entry)
	p.unlock()
}

// retire removes the entry from the pool; it closes once its dial ended and
// no request holds it. The caller holds mu.
func (p *dexConnectionPool) retire(entry *pooledConnection) {
	if entry.retired {
		return
	}
	entry.retired = true
	if element, found := p.entries[entry.key]; found && element.Value == entry {
		p.order.Remove(element)
		delete(p.entries, entry.key)
	}
	p.closeIfUnused(entry)
}

// closeIfUnused queues a retired entry's connection for closing once its dial
// ended and its last lease returned. The caller holds mu.
func (p *dexConnectionPool) closeIfUnused(entry *pooledConnection) {
	if !entry.retired || entry.leases > 0 || entry.conn == nil {
		return
	}
	select {
	case <-entry.ready:
		p.closable = append(p.closable, entry)
		// Queue each connection once.
		entry.conn = nil
	default:
	}
}

// unlock releases mu and then closes the queued connections.
func (p *dexConnectionPool) unlock() {
	closable := p.closable
	p.closable = nil
	p.mu.Unlock()
	for _, entry := range closable {
		if err := entry.closeConn(); err != nil {
			p.logger.Warn("Studio Runs connection cleanup failed", "error", err)
		}
	}
}

// close retires every entry: idle connections close now, held ones when their
// requests release them, and later requests are refused.
func (p *dexConnectionPool) close() {
	p.mu.Lock()
	defer p.unlock()
	p.closed = true
	for p.order.Len() > 0 {
		p.retire(p.order.Front().Value.(*pooledConnection))
	}
}
