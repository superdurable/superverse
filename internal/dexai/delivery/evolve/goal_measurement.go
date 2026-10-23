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
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// boundRepository is the full name of the Project's bound repository, whose
// installation the host serves, for reads at the Live build commit.
func (f *EvolveFlow) boundRepository(ctx context.Context, projectID, commit string) (string, error) {
	if !flowid.IsCommitSHA(commit) {
		return "", fmt.Errorf("the Live build commit %q is not a commit SHA", commit)
	}
	project, err := f.projects.Read(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("read the Project: %w", err)
	}
	binding := project.RepositoryBinding
	if binding.RepositoryID == 0 {
		return "", errors.New("the Project has no bound repository")
	}
	if err = repohost.CheckInstallation(f.host, binding.InstallationID); err != nil {
		return "", err
	}
	return binding.FullName, nil
}

// readGoalsFile reads the goals file at commit from repository; found is
// false when the commit has none.
func (f *EvolveFlow) readGoalsFile(ctx context.Context, repository, commit string) ([]byte, bool, error) {
	file, err := f.host.ReadFile(ctx, repository, commit, goals.FilePath)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil, false, nil
	}
	return file, err == nil, err
}

// heldCommits says, for each proposal by review ID, whether the build commit
// holds its applied commit; one the repository no longer has is not held.
func (f *EvolveFlow) heldCommits(ctx context.Context, repository, build string, proposals []ProposalRecord) (map[string]bool, error) {
	holds := make(map[string]bool, len(proposals))
	for _, proposal := range proposals {
		held, err := f.host.IsAncestor(ctx, repository, proposal.AppliedCommit, build)
		if err != nil && !errors.Is(err, repohost.ErrNotFound) {
			return nil, fmt.Errorf("check whether %s holds the commit of proposal %s: %w", build, proposal.ReviewID, err)
		}
		holds[proposal.ReviewID] = held
	}
	return holds, nil
}

// deleteProposalDocuments deletes the stored documents of the pruned
// proposals and returns their reviews; a repeated delete of the exact
// version deletes nothing.
func (f *EvolveFlow) deleteProposalDocuments(ctx context.Context, pruned []ProposalRecord) ([]string, error) {
	deleted := make([]string, 0, len(pruned))
	for _, proposal := range pruned {
		if err := f.objects.Delete(ctx, proposal.Object); err != nil {
			return nil, fmt.Errorf("delete the proposal of %s: %w", proposal.ReviewID, err)
		}
		deleted = append(deleted, proposal.ReviewID)
	}
	return deleted, nil
}

// reviewsOn reports whether agent reviews run for the Project, by its
// TeamSpace and the review access setting.
func (f *EvolveFlow) reviewsOn(ctx context.Context, projectID string) (bool, error) {
	if f.settings.ReviewAccess != ReviewAccessListed {
		return f.settings.ReviewsOn(""), nil
	}
	project, err := f.projects.Read(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("read the Project: %w", err)
	}
	return f.settings.ReviewsOn(project.TeamSpaceID), nil
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
// review's start, and stores the evaluated runs once, as the runs.jsonl a
// review places (runsWriter), and metrics.json once beside the parts. A FlowID found in several parts counts once, with its newest run
// (runexport.Canonical). It evaluates only cursor.Parts, which this review
// exported from the Live engine; the check of the manifest's source label is
// a consistency check, not that guarantee. It returns the goal values with
// the runs digest and count of the evaluated runs, which the placed
// runs.jsonl reproduces (runexport.WriteCanonical).
func (f *EvolveFlow) computeMetrics(ctx context.Context, projectID string, record ReviewRecord, cursor exportCursor, manifestRef objectstore.ObjectRef) (MetricsSummary, exportDigest, error) {
	var manifest runexport.Manifest
	if err := f.objects.GetJSON(ctx, manifestRef, &manifest); err != nil {
		return MetricsSummary{}, exportDigest{}, fmt.Errorf("read the manifest: %w", err)
	}
	if err := manifest.CheckLive(projectID, record.ID); err != nil {
		return MetricsSummary{}, exportDigest{}, err
	}
	var all []runexport.Record
	for _, part := range cursor.Parts {
		records, err := f.readPart(ctx, part)
		if err != nil {
			return MetricsSummary{}, exportDigest{}, err
		}
		all = append(all, records...)
	}
	records := runexport.Canonical(all)
	stored := &runsWriter{ctx: ctx, objects: f.objects, prefix: exportPrefix(projectID, record.ID)}
	digest, count, err := runexport.WriteCanonical(stored, records)
	if err == nil {
		err = stored.finish(digest, count)
	}
	if err != nil {
		return MetricsSummary{}, exportDigest{}, fmt.Errorf("store the runs: %w", err)
	}
	metrics, err := goals.Evaluate(Goals{Spec: record.GoalsSpec}.spec(), records, record.StartedAt)
	if err != nil {
		return MetricsSummary{}, exportDigest{}, &metricsError{err: err}
	}
	key := exportPrefix(projectID, record.ID) + "metrics.json"
	var existing goals.Metrics
	if _, err = f.objects.GetCurrentJSON(ctx, key, &existing); errors.Is(err, objectstore.ErrNotFound) {
		_, err = f.objects.PutJSON(ctx, key, metrics)
	}
	if err != nil {
		return MetricsSummary{}, exportDigest{}, fmt.Errorf("store the metrics: %w", err)
	}
	return summarize(metrics), exportDigest{RunsSHA256: digest, Records: count}, nil
}

// readPart reads the exact object of a part and decodes its records.
func (f *EvolveFlow) readPart(ctx context.Context, part exportPart) ([]runexport.Record, error) {
	data, err := f.objects.Get(ctx, part.Ref)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", part.Name, err)
	}
	return decodePart(part.Name, data)
}

// decodePart decompresses the data of part name and decodes its records.
func decodePart(name string, data []byte) ([]runexport.Record, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", name, err)
	}
	defer func() { _ = reader.Close() }()
	records, err := runexport.Read(reader)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	return records, nil
}

// The evaluated runs of a review as computeMetrics stores them beside its
// parts: runs.jsonl in objects runs-<nnnn>.jsonl.gz of runsObjectBytes each
// before compression, the last one shorter, and the index runs.json, written
// last. place_evolve_export streams them into the review's placed runs.jsonl
// (storedRuns) instead of decoding and encoding the runs again.
const (
	runsIndexObject = "runs.json"
	runsObjectBytes = 24 << 20
)

// runsIndex is runs.json: the digest, run count and size of runs.jsonl and
// the number of its objects.
type runsIndex struct {
	RunsSHA256 string `json:"runs_sha256"`
	Runs       int    `json:"runs"`
	Bytes      int64  `json:"bytes"`
	Objects    int    `json:"objects"`
}

// runsObjectKey names the object n of a review's stored runs.
func runsObjectKey(prefix string, n int) string {
	return fmt.Sprintf("%sruns-%04d.jsonl.gz", prefix, n)
}

// runsWriter stores what it is given as the objects of a review's runs,
// holding one compressed object at a time; finish stores the last one and
// the index. The split depends only on the uncompressed bytes, so a retry
// stores the same objects.
type runsWriter struct {
	ctx     context.Context
	objects *objectstore.Store
	prefix  string
	buffer  bytes.Buffer
	gzip    *gzip.Writer
	content hash.Hash
	held    int64
	total   int64
	stored  int
}

func (w *runsWriter) Write(data []byte) (int, error) {
	for rest := data; len(rest) > 0; {
		if w.gzip == nil {
			w.buffer.Reset()
			w.gzip, w.content, w.held = gzip.NewWriter(&w.buffer), sha256.New(), 0
		}
		taken := rest[:min(int64(len(rest)), runsObjectBytes-w.held)]
		if _, err := w.gzip.Write(taken); err != nil {
			return 0, err
		}
		w.content.Write(taken)
		w.held, w.total, rest = w.held+int64(len(taken)), w.total+int64(len(taken)), rest[len(taken):]
		if w.held == runsObjectBytes {
			if err := w.store(); err != nil {
				return 0, err
			}
		}
	}
	return len(data), nil
}

// store stores the object being written. An object already stored with the
// same content, which another gzip may have compressed differently, stands.
func (w *runsWriter) store() error {
	if err := w.gzip.Close(); err != nil {
		return err
	}
	key := runsObjectKey(w.prefix, w.stored)
	if _, err := w.objects.Put(w.ctx, key, "application/gzip", w.buffer.Bytes()); err != nil {
		data, _, readErr := w.objects.GetCurrent(w.ctx, key)
		if readErr != nil || gunzipDigest(data) != hex.EncodeToString(w.content.Sum(nil)) {
			return fmt.Errorf("store %s: %w", key, err)
		}
	}
	w.gzip, w.stored = nil, w.stored+1
	return nil
}

// finish stores the last object, one of no runs for an empty export, and
// then the index of the runs whose digest and count WriteCanonical answered.
func (w *runsWriter) finish(digest string, runs int) error {
	if w.gzip != nil || w.stored == 0 {
		if w.gzip == nil {
			w.buffer.Reset()
			w.gzip, w.content = gzip.NewWriter(&w.buffer), sha256.New()
		}
		if err := w.store(); err != nil {
			return err
		}
	}
	_, err := w.objects.PutJSON(w.ctx, w.prefix+runsIndexObject, runsIndex{RunsSHA256: digest, Runs: runs, Bytes: w.total, Objects: w.stored})
	return err
}

// gunzipDigest is the hex SHA-256 of the decompressed data, empty when it
// does not decompress.
func gunzipDigest(data []byte) string {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	digest := sha256.New()
	if _, err = io.Copy(digest, reader); err != nil {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// storedRuns reads a review's stored runs.jsonl one object at a time,
// decompressed, and fails at its end unless it has the index's size and
// digest: a stream, never the whole file or its records.
type storedRuns struct {
	ctx     context.Context
	objects *objectstore.Store
	prefix  string
	index   runsIndex
	next    int
	current *gzip.Reader
	digest  hash.Hash
	read    int64
}

func (r *storedRuns) Read(p []byte) (int, error) {
	for {
		if r.current != nil {
			n, err := r.current.Read(p)
			r.digest.Write(p[:n])
			r.read += int64(n)
			if errors.Is(err, io.EOF) {
				r.current = nil
				err = nil
			}
			if n > 0 || err != nil {
				return n, err
			}
			continue
		}
		if r.next == r.index.Objects {
			if r.read != r.index.Bytes || hex.EncodeToString(r.digest.Sum(nil)) != r.index.RunsSHA256 {
				return 0, fmt.Errorf("%w: the stored runs are not the runs the review evaluated", runexport.ErrNotLive)
			}
			return 0, io.EOF
		}
		key := runsObjectKey(r.prefix, r.next)
		data, _, err := r.objects.GetCurrent(r.ctx, key)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", key, err)
		}
		if r.current, err = gzip.NewReader(bytes.NewReader(data)); err != nil {
			return 0, fmt.Errorf("decompress %s: %w", key, err)
		}
		r.next++
	}
}
