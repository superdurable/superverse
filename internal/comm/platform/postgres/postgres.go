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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	// The pgx driver registers itself as "pgx" with database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The roles that migrations grant to. Each environment's private procedure
// creates them with their passwords; no migration or Terraform state holds one.
const (
	// OwnerRole owns the schema; only migrations connect as it.
	OwnerRole = "superverse_owner"
	// ServerRole is the server's role: full access to the tables it owns and
	// read access to the read model.
	ServerRole = "superverse_server"
	// dexRole is the platform Dex's Attribute Store role, the only writer of
	// the read model's tables.
	dexRole = "superverse_dex"
)

// Connection is where and as whom the server connects. The password comes
// from the server's secret environment; everything else from its
// configuration.
type Connection struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	SSLMode        string
	MaxConnections int
}

// dataSourceName renders the connection as a URL that never reaches a log.
func (c Connection) dataSourceName() string {
	location := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Database,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
	return location.String()
}

// Open returns a pool of at most MaxConnections connections that has answered
// a ping. The caller closes it.
func Open(ctx context.Context, c Connection) (*sql.DB, error) {
	db, err := sql.Open("pgx", c.dataSourceName())
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL %s/%s as %s: %w", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), c.Database, c.User, err)
	}
	db.SetMaxOpenConns(c.MaxConnections)
	db.SetMaxIdleConns(c.MaxConnections)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("reach PostgreSQL %s/%s as %s: %w", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), c.Database, c.User, err), db.Close())
	}
	return db, nil
}

// InTransaction runs work in one transaction, committing when it returns nil
// and rolling back otherwise.
func InTransaction(ctx context.Context, db *sql.DB, work func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a transaction: %w", err)
	}
	if err = work(tx); err != nil {
		return errors.Join(err, rollback(tx))
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit a transaction: %w", err)
	}
	return nil
}

func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("roll back a transaction: %w", err)
	}
	return nil
}

// JSONColumn is the scan destination of a nullable JSON or JSONB column held
// as raw JSON: database/sql scans NULL into a byte slice as nil, which a
// json.RawMessage destination refuses.
func JSONColumn(value *json.RawMessage) any { return (*[]byte)(value) }

// ErrorMetadata exposes only SQLSTATE and schema-owned constraint identity.
// Message, Detail, Where and other value-bearing fields never leave this helper.
func ErrorMetadata(err error) (sqlState, constraint string) {
	var rejected *pgconn.PgError
	if errors.As(err, &rejected) {
		return rejected.Code, rejected.ConstraintName
	}
	return "", ""
}
