// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport/history"
)

// Bounds of one export.
const (
	// partRuns is the most runs a part holds: pagesPerPart search pages of the hosted store's largest size.
	partRuns     = 500
	pagesPerPart = partRuns / history.SearchPageSize
	// maximumExportBytes ends an export, truncated, once its compressed parts reach it.
	maximumExportBytes = 64 << 20
	// The Live engine reads an export makes per second: every read, and searches among them.
	readsPerSecond    = 20
	searchesPerSecond = 2
	exporterVersion   = "EvolveFlow/1"
)

// partTooLargeError is a part whose compressed runs exceed one object.
type partTooLargeError struct{ part, bytes int }

func (e *partTooLargeError) Error() string {
	return fmt.Sprintf("part %d takes %d compressed bytes, more than the %d an object holds", e.part, e.bytes, objectstore.MaxObjectBytes)
}

// exportPart exports the next part of a review: it pages the search of the
// goals' Flow types from cursor, keeps each FlowID's newest run that is open
// or closed within the window, reads and compacts those runs, writes them as
// one part and returns the cursor after it and whether the export is done.
// Every search page and every run is one bounded Live call.
func (f *EvolveFlow) exportPart(ctx context.Context, projectID string, record ReviewRecord, cursor exportCursor, attempt int32) (exportCursor, bool, error) {
	spec := Goals{Spec: record.GoalsSpec}.spec()
	pace := &exportPace{reads: pacer{every: time.Second / readsPerSecond}, searches: pacer{every: time.Second / searchesPerSecond}, count: &cursor.Reads}
	runs, err := f.searchPart(ctx, projectID, spec.Flows, record.Window.From, &cursor, pace)
	if err != nil {
		return cursor, false, err
	}
	if len(runs) == 0 {
		// Pages of runs that all closed before the window, or the Flow types' last empty page, write no part.
		cursor.Part++
		return cursor, cursor.Truncated || cursor.Flow >= len(spec.Flows), nil
	}
	settings, err := f.recordSettings(ctx, projectID, spec, record.Window.To)
	if err != nil {
		return cursor, false, err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	count := 0
	for _, run := range runs {
		if cursor.Reads >= f.settings.MaxReadsPerExport {
			cursor.Truncated = true
			break
		}
		exported, found, err := f.recordRun(ctx, projectID, run, settings, pace)
		if err != nil {
			return cursor, false, err
		}
		if found {
			if err := runexport.Write(writer, exported); err != nil {
				return cursor, false, fmt.Errorf("compress the record of %s: %w", run.FlowID, err)
			}
			count++
		}
	}
	if err := writer.Close(); err != nil {
		return cursor, false, fmt.Errorf("compress part %d: %w", cursor.Part, err)
	}
	if count > 0 {
		if compressed.Len() > objectstore.MaxObjectBytes {
			return cursor, false, &partTooLargeError{part: cursor.Part, bytes: compressed.Len()}
		}
		key := partKey(projectID, record.ID, cursor.Part, attempt)
		ref, err := f.objects.Put(ctx, key, objectstore.MediaTypeGzip, compressed.Bytes())
		if err != nil {
			return cursor, false, fmt.Errorf("store part %d: %w", cursor.Part, err)
		}
		sum := sha256.Sum256(compressed.Bytes())
		cursor.Parts = append(cursor.Parts, exportPart{Part: runexport.Part{Name: path.Base(key), Runs: count, SHA256: hex.EncodeToString(sum[:])}, Ref: ref})
		cursor.Runs += count
		cursor.Bytes += int64(compressed.Len())
	}
	cursor.Part++
	if cursor.Bytes >= maximumExportBytes && cursor.Flow < len(spec.Flows) {
		cursor.Truncated = true
	}
	return cursor, cursor.Truncated || cursor.Flow >= len(spec.Flows), nil
}

// searchPart reads up to pagesPerPart search pages from cursor and returns the
// runs to export, newest start first, within the run limit.
func (f *EvolveFlow) searchPart(ctx context.Context, projectID string, flows []string, from time.Time, cursor *exportCursor, pace *exportPace) ([]history.Found, error) {
	newest := map[string]history.Found{}
	remaining := f.settings.MaxRunsPerExport - cursor.Runs
	for page := 0; page < pagesPerPart && cursor.Flow < len(flows) && len(newest) < remaining; page++ {
		if cursor.Reads >= f.settings.MaxReadsPerExport {
			cursor.Truncated = true
			break
		}
		var found []history.Found
		var next string
		err := f.dependency.InvokeEvolveLive(ctx, projectID, func(ctx context.Context, live dexpb.FlowServiceClient) error {
			var searchErr error
			found, next, searchErr = history.Engine{Flows: pacedFlows{FlowServiceClient: live, pace: pace}}.SearchPage(ctx, flows[cursor.Flow], cursor.PageToken)
			return searchErr
		})
		if err != nil {
			return nil, fmt.Errorf("search the Live runs: %w", err)
		}
		for _, run := range found {
			if known, seen := newest[run.FlowID]; (!seen || run.StartedAt.After(known.StartedAt)) && run.InWindow(from) {
				newest[run.FlowID] = run
			}
		}
		if cursor.PageToken = next; next == "" {
			cursor.Flow++
		}
	}
	runs := slices.SortedFunc(maps.Values(newest), func(a, b history.Found) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.FlowID, b.FlowID))
	})
	if len(runs) >= remaining {
		cursor.Truncated = cursor.Truncated || len(runs) > remaining || cursor.Flow < len(flows)
		runs = runs[:max(remaining, 0)]
	}
	return runs, nil
}

// recordSettings choose what the part's records keep: the goals' fields, the
// Live redaction, the price table and the release timeline up to the review.
func (f *EvolveFlow) recordSettings(ctx context.Context, projectID string, spec goals.Spec, at time.Time) (history.Settings, error) {
	redact, err := f.dependency.LiveRedaction(ctx, projectID)
	if err != nil {
		return history.Settings{}, fmt.Errorf("read the Live redaction: %w", err)
	}
	timeline, err := f.dependency.LiveTimeline(ctx, projectID, at)
	if err != nil {
		return history.Settings{}, fmt.Errorf("read the Live timeline: %w", err)
	}
	fields := map[string]bool{}
	for _, field := range spec.ExportFields() {
		fields[field] = true
	}
	return history.Settings{Fields: fields, Redact: redact, Prices: f.settings.ModelPrices, Timeline: timeline, At: at}, nil
}

// recordRun reads and compacts one run; a run the engine no longer holds is not found.
func (f *EvolveFlow) recordRun(ctx context.Context, projectID string, run history.Found, settings history.Settings, pace *exportPace) (runexport.Record, bool, error) {
	var record runexport.Record
	err := f.dependency.InvokeEvolveLive(ctx, projectID, func(ctx context.Context, live dexpb.FlowServiceClient) error {
		var readErr error
		engine := history.Engine{Flows: pacedFlows{FlowServiceClient: live, pace: pace}, MaxHistoryEvents: f.settings.MaxHistoryEventsPerRun}
		record, readErr = engine.Record(ctx, run, settings)
		return readErr
	})
	if status.Code(err) == codes.NotFound {
		return runexport.Record{}, false, nil
	}
	if err != nil {
		return runexport.Record{}, false, fmt.Errorf("read the Live run %s: %w", run.FlowID, err)
	}
	return record, true, nil
}

// writeManifest stores the export's manifest once; a retry finds the one an
// earlier attempt stored. Its content comes from Flow state and the timeline
// up to the review's start, so a retry writes the same bytes.
func (f *EvolveFlow) writeManifest(ctx context.Context, projectID string, record ReviewRecord, cursor exportCursor) (objectstore.ObjectRef, error) {
	key := exportPrefix(projectID, record.ID) + "manifest.json"
	var existing runexport.Manifest
	ref, err := f.objects.GetCurrentJSON(ctx, key, &existing)
	if !errors.Is(err, objectstore.ErrNotFound) {
		return ref, err
	}
	timeline, err := f.dependency.LiveTimeline(ctx, projectID, record.Window.To)
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("read the Live timeline: %w", err)
	}
	parts := make([]runexport.Part, 0, len(cursor.Parts))
	for _, part := range cursor.Parts {
		parts = append(parts, part.Part)
	}
	manifest := runexport.Manifest{
		Source: &runexport.Source{Kind: runexport.SourceLive}, ReviewID: record.ID, ProjectID: projectID, Window: record.Window, Flows: Goals{Spec: record.GoalsSpec}.spec().Flows, Parts: parts,
		Runs: cursor.Runs, Truncated: cursor.Truncated, Complete: true, GeneratedAt: record.Window.To, GoalsCommit: record.GoalsCommit,
		Timeline: windowTimeline(timeline, record.Window), PriceTableAsOf: f.settings.PriceTableAsOf, ExporterVersion: exporterVersion,
	}
	return f.objects.PutJSON(ctx, key, manifest)
}

// windowTimeline keeps the releases committed within the window and the one Live at its start.
func windowTimeline(timeline []runexport.Release, window runexport.Window) []runexport.Release {
	first := 0
	for i, release := range timeline {
		if !release.LiveAt.After(window.From) {
			first = i
		}
	}
	return timeline[first:]
}

// deleteOldExports deletes the exports of the reviews before current beyond
// the newest kept ones, which the review being begun joins, and returns those
// reviews. A skipped review stored nothing.
func (f *EvolveFlow) deleteOldExports(ctx context.Context, projectID string, records []ReviewRecord, current string) ([]string, error) {
	stored := slices.DeleteFunc(records, func(r ReviewRecord) bool { return r.ID == current || r.Status == ReviewSkipped || r.Export.Deleted })
	newestFirst(stored)
	var deleted []string
	for _, record := range stored[min(len(stored), max(f.settings.KeepExports-1, 0)):] {
		for done := false; !done; {
			var err error
			if done, err = f.objects.DeletePrefix(ctx, exportPrefix(projectID, record.ID)); err != nil {
				return nil, fmt.Errorf("delete the export of %s: %w", record.ID, err)
			}
		}
		deleted = append(deleted, record.ID)
	}
	return deleted, nil
}

// exportPace paces one export's Live reads and counts them in the cursor.
type exportPace struct {
	reads, searches pacer
	count           *int
}

func (p *exportPace) read(ctx context.Context, search bool) error {
	if search {
		if err := p.searches.wait(ctx); err != nil {
			return err
		}
	}
	if err := p.reads.wait(ctx); err != nil {
		return err
	}
	*p.count++
	return nil
}

// pacer spaces calls at least every apart.
type pacer struct {
	every time.Duration
	next  time.Time
}

func (p *pacer) wait(ctx context.Context) error {
	now := time.Now()
	if delay := p.next.Sub(now); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		now = p.next
	}
	p.next = now.Add(p.every)
	return nil
}

// pacedFlows is a Live engine connection whose reads keep the export's pace.
type pacedFlows struct {
	dexpb.FlowServiceClient
	pace *exportPace
}

func (p pacedFlows) SearchFlows(ctx context.Context, in *dexpb.SearchFlowsRequest, opts ...grpc.CallOption) (*dexpb.SearchFlowsResponse, error) {
	if err := p.pace.read(ctx, true); err != nil {
		return nil, err
	}
	return p.FlowServiceClient.SearchFlows(ctx, in, opts...)
}

func (p pacedFlows) GetHistoryEvents(ctx context.Context, in *dexpb.GetHistoryEventsRequest, opts ...grpc.CallOption) (*dexpb.GetHistoryEventsResponse, error) {
	if err := p.pace.read(ctx, false); err != nil {
		return nil, err
	}
	return p.FlowServiceClient.GetHistoryEvents(ctx, in, opts...)
}

func (p pacedFlows) LoadBlobs(ctx context.Context, in *dexpb.LoadBlobsRequest, opts ...grpc.CallOption) (*dexpb.LoadBlobsResponse, error) {
	if err := p.pace.read(ctx, false); err != nil {
		return nil, err
	}
	return p.FlowServiceClient.LoadBlobs(ctx, in, opts...)
}
