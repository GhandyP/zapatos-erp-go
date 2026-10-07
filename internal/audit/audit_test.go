package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStorePersistsEventsOnRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	store := NewStore(path)

	recorded := store.Record("admin", "saved", "packaging:pkg-1")
	if recorded.Actor != "admin" || recorded.Action != "saved" || recorded.Entity != "packaging:pkg-1" {
		t.Fatalf("unexpected recorded event: %+v", recorded)
	}

	reloaded := NewStore(path)
	events := reloaded.List()
	if len(events) != 1 {
		t.Fatalf("expected 1 reloaded event, got %d", len(events))
	}
	if events[0] != recorded {
		t.Fatalf("reloaded event = %+v, want %+v", events[0], recorded)
	}
}

func TestStoreContextAccessReportsPersistenceAndCancellationErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create directory at audit path: %v", err)
	}
	store := NewStore(path)
	if _, err := store.RecordContext(context.Background(), "admin", "saved", "packaging:pkg-1"); err == nil {
		t.Fatal("RecordContext returned nil, want JSON persistence error")
	}
	if events := store.List(); len(events) != 0 {
		t.Fatalf("events after failed persistence = %#v, want no in-memory change", events)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListContext(ctx); err == nil {
		t.Fatal("ListContext returned nil, want context cancellation error")
	}
	if _, err := store.RecordContext(ctx, "admin", "saved", "packaging:pkg-1"); err == nil {
		t.Fatal("RecordContext returned nil, want context cancellation error")
	}
}

func TestStoreReloadsEventsInTimestampOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent([]Event{
		{Actor: "admin", Action: "later", Entity: "packaging:pkg-2", At: "2026-06-11T10:00:00Z"},
		{Actor: "admin", Action: "earlier", Entity: "packaging:pkg-1", At: "2026-06-11T09:00:00Z"},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	store := NewStore(path)
	events := store.List()
	want := []Event{
		{Actor: "admin", Action: "earlier", Entity: "packaging:pkg-1", At: "2026-06-11T09:00:00Z"},
		{Actor: "admin", Action: "later", Entity: "packaging:pkg-2", At: "2026-06-11T10:00:00Z"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}
