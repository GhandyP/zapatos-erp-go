package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const sqliteMigrationSchema = `
CREATE TABLE IF NOT EXISTS store_migrations (
	migration_id TEXT NOT NULL PRIMARY KEY,
	status TEXT NOT NULL
)`

// JSONImportPlan contains app-agnostic collections and their raw JSON records.
// Collection names and record IDs are selected by the caller at the wiring
// boundary.
type JSONImportPlan struct {
	Collections []JSONImportCollection
}

// JSONImportCollection is one named collection in a JSON import plan. Empty
// collections are valid and are verified during import.
type JSONImportCollection struct {
	Name    string
	Records []JSONImportRecord
}

// JSONImportRecord is one identified JSON payload to insert without upsert.
type JSONImportRecord struct {
	ID      string
	Payload json.RawMessage
}

// JSONImportResult describes whether the requested import was skipped because
// its migration marker was already complete.
type JSONImportResult struct {
	Skipped bool
}

// ImportJSONOnce imports a prepared JSON plan exactly once. The preparation
// callback is invoked only after the migration marker and empty-database
// prechecks pass, so completed migrations do not need to read legacy snapshots.
// The marker reservation, record inserts, count checks, and completion update
// are committed atomically.
func ImportJSONOnce(db *sql.DB, migrationID string, prepare func() (JSONImportPlan, error)) (JSONImportResult, error) {
	if db == nil {
		return JSONImportResult{}, errors.New("SQLite database must not be nil")
	}
	if err := validateImportName("migration ID", migrationID); err != nil {
		return JSONImportResult{}, err
	}
	if err := EnsureSQLiteSchema(db); err != nil {
		return JSONImportResult{}, err
	}
	if _, err := db.Exec(sqliteMigrationSchema); err != nil {
		return JSONImportResult{}, fmt.Errorf("ensure SQLite migration schema: %w", err)
	}

	status, found, err := lookupJSONImportMarker(db, migrationID)
	if err != nil {
		return JSONImportResult{}, err
	}
	if found {
		if status == "complete" {
			return JSONImportResult{Skipped: true}, nil
		}
		return JSONImportResult{}, fmt.Errorf("migration %q has non-complete marker status %q", migrationID, status)
	}

	var existingRecords int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM store_records`).Scan(&existingRecords); err != nil {
		return JSONImportResult{}, fmt.Errorf("check unmarked store records: %w", err)
	}
	if existingRecords != 0 {
		status, found, err := lookupJSONImportMarker(db, migrationID)
		if err != nil {
			return JSONImportResult{}, err
		}
		if found && status == "complete" {
			return JSONImportResult{Skipped: true}, nil
		}
		return JSONImportResult{}, fmt.Errorf("refusing JSON import for unmarked database with %d existing records", existingRecords)
	}
	if prepare == nil {
		return JSONImportResult{}, errors.New("JSON import preparation callback must not be nil")
	}

	plan, err := prepare()
	if err != nil {
		return JSONImportResult{}, fmt.Errorf("prepare JSON import: %w", err)
	}
	totalRecords, err := validateJSONImportPlan(plan)
	if err != nil {
		return JSONImportResult{}, fmt.Errorf("validate JSON import plan: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return JSONImportResult{}, fmt.Errorf("begin JSON import transaction: %w", err)
	}
	defer tx.Rollback()

	// This is deliberately the transaction's first write. SQLite's write lock
	// serializes competing importers before either one inserts records.
	if _, err := tx.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, 'in_progress')`, migrationID); err != nil {
		rollbackErr := tx.Rollback()
		status, found, lookupErr := lookupJSONImportMarker(db, migrationID)
		if lookupErr == nil && found && status == "complete" {
			return JSONImportResult{Skipped: true}, nil
		}
		reserveErr := fmt.Errorf("reserve JSON import migration marker: %w", err)
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			reserveErr = fmt.Errorf("%w (rollback reservation: %v)", reserveErr, rollbackErr)
		}
		if lookupErr != nil {
			reserveErr = fmt.Errorf("%w (recheck migration marker: %v)", reserveErr, lookupErr)
		}
		return JSONImportResult{}, reserveErr
	}

	for _, collection := range plan.Collections {
		for _, record := range collection.Records {
			if _, err := tx.Exec(
				`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`,
				collection.Name,
				record.ID,
				string(record.Payload),
			); err != nil {
				return JSONImportResult{}, fmt.Errorf("insert JSON import record %q in collection %q: %w", record.ID, collection.Name, err)
			}
		}
	}

	for _, collection := range plan.Collections {
		var got int64
		if err := tx.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ?`, collection.Name).Scan(&got); err != nil {
			return JSONImportResult{}, fmt.Errorf("count imported collection %q: %w", collection.Name, err)
		}
		want := int64(len(collection.Records))
		if got != want {
			return JSONImportResult{}, fmt.Errorf("imported collection %q has %d records, want %d", collection.Name, got, want)
		}
	}

	var globalRecords int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM store_records`).Scan(&globalRecords); err != nil {
		return JSONImportResult{}, fmt.Errorf("count all imported records: %w", err)
	}
	if globalRecords != totalRecords {
		return JSONImportResult{}, fmt.Errorf("store has %d records after import, want exactly %d", globalRecords, totalRecords)
	}

	result, err := tx.Exec(`UPDATE store_migrations SET status = 'complete' WHERE migration_id = ? AND status = 'in_progress'`, migrationID)
	if err != nil {
		return JSONImportResult{}, fmt.Errorf("complete JSON import migration marker: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return JSONImportResult{}, fmt.Errorf("count completed migration markers: %w", err)
	}
	if updated != 1 {
		return JSONImportResult{}, fmt.Errorf("completed %d migration markers for %q, want exactly one", updated, migrationID)
	}
	if err := tx.Commit(); err != nil {
		return JSONImportResult{}, fmt.Errorf("commit JSON import: %w", err)
	}
	return JSONImportResult{}, nil
}

func lookupJSONImportMarker(db *sql.DB, migrationID string) (string, bool, error) {
	var status string
	err := db.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, migrationID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read migration marker %q: %w", migrationID, err)
	}
	return status, true, nil
}

func validateJSONImportPlan(plan JSONImportPlan) (int64, error) {
	collections := make(map[string]struct{}, len(plan.Collections))
	keys := make(map[struct{ collection, id string }]struct{})
	var totalRecords int64
	for collectionIndex, collection := range plan.Collections {
		if err := validateImportName("collection name", collection.Name); err != nil {
			return 0, fmt.Errorf("collection %d: %w", collectionIndex, err)
		}
		if _, exists := collections[collection.Name]; exists {
			return 0, fmt.Errorf("duplicate collection name %q", collection.Name)
		}
		collections[collection.Name] = struct{}{}

		for recordIndex, record := range collection.Records {
			if strings.TrimSpace(record.ID) == "" || strings.IndexByte(record.ID, 0) >= 0 {
				return 0, fmt.Errorf("record %d in collection %q has an invalid ID", recordIndex, collection.Name)
			}
			key := struct{ collection, id string }{collection: collection.Name, id: record.ID}
			if _, exists := keys[key]; exists {
				return 0, fmt.Errorf("duplicate record key %q in collection %q", record.ID, collection.Name)
			}
			keys[key] = struct{}{}

			if !utf8.Valid(record.Payload) || !json.Valid(record.Payload) {
				return 0, fmt.Errorf("record %q in collection %q has invalid raw JSON payload", record.ID, collection.Name)
			}
			if err := rejectDuplicateJSONKeys(record.Payload); err != nil {
				return 0, fmt.Errorf("record %q in collection %q has invalid raw JSON payload: %w", record.ID, collection.Name, err)
			}
			totalRecords++
		}
	}
	return totalRecords, nil
}

func validateImportName(label, name string) error {
	if err := validateSafeName(label, name); err != nil {
		return err
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("%s %q must not have leading or trailing whitespace", label, name)
	}
	return nil
}
