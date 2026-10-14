// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// Run composes the server from the validated operator configuration and the
// secrets of the environment, and serves it until ctx ends. A secret that is
// absent or invalid fails before any listener opens.
func Run(ctx context.Context, logger *slog.Logger, settings serverconfig.Config) (err error) {
	config, err := loadConfig(ctx, logger, settings)
	if err != nil {
		return err
	}
	var owned resources
	defer func() { err = errors.Join(err, owned.release()) }()
	server, err := compose(ctx, config, logger, &owned)
	if err != nil {
		return err
	}
	return serve(ctx, config, server)
}

// Migrate applies the pending database migrations as the owner role and
// returns; the server itself never migrates.
func Migrate(ctx context.Context, logger *slog.Logger, settings serverconfig.Config) error {
	connection, err := postgresConnection(settings.Postgres, postgres.OwnerRole)
	if err != nil {
		return err
	}
	connection.MaxConnections = 1
	db, err := postgres.Open(ctx, connection)
	if err != nil {
		return err
	}
	applied, err := postgres.Migrate(ctx, db)
	logger.InfoContext(ctx, "Superverse database migrations", "applied", applied)
	return errors.Join(err, db.Close())
}

// resources are the process-local connections, caches and subprocesses that
// composition opened. release closes them in reverse order.
type resources []func() error

func (r *resources) add(release func() error) { *r = append(*r, release) }

func (r resources) release() error {
	var err error
	for i := len(r) - 1; i >= 0; i-- {
		err = errors.Join(err, r[i]())
	}
	return err
}
