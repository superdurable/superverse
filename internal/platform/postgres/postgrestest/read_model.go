// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package postgrestest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// MigratedScratchDatabaseForTest returns a scratch database, as
// ScratchDatabaseForTest connects to it, with every embedded migration
// applied. The lane's own database is never migrated by a test.
func MigratedScratchDatabaseForTest(t testing.TB) *sql.DB {
	t.Helper()
	db := ScratchDatabaseForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if _, err := postgres.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate the scratch database: %v", err)
	}
	return db
}

// ReadModelTableForTest is a read model table as its Flows define it. Key is
// its primary key, the columns of the store's row key template (and the
// instance key of a child table). Values are the columns that Dex writes
// synced Attributes or map instance fields to, with the Go type of each
// value; a value named like a key column is skipped by Dex. Skipped are
// values that deliberately have no column.
type ReadModelTableForTest struct {
	Name    string
	Key     []string
	Values  map[string]reflect.Type
	Skipped []string
}

// RequireReadModelTableForTest fails the test unless the table of db has
// exactly the key as its primary key and exactly the other columns, each of
// the type that Dex writes its Go value type to, because Dex silently skips a
// value without a column or with a column of another type.
func RequireReadModelTableForTest(t testing.TB, db *sql.DB, table ReadModelTableForTest) {
	t.Helper()
	columns := map[string]string{}
	for _, row := range queryRows(t, db, `SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, table.Name) {
		columns[row[0]] = row[1]
	}
	var key []string
	for _, row := range queryRows(t, db, `SELECT k.column_name, '' FROM information_schema.table_constraints c
		JOIN information_schema.key_column_usage k ON k.constraint_name = c.constraint_name AND k.table_schema = c.table_schema
		WHERE c.table_schema = 'public' AND c.table_name = $1 AND c.constraint_type = 'PRIMARY KEY' ORDER BY k.ordinal_position`, table.Name) {
		key = append(key, row[0])
	}
	if !slices.Equal(key, table.Key) {
		t.Fatalf("%s has the primary key %v, its row key yields %v", table.Name, key, table.Key)
	}
	for _, name := range table.Key {
		if columns[name] != "text" {
			t.Fatalf("%s.%s is a key column of type %q, not text", table.Name, name, columns[name])
		}
	}
	want := map[string]string{}
	for name, value := range table.Values {
		if !slices.Contains(table.Key, name) && !slices.Contains(table.Skipped, name) {
			want[name] = dexColumnType(value)
		}
	}
	for _, name := range table.Skipped {
		if _, found := table.Values[name]; !found {
			t.Fatalf("%s skips %s, which no synced value writes", table.Name, name)
		}
	}
	for _, name := range table.Key {
		want[name] = columns[name]
	}
	if !maps.Equal(columns, want) {
		t.Fatalf("%s has the columns %v; its synced values need %v", table.Name, columns, want)
	}
}

// SyncedColumnsForTest returns, of the Attributes of a Flow's
// PersistenceSchema, the column and the Go value type of each synced
// Attribute, and the instance value type of each synced AttributeMap by name.
// The SDK keeps the sync option private, so it is read by reflection.
func SyncedColumnsForTest(attributes []dex.AttributeDef) (columns, attributeMaps map[string]reflect.Type) {
	columns, attributeMaps = map[string]reflect.Type{}, map[string]reflect.Type{}
	for _, definition := range attributes {
		value := reflect.ValueOf(definition)
		if !value.FieldByName("syncToAttributeStore").Bool() {
			continue
		}
		name := value.FieldByName("name").String()
		get, _ := value.Type().MethodByName("Get")
		if _, isMap := value.Type().MethodByName("MapSize"); isMap {
			attributeMaps[name] = get.Type.Out(0)
			continue
		}
		columns[snakeCase(name)] = get.Type.Out(0)
	}
	return columns, attributeMaps
}

// FieldColumnsForTest returns the column and the Go type of each top-level
// JSON field of an AttributeMap instance value, which Dex writes to the
// child table's column of the same snake_case name.
func FieldColumnsForTest(value reflect.Type) map[string]reflect.Type {
	columns := map[string]reflect.Type{}
	for index := range value.NumField() {
		field := value.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		columns[snakeCase(name)] = field.Type
	}
	return columns
}

// dexColumnType is the PostgreSQL type that the Attribute Store writes a
// value of the Go type to: the SDK sends strings, Booleans and integers as
// such, a time as an RFC 3339 string for a timestamp, and the rest as JSON.
func dexColumnType(value reflect.Type) string {
	if value == reflect.TypeFor[time.Time]() {
		return "timestamp with time zone"
	}
	switch value.Kind() {
	case reflect.String:
		return "text"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "bigint"
	default:
		return "jsonb"
	}
}

// snakeCase is the Attribute Store's snake_case column naming: an underscore
// before each upper-case letter that follows a lower-case letter or digit, or
// that ends an acronym before a lower-case letter, and every letter lower-cased.
func snakeCase(name string) string {
	runes := []rune(name)
	var result strings.Builder
	for index, character := range runes {
		if index > 0 && unicode.IsUpper(character) {
			previous := runes[index-1]
			endsAcronym := unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || endsAcronym {
				result.WriteByte('_')
			}
		}
		result.WriteRune(unicode.ToLower(character))
	}
	return result.String()
}

// queryRows returns the two text columns of each row of a catalog query.
func queryRows(t testing.TB, db *sql.DB, query string, args ...any) [][2]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query the catalog: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var result [][2]string
	for rows.Next() {
		var row [2]string
		if err = rows.Scan(&row[0], &row[1]); err != nil {
			t.Fatalf("read the catalog: %v", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read the catalog: %v", err)
	}
	return result
}

// AwaitRowForTest waits until the row of table that where selects holds
// exactly the columns that want returns, and fails the test with the last
// difference when ctx ends first. Dex writes a row some time after the Flow
// changes, so want reads the Flow's state again each round and returns the
// column values it implies. A time is compared to the microsecond, a number
// as a JSON number, and any other value by its JSON encoding, which is how
// Dex writes a struct to a jsonb column.
func AwaitRowForTest(ctx context.Context, t testing.TB, db *sql.DB, table string, want func() map[string]any, where string, args ...any) {
	t.Helper()
	difference := "no row"
	for {
		expected := want()
		var raw []byte
		err := db.QueryRowContext(ctx, "SELECT row_to_json(r) FROM "+table+" r WHERE "+where, args...).Scan(&raw)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			difference = "no row"
		case err != nil:
			difference = "read the row: " + err.Error()
		default:
			var row map[string]any
			if err = json.Unmarshal(raw, &row); err != nil {
				t.Fatalf("decode the %s row: %v", table, err)
			}
			if difference = rowDifference(row, expected); difference == "" {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the %s row (%s %v) does not match its Flow: %s", table, where, args, difference)
		case <-time.After(2 * time.Second):
		}
	}
}

// rowDifference names the wanted columns whose value in row differs, and any
// wanted column the row lacks.
func rowDifference(row, want map[string]any) string {
	var differences []string
	for _, column := range slices.Sorted(maps.Keys(want)) {
		got, present := row[column]
		if !present {
			differences = append(differences, column+": no such column")
		} else if !columnEquals(got, want[column]) {
			differences = append(differences, fmt.Sprintf("%s: row %.120v, Flow %.120v", column, got, want[column]))
		}
	}
	return strings.Join(differences, "; ")
}

// columnEquals compares a row_to_json value with the Go value of the Flow.
func columnEquals(got, want any) bool {
	if instant, isTime := want.(time.Time); isTime {
		text, isText := got.(string)
		parsed, err := time.Parse(time.RFC3339Nano, text)
		return isText && err == nil && parsed.Truncate(time.Microsecond).Equal(instant.Truncate(time.Microsecond))
	}
	value := reflect.ValueOf(want)
	switch value.Kind() {
	case reflect.String:
		return got == value.String()
	case reflect.Bool:
		return got == value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return got == float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return got == float64(value.Uint())
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		return false
	}
	var decoded any
	return json.Unmarshal(encoded, &decoded) == nil && reflect.DeepEqual(got, decoded)
}

// StringsForTest returns the first column of each row of a query, in order.
func StringsForTest(ctx context.Context, t testing.TB, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var result []string
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			t.Fatalf("read %q: %v", query, err)
		}
		result = append(result, value)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read %q: %v", query, err)
	}
	return result
}

// AwaitSameIDsForTest waits until the IDs that fromFlows lists equal the IDs
// that fromRows lists, and fails the test with both differences when ctx ends
// first: a Flow without a row is a write that Dex never made, a row without a
// Flow a row that nothing owns. Dex writes a row some time after the Flow
// changes, so both sides are listed again each round.
func AwaitSameIDsForTest(ctx context.Context, t testing.TB, what string, fromFlows, fromRows func() []string) {
	t.Helper()
	for {
		flows, rows := fromFlows(), fromRows()
		if slices.Equal(flows, rows) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the %s Flows and rows differ: Flows without a row %v, rows without a Flow %v",
				what, difference(flows, rows), difference(rows, flows))
		case <-time.After(2 * time.Second):
		}
	}
}

// difference lists what is in the first list and not in the second.
func difference(first, second []string) []string {
	var result []string
	for _, id := range first {
		if !slices.Contains(second, id) {
			result = append(result, id)
		}
	}
	return result
}

// LockTableForTest holds a SHARE lock on a read model table of the lane's
// database, as the owner role, until release or the end of the test: Dex's
// writes, which need ROW EXCLUSIVE, wait for it, while the server's reads go
// on. Platform Dex retries a sync batch that waits past an attempt's timeout,
// so a lock of seconds delays the table's rows and loses none.
func LockTableForTest(ctx context.Context, t testing.TB, table string) (release func()) {
	t.Helper()
	tx, err := open(t, laneDatabase, postgres.OwnerRole).BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the lock of %s: %v", table, err)
	}
	if _, err = tx.ExecContext(ctx, "LOCK TABLE "+table+" IN SHARE MODE"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("lock %s: %v", table, err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				t.Errorf("release the lock of %s: %v", table, rollbackErr)
			}
		})
	}
	t.Cleanup(release)
	return release
}
