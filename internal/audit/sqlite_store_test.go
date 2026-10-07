package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zapatos-erp-go/internal/store"
)

func TestSQLiteStoreRecordAndListContext(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	auditStore := NewSQLiteStore(db)
	want := Event{Actor: "admin", Action: "saved", Entity: "packaging:pkg-1"}
	got, err := auditStore.RecordContext(context.Background(), want.Actor, want.Action, want.Entity)
	if err != nil {
		t.Fatalf("RecordContext returned error: %v", err)
	}
	if got.Actor != want.Actor || got.Action != want.Action || got.Entity != want.Entity {
		t.Fatalf("recorded event = %+v, want actor/action/entity %+v", got, want)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.At); err != nil {
		t.Fatalf("recorded timestamp %q is invalid: %v", got.At, err)
	}

	var id string
	if err := db.QueryRow("SELECT id FROM store_records WHERE collection = ?", store.CollectionAuditEvents).Scan(&id); err != nil {
		t.Fatalf("read generated audit ID: %v", err)
	}
	if !strings.HasPrefix(id, "aud_") || len(id) != len("aud_")+32 {
		t.Fatalf("generated audit ID = %q, want aud_ plus 32 hex characters", id)
	}
	for _, char := range id[len("aud_"):] {
		if !strings.ContainsRune("0123456789abcdef", char) {
			t.Fatalf("generated audit ID = %q, contains non-hex character %q", id, char)
		}
	}

	events, err := auditStore.ListContext(context.Background())
	if err != nil {
		t.Fatalf("ListContext returned error: %v", err)
	}
	want.At = got.At
	if len(events) != 1 || events[0] != want {
		t.Fatalf("listed events = %#v, want [%#v]", events, want)
	}
}

func TestSQLiteStoreListsChronologicallyWithDeterministicTies(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	seed := []struct {
		id    string
		event Event
	}{
		{"tie-z", Event{Actor: "z", Action: "same", Entity: "x", At: "2026-01-02T03:04:05Z"}},
		{"later", Event{Actor: "later", Action: "later", Entity: "x", At: "2026-01-02T03:04:06Z"}},
		{"tie-a", Event{Actor: "a", Action: "same", Entity: "x", At: "2026-01-02T03:04:05Z"}},
		{"earlier", Event{Actor: "earlier", Action: "earlier", Entity: "x", At: "2026-01-02T03:04:04Z"}},
	}
	for _, item := range seed {
		payload, err := json.Marshal(item.event)
		if err != nil {
			t.Fatalf("marshal seed event: %v", err)
		}
		if _, err := db.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", store.CollectionAuditEvents, item.id, string(payload)); err != nil {
			t.Fatalf("insert seed event %q: %v", item.id, err)
		}
	}

	events, err := NewSQLiteStore(db).ListContext(context.Background())
	if err != nil {
		t.Fatalf("ListContext returned error: %v", err)
	}
	wantActors := []string{"earlier", "a", "z", "later"}
	if len(events) != len(wantActors) {
		t.Fatalf("listed %d events, want %d", len(events), len(wantActors))
	}
	for i, want := range wantActors {
		if events[i].Actor != want {
			t.Errorf("event %d actor = %q, want %q", i, events[i].Actor, want)
		}
	}
}

func TestSQLiteStoreRetainsNewest500EventsInOrder(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	start := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < sqliteAuditLimit; i++ {
		event := Event{Actor: fmt.Sprintf("seed-%03d", i), Action: "seed", Entity: "test", At: start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)}
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal seed event %d: %v", i, err)
		}
		if _, err := db.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", store.CollectionAuditEvents, fmt.Sprintf("seed-%03d", i), string(payload)); err != nil {
			t.Fatalf("insert seed event %d: %v", i, err)
		}
	}

	auditStore := NewSQLiteStore(db)
	if _, err := auditStore.RecordContext(context.Background(), "admin", "new", "test:newest"); err != nil {
		t.Fatalf("RecordContext returned error: %v", err)
	}
	events, err := auditStore.ListContext(context.Background())
	if err != nil {
		t.Fatalf("ListContext returned error: %v", err)
	}
	if len(events) != sqliteAuditLimit {
		t.Fatalf("listed %d events, want retention cap %d", len(events), sqliteAuditLimit)
	}
	if events[0].Actor != "seed-001" {
		t.Fatalf("oldest retained actor = %q, want seed-001", events[0].Actor)
	}
	if events[len(events)-1].Entity != "test:newest" {
		t.Fatalf("newest retained entity = %q, want test:newest", events[len(events)-1].Entity)
	}
}

func TestSQLiteStoreGeneratedIDCollisionFailsClosed(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	original := Event{Actor: "admin", Action: "original", Entity: "test:original", At: "2026-01-02T03:04:05Z"}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original event: %v", err)
	}
	const collisionID = "aud_00000000000000000000000000000000"
	if _, err := db.Exec("INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)", store.CollectionAuditEvents, collisionID, string(payload)); err != nil {
		t.Fatalf("seed collision row: %v", err)
	}

	auditStore := &SQLiteStore{
		db:          db,
		idGenerator: func() (string, error) { return collisionID, nil },
	}
	if _, err := auditStore.RecordContext(context.Background(), "admin", "duplicate", "test:duplicate"); err == nil {
		t.Fatal("RecordContext succeeded for a colliding ID, want insert error")
	}
	var gotPayload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", store.CollectionAuditEvents, collisionID).Scan(&gotPayload); err != nil {
		t.Fatalf("read original collision row: %v", err)
	}
	var got Event
	if err := json.Unmarshal([]byte(gotPayload), &got); err != nil {
		t.Fatalf("decode original collision row: %v", err)
	}
	if got != original {
		t.Fatalf("collision changed original event = %+v, want %+v", got, original)
	}
}

func TestSQLiteStoreReturnsDatabaseErrors(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}

	auditStore := NewSQLiteStore(db)
	if _, err := auditStore.ListContext(context.Background()); err == nil {
		t.Fatal("ListContext returned nil, want closed database error")
	}
	if _, err := auditStore.RecordContext(context.Background(), "admin", "saved", "test:one"); err == nil {
		t.Fatal("RecordContext returned nil, want closed database error")
	}
}

func TestSQLiteStoreContextCancellation(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auditStore := NewSQLiteStore(db)
	if _, err := auditStore.ListContext(ctx); err == nil {
		t.Fatal("ListContext returned nil, want context cancellation error")
	}
	if _, err := auditStore.RecordContext(ctx, "admin", "saved", "test:one"); err == nil {
		t.Fatal("RecordContext returned nil, want context cancellation error")
	}
}

var _ Access = (*SQLiteStore)(nil)
