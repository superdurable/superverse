// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package watch

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// A resource Watch combines lossy Dex Streams with authoritative resource
// reads. It owns no operation resource or persisted state: every connection
// starts with a snapshot, and the signed cursor of each frame only lets a
// reconnect resume the Streams that frame was read from.

const (
	// reconcileInterval is how often a Watch authorizes the caller, rereads
	// its snapshot and, while a send is blocked, rechecks its sources.
	reconcileInterval = 5 * time.Second
	// readTimeout bounds one authoritative read: the caller's
	// authorization, a snapshot read with its reauthentication, a source
	// resolution or one source tail. It leaves room for a Flow client's own
	// 15-second retained read.
	readTimeout = 30 * time.Second
	// sendTimeout bounds the delivery of one frame to the client.
	sendTimeout = 10 * time.Second
	// bufferSize bounds the progress buffered for a slow client. On
	// overflow the Watch drops it and realigns on a fresh snapshot.
	bufferSize = 128
)

// ErrSourcesChanged ends a connection whose sources changed while a send
// was blocked: the transport may still own that send, so the client must
// reconnect for a fresh snapshot of the new sources.
var ErrSourcesChanged = errors.New("watch sources changed")

// Source is one lossy progress Stream of a watched resource.
type Source[T any] struct {
	// Identity names the Stream by its FlowID and Stream name, which a
	// Session Agent source prefixes with its FlowType. A generation with its
	// own progress, such as a Project setup, has its own FlowID.
	// Signed resume cursors are keyed by it, so it must stay byte-identical
	// across reads.
	Identity string
	// Tail returns the cursor of the newest retained message.
	Tail func(context.Context) (string, error)
	// Read blocks until the message after cursor and returns it with its cursor.
	Read func(ctx context.Context, cursor string) (T, string, error)
}

// Connection is what every Watch RPC binds from its request: the caller,
// the scope its resume cursors are signed for and the cursor to resume.
type Connection struct {
	Scope, ResumeCursor string
	Cursors             *CursorCodec
	// Logger records a panic of a send or a source read. Both run on
	// goroutines of the Watch that the gRPC recovery interceptors cannot reach.
	Logger *slog.Logger
	// Reauthenticate verifies the caller afresh and returns its account ID.
	Reauthenticate func(context.Context) (string, error)
}

// Resource serves one resource as its snapshot followed by progress.
type Resource[S proto.Message, T any] struct {
	Connection
	// Authorize checks the account's access to the resource, without reading
	// it. The Watch runs it when it starts and every reconcileInterval after,
	// on its own ticker, and ends when it fails.
	Authorize func(ctx context.Context, accountID string) error
	// ReadSnapshot authorizes the account and returns its snapshot with the
	// sources derived from that same read, never from a cross-cycle cache.
	ReadSnapshot func(ctx context.Context, accountID string) (S, []Source[T], error)
	// ResolveSources, if set, returns the account's current sources more
	// cheaply than ReadSnapshot. It need not authorize: Authorize does.
	ResolveSources func(ctx context.Context, accountID string) ([]Source[T], error)
	// Terminal, if set, ends the Watch after it sent a terminal snapshot.
	Terminal func(S) bool
	// SendSnapshot and SendProgress are gRPC server stream sends. Run may
	// return before a blocked send exits; the RPC handler must then return at
	// once, so that gRPC cancels its transport and releases that send.
	SendSnapshot func(snapshot S, cursor string) error
	SendProgress func(progress T, cursor string) error
}

// Run serves the Watch until the client leaves, the resource reaches a
// terminal snapshot or an error ends it. The caller's authentication and
// access are checked when the Watch starts and every reconcileInterval after,
// independently of its reads and sends, so a caller whose login ended or whose
// access was withdrawn stops receiving frames within reconcileInterval plus
// one authorization. Every snapshot read also reauthenticates; each read is
// bounded by readTimeout.
func (w Resource[S, T]) Run(ctx context.Context) error {
	resume, err := w.Cursors.decode(w.Scope, w.ResumeCursor)
	if err != nil {
		return err
	}
	accountID, err := w.authorize(ctx)
	if err != nil {
		return err
	}
	// This context owns only the Watch's goroutines. Cancelling it cannot
	// interrupt a blocked gRPC send; returning the RPC handler cancels the
	// transport. Never join the sender before that return: the join would
	// deadlock on the transport this boundary is meant to release.
	watchCtx, cancel := context.WithCancelCause(ctx)
	run := &watchRun[S, T]{Resource: w, accountID: accountID, sends: make(chan func() error), sent: make(chan error, 1)}
	go run.sendLoop(watchCtx)
	// The authorizer ends with the Watch's context, which the deferred cancel
	// ends before the join.
	var authorizer sync.WaitGroup
	defer authorizer.Wait()
	defer cancel(nil)
	authorizer.Go(func() { run.authorizeEvery(watchCtx, cancel) })
	for {
		realign, err := run.cycle(watchCtx, resume)
		if ctx.Err() == nil && watchCtx.Err() != nil {
			// A failed authorization ended the Watch.
			return context.Cause(watchCtx)
		}
		if !realign {
			return err
		}
		// Realigning discards the old cursors: fresh tails follow a fresh snapshot.
		resume = nil
	}
}

// authorize authenticates the caller afresh and checks its access to the
// resource, and returns its account ID.
func (w Resource[S, T]) authorize(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	accountID, err := w.Reauthenticate(ctx)
	if err != nil {
		return "", err
	}
	return accountID, w.Authorize(ctx, accountID)
}

// authorizeEvery authorizes the caller every reconcileInterval until the Watch
// ends, and ends it with the first failure.
func (r *watchRun[S, T]) authorizeEvery(ctx context.Context, fail context.CancelCauseFunc) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.authorize(ctx); err != nil {
				if ctx.Err() == nil {
					fail(err)
				}
				return
			}
		}
	}
}

// watchRun is the state of one running Watch.
type watchRun[S proto.Message, T any] struct {
	Resource[S, T]
	// accountID is the caller's account, which every authentication confirms.
	accountID string
	// sends hands one frame to the sender goroutine and sent returns its result.
	sends chan func() error
	sent  chan error
	// last is the snapshot most recently delivered to the client.
	last S
}

// cycle serves one source set. It attaches a reader to every source, sends a
// snapshot and then forwards progress until the set changes, which it reports
// as realign, or until the Watch ends.
func (r *watchRun[S, T]) cycle(ctx context.Context, resume map[string]string) (realign bool, err error) {
	sources, err := r.resolveSources(ctx)
	if err != nil {
		return false, err
	}
	key := sourceKey(sources)
	readerCtx, stopReaders := context.WithCancel(ctx)
	var readers sync.WaitGroup
	defer readers.Wait()
	defer stopReaders()
	messages := make(chan sourceMessage[T], bufferSize)
	overflow := make(chan struct{}, 1)
	// Attach every reader at its tail before the snapshot is read, so progress
	// committed after that read cannot be missed.
	cursors := map[string]string{}
	for _, source := range sources {
		cursor, resumed := resume[source.Identity]
		if !resumed {
			if cursor, err = tail(readerCtx, source); err != nil {
				return false, err
			}
		}
		cursors[source.Identity] = cursor
		readers.Go(func() { r.read(readerCtx, source, cursor, messages, overflow) })
	}
	if changed, terminal, err := r.sendSnapshot(ctx, key, cursors, true); changed || terminal || err != nil {
		return changed, err
	}
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-overflow:
			// Drop the buffered progress and reread the state on fresh tails.
			return true, nil
		case message := <-messages:
			// A reader may have queued this message before a source or
			// generation change. Check its source before signing or
			// publishing it; this is not an atomic transaction of snapshot and
			// Stream.
			current, err := r.resolveSources(ctx)
			if err != nil {
				return false, err
			}
			if sourceKey(current) != key {
				return true, nil
			}
			if message.err != nil {
				return false, message.err
			}
			cursors[message.source] = message.cursor
			cursor := r.Cursors.encode(r.Scope, cursors)
			if err := r.deliver(ctx, key, func() error { return r.SendProgress(message.value, cursor) }); err != nil {
				return false, err
			}
		case <-ticker.C:
			// Compare the fresh read's sources with the attached readers before
			// publishing, also when the snapshot itself is unchanged.
			if changed, terminal, err := r.sendSnapshot(ctx, key, cursors, false); changed || terminal || err != nil {
				return changed, err
			}
		}
	}
}

// sendSnapshot reads the snapshot and delivers it when forced or changed. A
// changed source set is reported instead of sent.
func (r *watchRun[S, T]) sendSnapshot(ctx context.Context, key string, cursors map[string]string, force bool) (changed, terminal bool, err error) {
	snapshot, sources, err := r.readSnapshot(ctx)
	if err != nil {
		return false, false, err
	}
	if sourceKey(sources) != key {
		return true, false, nil
	}
	if force || !proto.Equal(r.last, snapshot) {
		cursor := r.Cursors.encode(r.Scope, cursors)
		if err := r.deliver(ctx, key, func() error { return r.SendSnapshot(snapshot, cursor) }); err != nil {
			return false, false, err
		}
		r.last = snapshot
	}
	return false, r.Terminal != nil && r.Terminal(snapshot), nil
}

// readSnapshot authenticates the caller afresh and reads its snapshot.
func (r *watchRun[S, T]) readSnapshot(ctx context.Context) (S, []Source[T], error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	accountID, err := r.Reauthenticate(ctx)
	if err != nil {
		var none S
		return none, nil, err
	}
	return r.ReadSnapshot(ctx, accountID)
}

// resolveSources returns the caller's current sources: through
// ResolveSources, which the authorization ticker covers, or else through
// ReadSnapshot.
func (r *watchRun[S, T]) resolveSources(ctx context.Context) ([]Source[T], error) {
	if r.ResolveSources == nil {
		_, sources, err := r.readSnapshot(ctx)
		return sources, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return r.ResolveSources(ctx, r.accountID)
}

// tail returns the cursor of a source's newest retained message.
func tail[T any](ctx context.Context, source Source[T]) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return source.Tail(ctx)
}

// sourceMessage is a message or the terminal error of one source's reader.
type sourceMessage[T any] struct {
	source string
	value  T
	cursor string
	err    error
}

// read forwards one source's messages until it fails or its cycle ends. A
// full buffer signals overflow instead of blocking the source.
func (r *watchRun[S, T]) read(ctx context.Context, source Source[T], cursor string, messages chan<- sourceMessage[T], overflow chan<- struct{}) {
	for {
		value, next, err := r.readSource(ctx, source, cursor)
		if err != nil {
			select {
			case messages <- sourceMessage[T]{source: source.Identity, err: err}:
			case <-ctx.Done():
			}
			return
		}
		cursor = next
		select {
		case messages <- sourceMessage[T]{source: source.Identity, value: value, cursor: next}:
		case <-ctx.Done():
			return
		default:
			select {
			case overflow <- struct{}{}:
			default:
			}
		}
	}
}

// deliver hands one frame to the sender and waits for the send. While it is
// blocked, the sources are rechecked every reconcile interval, and a change
// ends the connection with ErrSourcesChanged.
func (r *watchRun[S, T]) deliver(ctx context.Context, key string, send func() error) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	select {
	case r.sends <- send:
	case <-ctx.Done():
		return ctx.Err()
	}
	recheck := time.NewTicker(reconcileInterval)
	defer recheck.Stop()
	for {
		select {
		case err := <-r.sent:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-recheck.C:
			sources, err := r.resolveSources(ctx)
			if err != nil {
				return err
			}
			if sourceKey(sources) != key {
				return ErrSourcesChanged
			}
		}
	}
}

// sendLoop owns the gRPC stream writes. At most one send is outstanding, and
// every send error ends the Watch, so no send overlaps an abandoned one.
func (r *watchRun[S, T]) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case send := <-r.sends:
			if ctx.Err() != nil {
				return
			}
			err := r.send(ctx, send)
			select {
			case r.sent <- err:
			case <-ctx.Done():
				return
			}
		}
	}
}

// send and readSource turn a panic on a Watch goroutine into the error that
// ends the Watch.
func (r *watchRun[S, T]) send(ctx context.Context, send func() error) (err error) {
	defer RecoverAsError(ctx, r.Logger, "Watch send", &err)
	return send()
}

func (r *watchRun[S, T]) readSource(ctx context.Context, source Source[T], cursor string) (_ T, _ string, err error) {
	defer RecoverAsError(ctx, r.Logger, "Watch read", &err)
	return source.Read(ctx, cursor)
}

// sourceKey identifies a source set independently of its order.
func sourceKey[T any](sources []Source[T]) string {
	identities := make([]string, len(sources))
	for i, source := range sources {
		identities[i] = source.Identity
	}
	sort.Strings(identities)
	return strings.Join(identities, "\x00")
}

// ErrRecoveredPanic is the status of a recovered panic: a panic is a
// programming error, so the caller receives a fixed Internal status and the
// log keeps the value and stack. Error mappers return it unchanged, so a panic
// on a Watch goroutine also reaches the caller as Internal.
var ErrRecoveredPanic = status.Error(codes.Internal, "internal server error")

// RecoverAsError must be deferred directly. It also covers goroutines that a
// handler starts, which the gRPC recovery interceptors cannot reach.
func RecoverAsError(ctx context.Context, logger *slog.Logger, operation string, err *error) {
	value := recover()
	if value == nil {
		return
	}
	logger.ErrorContext(ctx, "Business gRPC panic recovered", "operation", operation, "panic", value, "stack", string(debug.Stack()))
	*err = ErrRecoveredPanic
}
