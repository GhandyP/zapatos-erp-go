package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestJSONFileRepositorySeedFallbackAndLoad(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, path string)
		wantIDs []string
	}{
		{name: "missing file falls back to seeds", wantIDs: []string{"seed-1"}},
		{
			name: "empty file falls back to seeds",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
					t.Fatalf("write empty file: %v", err)
				}
			},
			wantIDs: []string{"seed-1"},
		},
		{
			name: "malformed file falls back to seeds",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
					t.Fatalf("write malformed file: %v", err)
				}
			},
			wantIDs: []string{"seed-1"},
		},
		{
			name: "valid file loads records instead of seeds",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				data, err := json.MarshalIndent([]testRecord{{ID: "file-1", Name: "File"}}, "", "  ")
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatalf("write valid file: %v", err)
				}
			},
			wantIDs: []string{"file-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "data.json")
			if tt.setup != nil {
				tt.setup(t, path)
			}

			repo := NewJSONFileRepository(path, []testRecord{{ID: "seed-1", Name: "Seed"}}, func(v testRecord) string { return v.ID })
			items, err := repo.List()
			if err != nil {
				t.Fatalf("List returned error: %v", err)
			}

			gotIDs := make([]string, 0, len(items))
			for _, item := range items {
				gotIDs = append(gotIDs, item.ID)
			}

			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("IDs = %#v, want %#v", gotIDs, tt.wantIDs)
			}
		})
	}
}
