// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package trace keeps bounded, private Bridge call timing records.
package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const (
	File         = "calls.jsonl"
	maxBytes     = 4 << 20
	maxRotations = 2
)

// Call contains only identities, timing points and an outcome code.
type Call struct {
	CallID         string `json:"callId,omitempty"`
	LocalID        string `json:"localId"`
	Tool           string `json:"tool"`
	MCPAt          int64  `json:"mcpAt"`
	ReportedAt     int64  `json:"reportedAt,omitempty"`
	ReportAnswerAt int64  `json:"reportAnswerAt,omitempty"`
	SyncResultAt   int64  `json:"syncResultAt,omitempty"`
	CodexAt        int64  `json:"codexAt"`
	Outcome        string `json:"outcome"`
}

// Append writes one complete call under a process-shared lock and rotates at
// 4 MiB. The lock and all trace files are private within the 0700 state dir.
func Append(dir string, call Call) error {
	lock, err := os.OpenFile(filepath.Join(dir, "calls.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck // process exit also releases the lock
	data, err := json.Marshal(call)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, File)
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.Size()+int64(len(data)+1) > maxBytes {
		for i := maxRotations; i >= 1; i-- {
			from := path
			if i > 1 {
				from = fmt.Sprintf("%s.%d", path, i-1)
			}
			to := fmt.Sprintf("%s.%d", path, i)
			if moveErr := os.Rename(from, to); moveErr != nil && !os.IsNotExist(moveErr) {
				return moveErr
			}
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

// Read returns the bounded trace in chronological file order.
func Read(dir string) ([]Call, error) {
	var calls []Call
	for i := maxRotations; i >= 0; i-- {
		path := filepath.Join(dir, File)
		if i > 0 {
			path = fmt.Sprintf("%s.%d", path, i)
		}
		data, err := os.ReadFile(path) //nolint:gosec // private, size-capped Bridge trace
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, line := range splitLines(data) {
			var call Call
			if err := json.Unmarshal(line, &call); err != nil {
				return nil, fmt.Errorf("decode %s: %w", path, err)
			}
			calls = append(calls, call)
		}
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].MCPAt < calls[j].MCPAt })
	return calls, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	return lines
}
