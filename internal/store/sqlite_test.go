package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOpenSQLiteCreatesSchemaAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	if err := EnsureSQLiteSchema(db); err != nil {
		t.Fatalf("first EnsureSQLiteSchema returned error: %v", err)
	}
	if err := EnsureSQLiteSchema(db); err != nil {
		t.Fatalf("second EnsureSQLiteSchema returned error: %v", err)
	}

	repo := NewSQLiteRepository(db, "widgets", func(v testRecord) string { return v.ID })
	if _, err := repo.Save(testRecord{ID: "widget-1", Name: "Widget"}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen returned error: %v", err)
	}
	defer reopened.Close()

	items, err := NewSQLiteRepository(reopened, "widgets", func(v testRecord) string { return v.ID }).List()
	if err != nil {
		t.Fatalf("List after reopen returned error: %v", err)
	}
	want := []testRecord{{ID: "widget-1", Name: "Widget"}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("List after reopen = %#v, want %#v", items, want)
	}
}

func TestOpenSQLitePreservesBusyTimeoutAfterConnectionReplacement(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	// Closing idle connections forces the next query to use a newly opened
	// driver connection instead of the one configured during OpenSQLite.
	db.SetMaxIdleConns(0)
	if got := db.Stats().OpenConnections; got != 0 {
		t.Fatalf("OpenConnections after closing idle connections = %d, want 0", got)
	}

	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout on replacement connection: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("replacement connection busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestSQLiteWriterWaitsForHeldWriteTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	first, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open first database handle: %v", err)
	}
	defer first.Close()

	second, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open second database handle: %v", err)
	}
	defer second.Close()

	tx, err := first.Begin()
	if err != nil {
		t.Fatalf("begin holding transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", "locks", "first", "{}"); err != nil {
		t.Fatalf("write in holding transaction: %v", err)
	}

	writeStarted := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		close(writeStarted)
		_, err := second.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", "locks", "second", "{}")
		writeDone <- err
	}()
	<-writeStarted

	select {
	case err := <-writeDone:
		t.Fatalf("second writer completed while the first transaction held its lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit holding transaction: %v", err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("second writer after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer did not finish after the held transaction committed")
	}
}

func TestSQLiteRepositoryCRUDAndUpsert(t *testing.T) {
	db, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := NewSQLiteRepository(db, "widgets", func(v testRecord) string { return v.ID })
	items, err := repo.List()
	if err != nil {
		t.Fatalf("initial List returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("initial List = %#v, want empty", items)
	}

	missing, found, err := repo.FindByID("missing")
	if err != nil {
		t.Fatalf("FindByID for missing ID returned error: %v", err)
	}
	if found || missing != (testRecord{}) {
		t.Fatalf("FindByID for missing ID = (%#v, %v), want zero value and false", missing, found)
	}

	created := testRecord{ID: "widget-1", Name: "First name"}
	saved, err := repo.Save(created)
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if saved != created {
		t.Fatalf("Save = %#v, want %#v", saved, created)
	}

	updated := testRecord{ID: "widget-1", Name: "Updated name"}
	if saved, err = repo.Save(updated); err != nil {
		t.Fatalf("upsert Save returned error: %v", err)
	}
	if saved != updated {
		t.Fatalf("upsert Save = %#v, want %#v", saved, updated)
	}

	got, found, err := repo.FindByID("widget-1")
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if !found || got != updated {
		t.Fatalf("FindByID = (%#v, %v), want (%#v, true)", got, found, updated)
	}
	items, err = repo.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if want := []testRecord{updated}; !reflect.DeepEqual(items, want) {
		t.Fatalf("List = %#v, want %#v", items, want)
	}

	deleted, err := repo.Delete("widget-1")
	if err != nil {
		t.Fatalf("Delete existing ID returned error: %v", err)
	}
	if !deleted {
		t.Fatal("Delete existing ID = false, want true")
	}
	deleted, err = repo.Delete("widget-1")
	if err != nil {
		t.Fatalf("Delete missing ID returned error: %v", err)
	}
	if deleted {
		t.Fatal("Delete missing ID = true, want false")
	}
	items, err = repo.List()
	if err != nil {
		t.Fatalf("final List returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("final List = %#v, want empty", items)
	}
}

func TestSQLiteRepositoryAcceptsSQLTransactionExecutor(t *testing.T) {
	db, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	txRepo := NewSQLiteRepository(tx, "widgets", func(v testRecord) string { return v.ID })
	if _, err := txRepo.Save(testRecord{ID: "rolled-back", Name: "Temporary"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("transaction-backed Save returned error: %v", err)
	}
	if _, found, err := txRepo.FindByID("rolled-back"); err != nil || !found {
		_ = tx.Rollback()
		t.Fatalf("transaction-backed FindByID = found %v, error %v; want found", found, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback returned error: %v", err)
	}

	dbRepo := NewSQLiteRepository(db, "widgets", func(v testRecord) string { return v.ID })
	if _, found, err := dbRepo.FindByID("rolled-back"); err != nil {
		t.Fatalf("database-backed FindByID returned error: %v", err)
	} else if found {
		t.Fatal("database-backed FindByID found record after transaction rollback")
	}
}
