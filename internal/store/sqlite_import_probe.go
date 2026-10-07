package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SQLiteJSONImportState reports whether the database exists and whether the
// requested JSON import has a completed marker. A completed marker lets the
// caller skip reading legacy JSON files.
type SQLiteJSONImportState struct {
	DatabaseExists bool
	Completed      bool
}

// ProbeSQLiteJSONImportState inspects SQLite database contents without
// changing the database. It uses a true read-only SQLite connection, which can
// observe committed contents in an existing WAL. SQLite may update transient
// shared-memory lock state while reading an active WAL. Orphan WAL/SHM
// sidecars are rejected when the main database is absent. An absent database
// and a recognized empty, unmarked database are eligible for JSON preflight.
// Existing records require
// the requested migration's completed marker.
func ProbeSQLiteJSONImportState(databasePath, migrationID string) (state SQLiteJSONImportState, err error) {
	if strings.TrimSpace(databasePath) == "" || strings.IndexByte(databasePath, 0) >= 0 {
		return state, errors.New("SQLite database path must not be empty or contain NUL")
	}
	if err := validateImportName("migration ID", migrationID); err != nil {
		return state, fmt.Errorf("validate SQLite import probe: %w", err)
	}

	info, err := os.Stat(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		if err := rejectOrphanSQLiteSidecars(databasePath); err != nil {
			return state, err
		}
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("inspect SQLite database %q: %w", databasePath, err)
	}
	if !info.Mode().IsRegular() {
		return state, fmt.Errorf("SQLite database %q is not a regular file", databasePath)
	}
	state.DatabaseExists = true

	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return state, fmt.Errorf("resolve SQLite database path %q: %w", databasePath, err)
	}
	dsn := (&url.URL{Scheme: "file", Path: absolutePath}).String() + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return state, fmt.Errorf("open SQLite database read-only %q: %w", databasePath, err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close read-only SQLite database %q: %w", databasePath, closeErr)
		}
	}()
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return state, fmt.Errorf("connect to SQLite database read-only %q: %w", databasePath, err)
	}

	hasMigrations, err := validateSQLiteImportSchema(db)
	if err != nil {
		return state, fmt.Errorf("recognize SQLite import state in %q: %w", databasePath, err)
	}

	var recordCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM store_records`).Scan(&recordCount); err != nil {
		return state, fmt.Errorf("count SQLite store records in %q: %w", databasePath, err)
	}

	if hasMigrations {
		rows, err := db.Query(`SELECT migration_id, status FROM store_migrations ORDER BY migration_id`)
		if err != nil {
			return state, fmt.Errorf("read SQLite migration markers in %q: %w", databasePath, err)
		}
		for rows.Next() {
			var foundID, status string
			if err := rows.Scan(&foundID, &status); err != nil {
				_ = rows.Close()
				return state, fmt.Errorf("scan SQLite migration marker in %q: %w", databasePath, err)
			}
			if err := validateImportName("stored migration ID", foundID); err != nil {
				_ = rows.Close()
				return state, fmt.Errorf("invalid SQLite migration marker in %q: %w", databasePath, err)
			}
			if foundID != migrationID {
				_ = rows.Close()
				return state, fmt.Errorf("SQLite migration marker %q does not match requested migration %q", foundID, migrationID)
			}
			if status != "complete" {
				_ = rows.Close()
				if status == "in_progress" {
					return state, fmt.Errorf("SQLite migration %q is incomplete (status %q)", foundID, status)
				}
				return state, fmt.Errorf("SQLite migration %q has unknown status %q", foundID, status)
			}
			if state.Completed {
				_ = rows.Close()
				return state, fmt.Errorf("SQLite database has duplicate migration markers for %q", migrationID)
			}
			state.Completed = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("iterate SQLite migration markers in %q: %w", databasePath, err)
		}
		if err := rows.Close(); err != nil {
			return state, fmt.Errorf("close SQLite migration markers in %q: %w", databasePath, err)
		}
	}

	if !state.Completed && recordCount != 0 {
		return state, fmt.Errorf("refusing JSON preflight for SQLite database with %d unmarked store records", recordCount)
	}
	return state, nil
}

func rejectOrphanSQLiteSidecars(databasePath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecarPath := databasePath + suffix
		_, err := os.Lstat(sidecarPath)
		if err == nil {
			return fmt.Errorf("SQLite database %q is absent but orphan %s sidecar exists", databasePath, suffix)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect SQLite %s sidecar for absent database %q: %w", suffix, databasePath, err)
		}
	}
	return nil
}

type sqliteImportColumn struct {
	name       string
	primaryKey int
}

func validateSQLiteImportSchema(db *sql.DB) (bool, error) {
	rows, err := db.Query(`SELECT type, name FROM sqlite_master WHERE name NOT GLOB 'sqlite_*' ORDER BY type, name`)
	if err != nil {
		return false, fmt.Errorf("list SQLite schema objects: %w", err)
	}
	hasRecords := false
	hasMigrations := false
	for rows.Next() {
		var objectType, name string
		if err := rows.Scan(&objectType, &name); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("scan SQLite schema object: %w", err)
		}
		if objectType != "table" || (name != "store_records" && name != "store_migrations") {
			_ = rows.Close()
			return false, fmt.Errorf("unrecognized SQLite schema object %q of type %q", name, objectType)
		}
		switch name {
		case "store_records":
			hasRecords = true
		case "store_migrations":
			hasMigrations = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("iterate SQLite schema objects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close SQLite schema objects: %w", err)
	}
	if !hasRecords {
		return false, errors.New("recognized store_records table is missing")
	}

	if err := validateSQLiteImportColumns(db, "store_records", []sqliteImportColumn{
		{name: "collection", primaryKey: 1},
		{name: "id", primaryKey: 2},
		{name: "payload", primaryKey: 0},
	}); err != nil {
		return false, err
	}
	if hasMigrations {
		if err := validateSQLiteImportColumns(db, "store_migrations", []sqliteImportColumn{
			{name: "migration_id", primaryKey: 1},
			{name: "status", primaryKey: 0},
		}); err != nil {
			return false, err
		}
	}
	return hasMigrations, nil
}

func validateSQLiteImportColumns(db *sql.DB, table string, expected []sqliteImportColumn) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	index := 0
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s column: %w", table, err)
		}
		if index >= len(expected) {
			_ = rows.Close()
			return fmt.Errorf("unrecognized extra column %q in %s", name, table)
		}
		want := expected[index]
		if cid != index || name != want.name || !strings.EqualFold(dataType, "TEXT") || notNull != 1 || primaryKey != want.primaryKey || defaultValue.Valid {
			_ = rows.Close()
			return fmt.Errorf("unrecognized %s column %d: got name=%q type=%q not-null=%d primary-key=%d default=%t", table, index, name, dataType, notNull, primaryKey, defaultValue.Valid)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s columns: %w", table, err)
	}
	if index != len(expected) {
		return fmt.Errorf("unrecognized %s schema: found %d columns, want %d", table, index, len(expected))
	}
	return nil
}
