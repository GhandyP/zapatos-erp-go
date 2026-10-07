package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sqliteImportGateOptions(databasePath, backupDirectory, lockFilePath string, prepare func() (SQLiteJSONImportPreparation, error)) SQLiteJSONImportGateOptions {
	return SQLiteJSONImportGateOptions{
		Context:         context.Background(),
		DatabasePath:    databasePath,
		LockFilePath:    lockFilePath,
		MigrationID:     "legacy-json-v1",
		BackupDirectory: backupDirectory,
		Prepare:         prepare,
	}
}

func validSQLiteImportPreparation() SQLiteJSONImportPreparation {
	return SQLiteJSONImportPreparation{
		Plan: JSONImportPlan{Collections: []JSONImportCollection{{
			Name:    "widgets",
			Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one","label":"imported"}`)}},
		}}},
		BackupSources: []RawBackupSource{{Name: "widgets.json", Data: []byte("[ { \"id\": \"one\" } ]\n"), Exists: true}},
	}
}

func requireSQLiteDatabaseAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("SQLite database exists or stat failed before expected creation: %v", err)
	}
}

func TestRunSQLiteJSONImportGatePublishesBackupBeforeOpeningAndImportsOnce(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "store.sqlite")
	backupDirectory := filepath.Join(dir, "backup")
	lockFilePath := filepath.Join(dir, "migration.lock")
	backupBytes := []byte("[ { \"id\": \"one\" } ]\n")
	prepared := false
	options := sqliteImportGateOptions(databasePath, backupDirectory, lockFilePath, func() (SQLiteJSONImportPreparation, error) {
		prepared = true
		requireSQLiteDatabaseAbsent(t, databasePath)
		preparation := validSQLiteImportPreparation()
		preparation.BackupSources[0].Data = append([]byte(nil), backupBytes...)
		return preparation, nil
	})

	result, err := RunSQLiteJSONImportGate(options)
	if err != nil {
		t.Fatalf("RunSQLiteJSONImportGate returned error: %v", err)
	}
	if !prepared {
		t.Fatal("preparation callback did not run for an absent database")
	}
	if result.Database == nil {
		t.Fatal("Database = nil, want open database returned on successful import")
	}
	defer result.Database.Close()
	if result.Import.Skipped {
		t.Fatal("Import.Skipped = true, want a new import")
	}
	gotBackup, err := os.ReadFile(filepath.Join(backupDirectory, "widgets.json"))
	if err != nil {
		t.Fatalf("read published backup: %v", err)
	}
	if string(gotBackup) != string(backupBytes) {
		t.Fatalf("backup bytes = %q, want exact source bytes %q", gotBackup, backupBytes)
	}
	var recordCount int
	if err := result.Database.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?`, "widgets", "one").Scan(&recordCount); err != nil {
		t.Fatalf("count imported record: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("imported record count = %d, want 1", recordCount)
	}
	var markerStatus string
	if err := result.Database.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&markerStatus); err != nil {
		t.Fatalf("read migration marker: %v", err)
	}
	if markerStatus != "complete" {
		t.Fatalf("migration marker status = %q, want complete", markerStatus)
	}
}

func TestRunSQLiteJSONImportGateBackupFailureLeavesDatabaseAbsent(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "store.sqlite")
	backupDirectory := filepath.Join(dir, "backup")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatalf("create conflicting backup directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write partial backup: %v", err)
	}
	prepared := false
	options := sqliteImportGateOptions(databasePath, backupDirectory, filepath.Join(dir, "migration.lock"), func() (SQLiteJSONImportPreparation, error) {
		prepared = true
		return validSQLiteImportPreparation(), nil
	})

	if _, err := RunSQLiteJSONImportGate(options); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("RunSQLiteJSONImportGate error = %v, want backup publication error", err)
	}
	if !prepared {
		t.Fatal("preparation callback did not run before backup publication")
	}
	requireSQLiteDatabaseAbsent(t, databasePath)
}

func TestRunSQLiteJSONImportGateSkipsCompletedMigrationBeforePreparation(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "store.sqlite")
	db, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	if _, err := db.Exec(sqliteMigrationSchema); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "complete"); err != nil {
		t.Fatalf("insert completed marker: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", `{"id":"one"}`); err != nil {
		t.Fatalf("insert completed record: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}

	prepared := false
	options := sqliteImportGateOptions(databasePath, filepath.Join(dir, "backup"), filepath.Join(dir, "migration.lock"), func() (SQLiteJSONImportPreparation, error) {
		prepared = true
		return SQLiteJSONImportPreparation{}, nil
	})
	result, err := RunSQLiteJSONImportGate(options)
	if err != nil {
		t.Fatalf("RunSQLiteJSONImportGate returned error: %v", err)
	}
	if prepared {
		t.Fatal("preparation callback ran for a completed migration")
	}
	if !result.Import.Skipped {
		t.Fatal("Import.Skipped = false, want true for completed migration")
	}
	if result.Database == nil {
		t.Fatal("Database = nil, want open database returned for serving requests")
	}
	defer result.Database.Close()
	var markerStatus string
	if err := result.Database.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&markerStatus); err != nil {
		t.Fatalf("read completed marker through returned database: %v", err)
	}
	if markerStatus != "complete" {
		t.Fatalf("returned database marker status = %q, want complete", markerStatus)
	}
	var recordCount int
	if err := result.Database.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?`, "widgets", "one").Scan(&recordCount); err != nil {
		t.Fatalf("read existing record through returned database: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("returned database record count = %d, want 1", recordCount)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Fatalf("backup directory was created for skipped migration: %v", err)
	}
}

func TestRunSQLiteJSONImportGateFailsBeforePreparationOnProbeError(t *testing.T) {
	for _, test := range []struct {
		name      string
		prepareDB func(t *testing.T, path string)
	}{
		{
			name: "incomplete marker",
			prepareDB: func(t *testing.T, path string) {
				db, err := OpenSQLite(path)
				if err != nil {
					t.Fatalf("open SQLite database: %v", err)
				}
				if _, err := db.Exec(sqliteMigrationSchema); err != nil {
					t.Fatalf("create migration schema: %v", err)
				}
				if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "in_progress"); err != nil {
					t.Fatalf("insert incomplete marker: %v", err)
				}
				if err := db.Close(); err != nil {
					t.Fatalf("close SQLite database: %v", err)
				}
			},
		},
		{
			name: "unrecognized database",
			prepareDB: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("not SQLite"), 0o600); err != nil {
					t.Fatalf("write invalid SQLite bytes: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			databasePath := filepath.Join(dir, "store.sqlite")
			test.prepareDB(t, databasePath)
			prepared := false
			options := sqliteImportGateOptions(databasePath, filepath.Join(dir, "backup"), filepath.Join(dir, "migration.lock"), func() (SQLiteJSONImportPreparation, error) {
				prepared = true
				return validSQLiteImportPreparation(), nil
			})
			if _, err := RunSQLiteJSONImportGate(options); err == nil {
				t.Fatal("RunSQLiteJSONImportGate returned nil error for invalid import state")
			}
			if prepared {
				t.Fatal("preparation callback ran after probe failure")
			}
			if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
				t.Fatalf("backup directory exists or stat failed before preparation: %v", err)
			}
		})
	}
}

func TestRunSQLiteJSONImportGateValidatesPlanBeforeBackupAndOpen(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "store.sqlite")
	backupDirectory := filepath.Join(dir, "backup")
	options := sqliteImportGateOptions(databasePath, backupDirectory, filepath.Join(dir, "migration.lock"), func() (SQLiteJSONImportPreparation, error) {
		return SQLiteJSONImportPreparation{
			Plan:          JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`not JSON`)}}}}},
			BackupSources: []RawBackupSource{{Name: "widgets.json", Data: []byte("source"), Exists: true}},
		}, nil
	})
	if _, err := RunSQLiteJSONImportGate(options); err == nil || !strings.Contains(err.Error(), "validate") {
		t.Fatalf("RunSQLiteJSONImportGate error = %v, want plan-validation error", err)
	}
	requireSQLiteDatabaseAbsent(t, databasePath)
	if _, err := os.Stat(backupDirectory); !os.IsNotExist(err) {
		t.Fatalf("backup was published for invalid plan: %v", err)
	}
}

func TestRunSQLiteJSONImportGateConcurrentStartupSeesCompletedMarkerInLiveWAL(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "store.sqlite")
	backupDirectory := filepath.Join(dir, "backup")
	lockFilePath := filepath.Join(dir, "migration.lock")
	firstPrepared := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()
	type gateOutcome struct {
		result SQLiteJSONImportGateResult
		err    error
	}
	firstDone := make(chan gateOutcome, 1)
	firstOptions := sqliteImportGateOptions(databasePath, backupDirectory, lockFilePath, func() (SQLiteJSONImportPreparation, error) {
		if _, err := os.Lstat(databasePath); !os.IsNotExist(err) {
			close(firstPrepared)
			return SQLiteJSONImportPreparation{}, fmt.Errorf("database exists before preparation: %v", err)
		}
		close(firstPrepared)
		<-releaseFirst
		return validSQLiteImportPreparation(), nil
	})
	go func() {
		result, err := RunSQLiteJSONImportGate(firstOptions)
		firstDone <- gateOutcome{result: result, err: err}
	}()
	select {
	case <-firstPrepared:
	case <-time.After(5 * time.Second):
		t.Fatal("first gate did not reach its preparation callback")
	}

	secondPrepared := make(chan struct{}, 1)
	secondStarted := make(chan struct{})
	secondDone := make(chan gateOutcome, 1)
	secondOptions := sqliteImportGateOptions(databasePath, backupDirectory, lockFilePath, func() (SQLiteJSONImportPreparation, error) {
		secondPrepared <- struct{}{}
		return validSQLiteImportPreparation(), nil
	})
	go func() {
		close(secondStarted)
		result, err := RunSQLiteJSONImportGate(secondOptions)
		secondDone <- gateOutcome{result: result, err: err}
	}()
	<-secondStarted
	select {
	case outcome := <-secondDone:
		t.Fatalf("second gate returned while first held migration lock: result=%+v err=%v", outcome.result, outcome.err)
	case <-time.After(100 * time.Millisecond):
	}
	release()

	first := <-firstDone
	if first.err != nil {
		t.Fatalf("first gate returned error: %v", first.err)
	}
	if first.result.Database == nil {
		t.Fatal("first gate returned nil database after import")
	}
	defer first.result.Database.Close()
	walInfo, err := os.Stat(databasePath + "-wal")
	if err != nil {
		t.Fatalf("stat live WAL while first database handle remains open: %v", err)
	}
	if walInfo.Size() <= 32 {
		t.Fatalf("live WAL size = %d, want committed frames", walInfo.Size())
	}

	second := <-secondDone
	if second.err != nil {
		t.Fatalf("second gate returned error: %v", second.err)
	}
	if !second.result.Import.Skipped {
		t.Fatal("second gate Import.Skipped = false, want completed marker skip")
	}
	if second.result.Database == nil {
		t.Fatal("second gate returned nil database for completed marker")
	}
	defer second.result.Database.Close()
	var markerStatus string
	if err := second.result.Database.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&markerStatus); err != nil {
		t.Fatalf("second gate database cannot read completed marker: %v", err)
	}
	if markerStatus != "complete" {
		t.Fatalf("second gate marker status = %q, want complete", markerStatus)
	}
	select {
	case <-secondPrepared:
		t.Fatal("second gate ran legacy preparation despite completed marker in WAL")
	default:
	}
}
