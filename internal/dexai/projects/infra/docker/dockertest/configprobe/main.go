// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command configprobe loads a workload's project configuration through the
// Connector SDK from the grant environment it runs with, as an application
// does when it starts, and resolves the application environment the
// configuration pins. It prints the configuration's revision and the SHA-256
// of each named value, never a value, and exits 1 with the SDK's error when
// either step fails. The Docker integration tests run it in a container on a
// project network.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		fail("load the project configuration", err)
	}
	environment, err := project.ResolveApplicationEnvironment(ctx)
	if err != nil {
		fail("resolve the application environment", err)
	}
	fmt.Printf("revision %d\n", project.Configuration.Revision)
	for _, name := range os.Args[1:] {
		value, found := environment.Lookup(name)
		if !found {
			fmt.Printf("%s absent\n", name)
			continue
		}
		fmt.Printf("%s %x\n", name, sha256.Sum256([]byte(value)))
	}
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", step, err)
	os.Exit(1)
}
