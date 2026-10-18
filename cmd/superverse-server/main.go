// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/dexai/bootstrap"
)

// configFileVariable names the configuration files, comma separated, later
// files overriding earlier ones. It is the only setting the server reads
// from its environment; everything else is in those files or is a secret.
const configFileVariable = "SUPERVERSE_CONFIG_FILE"

// main serves, or with the single argument migrate applies the database
// migrations and exits, which deployments run before the server starts. The
// file-extract subcommand is the extractor child that the server itself
// starts; it reads neither configuration nor environment.
func main() {
	if len(os.Args) > 1 && os.Args[1] == fileextract.Subcommand {
		os.Exit(fileextract.ServeChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	logger := slog.Default()
	migrate := len(os.Args) == 2 && os.Args[1] == "migrate"
	if len(os.Args) > 1 && !migrate {
		fmt.Fprintln(os.Stderr, "usage: superverse-server [migrate]")
		os.Exit(2)
	}
	settings, err := loadSettings()
	if err != nil {
		// Plain text keeps a list of violations readable.
		fmt.Fprintf(os.Stderr, "Superverse server configuration is invalid:\n%v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if migrate {
		err = bootstrap.Migrate(ctx, logger, settings)
	} else {
		err = bootstrap.Run(ctx, logger, settings)
	}
	stop()
	if err != nil {
		logger.ErrorContext(ctx, "Superverse server stopped", "error", err)
		os.Exit(1)
	}
}

// loadSettings reads and validates the configuration files before anything starts.
func loadSettings() (config.Config, error) {
	value := os.Getenv(configFileVariable)
	if value == "" {
		return config.Config{}, errors.New(configFileVariable + " must name the configuration file")
	}
	paths := strings.Split(value, ",")
	for index, path := range paths {
		if paths[index] = strings.TrimSpace(path); paths[index] == "" {
			return config.Config{}, errors.New(configFileVariable + " contains an empty path")
		}
	}
	settings, err := config.Load(paths...)
	if err != nil {
		return config.Config{}, err
	}
	return settings, settings.Validate()
}
