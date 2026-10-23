// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package reviewfolder

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

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// folderRoot is where place_evolve_export places a review's folder
// (conversation.EvolveWorkspaceRoot, which this package cannot import: the
// coding image builds it alone), relative to the repository root of the
// workspace or of a subagent's worktree, which links the folder there.
const folderRoot = ".local/superverse-evolve"

// reviewIDPattern is a review ID as EvolveFlow writes it.
var reviewIDPattern = regexp.MustCompile(`^r-[1-9][0-9]{0,17}$`)

// Folder is a review's folder as place_evolve_export placed it: the goals of
// the Live build (goals.md), the canonical runs (runs.jsonl), the time
// metrics.json evaluated them at, and the binding of what was read (review,
// goals commit, runs digest and count), which compare and open-runs complete
// with the before and base commits.
type Folder struct {
	Spec        goals.Spec
	Records     []runexport.Record
	EvaluatedAt time.Time
	Binding     goals.ExportBinding
}

// Read reads the folder of review reviewID under root, a repository root,
// refusing it unless its manifest names the Live source, this review and a
// Project (runexport.ErrNotLive), and its runs.jsonl has the manifest's runs
// digest. It never reads the workspace's goals file.
func Read(root, reviewID string) (Folder, error) {
	if !reviewIDPattern.MatchString(reviewID) {
		return Folder{}, fmt.Errorf("%q is not a review ID such as r-12", reviewID)
	}
	folder := filepath.Join(root, folderRoot, reviewID)
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
		return Folder{}, fmt.Errorf("manifest.json: %w", err)
	}
	if err = manifest.CheckPlaced(manifest.ProjectID, reviewID); err != nil {
		return Folder{}, err
	}
	if manifest.GoalsCommit == "" || manifest.RunsSHA256 == "" {
		return Folder{}, fmt.Errorf("%w: its manifest names no goals commit or no runs digest", runexport.ErrNotLive)
	}
	runs, err := read("runs.jsonl")
	if err != nil {
		return Folder{}, err
	}
	if sum := sha256.Sum256(runs); hex.EncodeToString(sum[:]) != manifest.RunsSHA256 {
		return Folder{}, fmt.Errorf("runs.jsonl has SHA-256 %s, not the manifest's runs_sha256 %s: it is not the review's placed export of the Live runs",
			hex.EncodeToString(sum[:]), manifest.RunsSHA256)
	}
	records, err := runexport.ReadPlaced(bytes.NewReader(runs))
	if err != nil {
		return Folder{}, fmt.Errorf("runs.jsonl: %w", err)
	}
	file, err := read("goals.md")
	if err != nil {
		return Folder{}, err
	}
	// The server read these goals of the Live build; a local profile's accepts a within from a minute.
	spec, problems := goals.Parse(file, goals.MinuteDurations)
	if len(problems) != 0 {
		return Folder{}, fmt.Errorf("goals.md of the Live build %s has %d problem(s), the first %s", manifest.GoalsCommit, len(problems), problems[0])
	}
	var metrics goals.Metrics
	if data, err = read("metrics.json"); err == nil {
		err = json.Unmarshal(data, &metrics)
	}
	if err != nil {
		return Folder{}, fmt.Errorf("metrics.json: %w", err)
	}
	if metrics.EvaluatedAt.IsZero() {
		return Folder{}, errors.New("metrics.json names no evaluation time")
	}
	return Folder{Spec: spec, Records: records, EvaluatedAt: metrics.EvaluatedAt, Binding: goals.ExportBinding{
		ReviewID: reviewID, GoalsCommit: manifest.GoalsCommit, RunsSHA256: manifest.RunsSHA256, Records: len(records),
	}}, nil
}
