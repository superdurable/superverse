// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command app-manifest renders trusted Release AppManifest artifacts.
// It parses a manifest as data through the same owner used by the server; it
// never executes code or scripts from the application repository.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

func render() error {
	manifestPath := flag.String("manifest", "", "exact source dex-app.yaml file")
	commit := flag.String("source-commit", "", "full accepted Git commit")
	project := flag.String("project-id", "", "accepted project ID")
	release := flag.String("release-id", "", "accepted immutable Release ID")
	flag.Parse()
	// Per-commit publishing uses the exact commit as ReleaseID, which
	// ExtractAppManifest checks as a full commit SHA.
	if flag.NArg() != 0 || *manifestPath == "" || len(*project) > 32 || !flowid.IsLowerAlphanumeric(*project) || *release != *commit {
		return errors.New("exact manifest and Release identity are required")
	}
	info, err := os.Lstat(*manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("manifest must be a regular source file")
	}
	input, err := os.Open(*manifestPath)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	const maxManifestBytes = 4 << 20
	contents, err := io.ReadAll(io.LimitReader(input, maxManifestBytes+1))
	if err != nil || len(contents) > maxManifestBytes {
		return errors.New("manifest cannot be read within its size bound")
	}
	manifest, err := appmanifest.ExtractAppManifest(*commit, contents)
	if err != nil {
		return err
	}
	connectors, environment := appmanifest.ReleaseContracts(*project, *release, manifest)
	result := struct {
		Connectors        appmanifest.ConnectorContract   `json:"connectorContract"`
		Environment       appmanifest.EnvironmentContract `json:"environmentContract"`
		ConnectorDigest   string                          `json:"connectorContractDigest"`
		EnvironmentDigest string                          `json:"environmentContractDigest"`
	}{connectors, environment, manifest.ConnectorContractDigest, manifest.EnvironmentContractDigest}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := render(); err != nil {
		// App-controlled manifest contents never become a diagnostic payload.
		fmt.Fprintln(os.Stderr, "Release AppManifest extraction failed")
		os.Exit(1)
	}
}
