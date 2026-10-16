// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package slackrelay

import (
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// The recent events' bounds: a scope keeps its newest recentEventsPerScope
// entries, for at most recentEventScopes scopes, the least recently written
// scope dropped first; every string of an entry is cut to maxMetadataBytes,
// which bounds the whole to about 25 MiB. The counts of events without a
// route are kept for at most unroutedConnections connections. Everything is
// memory only: a restart clears it and each replica keeps its own.
const (
	recentEventsPerScope = 50
	recentEventScopes    = 512
	maxMetadataBytes     = 128
	unroutedConnections  = 1024
)

// EventFate is the closed set of outcomes a routed event reaches, declared
// once here; its value is the persisted and wire spelling.
type EventFate string

const (
	// FateRecorded: the application answered 200.
	FateRecorded EventFate = "RECORDED"
	// FateNotRunning: a transport error or a gateway 502 or 504.
	FateNotRunning EventFate = "NOT_RUNNING"
	// FateApplicationUnavailable: the receiver answered 503, with no source
	// running or a failed recording.
	FateApplicationUnavailable EventFate = "APPLICATION_UNAVAILABLE"
	// FateRefused: any other answer but 200.
	FateRefused EventFate = "REFUSED"
	// FateTimedOut: no answer within the delivery timeout.
	FateTimedOut EventFate = "TIMED_OUT"
	// FateTokenUnavailable: the scope token could not be read to sign.
	FateTokenUnavailable EventFate = "TOKEN_UNAVAILABLE"
	// FateQueueFull: the queue refused the event for lack of room.
	FateQueueFull EventFate = "QUEUE_FULL"
	// FateEventTooLarge: the event exceeded the per-event size limit.
	FateEventTooLarge EventFate = "EVENT_TOO_LARGE"
)

// RecentEvent is the metadata of one event routed to a scope: never its text,
// its users, files or blocks, the body, a token or a signature.
type RecentEvent struct {
	ReceivedAt     time.Time
	EventID        string
	EventType      string
	EventSubtype   string
	TeamID         string
	ChannelID      string
	ConnectionName string
	Fate           EventFate
	// HTTPStatus is the application's answer, 0 without one.
	HTTPStatus int
	Duration   time.Duration
}

// RecentEventsView is what a process knows of the events routed to a scope.
type RecentEventsView struct {
	// Events are the scope's entries, newest first.
	Events []RecentEvent
	// BufferStartedAt is when this process began recording. An empty list
	// proves only that nothing arrived since then, not that Slack sent
	// nothing.
	BufferStartedAt time.Time
	// BufferInstanceID is random per process: two reads that differ in it came
	// from different server instances, whose buffers are separate.
	BufferInstanceID string
}

// UnroutedEvents counts the events of a connection's Slack installation that
// found no route since the process started: the connection is active, but no
// environment registered to receive its events.
type UnroutedEvents struct {
	Count       uint64
	LastEventAt time.Time
	LastEventID string
	lastWritten uint64
}

// recentEvents keeps the newest events of each scope in memory.
type recentEvents struct {
	mutex     sync.Mutex
	clock     uint64
	startedAt time.Time
	instance  string
	scopes    map[string]*scopeEvents
	unrouted  map[connectiondao.ConnectionKey]UnroutedEvents
}

// bounded cuts every string of the entry to maxMetadataBytes.
func (e RecentEvent) bounded() RecentEvent {
	for _, field := range []*string{&e.EventID, &e.EventType, &e.EventSubtype, &e.TeamID, &e.ChannelID, &e.ConnectionName} {
		*field = comm.TruncateUTF8(*field, maxMetadataBytes)
	}
	return e
}

type scopeEvents struct {
	// entries is a ring of recentEventsPerScope: the oldest sits at next
	// once full.
	entries     []RecentEvent
	next        int
	lastWritten uint64
}

func newRecentEvents(now time.Time) *recentEvents {
	return &recentEvents{startedAt: now, instance: flowid.RandomID(16), scopes: map[string]*scopeEvents{}, unrouted: map[connectiondao.ConnectionKey]UnroutedEvents{}}
}

func (e *recentEvents) add(key string, event RecentEvent) {
	event = event.bounded()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.clock++
	ring, found := e.scopes[key]
	if !found {
		ring = &scopeEvents{}
		e.scopes[key] = ring
	}
	ring.lastWritten = e.clock
	if len(ring.entries) < recentEventsPerScope {
		ring.entries = append(ring.entries, event)
	} else {
		ring.entries[ring.next] = event
		ring.next = (ring.next + 1) % recentEventsPerScope
	}
	for len(e.scopes) > recentEventScopes {
		oldest := ""
		for candidate, scope := range e.scopes {
			if candidate != key && (oldest == "" || scope.lastWritten < e.scopes[oldest].lastWritten) {
				oldest = candidate
			}
		}
		delete(e.scopes, oldest)
	}
}

// countUnrouted counts one event that found no route for the connection.
func (e *recentEvents) countUnrouted(key connectiondao.ConnectionKey, eventID string, at time.Time) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.clock++
	if _, found := e.unrouted[key]; !found && len(e.unrouted) >= unroutedConnections {
		var oldest connectiondao.ConnectionKey
		first := true
		for candidate, counted := range e.unrouted {
			if first || counted.lastWritten < e.unrouted[oldest].lastWritten {
				oldest, first = candidate, false
			}
		}
		delete(e.unrouted, oldest)
	}
	counted := e.unrouted[key]
	e.unrouted[key] = UnroutedEvents{Count: counted.Count + 1, LastEventAt: at, LastEventID: comm.TruncateUTF8(eventID, maxMetadataBytes), lastWritten: e.clock}
}

// list returns the scope's entries, newest first.
func (e *recentEvents) list(key string) []RecentEvent {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	ring, found := e.scopes[key]
	if !found {
		return nil
	}
	entries := make([]RecentEvent, 0, len(ring.entries))
	for i := 1; i <= len(ring.entries); i++ {
		entries = append(entries, ring.entries[(ring.next-i+2*len(ring.entries))%len(ring.entries)])
	}
	return entries
}

// RecentEvents returns the events this process routed to the scope, newest
// first, at most 50, with the time this process began recording.
func (r *Relay) RecentEvents(projectID, scope string) RecentEventsView {
	return RecentEventsView{Events: r.recent.list(scopeKey(projectID, scope)), BufferStartedAt: r.recent.startedAt, BufferInstanceID: r.recent.instance}
}

// UnroutedEvents returns how many events of the connection's Slack
// installation this process found no route for.
func (r *Relay) UnroutedEvents(teamSpaceID, connectionID string) UnroutedEvents {
	r.recent.mutex.Lock()
	defer r.recent.mutex.Unlock()
	return r.recent.unrouted[connectiondao.ConnectionKey{TeamSpaceID: teamSpaceID, ConnectionID: connectionID}]
}

// record adds the delivery's event with its fate to its scope's entries;
// duration is how long its delivery took, 0 for an event that never left the
// queue.
func (r *Relay) record(d delivery, fate EventFate, status int, duration time.Duration) {
	r.recent.add(scopeKey(d.projectID, d.scope), RecentEvent{
		ReceivedAt: d.receivedAt, EventID: d.eventID, EventType: d.eventType, EventSubtype: d.eventSubtype, TeamID: d.teamID,
		ChannelID: d.channelID, ConnectionName: d.connectionName, Fate: fate, HTTPStatus: status, Duration: duration,
	})
}
