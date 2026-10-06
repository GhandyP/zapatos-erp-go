package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"zapatos-erp-go/internal/store"
)

type auditBusinessRecord struct {
	ID string `json:"id"`
}

func TestSQLiteWriterPersistsBusinessAndAuditInOneUnitOfWork(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	wantEvent := Event{Actor: "admin", Action: "saved", Entity: "widget:one", At: "2026-01-02T03:04:05Z"}
	err = store.NewSQLiteUnitOfWork(db).Run(context.Background(), func(tx *sql.Tx) error {
		repo := store.NewSQLiteRepository(tx, "widgets", func(v auditBusinessRecord) string { return v.ID })
		if _, err := repo.Save(auditBusinessRecord{ID: "one"}); err != nil {
			return err
		}
		return NewSQLiteWriter(tx, "audit-events").Write("event-one", wantEvent)
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var businessCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", "widgets", "one").Scan(&businessCount); err != nil {
		t.Fatalf("query business row: %v", err)
	}
	if businessCount != 1 {
		t.Fatalf("business row count = %d, want 1", businessCount)
	}

	var payload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", "audit-events", "event-one").Scan(&payload); err != nil {
		t.Fatalf("query audit row: %v", err)
	}
	var gotEvent Event
	if err := json.Unmarshal([]byte(payload), &gotEvent); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if gotEvent != wantEvent {
		t.Fatalf("persisted event = %+v, want %+v", gotEvent, wantEvent)
	}
}

func TestSQLiteWriterInsertFailureRollsBackBusinessWrite(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	original := Event{Actor: "admin", Action: "original", Entity: "widget:one", At: "2026-01-02T03:04:05Z"}
	if err := store.NewSQLiteUnitOfWork(db).Run(context.Background(), func(tx *sql.Tx) error {
		return NewSQLiteWriter(tx, "audit-events").Write("duplicate-id", original)
	}); err != nil {
		t.Fatalf("seed existing event: %v", err)
	}

	duplicate := Event{Actor: "admin", Action: "duplicate", Entity: "widget:two", At: "2026-01-02T03:04:06Z"}
	err = store.NewSQLiteUnitOfWork(db).Run(context.Background(), func(tx *sql.Tx) error {
		repo := store.NewSQLiteRepository(tx, "widgets", func(v auditBusinessRecord) string { return v.ID })
		if _, err := repo.Save(auditBusinessRecord{ID: "rolled-back"}); err != nil {
			return err
		}
		return NewSQLiteWriter(tx, "audit-events").Write("duplicate-id", duplicate)
	})
	if err == nil {
		t.Fatal("Run returned nil, want rejected duplicate audit insert error")
	}

	var businessCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", "widgets", "rolled-back").Scan(&businessCount); err != nil {
		t.Fatalf("query business row: %v", err)
	}
	if businessCount != 0 {
		t.Fatalf("business row count after rejected audit insert = %d, want 0", businessCount)
	}
	var payload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", "audit-events", "duplicate-id").Scan(&payload); err != nil {
		t.Fatalf("query original audit row: %v", err)
	}
	var gotEvent Event
	if err := json.Unmarshal([]byte(payload), &gotEvent); err != nil {
		t.Fatalf("decode original audit payload: %v", err)
	}
	if gotEvent != original {
		t.Fatalf("original event after rejected insert = %+v, want %+v", gotEvent, original)
	}
}

func TestSQLiteWriterReturnsRetentionFailureAndRollsBack(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	payload, err := json.Marshal(Event{Actor: "admin", Action: "saved", Entity: "seed", At: "2026-01-02T03:04:05Z"})
	if err != nil {
		t.Fatalf("marshal seed event: %v", err)
	}
	for i := 0; i < 500; i++ {
		if _, err := db.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", "audit-events", fmt.Sprintf("seed-%03d", i), string(payload)); err != nil {
			t.Fatalf("insert seed event %d: %v", i, err)
		}
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_audit_retention BEFORE DELETE ON store_records
		WHEN OLD.collection = 'audit-events'
		BEGIN SELECT RAISE(ABORT, 'retention rejected'); END`); err != nil {
		t.Fatalf("create retention trigger: %v", err)
	}

	err = store.NewSQLiteUnitOfWork(db).Run(context.Background(), func(tx *sql.Tx) error {
		repo := store.NewSQLiteRepository(tx, "widgets", func(v auditBusinessRecord) string { return v.ID })
		if _, err := repo.Save(auditBusinessRecord{ID: "rolled-back"}); err != nil {
			return err
		}
		return NewSQLiteWriter(tx, "audit-events").Write("new-event", Event{
			Actor: "admin", Action: "saved", Entity: "new", At: "2026-01-02T03:04:06Z",
		})
	})
	if err == nil {
		t.Fatal("Run returned nil, want retention error")
	}

	var businessCount, auditCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", "widgets", "rolled-back").Scan(&businessCount); err != nil {
		t.Fatalf("query business row: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", "audit-events").Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if businessCount != 0 || auditCount != 500 {
		t.Fatalf("counts after retention failure = (business %d, audit %d), want (0, 500)", businessCount, auditCount)
	}
}

func TestSQLiteWriterRetainsNewest500EventsPerCollection(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", "other-events", "untouched", `{}`); err != nil {
		t.Fatalf("insert event in other collection: %v", err)
	}

	start := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	err = store.NewSQLiteUnitOfWork(db).Run(context.Background(), func(tx *sql.Tx) error {
		writer := NewSQLiteWriter(tx, "audit-events")
		for i := 0; i < 502; i++ {
			event := Event{
				Actor: "admin", Action: "saved", Entity: fmt.Sprintf("widget:%03d", i),
				At: start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			}
			if err := writer.Write(fmt.Sprintf("event-%03d", i), event); err != nil {
				return fmt.Errorf("write event %d: %w", i, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var eventCount, otherCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", "audit-events").Scan(&eventCount); err != nil {
		t.Fatalf("count retained events: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", "other-events").Scan(&otherCount); err != nil {
		t.Fatalf("count other collection events: %v", err)
	}
	if eventCount != 500 || otherCount != 1 {
		t.Fatalf("collection counts = (audit %d, other %d), want (500, 1)", eventCount, otherCount)
	}
	for _, id := range []string{"event-000", "event-001"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", "audit-events", id).Scan(&count); err != nil {
			t.Fatalf("check expired event %q: %v", id, err)
		}
		if count != 0 {
			t.Errorf("expired event %q remains in the collection", id)
		}
	}
	for _, id := range []string{"event-002", "event-501"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", "audit-events", id).Scan(&count); err != nil {
			t.Fatalf("check retained event %q: %v", id, err)
		}
		if count != 1 {
			t.Errorf("retained event %q count = %d, want 1", id, count)
		}
	}
}
