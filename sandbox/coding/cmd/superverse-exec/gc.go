// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// finishedRun is a run that ended, with its end and its size on disk.
type finishedRun struct {
	run     ledgerRun
	endedAt time.Time
	bytes   int64
}

// collectGarbage removes finished runs that ended more than two hours ago,
// then the oldest finished runs until the ledger holds at most 256 MiB. A
// running run is never removed, and neither is the run of the calling begin.
// Each removal holds the run's lock, so no begin decides the ID meanwhile.
// Errors are left for the next begin: garbage collection never fails a begin.
func collectGarbage(current string) {
	entries, err := os.ReadDir(runsDirectory)
	if err != nil {
		return
	}
	var finished []finishedRun
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current {
			continue
		}
		run := runOf(entry.Name())
		size := directorySize(run.directory)
		total += size
		if ended, ok := run.endedAt(entry); ok {
			finished = append(finished, finishedRun{run: run, endedAt: ended, bytes: size})
		}
	}
	slices.SortFunc(finished, func(left, right finishedRun) int { return left.endedAt.Compare(right.endedAt) })
	cutoff := time.Now().Add(-finishedRunRetention)
	for _, candidate := range finished {
		if !candidate.endedAt.Before(cutoff) && total <= maximumLedgerBytes {
			break
		}
		if candidate.run.remove() {
			total -= candidate.bytes
		}
	}
}

// endedAt is when a run ended: its recorded end, or for a lost run its last
// output. A directory without an admission is the leftover of an interrupted
// begin and counts as ended when it was last changed.
func (r ledgerRun) endedAt(entry fs.DirEntry) (time.Time, bool) {
	current, err := r.observe()
	switch {
	case err != nil || current.state == stateRunning:
		return time.Time{}, false
	case current.state == stateExited:
		return current.exit.EndedAt, true
	case current.state == stateLost:
		if info, statErr := os.Stat(r.outputPath()); statErr == nil {
			return info.ModTime(), true
		}
		return current.record.AdmittedAt, true
	}
	info, err := entry.Info()
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// remove deletes the run's directory under its lock. A run whose lock is held
// is running or being decided again and stays.
func (r ledgerRun) remove() bool {
	lock, acquired, err := r.tryLock(false)
	if err != nil || !acquired {
		return false
	}
	defer func() { _ = lock.Close() }()
	return os.RemoveAll(r.directory) == nil
}

func directorySize(directory string) int64 {
	var total int64
	_ = filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			if info, infoErr := entry.Info(); infoErr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
