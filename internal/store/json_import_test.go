package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func openJSONImportTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close SQLite database: %v", err)
		}
	})
	return db
}

func TestImportJSONOnceSkipsCompletedMigrationBeforePreparing(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`CREATE TABLE store_migrations (migration_id TEXT PRIMARY KEY, status TEXT NOT NULL)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", "complete"); err != nil {
		t.Fatalf("insert completed marker: %v", err)
	}

	prepared := false
	result, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) {
		prepared = true
		return JSONImportPlan{}, nil
	})
	if err != nil {
		t.Fatalf("ImportJSONOnce returned error: %v", err)
	}
	if !result.Skipped {
		t.Fatal("Skipped = false, want true for completed migration")
	}
	if prepared {
		t.Fatal("preparation callback ran for completed migration")
	}
}

func TestImportJSONOnceRejectsNonEmptyUnmarkedDatabaseBeforePreparing(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "existing", "one", `{"id":"one"}`); err != nil {
		t.Fatalf("insert unmarked record: %v", err)
	}

	prepared := false
	_, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) {
		prepared = true
		return JSONImportPlan{}, nil
	})
	if err == nil {
		t.Fatal("ImportJSONOnce returned nil error for non-empty unmarked database")
	}
	if prepared {
		t.Fatal("preparation callback ran for non-empty unmarked database")
	}
}

func TestImportJSONOnceFailsClosedOnIncompleteOrUnknownMarker(t *testing.T) {
	for _, status := range []string{"in_progress", "mystery"} {
		t.Run(status, func(t *testing.T) {
			db := openJSONImportTestDB(t)
			if _, err := db.Exec(`CREATE TABLE store_migrations (migration_id TEXT PRIMARY KEY, status TEXT NOT NULL)`); err != nil {
				t.Fatalf("create migration table: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO store_migrations (migration_id, status) VALUES (?, ?)`, "legacy-json-v1", status); err != nil {
				t.Fatalf("insert migration marker: %v", err)
			}

			prepared := false
			if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) {
				prepared = true
				return JSONImportPlan{}, nil
			}); err == nil {
				t.Fatal("ImportJSONOnce returned nil error for incomplete or unknown marker")
			}
			if prepared {
				t.Fatal("preparation callback ran for incomplete or unknown marker")
			}
		})
	}
}

func TestImportJSONOnceImportsRowsAndVerifiesEmptyCollection(t *testing.T) {
	db := openJSONImportTestDB(t)
	plan := JSONImportPlan{Collections: []JSONImportCollection{
		{Name: "widgets", Records: []JSONImportRecord{
			{ID: "widget-1", Payload: json.RawMessage(`{"id":"widget-1","name":"Widget"}`)},
			{ID: "widget-2", Payload: json.RawMessage(`{"id":"widget-2","name":"Second"}`)},
		}},
		{Name: "empty-collection"},
	}}

	result, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return plan, nil })
	if err != nil {
		t.Fatalf("ImportJSONOnce returned error: %v", err)
	}
	if result.Skipped {
		t.Fatal("Skipped = true, want false for a new migration")
	}

	for _, want := range []struct {
		collection string
		count      int
	}{{"widgets", 2}, {"empty-collection", 0}} {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ?`, want.collection).Scan(&got); err != nil {
			t.Fatalf("count collection %q: %v", want.collection, err)
		}
		if got != want.count {
			t.Errorf("collection %q count = %d, want %d", want.collection, got, want.count)
		}
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&status); err != nil {
		t.Fatalf("read migration status: %v", err)
	}
	if status != "complete" {
		t.Fatalf("migration status = %q, want complete", status)
	}
}

func TestImportJSONOnceRollsBackReservationAndRowsWhenInsertFails(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_import BEFORE INSERT ON store_records BEGIN SELECT RAISE(ABORT, 'injected import failure'); END`); err != nil {
		t.Fatalf("create failing trigger: %v", err)
	}
	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}}}}}

	if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return plan, nil }); err == nil {
		t.Fatal("ImportJSONOnce returned nil error after injected insert failure")
	}
	assertJSONImportStateEmpty(t, db)
}

func TestImportJSONOnceRollsBackWhenPlannedCollectionCountDiffers(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER erase_import AFTER INSERT ON store_records WHEN NEW.collection = 'widgets' BEGIN DELETE FROM store_records WHERE collection = NEW.collection AND id = NEW.id; END`); err != nil {
		t.Fatalf("create count-mismatch trigger: %v", err)
	}
	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}}}}}

	if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return plan, nil }); err == nil {
		t.Fatal("ImportJSONOnce returned nil error after planned collection count mismatch")
	}
	assertJSONImportStateEmpty(t, db)
}

func TestImportJSONOnceVerifiesEmptyCollectionCount(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER populate_empty_import AFTER INSERT ON store_records WHEN NEW.collection = 'widgets' BEGIN INSERT INTO store_records (collection, id, payload) VALUES ('empty-collection', 'unexpected', '{}'); END`); err != nil {
		t.Fatalf("create empty-collection trigger: %v", err)
	}
	plan := JSONImportPlan{Collections: []JSONImportCollection{
		{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}}},
		{Name: "empty-collection"},
	}}

	if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return plan, nil }); err == nil {
		t.Fatal("ImportJSONOnce returned nil error after a planned empty collection gained a record")
	}
	assertJSONImportStateEmpty(t, db)
}

func TestImportJSONOnceRejectsUnexpectedGlobalRowsAndRollsBack(t *testing.T) {
	db := openJSONImportTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER add_unexpected_import AFTER INSERT ON store_records WHEN NEW.collection = 'widgets' BEGIN INSERT INTO store_records (collection, id, payload) VALUES ('unexpected', 'rogue', '{}'); END`); err != nil {
		t.Fatalf("create unexpected-row trigger: %v", err)
	}
	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}}}}}

	if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return plan, nil }); err == nil {
		t.Fatal("ImportJSONOnce returned nil error after unexpected global row was inserted")
	}
	assertJSONImportStateEmpty(t, db)
}

func TestImportJSONOnceDoesNotUpsertWhenKeyAppearsAfterPrecheck(t *testing.T) {
	db := openJSONImportTestDB(t)
	original := `{"id":"one","value":"pre-existing"}`
	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one","value":"imported"}`)}}}}}

	_, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) {
		if _, err := db.Exec(`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)`, "widgets", "one", original); err != nil {
			return JSONImportPlan{}, err
		}
		return plan, nil
	})
	if err == nil {
		t.Fatal("ImportJSONOnce returned nil error when the planned key already existed")
	}
	var got string
	if err := db.QueryRow(`SELECT payload FROM store_records WHERE collection = ? AND id = ?`, "widgets", "one").Scan(&got); err != nil {
		t.Fatalf("read original record after failed import: %v", err)
	}
	if got != original {
		t.Fatalf("existing payload = %s, want unchanged %s", got, original)
	}
	var markerCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM store_migrations WHERE migration_id = ?`, "legacy-json-v1").Scan(&markerCount); err != nil {
		t.Fatalf("count migration marker: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("migration marker count = %d, want 0 after rollback", markerCount)
	}
}

func TestImportJSONOnceValidatesMigrationAndPlanIdentifiersAndPayloads(t *testing.T) {
	validRecord := JSONImportRecord{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}
	tests := []struct {
		name string
		plan JSONImportPlan
	}{
		{name: "empty collection name", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: " "}}}},
		{name: "unsafe collection name", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "../widgets"}}}},
		{name: "duplicate collection names", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets"}, {Name: "widgets"}}}},
		{name: "empty record ID", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: " \t", Payload: json.RawMessage(`{}`)}}}}}},
		{name: "NUL record ID", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one\x00two", Payload: json.RawMessage(`{}`)}}}}}},
		{name: "duplicate record key", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{validRecord, validRecord}}}}},
		{name: "invalid JSON", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":`)}}}}}},
		{name: "duplicate JSON object key", plan: JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one","id":"two"}`)}}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openJSONImportTestDB(t)
			if _, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) { return tt.plan, nil }); err == nil {
				t.Fatal("ImportJSONOnce returned nil error for invalid import plan")
			}
			assertJSONImportStateEmpty(t, db)
		})
	}

	db := openJSONImportTestDB(t)
	prepared := false
	if _, err := ImportJSONOnce(db, "../unsafe-migration", func() (JSONImportPlan, error) {
		prepared = true
		return JSONImportPlan{}, nil
	}); err == nil {
		t.Fatal("ImportJSONOnce returned nil error for unsafe migration ID")
	}
	if prepared {
		t.Fatal("preparation callback ran for unsafe migration ID")
	}
}

func TestImportJSONOnceConcurrentSeparateHandlesImportOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	first, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open first SQLite handle: %v", err)
	}
	defer first.Close()
	second, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open second SQLite handle: %v", err)
	}
	defer second.Close()

	start := make(chan struct{})
	prepared := make(chan struct{}, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	type attempt struct {
		result JSONImportResult
		err    error
	}
	results := make(chan attempt, 2)
	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{ID: "one", Payload: json.RawMessage(`{"id":"one"}`)}}}}}
	for _, db := range []*sql.DB{first, second} {
		go func(db *sql.DB) {
			result, err := ImportJSONOnce(db, "legacy-json-v1", func() (JSONImportPlan, error) {
				prepared <- struct{}{}
				ready.Done()
				<-start
				return plan, nil
			})
			results <- attempt{result: result, err: err}
		}(db)
	}
	ready.Wait()
	if len(prepared) != 2 {
		t.Fatalf("preparation callbacks entered = %d, want 2", len(prepared))
	}
	close(start)

	var skipped, imported int
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent ImportJSONOnce returned error: %v", got.err)
		}
		if got.result.Skipped {
			skipped++
		} else {
			imported++
		}
	}
	if skipped != 1 || imported != 1 {
		t.Fatalf("concurrent outcomes: imported=%d skipped=%d, want one each", imported, skipped)
	}
	var count int
	if err := first.QueryRow(`SELECT COUNT(*) FROM store_records WHERE collection = ?`, "widgets").Scan(&count); err != nil {
		t.Fatalf("count imported records: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported record count = %d, want 1", count)
	}
}

func TestImportJSONOnceSkipsWhenSameMigrationCompletesBetweenPrechecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	winnerDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open winning SQLite handle: %v", err)
	}
	t.Cleanup(func() {
		if err := winnerDB.Close(); err != nil {
			t.Errorf("close winning SQLite handle: %v", err)
		}
	})

	countRead := make(chan struct{}, 1)
	allowCount := make(chan struct{})
	var releaseOnce sync.Once
	releaseCount := func() { releaseOnce.Do(func() { close(allowCount) }) }
	loserDB := sql.OpenDB(&precheckBarrierConnector{
		driver:     &sqlite.Driver{},
		name:       path,
		countRead:  countRead,
		allowCount: allowCount,
	})
	loserDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		releaseCount()
		if err := loserDB.Close(); err != nil {
			t.Errorf("close losing SQLite handle: %v", err)
		}
	})

	plan := JSONImportPlan{Collections: []JSONImportCollection{{Name: "widgets", Records: []JSONImportRecord{{
		ID: "one", Payload: json.RawMessage(`{"id":"one"}`),
	}}}}}
	type attempt struct {
		result JSONImportResult
		err    error
	}
	loserResult := make(chan attempt, 1)
	loserPrepared := false
	go func() {
		result, err := ImportJSONOnce(loserDB, "legacy-json-v1", func() (JSONImportPlan, error) {
			loserPrepared = true
			return plan, nil
		})
		loserResult <- attempt{result: result, err: err}
	}()

	select {
	case <-countRead:
		// The losing call has observed no marker and is paused before its count.
	case <-time.After(5 * time.Second):
		t.Fatal("losing import did not reach the record-count precheck")
	}

	winnerResult, err := ImportJSONOnce(winnerDB, "legacy-json-v1", func() (JSONImportPlan, error) {
		return plan, nil
	})
	releaseCount()
	if err != nil {
		t.Fatalf("winning import returned error: %v", err)
	}
	if winnerResult.Skipped {
		t.Fatal("winning import was skipped, want it to complete the migration")
	}

	select {
	case got := <-loserResult:
		if got.err != nil {
			t.Fatalf("losing same-migration import returned error after winner committed: %v", got.err)
		}
		if !got.result.Skipped {
			t.Fatal("losing same-migration import Skipped = false, want true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("losing import did not finish after releasing the record-count precheck")
	}
	if loserPrepared {
		t.Fatal("losing import preparation callback ran after the winner committed")
	}

	var records int
	if err := winnerDB.QueryRow(`SELECT COUNT(*) FROM store_records`).Scan(&records); err != nil {
		t.Fatalf("count imported records: %v", err)
	}
	if records != 1 {
		t.Fatalf("store record count = %d, want only the winner's record", records)
	}
}

type precheckBarrierConnector struct {
	driver     driver.Driver
	name       string
	countRead  chan<- struct{}
	allowCount <-chan struct{}
}

func (c *precheckBarrierConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(c.name)
	if err != nil {
		return nil, err
	}
	return &precheckBarrierConn{Conn: conn, countRead: c.countRead, allowCount: c.allowCount}, nil
}

func (c *precheckBarrierConnector) Driver() driver.Driver { return c.driver }

type precheckBarrierConn struct {
	driver.Conn
	countRead  chan<- struct{}
	allowCount <-chan struct{}
}

func (c *precheckBarrierConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if query == `SELECT COUNT(*) FROM store_records` {
		c.countRead <- struct{}{}
		select {
		case <-c.allowCount:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return queryer.QueryContext(ctx, query, args)
}

func assertJSONImportStateEmpty(t *testing.T, db *sql.DB) {
	t.Helper()
	var records int
	if err := db.QueryRow(`SELECT COUNT(*) FROM store_records`).Scan(&records); err != nil {
		t.Fatalf("count store records: %v", err)
	}
	if records != 0 {
		t.Errorf("store record count = %d, want 0 after failed import", records)
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM store_migrations`).Scan(&migrations); err != nil {
		t.Fatalf("count migration markers: %v", err)
	}
	if migrations != 0 {
		t.Errorf("migration marker count = %d, want 0 after failed import", migrations)
	}
}
