// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"os"
	"slices"
	"strings"
)

// everyExamplePaths are the repository paths outside an example folder that
// every example's check reads: the shared base, this command, the Makefile
// with the dexcli pin and the check, the pinned-binary installer and the
// validation workflow with its Go and Node versions. A path ending in "/"
// names a directory.
var everyExamplePaths = []string{
	"app-template/",
	"cmd/tools/app-template-archive/",
	"Makefile",
	"scripts/install-pinned-binary.sh",
	".github/workflows/application-validate.yml",
}

// readChangedPaths reads one repository-relative path per line of file.
func readChangedPaths(file string) ([]string, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(contents), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// changedExamples returns, in examples' order, the default example and every
// example the changed paths reach: a path of an example folder reaches that
// example and the examples extending it, one of everyExamplePaths every example
// and a file every example shares, which no tree holds, none.
func changedExamples(examples []composedExample, changed []string) []string {
	reached := map[string]bool{}
	for _, name := range changed {
		if id, found := exampleOfPath(name); found {
			reached[id] = true
			continue
		}
		if shared, found := strings.CutPrefix(name, "app-template/"+examplesDirectory+"/"); found && slices.Contains(sharedExampleFiles, shared) {
			continue
		}
		for _, every := range everyExamplePaths {
			if name == every || (strings.HasSuffix(every, "/") && strings.HasPrefix(name, every)) {
				for _, example := range examples {
					reached[example.manifest.ID] = true
				}
			}
		}
	}
	// An example extending a reached one is reached; examples extend at most a few levels.
	for grew := true; grew; {
		grew = false
		for _, example := range examples {
			if extends := example.manifest.Extends; extends != "" && reached[extends] && !reached[example.manifest.ID] {
				reached[example.manifest.ID], grew = true, true
			}
		}
	}
	reached[examples[0].manifest.ID] = true
	ids := make([]string, 0, len(reached))
	for _, example := range examples {
		if reached[example.manifest.ID] {
			ids = append(ids, example.manifest.ID)
		}
	}
	return ids
}

// exampleOfPath is the example whose folder holds the repository path name.
func exampleOfPath(name string) (string, bool) {
	rest, found := strings.CutPrefix(name, "app-template/"+examplesDirectory+"/")
	if !found {
		return "", false
	}
	id, _, inFolder := strings.Cut(rest, "/")
	return id, inFolder
}
