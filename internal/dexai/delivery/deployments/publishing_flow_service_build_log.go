// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"golang.org/x/sync/singleflight"
)

// BuildLogReads is how a server replica reads build logs: the redactor of the
// Projects' internal resource names and the most provider reads it starts per
// second, which every viewer of the replica shares.
type BuildLogReads struct {
	Redactor  infra.Redactor
	PerSecond float64
}

const (
	// MaximumBuildLogPageBytes bounds the redacted text of one log page.
	MaximumBuildLogPageBytes = 64 << 10
	// buildLogReadBytes is the build output one page reads: redaction may
	// lengthen it, and the page stays within MaximumBuildLogPageBytes.
	buildLogReadBytes = 48 << 10
	// buildLogInterval is how long viewers share a page, and the soonest a
	// viewer reads again.
	buildLogInterval = 2 * time.Second
	// maximumBuildLogPages bounds the shared pages a replica keeps, at most
	// 64 KiB each.
	maximumBuildLogPages = 128
	// buildLogReadTimeout bounds one shared read: two Flow reads, the provider's
	// page and the configured values.
	buildLogReadTimeout = 45 * time.Second
	// The backoff of a build whose provider throttled its log read.
	buildLogBackoffFirst, buildLogBackoffMaximum = 4 * time.Second, time.Minute
)

// BuildLogPage is one redacted page of a build's log: its text, the cursor
// after it, Complete once the build ended and its log holds nothing after
// Next, and RetryAfter, the soonest to read again. A page the replica's read
// budget or the provider's rate limit deferred is empty, with Next the cursor
// read and a longer RetryAfter.
type BuildLogPage struct {
	Text       string
	Next       string
	Complete   bool
	RetryAfter time.Duration
}

// buildLogKey names one page read: the build and the read.
type buildLogKey struct {
	project, release, build string
	read                    build.LogRead
}

// buildLogs is the replica's sharing of log reads: one read per key at a time
// (reads), each page kept for buildLogInterval (pages, oldest first in order),
// the read budget (bucket) and the throttled builds (backoff, by build ID,
// which expires).
type buildLogs struct {
	reads singleflight.Group
	mu    sync.Mutex
	pages map[buildLogKey]sharedBuildLogPage
	order []buildLogKey
	// bucket holds up to burst read tokens, refilled at perSecond.
	tokens, perSecond, burst float64
	filled                   time.Time
	backoff                  map[string]buildLogBackoff
}

type sharedBuildLogPage struct {
	page BuildLogPage
	read time.Time
}

type buildLogBackoff struct {
	until time.Time
	delay time.Duration
}

func newBuildLogs(perSecond float64) *buildLogs {
	burst := max(1, 2*perSecond)
	return &buildLogs{pages: map[buildLogKey]sharedBuildLogPage{}, perSecond: perSecond, burst: burst, tokens: burst, filled: time.Now(), backoff: map[string]buildLogBackoff{}}
}

// ReadBuildLog reads one page of a build revision's provider log, redacted
// with the build's configured values and the Project's internal resource
// names, for a caller that authorized project. Viewers of the same page on
// this replica share one read; under the replica's read budget or a provider
// rate limit the page is deferred instead of failing, because the build
// snapshot, not its log, is the durable record. Nothing is stored beyond the
// shared redacted page.
func (s *PublishingFlowService) ReadBuildLog(ctx context.Context, project projects.Project, release, buildRevisionID string, read build.LogRead) (BuildLogPage, error) {
	key := buildLogKey{project: project.ProjectID, release: release, build: buildRevisionID, read: read}
	if page, shared := s.logs.shared(key); shared {
		return page, nil
	}
	result, err, _ := s.logs.reads.Do(fmt.Sprint(key), func() (any, error) {
		if wait := s.logs.deferral(buildRevisionID); wait > 0 {
			return BuildLogPage{Next: read.Cursor, RetryAfter: wait}, nil
		}
		// The read serves every viewer that waits for it, beyond the first one's cancellation.
		shared, cancel := context.WithTimeout(context.WithoutCancel(ctx), buildLogReadTimeout)
		defer cancel()
		page, err := s.readBuildLog(shared, project, release, buildRevisionID, read)
		if errors.Is(err, build.ErrLogThrottled) {
			return BuildLogPage{Next: read.Cursor, RetryAfter: s.logs.throttled(buildRevisionID)}, nil
		}
		if err == nil {
			s.logs.keep(key, page)
		}
		return page, err
	})
	if err != nil {
		return BuildLogPage{}, err
	}
	return result.(BuildLogPage), nil
}

// readBuildLog makes the read that ReadBuildLog shares: the build's provider
// identity from its Release, the provider's page and, for a page with text,
// the build's configured values, held for this one redaction.
func (s *PublishingFlowService) readBuildLog(ctx context.Context, project projects.Project, release, buildRevisionID string, read build.LogRead) (BuildLogPage, error) {
	snapshot, err := s.deployments.Releases.ReadBuildSnapshot(ctx, project.ProjectID, release, buildRevisionID)
	if err != nil {
		return BuildLogPage{}, err
	}
	read.MaximumBytes = min(read.MaximumBytes, buildLogReadBytes)
	page, err := s.deployments.Releases.ReadBuildLog(ctx, snapshot, read)
	if err != nil {
		return BuildLogPage{}, err
	}
	out := BuildLogPage{Next: page.Next, Complete: page.Complete && buildEnded(snapshot.Build), RetryAfter: buildLogInterval}
	if page.Text == "" {
		return out, nil
	}
	scope := infra.ScopeRef{ProjectID: project.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: project.Storage}
	values, err := s.configuration.ConfiguredValues(ctx, scope, snapshot.Build.Configuration.Object)
	if err != nil {
		return BuildLogPage{}, fmt.Errorf("read the configured values of build revision %s: %w", buildRevisionID, err)
	}
	redaction := projectconfig.OutputRedaction{Project: project.ProjectID, Infrastructure: s.logReads.Redactor, Values: values}
	out.Text = comm.TruncateUTF8(redaction.Text(page.Text), MaximumBuildLogPageBytes)
	return out, nil
}

// buildEnded reports a build revision whose provider build can no longer
// start or run: READY, FAILED, or CANCELLED.
func buildEnded(b releases.ReleaseBuildRevision) bool {
	return b.Status == releases.Ready || b.Status == releases.Failed || b.Status == releases.Cancelled
}

// shared returns the page a viewer read less than buildLogInterval ago, with
// the rest of that interval as its RetryAfter.
func (l *buildLogs) shared(key buildLogKey) (BuildLogPage, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept, found := l.pages[key]
	if age := time.Since(kept.read); !found || age >= buildLogInterval {
		return BuildLogPage{}, false
	}
	page := kept.page
	page.RetryAfter = buildLogInterval - time.Since(kept.read)
	return page, true
}

// keep shares page for buildLogInterval, dropping the oldest page beyond
// maximumBuildLogPages.
func (l *buildLogs) keep(key buildLogKey, page BuildLogPage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, found := l.pages[key]; !found {
		l.order = append(l.order, key)
	}
	l.pages[key] = sharedBuildLogPage{page: page, read: time.Now()}
	for len(l.order) > maximumBuildLogPages {
		delete(l.pages, l.order[0])
		l.order = l.order[1:]
	}
}

// deferral takes one read token, or returns how long the read waits: for
// the build's backoff after a throttled read, or for the next token.
func (l *buildLogs) deferral(buildRevisionID string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if backoff, found := l.backoff[buildRevisionID]; found && now.Before(backoff.until) {
		return backoff.until.Sub(now)
	}
	l.tokens = min(l.burst, l.tokens+now.Sub(l.filled).Seconds()*l.perSecond)
	l.filled = now
	if l.tokens < 1 {
		return max(buildLogInterval, time.Duration((1-l.tokens)/l.perSecond*float64(time.Second)))
	}
	l.tokens--
	return 0
}

// throttled backs the build's log reads off, doubling up to
// buildLogBackoffMaximum, and returns the wait. Expired backoffs of other
// builds are dropped, so the map holds only builds throttled within the last
// minute.
func (l *buildLogs) throttled(buildRevisionID string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for id, backoff := range l.backoff {
		if now.After(backoff.until.Add(buildLogBackoffMaximum)) {
			delete(l.backoff, id)
		}
	}
	delay := buildLogBackoffFirst
	if previous, found := l.backoff[buildRevisionID]; found {
		delay = min(2*previous.delay, buildLogBackoffMaximum)
	}
	l.backoff[buildRevisionID] = buildLogBackoff{until: now.Add(delay), delay: delay}
	return delay
}

// Lines splits page text into its lines without their line ends.
func (p BuildLogPage) Lines() []string {
	if p.Text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(p.Text, "\n"), "\n")
}
