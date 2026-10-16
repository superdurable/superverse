// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command check-flow-registry builds the Dex SDK registry of every server Flow
// the way the server bootstrap does, so that a Flow the SDK refuses, or whose
// StepOptions do not resolve (a failure route that leads back to its own Step
// recurses without end), fails `make check` instead of the server's start.
// The graphs dexcli derives from the source do not build this registry.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/bootstrap"
)

func main() {
	types, err := bootstrap.CheckFlowRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Flow registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Flow registry: the Dex SDK registers all %d server Flows (%s)\n", len(types), strings.Join(types, ", "))
}
