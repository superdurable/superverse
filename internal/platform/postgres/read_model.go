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
	"strings"
	"time"
)

// ErrNotFound is a read model row that does not exist: Dex has not written
// it yet, or the resource does not exist.
var ErrNotFound = errors.New("read model row not found")

const (
	// rowWait bounds how long AwaitRow waits for the row of a resource
	// created a moment ago, which Dex writes some time after its Flow.
	rowWait = 10 * time.Second
	// rowPoll is how often AwaitRow reads again.
	rowPoll = 500 * time.Millisecond
)

// AwaitRow reads the row of one resource with read. When the row is missing,
// it asks once whether the resource's Flow exists: a resource without one is
// ErrNotFound at once, and one whose row Dex has not written yet is read again
// every rowPoll for at most rowWait, after which it is ErrNotFound.
func AwaitRow[T any](ctx context.Context, read func(context.Context) (T, error), exists func(context.Context) (bool, error)) (T, error) {
	value, err := read(ctx)
	if !errors.Is(err, ErrNotFound) {
		return value, err
	}
	found, existsErr := exists(ctx)
	if existsErr != nil {
		return value, fmt.Errorf("check the resource of a missing row: %w", existsErr)
	}
	if !found {
		return value, err
	}
	deadline := time.Now().Add(rowWait)
	for errors.Is(err, ErrNotFound) && time.Now().Add(rowPoll).Before(deadline) {
		select {
		case <-ctx.Done():
			return value, ctx.Err()
		case <-time.After(rowPoll):
		}
		value, err = read(ctx)
	}
	return value, err
}

// PageKey is the keyset position after the last row of a page that lists
// rows newest first, by creation time and then by ID.
type PageKey struct {
	CreatedAt time.Time
	ID        string
}

// String encodes the key for a page token; ParsePageKey reads it back exactly.
func (k PageKey) String() string { return k.CreatedAt.UTC().Format(time.RFC3339Nano) + "/" + k.ID }

// ParsePageKey reads a key that String encoded.
func ParsePageKey(text string) (PageKey, error) {
	createdAt, id, found := strings.Cut(text, "/")
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if !found || id == "" || err != nil {
		return PageKey{}, fmt.Errorf("page key %q is malformed", text)
	}
	return PageKey{CreatedAt: created, ID: id}, nil
}

// AfterValues are the keyset parameters of the page after key: NULL and ""
// for the first page.
func AfterValues(after *PageKey) (sql.Null[time.Time], string) {
	if after == nil {
		return sql.Null[time.Time]{}, ""
	}
	return sql.Null[time.Time]{V: after.CreatedAt, Valid: true}, after.ID
}

// Page cuts the rows of a query that read limit+1 rows to limit and returns
// the key of the page after them, nil when no row follows.
func Page[T any](rows []T, limit int, key func(T) PageKey) ([]T, *PageKey) {
	if len(rows) <= limit {
		return rows, nil
	}
	rows = rows[:limit]
	next := key(rows[limit-1])
	return rows, &next
}

// QueryRows runs a query and scans every row it returns with scan, which
// reads one row through the Scan it receives. Errors name operation.
func QueryRows[T any](ctx context.Context, db *sql.DB, operation string, scan func(func(...any) error) (T, error), statement string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = rows.Close() }()
	var result []T
	for rows.Next() {
		value, scanErr := scan(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%s: %w", operation, scanErr)
		}
		result = append(result, value)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return result, nil
}

// RowError classifies the error of a one-row read: no row is ErrNotFound,
// any other error names operation.
func RowError(operation string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

// DecodeJSON decodes the JSON column of a read model row into value, a
// pointer to the Go type of the Attribute Dex wrote it from. A NULL column
// leaves value unchanged.
func DecodeJSON(column string, raw json.RawMessage, value any) error {
	if raw == nil {
		return nil
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("decode the %s column: %w", column, err)
	}
	return nil
}

// Unsigned is the uint64 Attribute that Dex wrote to a bigint column, zero
// for NULL. The SDK writes no uint64 above math.MaxInt64, so the value is
// never negative.
func Unsigned(column sql.Null[int64]) uint64 {
	return uint64(column.V) // #nosec G115 -- the SDK refuses a uint64 Attribute above math.MaxInt64.
}
