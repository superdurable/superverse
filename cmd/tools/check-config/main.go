// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command check-config loads server configuration with the server's own strict
// Load and Validate, so a file with an unknown key or an out-of-range value
// fails before a deploy or `make check` instead of in a restarting Pod.
//
// Without arguments it checks every checked-in profile. local-kind.yaml is the
// base layer of the Kind profiles; every other Kind profile is validated on
// top of it, the way `sv2 kind deploy` layers them. native.yaml is a complete
// profile of its own. The deploy and the Native launcher compute the project
// image digests and the run's own values, so this mode completes them with
// shape-only values that it never deploys.
//
// With arguments it checks exactly those files, layered in order, and
// completes nothing: that is the preflight of a rendered document or of a
// Native run's layers.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/superdurable/superversev2/cmd/superverse-server/config"
)

const (
	directory = "cmd/superverse-server/config"
	base      = "local-kind.yaml"
	// native is the complete profile of Local Native.
	native = "native.yaml"
	// shapeImage is a digest-pinned reference shape the deploy replaces.
	shapeImage = "registry.invalid/image@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	// shapeDirectory and shapeRunID stand for what the Native launcher computes
	// for a run.
	shapeDirectory = "/shape/superverse-v2/native"
	shapeRunID     = "00000000-0000-4000-8000-000000000000"
)

func main() {
	if len(os.Args) > 1 {
		if err := check(false, os.Args[1:]...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Configuration: the files load strictly and validate")
		return
	}
	profiles, err := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if err != nil || len(profiles) == 0 {
		fmt.Fprintf(os.Stderr, "Configuration profiles: no profile found in %s (run from the repository root)\n", directory)
		os.Exit(1)
	}
	var failed bool
	var names []string
	for _, profile := range profiles {
		name := filepath.Base(profile)
		paths := []string{filepath.Join(directory, base)}
		switch name {
		case base:
		case native:
			paths = []string{profile}
		default:
			paths = append(paths, profile)
		}
		if err = check(true, paths...); err != nil {
			fmt.Fprintf(os.Stderr, "Configuration profile %s:\n%v\n", name, err)
			failed = true
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".yaml"))
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("Configuration profiles: %s load strictly and validate\n", strings.Join(names, ", "))
}

func check(complete bool, paths ...string) error {
	profile, err := config.Load(paths...)
	if err != nil {
		return err
	}
	if complete {
		profile.Live.DexImage, profile.Live.TemporalImage, profile.Live.S3Image = shapeImage, shapeImage, shapeImage
		if profile.Runtime == config.RuntimeDocker {
			profile.Native.RunID, profile.Native.StateDir, profile.LocalGit.Dir = shapeRunID, shapeDirectory, shapeDirectory+"/git"
		}
	}
	return profile.Validate()
}
