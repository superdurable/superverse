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
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// readGoalsFile reads the goals file at commit from the Project's bound
// repository; found is false when the commit has none.
func (f *EvolveFlow) readGoalsFile(ctx context.Context, projectID, commit string) ([]byte, bool, error) {
	if !flowid.IsCommitSHA(commit) {
		return nil, false, fmt.Errorf("the Live build commit %q is not a commit SHA", commit)
	}
	project, err := f.projects.Read(ctx, projectID)
	if err != nil {
		return nil, false, fmt.Errorf("read the Project: %w", err)
	}
	binding := project.RepositoryBinding
	if binding.RepositoryID == 0 {
		return nil, false, errors.New("the Project has no bound repository")
	}
	if err = repohost.CheckInstallation(f.host, binding.InstallationID); err != nil {
		return nil, false, err
	}
	file, err := f.host.ReadFile(ctx, binding.FullName, commit, goals.FilePath)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil, false, nil
	}
	return file, err == nil, err
}

// goalsSkip is why goals cannot be measured, empty when they are valid.
func goalsSkip(current Goals) ReviewReason {
	switch current.Status {
	case GoalsNone:
		return ReasonNoGoals
	case GoalsInvalid:
		return ReasonGoalsInvalid
	case GoalsValid:
	}
	return ""
}

// currentLive reads the Project's current Live deployment for a review, or
// ReasonNoLive without a ready one. A review measures the current Live's runs
// against the goals of its build, even when its announcement failed.
func (f *EvolveFlow) currentLive(ctx context.Context, projectID string) (*LiveRelease, ReviewReason, error) {
	live, err := f.dependency.CurrentLive(ctx, projectID)
	if errors.Is(err, ErrLiveNotReady) {
		return nil, ReasonNoLive, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("check the Live deployment: %w", err)
	}
	return &live, "", nil
}

// metricsError is a goals block the evaluator refuses, which a retry cannot change.
type metricsError struct{ err error }

func (e *metricsError) Error() string { return "evaluate the goals: " + e.err.Error() }
func (e *metricsError) Unwrap() error { return e.err }

// metricsFailure is the review failure of a computeMetrics error that a retry
// cannot change, nil for any other: goals the evaluator refuses, or a manifest
// that is not this review's export of the Live runs (runexport.ErrNotLive).
func metricsFailure(err error) *reviewFailure {
	var invalid *metricsError
	if errors.As(err, &invalid) || errors.Is(err, runexport.ErrNotLive) {
		return &reviewFailure{Code: ReasonMetricsFailed, Detail: err.Error()}
	}
	return nil
}

// computeMetrics evaluates the review's goals over its export, as of the
// review's start, and stores metrics.json once beside the parts. A FlowID
// found in several parts counts once, with its newest run. It evaluates only
// cursor.Parts, which this review exported from the Live engine; the check of
// the manifest's source label is a consistency check, not that guarantee.
func (f *EvolveFlow) computeMetrics(ctx context.Context, projectID string, record ReviewRecord, cursor exportCursor, manifestRef objectstore.ObjectRef) (MetricsSummary, error) {
	var manifest runexport.Manifest
	if err := f.objects.GetJSON(ctx, manifestRef, &manifest); err != nil {
		return MetricsSummary{}, fmt.Errorf("read the manifest: %w", err)
	}
	if err := manifest.CheckLive(projectID, record.ID); err != nil {
		return MetricsSummary{}, err
	}
	newest := map[string]runexport.Record{}
	for _, part := range cursor.Parts {
		records, err := f.readPart(ctx, part)
		if err != nil {
			return MetricsSummary{}, err
		}
		for _, run := range records {
			if known, seen := newest[run.FlowID]; !seen || run.StartedAt.After(known.StartedAt) {
				newest[run.FlowID] = run
			}
		}
	}
	records := slices.SortedFunc(maps.Values(newest), func(a, b runexport.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.FlowID, b.FlowID))
	})
	metrics, err := goals.Evaluate(Goals{Spec: record.GoalsSpec}.spec(), records, record.StartedAt)
	if err != nil {
		return MetricsSummary{}, &metricsError{err: err}
	}
	key := exportPrefix(projectID, record.ID) + "metrics.json"
	var existing goals.Metrics
	if _, err = f.objects.GetCurrentJSON(ctx, key, &existing); errors.Is(err, objectstore.ErrNotFound) {
		_, err = f.objects.PutJSON(ctx, key, metrics)
	}
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("store the metrics: %w", err)
	}
	return summarize(metrics), nil
}

// readPart reads the exact object of a part and decodes its records.
func (f *EvolveFlow) readPart(ctx context.Context, part exportPart) ([]runexport.Record, error) {
	data, err := f.objects.Get(ctx, part.Ref)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", part.Name, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", part.Name, err)
	}
	defer func() { _ = reader.Close() }()
	records, err := runexport.Read(reader)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", part.Name, err)
	}
	return records, nil
}
