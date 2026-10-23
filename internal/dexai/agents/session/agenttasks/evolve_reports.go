// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agenttasks

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
)

// The files of a change subagent's result that report_evolve_review reads for
// a proposal, under .superverse/evolve/<review ID>/, with the most bytes each
// may hold.
const (
	evolveProposalFile = "proposal.md"
	evolveBacktestFile = "backtest.json"
	evolveOpenRunsFile = "open-runs.json"
)

var evolveResultLimits = map[string]int64{evolveProposalFile: 32 << 10, evolveBacktestFile: 256 << 10, evolveOpenRunsFile: 64 << 10}

// EvolveResultFiles are the paths in a change's result, relative to the
// repository, of review reviewID's proposal files, with their limits.
func EvolveResultFiles(reviewID string) map[string]int64 {
	files := make(map[string]int64, len(evolveResultLimits))
	for name, limit := range evolveResultLimits {
		files[path.Join(".superverse/evolve", reviewID, name)] = limit
	}
	return files
}

// EvolveReviewFiles are the files a proposal of review reviewID rests on, as
// read from its change's result, for the review's report: their contents, or
// why they cannot back it, which the review answers after its stale check.
func EvolveReviewFiles(reviewID string, read map[string]git.ResultFile) *evolve.ReviewFiles {
	files := &evolve.ReviewFiles{}
	var missing, large []string
	for name, limit := range evolveResultLimits {
		file := read[path.Join(".superverse/evolve", reviewID, name)]
		switch {
		case file.Missing:
			missing = append(missing, name)
		case file.TooLarge || file.Binary:
			large = append(large, fmt.Sprintf("%s (%d bytes, at most %d of UTF-8 text)", name, file.Size, limit))
		}
		switch name {
		case evolveProposalFile:
			files.Proposal = []byte(file.Content)
		case evolveBacktestFile:
			files.Backtest = []byte(file.Content)
		case evolveOpenRunsFile:
			files.OpenRuns = []byte(file.Content)
		}
	}
	switch {
	case len(missing) > 0:
		files.Unreadable = &evolve.ReportDefect{Refusal: evolve.ReportInvalidBacktest,
			Detail: fmt.Sprintf("the change's result holds no %s under .superverse/evolve/%s/", strings.Join(missing, ", "), reviewID)}
	case len(large) > 0:
		files.Unreadable = &evolve.ReportDefect{Refusal: evolve.ReportTooLarge, Detail: "over the limit: " + strings.Join(large, ", ")}
	}
	return files
}

// EvolveRefusalCode is the tool failure the model reads for a review's
// refusal of its report.
func EvolveRefusalCode(refusal evolve.ReportRefusal) toolfailure.Code {
	switch refusal {
	case evolve.ReportStaleReview:
		return toolfailure.EvolveReviewStale
	case evolve.ReportStaleProposal:
		return toolfailure.EvolveProposalStale
	case evolve.ReportExportMismatch:
		return toolfailure.ExportMismatch
	case evolve.ReportInvalidBacktest:
		return toolfailure.InvalidBacktest
	case evolve.ReportTooLarge:
		return toolfailure.EvolveResultTooLarge
	}
	panic("agenttasks: the review refusal " + string(refusal) + " has no tool failure")
}

// EvolveUsage is the platform model usage of an agent review: turns, the
// Session's own in the review's turns, and the usage of agents, the
// subagents those turns started, which their ends reported.
func EvolveUsage(turns []conversation.ModelUsage, agents []string, records map[string]SubagentRecord) []conversation.ModelUsage {
	usage := turns
	for _, record := range SortedSubagents(records) {
		if slices.Contains(agents, record.AgentID) {
			usage = conversation.SumModelUsage(usage, record.Usage)
		}
	}
	return usage
}
