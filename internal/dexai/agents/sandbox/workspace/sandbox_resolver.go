// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	maximumConnections  = 256
	idleConnectionTTL   = 10 * time.Minute
	idleSweepInterval   = time.Minute
	connectProbeTimeout = 5 * time.Second
	establishTimeout    = time.Minute
)

var (
	errClosed   = errors.New("sandbox binding resolver is closed")
	errCapacity = errors.New("sandbox connection capacity is exhausted")
	// ErrUnreachable means no request was sent: the admitted Sandbox transport could
	// not be established. The caller may re-initialize the workspace without effect risk.
	ErrUnreachable = errors.New("sandbox is unreachable")
)

// Resolution is one borrow of a pooled transport. Release returns it.
type Resolution struct {
	Client *sandboxd.SandboxClient
	borrow *borrow
}

type borrow struct {
	once    sync.Once
	release func()
}

// Release returns this borrow to the pool. It never closes the transport: the
// resolver closes idle and superseded entries itself.
func (resolution Resolution) Release() {
	resolution.borrow.once.Do(resolution.borrow.release)
}

// workspaceKey names the pooled entry of one Project's Session, whose Sandbox
// instances replace each other.
type workspaceKey struct {
	projectID string
}

// poolEntry is the pooled transport of one Session: the process connection and
// the client built on it.
type poolEntry struct {
	binding Binding

	// established is closed when the shared dial finished. The fields below are
	// written once before that and are immutable afterwards.
	established chan struct{}
	err         error
	client      *sandboxd.SandboxClient
	connection  *grpc.ClientConn

	// Guarded by the resolver lock.
	borrowers int
	lastUsed  time.Time
	retired   bool
}

func (entry *poolEntry) isEstablished() bool {
	select {
	case <-entry.established:
		return true
	default:
		return false
	}
}

// close releases the connection.
func (entry *poolEntry) close() error {
	if entry.connection == nil {
		return nil
	}
	return entry.connection.Close()
}

// SandboxResolver pools one transport per Session. Borrowers share it and return
// it without closing; the resolver alone dials, evicts and closes.
type SandboxResolver struct {
	addresses     SandboxAddresses
	httpTransport *http.Transport
	// redirects are the lane's module redirects of every client's tool environment.
	redirects []sandboxd.ModuleRedirect

	// lifetime bounds the janitor and every dial; Close cancels it and awaits both.
	//nolint:containedctx // the pool outlives every caller
	lifetime   context.Context
	cancel     context.CancelFunc
	background sync.WaitGroup
	wake       chan struct{}

	mu      sync.Mutex
	closed  bool
	entries map[workspaceKey]*poolEntry // the current entry of each Session
	pool    map[*poolEntry]struct{}     // every entry that owns or is dialing a transport
	doomed  []*poolEntry                // unborrowed retired entries awaiting the janitor
}

// newSandboxResolver starts the pool and its janitor. addresses resolves the
// sandboxd addresses of each admitted Sandbox when its transport is dialed.
func newSandboxResolver(addresses SandboxAddresses, redirects []sandboxd.ModuleRedirect) *SandboxResolver {
	transport := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // net/http always installs a *Transport
	transport.Proxy = nil
	lifetime, cancel := context.WithCancel(context.Background())
	resolver := &SandboxResolver{
		addresses: addresses, httpTransport: transport, redirects: redirects,
		lifetime: lifetime, cancel: cancel, wake: make(chan struct{}, 1),
		entries: make(map[workspaceKey]*poolEntry), pool: make(map[*poolEntry]struct{}),
	}
	resolver.background.Add(1)
	go resolver.janitor()
	return resolver
}

// Bind borrows the pooled transport of the binding's Session, dialing it once for
// all concurrent callers. It returns only after the process service is proven
// reachable, so an ErrUnreachable result guarantees that no request was sent.
// Callers release each resolution when their workspace operation finishes.
func (resolver *SandboxResolver) Bind(ctx context.Context, binding Binding) (Resolution, error) {
	if err := binding.validate(); err != nil {
		return Resolution{}, err
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	//nolint:contextcheck // the shared dial runs under the pool lifetime, not one caller's
	entry, err := resolver.acquire(binding)
	if err != nil {
		return Resolution{}, err
	}
	select {
	case <-entry.established:
	case <-ctx.Done():
		resolver.release(entry)
		return Resolution{}, ctx.Err()
	}
	if entry.err != nil {
		resolver.release(entry)
		return Resolution{}, entry.err
	}
	if err := probe(ctx, entry); err != nil {
		if errors.Is(err, ErrUnreachable) {
			// The next dial resolves the addresses again, so a restarted
			// Sandbox that publishes new ports is found.
			resolver.retire(entry)
		}
		resolver.release(entry)
		if resolver.lifetime.Err() != nil {
			return Resolution{}, errClosed
		}
		return Resolution{}, err
	}
	return Resolution{Client: entry.client, borrow: &borrow{release: func() { resolver.release(entry) }}}, nil
}

// acquire borrows the Session's entry, starting its one shared dial when the pool
// holds no entry for this exact binding.
func (resolver *SandboxResolver) acquire(binding Binding) (*poolEntry, error) {
	key := workspaceKey{projectID: binding.ProjectID}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.closed {
		return nil, errClosed
	}
	entry := resolver.entries[key]
	if entry != nil && entry.binding != binding {
		// A new Sandbox instance of the Session supersedes the pooled one.
		resolver.retireLocked(entry)
		entry = nil
	}
	if entry == nil {
		if len(resolver.pool) >= maximumConnections && !resolver.evictIdleLocked() {
			return nil, errCapacity
		}
		entry = &poolEntry{binding: binding, established: make(chan struct{})}
		resolver.entries[key] = entry
		resolver.pool[entry] = struct{}{}
		resolver.background.Add(1)
		go resolver.establish(entry)
	}
	entry.borrowers++
	return entry, nil
}

func (resolver *SandboxResolver) release(entry *poolEntry) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	entry.borrowers--
	entry.lastUsed = time.Now()
	resolver.reapLocked(entry)
}

func (resolver *SandboxResolver) retire(entry *poolEntry) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	resolver.retireLocked(entry)
}

// retireLocked removes the entry from the index so that the next Bind dials
// again. The entry is closed once it has no borrower.
func (resolver *SandboxResolver) retireLocked(entry *poolEntry) {
	key := workspaceKey{projectID: entry.binding.ProjectID}
	if resolver.entries[key] == entry {
		delete(resolver.entries, key)
	}
	entry.retired = true
	resolver.reapLocked(entry)
}

// reapLocked hands a retired, unborrowed, established entry to the janitor
// exactly once.
func (resolver *SandboxResolver) reapLocked(entry *poolEntry) {
	if !entry.retired || entry.borrowers != 0 || !entry.isEstablished() {
		return
	}
	if _, pooled := resolver.pool[entry]; !pooled {
		return
	}
	delete(resolver.pool, entry)
	resolver.doomed = append(resolver.doomed, entry)
	select {
	case resolver.wake <- struct{}{}:
	default:
	}
}

// evictIdleLocked retires the least recently used entry that has no borrower.
func (resolver *SandboxResolver) evictIdleLocked() bool {
	var oldest *poolEntry
	for _, entry := range resolver.entries {
		if entry.borrowers == 0 && entry.isEstablished() &&
			(oldest == nil || entry.lastUsed.Before(oldest.lastUsed)) {
			oldest = entry
		}
	}
	if oldest == nil {
		return false
	}
	resolver.retireLocked(oldest)
	return true
}

// establish performs the one dial shared by every concurrent Bind of the entry.
// It runs under the resolver lifetime instead of a caller's context, so a
// cancelled caller neither fails the others nor leaves a half-open transport.
func (resolver *SandboxResolver) establish(entry *poolEntry) {
	defer resolver.background.Done()
	ctx, cancel := context.WithTimeout(resolver.lifetime, establishTimeout)
	err := resolver.dial(ctx, entry)
	cancel()

	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if err != nil {
		if resolver.lifetime.Err() != nil {
			err = errClosed
		}
		// A failed dial owns nothing: forget the entry so that the next Bind dials again.
		entry.err = err
		key := workspaceKey{projectID: entry.binding.ProjectID}
		if resolver.entries[key] == entry {
			delete(resolver.entries, key)
		}
		delete(resolver.pool, entry)
	}
	entry.lastUsed = time.Now()
	close(entry.established)
	resolver.reapLocked(entry)
}

// dial resolves the Sandbox's addresses and connects to them. Addresses the
// runtime cannot resolve mean that the Sandbox is unreachable now.
func (resolver *SandboxResolver) dial(ctx context.Context, entry *poolEntry) error {
	process, filesystem, err := resolver.addresses(ctx, entry.binding)
	if err != nil {
		return fmt.Errorf("%w: resolve the sandboxd addresses: %w", ErrUnreachable, err)
	}
	return resolver.connect(entry, "dns:///"+process, "http://"+filesystem)
}

// connect builds the process connection and the client of the entry. The
// connection dials lazily; probe proves it reachable before first use.
func (resolver *SandboxResolver) connect(entry *poolEntry, processTarget, filesystemBaseURL string) error {
	connection, err := grpc.NewClient(processTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		return fmt.Errorf("%w: dial sandbox process service: %w", ErrUnreachable, err)
	}
	entry.connection = connection
	entry.client = sandboxd.NewSandboxClient(sandboxd.SandboxClientConfig{
		FilesystemBaseURL: filesystemBaseURL,
		HTTPClient:        &http.Client{Transport: resolver.httpTransport},
		ProcessConnection: connection,
		ModuleRedirects:   resolver.redirects,
	})
	return nil
}

// probe proves the entry reachable before a request is dispatched on it. Only a
// transport that cannot become ready is unreachable; a caller whose own context
// ended learns nothing about the Sandbox and gets that context's error.
func probe(ctx context.Context, entry *poolEntry) error {
	probing, cancel := context.WithTimeout(ctx, connectProbeTimeout)
	defer cancel()
	for {
		state := entry.connection.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("%w: process connection is shut down", ErrUnreachable)
		}
		if state == connectivity.Idle {
			entry.connection.Connect()
		}
		if !entry.connection.WaitForStateChange(probing, state) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("%w: process service was not ready within %s", ErrUnreachable, connectProbeTimeout)
		}
	}
}

// janitor owns every close outside Close: idle entries and retired entries
// whose last borrower left.
func (resolver *SandboxResolver) janitor() {
	defer resolver.background.Done()
	ticker := time.NewTicker(idleSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-resolver.lifetime.Done():
			return
		case now := <-ticker.C:
			resolver.retireIdle(now)
		case <-resolver.wake:
		}
		resolver.mu.Lock()
		doomed := resolver.doomed
		resolver.doomed = nil
		resolver.mu.Unlock()
		for _, entry := range doomed {
			// Nothing is left to retry here: gRPC fails only a repeated close.
			_ = entry.close()
		}
	}
}

func (resolver *SandboxResolver) retireIdle(now time.Time) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	for _, entry := range resolver.entries {
		if entry.borrowers != 0 || !entry.isEstablished() {
			continue
		}
		if now.Sub(entry.lastUsed) >= idleConnectionTTL {
			resolver.retireLocked(entry)
		}
	}
}

// Close stops the janitor and every pending dial, then closes every connection,
// including borrowed ones. Later Bind calls return errClosed.
func (resolver *SandboxResolver) Close() error {
	resolver.mu.Lock()
	if resolver.closed {
		resolver.mu.Unlock()
		return nil
	}
	resolver.closed = true
	resolver.mu.Unlock()
	resolver.cancel()
	resolver.background.Wait()

	resolver.mu.Lock()
	entries := resolver.doomed
	for entry := range resolver.pool {
		entries = append(entries, entry)
	}
	resolver.doomed = nil
	resolver.entries = make(map[workspaceKey]*poolEntry)
	resolver.pool = make(map[*poolEntry]struct{})
	resolver.mu.Unlock()
	var result error
	for _, entry := range entries {
		result = errors.Join(result, entry.close())
	}
	resolver.httpTransport.CloseIdleConnections()
	return result
}
