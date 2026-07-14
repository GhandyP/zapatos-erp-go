package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestJSONFileRepositoryLoadSemantics(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, path string)
		wantIDs     []string
		wantErr     bool
		wantLoadErr bool
	}{
		{name: "missing file falls back to seeds", wantIDs: []string{"seed-1"}},
		{
			name: "whitespace-only file is rejected and reports degradation",
			setup: func(t *testing.T, path string) {
				t.Helper()
				writeRepositoryFile(t, path, []byte("   \n"))
			},
			wantErr:     true,
			wantLoadErr: true,
		},
		{
			name: "malformed file is rejected and reports degradation",
			setup: func(t *testing.T, path string) {
				t.Helper()
				writeRepositoryFile(t, path, []byte("not json"))
			},
			wantErr:     true,
			wantLoadErr: true,
		},
		{
			name: "null file is rejected and reports degradation",
			setup: func(t *testing.T, path string) {
				t.Helper()
				writeRepositoryFile(t, path, []byte("null"))
			},
			wantErr:     true,
			wantLoadErr: true,
		},
		{
			name: "valid empty array stays empty",
			setup: func(t *testing.T, path string) {
				t.Helper()
				writeRepositoryFile(t, path, []byte("[]"))
			},
			wantIDs: []string{},
		},
		{
			name: "valid file loads records in id order",
			setup: func(t *testing.T, path string) {
				t.Helper()
				data, err := json.MarshalIndent([]testRecord{
					{ID: "file-2", Name: "Second"},
					{ID: "file-1", Name: "First"},
				}, "", "  ")
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				writeRepositoryFile(t, path, data)
			},
			wantIDs: []string{"file-1", "file-2"},
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
			if (err != nil) != tt.wantErr {
				t.Fatalf("List error = %v, want error: %t", err, tt.wantErr)
			}
			if (repo.LoadError() != nil) != tt.wantLoadErr {
				t.Fatalf("LoadError() = %v, want error: %t", repo.LoadError(), tt.wantLoadErr)
			}
			if err != nil {
				return
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

func TestJSONFileRepositoryRetriesAfterFileRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeRepositoryFile(t, path, []byte("not json"))
	repo := NewJSONFileRepository(path, []testRecord{{ID: "seed-1"}}, func(v testRecord) string { return v.ID })

	if _, err := repo.List(); err == nil {
		t.Fatal("expected degraded file to be rejected")
	}
	if repo.LoadError() == nil {
		t.Fatal("expected degraded load error")
	}

	writeRepositoryFile(t, path, []byte(`[{"id":"recovered"}]`))
	items, err := repo.List()
	if err != nil {
		t.Fatalf("List after repair: %v", err)
	}
	if got := []string{items[0].ID}; !reflect.DeepEqual(got, []string{"recovered"}) {
		t.Fatalf("recovered IDs = %#v, want recovered record", got)
	}
	if repo.LoadError() != nil {
		t.Fatalf("LoadError after repair = %v, want nil", repo.LoadError())
	}
}

func TestJSONFileRepositoryRejectsWritesWhileDegraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeRepositoryFile(t, path, []byte("not json"))
	repo := NewJSONFileRepository(path, []testRecord{{ID: "seed-1"}}, func(v testRecord) string { return v.ID })

	if _, err := repo.List(); err == nil {
		t.Fatal("expected degraded file to be rejected")
	}
	if _, err := repo.Save(testRecord{ID: "new"}); err == nil {
		t.Fatal("expected writes to be rejected while file is degraded")
	}
}

func TestJSONFileRepositoryRollsBackAfterPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	repo := NewJSONFileRepository(path, []persistRecord{{ID: "one"}, {ID: "two"}}, func(v persistRecord) string { return v.ID })
	if _, err := repo.List(); err != nil {
		t.Fatalf("load seed data: %v", err)
	}

	persistFailure = true
	defer func() { persistFailure = false }()

	if _, err := repo.Save(persistRecord{ID: "three"}); err == nil {
		t.Fatal("expected Save to report persistence failure")
	}
	if _, ok, err := repo.FindByID("three"); err != nil {
		t.Fatalf("find after failed save: %v", err)
	} else if ok {
		t.Fatal("failed Save must not leave the new item in memory")
	}

	if deleted, err := repo.Delete("one"); err == nil {
		t.Fatal("expected Delete to report persistence failure")
	} else if deleted {
		t.Fatal("failed Delete must report that the item was not deleted")
	}
	if _, ok, err := repo.FindByID("one"); err != nil {
		t.Fatalf("find after failed delete: %v", err)
	} else if !ok {
		t.Fatal("failed Delete must restore the item in memory")
	}
}

func writeRepositoryFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write repository file: %v", err)
	}
}

type persistRecord struct {
	ID string `json:"id"`
}

var persistFailure bool

func (r persistRecord) MarshalJSON() ([]byte, error) {
	if persistFailure {
		return nil, errors.New("simulated persistence failure")
	}
	type alias persistRecord
	return json.Marshal(alias(r))
}
