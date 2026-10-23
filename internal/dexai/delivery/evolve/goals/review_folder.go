// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// reviewFolderRoot is where place_evolve_export places a review's folder
// (conversation.EvolveWorkspaceRoot, which this package cannot import: the
// coding image builds it alone), relative to the repository root of the
// workspace or of a subagent's worktree, which links the folder there.
const reviewFolderRoot = ".local/superverse-evolve"

// reviewIDPattern is a review ID as EvolveFlow writes it.
var reviewIDPattern = regexp.MustCompile(`^r-[1-9][0-9]{0,17}$`)

// ReviewFolder is a review's folder as place_evolve_export placed it: the
// goals of the Live build (goals.md), the canonical runs (runs.jsonl), the
// time metrics.json evaluated them at, and the binding of what was read
// (review, goals commit, runs digest and count), which compare and open-runs
// complete with the before and base commits.
type ReviewFolder struct {
	Spec        Spec
	Records     []runexport.Record
	EvaluatedAt time.Time
	Binding     ExportBinding
}

// ReadReviewFolder reads the folder of review reviewID under root, a
// repository root, refusing it unless its manifest names the Live source,
// this review and a Project (runexport.ErrNotLive), and its runs.jsonl has the
// manifest's runs digest. It never reads the workspace's goals file.
func ReadReviewFolder(root, reviewID string) (ReviewFolder, error) {
	if !reviewIDPattern.MatchString(reviewID) {
		return ReviewFolder{}, fmt.Errorf("%q is not a review ID such as r-12", reviewID)
	}
	folder := filepath.Join(root, reviewFolderRoot, reviewID)
	read := func(name string) ([]byte, error) {
		data, err := os.ReadFile(filepath.Join(folder, name)) // #nosec G304 -- the review's own placed folder.
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s has no %s; place the review's export with place_evolve_export first", folder, name)
		}
		return data, err
	}
	var manifest runexport.Manifest
	data, err := read("manifest.json")
	if err == nil {
		err = json.Unmarshal(data, &manifest)
	}
	if err != nil {
		return ReviewFolder{}, fmt.Errorf("manifest.json: %w", err)
	}
	if err = manifest.CheckPlaced(manifest.ProjectID, reviewID); err != nil {
		return ReviewFolder{}, err
	}
	if manifest.GoalsCommit == "" || manifest.RunsSHA256 == "" {
		return ReviewFolder{}, fmt.Errorf("%w: its manifest names no goals commit or no runs digest", runexport.ErrNotLive)
	}
	runs, err := read("runs.jsonl")
	if err != nil {
		return ReviewFolder{}, err
	}
	if sum := sha256.Sum256(runs); hex.EncodeToString(sum[:]) != manifest.RunsSHA256 {
		return ReviewFolder{}, fmt.Errorf("runs.jsonl has SHA-256 %s, not the manifest's runs_sha256 %s: it is not the review's placed export of the Live runs",
			hex.EncodeToString(sum[:]), manifest.RunsSHA256)
	}
	records, err := runexport.ReadPlaced(bytes.NewReader(runs))
	if err != nil {
		return ReviewFolder{}, fmt.Errorf("runs.jsonl: %w", err)
	}
	file, err := read("goals.md")
	if err != nil {
		return ReviewFolder{}, err
	}
	spec, problems := Parse(file)
	if len(problems) != 0 {
		return ReviewFolder{}, fmt.Errorf("goals.md of the Live build %s has %d problem(s), the first %s", manifest.GoalsCommit, len(problems), problems[0])
	}
	var metrics Metrics
	if data, err = read("metrics.json"); err == nil {
		err = json.Unmarshal(data, &metrics)
	}
	if err != nil || metrics.EvaluatedAt.IsZero() {
		return ReviewFolder{}, fmt.Errorf("metrics.json names no evaluation time (%w)", err)
	}
	return ReviewFolder{Spec: spec, Records: records, EvaluatedAt: metrics.EvaluatedAt, Binding: ExportBinding{
		ReviewID: reviewID, GoalsCommit: manifest.GoalsCommit, RunsSHA256: manifest.RunsSHA256, Records: len(records),
	}}, nil
}
