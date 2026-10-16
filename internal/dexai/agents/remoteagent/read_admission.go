// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"context"
	"errors"
	"sync"
)

var (
	errReadAdmissionFull   = errors.New("remote read admission is full")
	errReadAdmissionClosed = errors.New("remote read admission is closed")
)

// readAdmission is a disposable replica-local FIFO. The owning Endpoint first
// bounds sync requests, so this queue cannot accumulate unbounded waiters.
type readAdmission struct {
	mu                            sync.Mutex
	limit, maximumWaiters, active int
	waiters                       []*readWaiter
	closed                        bool
}

type readWaiter struct {
	ready   chan struct{}
	granted bool
}

func newReadAdmission(limit, maximumWaiters int) *readAdmission {
	if limit < 1 || limit > 64 || maximumWaiters < 1 || maximumWaiters > 10000 {
		panic("invalid remote read admission bounds")
	}
	return &readAdmission{limit: limit, maximumWaiters: maximumWaiters}
}

func (gate *readAdmission) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mu.Lock()
	if gate.closed {
		gate.mu.Unlock()
		return nil, errReadAdmissionClosed
	}
	if gate.active < gate.limit && len(gate.waiters) == 0 {
		gate.active++
		gate.mu.Unlock()
		return gate.releaseOnce(), nil
	}
	if len(gate.waiters) >= gate.maximumWaiters {
		gate.mu.Unlock()
		return nil, errReadAdmissionFull
	}
	waiter := &readWaiter{ready: make(chan struct{})}
	gate.waiters = append(gate.waiters, waiter)
	gate.mu.Unlock()
	select {
	case <-waiter.ready:
		gate.mu.Lock()
		granted := waiter.granted
		gate.mu.Unlock()
		if !granted {
			return nil, errReadAdmissionClosed
		}
		release := gate.releaseOnce()
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		gate.mu.Lock()
		if waiter.granted {
			gate.releaseLocked()
		} else {
			for index, queued := range gate.waiters {
				if queued == waiter {
					copy(gate.waiters[index:], gate.waiters[index+1:])
					gate.waiters[len(gate.waiters)-1] = nil
					gate.waiters = gate.waiters[:len(gate.waiters)-1]
					break
				}
			}
		}
		gate.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (gate *readAdmission) releaseOnce() func() {
	var once sync.Once
	return func() { once.Do(func() { gate.mu.Lock(); defer gate.mu.Unlock(); gate.releaseLocked() }) }
}

func (gate *readAdmission) releaseLocked() {
	gate.active--
	if gate.active < 0 {
		panic("remote read slot released without admission")
	}
	if gate.closed || len(gate.waiters) == 0 {
		return
	}
	next := gate.waiters[0]
	copy(gate.waiters, gate.waiters[1:])
	gate.waiters[len(gate.waiters)-1] = nil
	gate.waiters = gate.waiters[:len(gate.waiters)-1]
	next.granted = true
	gate.active++
	close(next.ready)
}

// close rejects new reads and wakes queued requests. Active reads retain their
// request cancellation and release ownership; shutdown does not double-release.
func (gate *readAdmission) close() {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.closed {
		return
	}
	gate.closed = true
	for _, waiter := range gate.waiters {
		close(waiter.ready)
	}
	gate.waiters = nil
}
