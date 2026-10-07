package store

import (
	"bytes"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func prepareSQLiteImportProbeDB(t *testing.T, path string, prepare func(*sql.DB)) {
	t.Helper()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	if prepare != nil {
		prepare(db)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}
}

func TestProbeSQLiteJSONImportStateAbsentDatabaseRemainsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")

	state, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1")
	if err != nil {
		t.Fatalf("ProbeSQLiteJSONImportState returned error: %v", err)
	}
	if state.DatabaseExists || state.Completed {
		t.Fatalf("state = %+v, want absent and incomplete", state)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("database path stat error = %v, want not-exist", err)
	}
}

func TestProbeSQLiteJSONImportStateDoesNotChangeExistingDatabaseContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(sqliteMigrationSchema); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "complete"); err != nil {
		t.Fatalf("insert completed marker: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", `{"id":"one"}`); err != nil {
		t.Fatalf("insert completed import record: %v", err)
	}
	before := snapshotSQLiteProbeDir(t, filepath.Dir(path))

	state, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1")
	if err != nil {
		t.Fatalf("ProbeSQLiteJSONImportState returned error: %v", err)
	}
	if !state.DatabaseExists || !state.Completed {
		t.Fatalf("state = %+v, want existing and completed", state)
	}
	after := snapshotSQLiteProbeDir(t, filepath.Dir(path))
	if !reflect.DeepEqual(after, before) {
		for name, previous := range before {
			current, exists := after[name]
			if !exists {
				t.Errorf("probe removed database file %q", name)
				continue
			}
			if name == filepath.Base(path)+"-shm" {
				if previous.mode != current.mode {
					t.Errorf("probe changed shared-memory sidecar permissions %q: %04o -> %04o", name, previous.mode.Perm(), current.mode.Perm())
				}
				continue
			}
			if previous.mode != current.mode || previous.modTime != current.modTime || !bytes.Equal(previous.data, current.data) {
				t.Errorf("probe changed database file %q (size %d -> %d, mode %04o -> %04o, mtime %d -> %d, bytes equal: %t)", name, len(previous.data), len(current.data), previous.mode.Perm(), current.mode.Perm(), previous.modTime, current.modTime, bytes.Equal(previous.data, current.data))
			}
		}
		for name := range after {
			if _, exists := before[name]; !exists {
				t.Errorf("probe created database file %q", name)
			}
		}
	}
}

func TestProbeSQLiteJSONImportStateCompletedMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	prepareSQLiteImportProbeDB(t, path, func(db *sql.DB) {
		if _, err := db.Exec(sqliteMigrationSchema); err != nil {
			t.Fatalf("create migration schema: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "complete"); err != nil {
			t.Fatalf("insert completed marker: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", `{"id":"one"}`); err != nil {
			t.Fatalf("insert record: %v", err)
		}
	})

	state, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1")
	if err != nil {
		t.Fatalf("ProbeSQLiteJSONImportState returned error: %v", err)
	}
	if !state.DatabaseExists || !state.Completed {
		t.Fatalf("state = %+v, want existing and completed", state)
	}
}

func TestProbeSQLiteJSONImportStateRejectsIncompleteUnknownAndMismatchedMarkers(t *testing.T) {
	tests := []struct {
		name        string
		markerID    string
		status      string
		wantMessage string
	}{
		{name: "incomplete", markerID: "legacy-json-v1", status: "in_progress", wantMessage: "incomplete"},
		{name: "unknown status", markerID: "legacy-json-v1", status: "future", wantMessage: "unknown status"},
		{name: "mismatched ID", markerID: "another-import", status: "complete", wantMessage: "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store.sqlite")
			prepareSQLiteImportProbeDB(t, path, func(db *sql.DB) {
				if _, err := db.Exec(sqliteMigrationSchema); err != nil {
					t.Fatalf("create migration schema: %v", err)
				}
				if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, test.markerID, test.status); err != nil {
					t.Fatalf("insert marker: %v", err)
				}
			})

			if _, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1"); err == nil || !containsText(err.Error(), test.wantMessage) {
				t.Fatalf("ProbeSQLiteJSONImportState error = %v, want message containing %q", err, test.wantMessage)
			}
		})
	}
}

func TestProbeSQLiteJSONImportStateRejectsUnmarkedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	prepareSQLiteImportProbeDB(t, path, func(db *sql.DB) {
		if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", `{"id":"one"}`); err != nil {
			t.Fatalf("insert unmarked record: %v", err)
		}
	})

	if _, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1"); err == nil || !containsText(err.Error(), "unmarked store records") {
		t.Fatalf("ProbeSQLiteJSONImportState error = %v, want unmarked-record error", err)
	}
}

func TestProbeSQLiteJSONImportStateEmptyDatabaseIsEligible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	prepareSQLiteImportProbeDB(t, path, nil)

	state, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1")
	if err != nil {
		t.Fatalf("ProbeSQLiteJSONImportState returned error: %v", err)
	}
	if !state.DatabaseExists || state.Completed {
		t.Fatalf("state = %+v, want existing and unmarked", state)
	}
}

func TestProbeSQLiteJSONImportStateRejectsUnrecognizedDatabase(t *testing.T) {
	t.Run("invalid SQLite bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "store.sqlite")
		if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
			t.Fatalf("write invalid database: %v", err)
		}
		if _, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1"); err == nil {
			t.Fatal("ProbeSQLiteJSONImportState returned nil error for invalid database")
		}
	})

	t.Run("unknown schema", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "store.sqlite")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open SQLite database: %v", err)
		}
		if _, err := db.Exec(`CREATE TABLE unrelated (value TEXT)`); err != nil {
			t.Fatalf("create unknown schema: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close SQLite database: %v", err)
		}
		if _, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1"); err == nil || !containsText(err.Error(), "unrecognized SQLite schema object") {
			t.Fatalf("ProbeSQLiteJSONImportState error = %v, want unrecognized-schema error", err)
		}
	})
}

func TestProbeSQLiteJSONImportStateReadsCompletedMarkerFromActiveWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(sqliteMigrationSchema); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "complete"); err != nil {
		t.Fatalf("insert completed marker: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", `{"id":"one"}`); err != nil {
		t.Fatalf("insert record: %v", err)
	}
	walInfo, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatalf("stat active WAL: %v", err)
	}
	if walInfo.Size() <= 32 {
		t.Fatalf("active WAL size = %d, want committed frames beyond its header", walInfo.Size())
	}

	state, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1")
	if err != nil {
		t.Fatalf("ProbeSQLiteJSONImportState returned error with active WAL: %v", err)
	}
	if !state.DatabaseExists || !state.Completed {
		t.Fatalf("state = %+v, want existing and completed from WAL", state)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve SQLite database path: %v", err)
	}
	readOnlyURI := (&url.URL{Scheme: "file", Path: absolutePath}).String() + "?mode=ro"
	reader, err := sql.Open("sqlite", readOnlyURI)
	if err != nil {
		t.Fatalf("open direct read-only SQLite connection: %v", err)
	}
	defer reader.Close()
	var markerStatus string
	if err := reader.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&markerStatus); err != nil {
		t.Fatalf("read completed marker from active WAL: %v", err)
	}
	var recordCount int
	if err := reader.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?`, "widgets", "one").Scan(&recordCount); err != nil {
		t.Fatalf("read committed record from active WAL: %v", err)
	}
	if markerStatus != "complete" || recordCount != 1 {
		t.Fatalf("read-only WAL state = marker %q, records %d; want complete and one record", markerStatus, recordCount)
	}
}

func TestProbeSQLiteJSONImportStateRejectsOrphanSidecarsWithoutMutation(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.sqlite")
			sidecarPath := path + suffix
			if err := os.WriteFile(sidecarPath, []byte("orphan sidecar"), 0o600); err != nil {
				t.Fatalf("write orphan sidecar: %v", err)
			}
			before := snapshotSQLiteProbeDir(t, filepath.Dir(path))

			if _, err := ProbeSQLiteJSONImportState(path, "legacy-json-v1"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "orphan") {
				t.Fatalf("ProbeSQLiteJSONImportState error = %v, want orphan-sidecar error", err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("probe created main database or stat failed: %v", err)
			}
			if after := snapshotSQLiteProbeDir(t, filepath.Dir(path)); !reflect.DeepEqual(after, before) {
				t.Fatal("probe changed orphan sidecar or created a database")
			}
		})
	}
}

func TestProbeSQLiteJSONImportStateValidatesArguments(t *testing.T) {
	for _, test := range []struct {
		name        string
		path        string
		migrationID string
	}{
		{name: "empty path", path: "", migrationID: "legacy-json-v1"},
		{name: "empty migration ID", path: filepath.Join(t.TempDir(), "store.sqlite"), migrationID: "  "},
		{name: "unsafe migration ID", path: filepath.Join(t.TempDir(), "store.sqlite"), migrationID: "../import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ProbeSQLiteJSONImportState(test.path, test.migrationID); err == nil {
				t.Fatal("ProbeSQLiteJSONImportState returned nil error for invalid argument")
			}
		})
	}
}

type sqliteProbeFile struct {
	mode    os.FileMode
	data    []byte
	modTime int64
}

func snapshotSQLiteProbeDir(t *testing.T, dir string) map[string]sqliteProbeFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read database directory: %v", err)
	}
	snapshot := make(map[string]sqliteProbeFile, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("inspect %q: %v", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		snapshot[entry.Name()] = sqliteProbeFile{mode: info.Mode(), data: data, modTime: info.ModTime().UnixNano()}
	}
	return snapshot
}

func containsText(value, fragment string) bool {
	return strings.Contains(value, fragment)
}
