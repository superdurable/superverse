// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"fmt"
	"sort"

	"github.com/superdurable/superversev2/cmd/dexai-connect/trace"
)

func runTrace(args []string) error {
	dir, err := newFlags("trace").open(args)
	if err != nil {
		return err
	}
	calls, err := trace.Read(dir.Path())
	if err != nil {
		return err
	}
	segments := map[string][]int64{"mcp_to_report": {}, "report_roundtrip": {}, "answer_to_sync": {}, "sync_to_codex": {}, "total": {}}
	for _, call := range calls {
		fmt.Printf("%s %s %s", call.LocalID, call.CallID, call.Tool)
		for _, part := range []struct {
			name       string
			start, end int64
		}{
			{"mcp_to_report", call.MCPAt, call.ReportedAt},
			{"report_roundtrip", call.ReportedAt, call.ReportAnswerAt},
			{"answer_to_sync", call.ReportAnswerAt, call.SyncResultAt},
			{"sync_to_codex", call.SyncResultAt, call.CodexAt},
			{"total", call.MCPAt, call.CodexAt},
		} {
			if part.start > 0 && part.end >= part.start {
				delta := part.end - part.start
				segments[part.name] = append(segments[part.name], delta)
				fmt.Printf(" %s=%dms", part.name, delta)
			}
		}
		fmt.Printf(" outcome=%s\n", call.Outcome)
	}
	for _, name := range []string{"mcp_to_report", "report_roundtrip", "answer_to_sync", "sync_to_codex", "total"} {
		values := segments[name]
		if len(values) == 0 {
			continue
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		fmt.Printf("%s n=%d p50=%dms p90=%dms p99=%dms\n", name, len(values), percentile(values, 50), percentile(values, 90), percentile(values, 99))
	}
	return nil
}

func percentile(sorted []int64, percent int) int64 {
	index := (percent*len(sorted)+99)/100 - 1
	return sorted[max(0, index)]
}
