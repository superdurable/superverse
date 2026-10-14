// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// migrations are the schema changes, applied in file name order. Each only
// adds: a released file never changes, and Migrate refuses one that did.
//
//go:embed migrations/*.sql
var migrations embed.FS

// migrationLock is the advisory lock key that serializes migrations.
const migrationLock = 7_350_202_610

// migration is one embedded schema change.
type migration struct {
	version, checksum, statements string
}

// Migrate applies every migration that schema_migrations does not record,
// each in its own transaction, under an advisory lock, and returns the
// versions it applied. A recorded migration whose file changed is an error.
func Migrate(ctx context.Context, db *sql.DB) ([]string, error) {
	pending, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("take a migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return nil, fmt.Errorf("take the migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock)
	}()
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	recorded, err := recordedMigrations(ctx, conn)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, m := range pending {
		checksum, found := recorded[m.version]
		if found {
			if checksum != m.checksum {
				return applied, fmt.Errorf("migration %s changed after it was applied", m.version)
			}
			continue
		}
		if err = apply(ctx, conn, m); err != nil {
			return applied, err
		}
		applied = append(applied, m.version)
	}
	return applied, nil
}

func embeddedMigrations() ([]migration, error) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list the migrations: %w", err)
	}
	slices.Sort(names)
	result := make([]migration, 0, len(names))
	for _, name := range names {
		content, err := migrations.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(content)
		result = append(result, migration{version: strings.TrimSuffix(path.Base(name), ".sql"), checksum: hex.EncodeToString(sum[:]), statements: string(content)})
	}
	return result, nil
}

func recordedMigrations(ctx context.Context, conn *sql.Conn) (map[string]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	recorded := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if err = rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("read schema_migrations: %w", err)
		}
		recorded[version] = checksum
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	return recorded, nil
}

func apply(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", m.version, err)
	}
	if _, err = tx.ExecContext(ctx, m.statements); err != nil {
		return errors.Join(fmt.Errorf("apply migration %s: %w", m.version, err), rollback(tx))
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", m.version, m.checksum); err != nil {
		return errors.Join(fmt.Errorf("record migration %s: %w", m.version, err), rollback(tx))
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.version, err)
	}
	return nil
}
