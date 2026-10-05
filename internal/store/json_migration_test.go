package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func migrationRecordID(record testRecord) string { return record.ID }

func TestPreflightJSONSnapshotMissingFileUsesSeeds(t *testing.T) {
	seeds := []testRecord{{ID: "seed-1", Name: "Seed"}}
	got, err := PreflightJSONSnapshot(t.TempDir(), "records.json", "records", seeds, migrationRecordID)
	if err != nil {
		t.Fatalf("PreflightJSONSnapshot returned error: %v", err)
	}
	if got.FileExisted {
		t.Fatal("FileExisted = true, want false")
	}
	if got.OriginalBytes != nil {
		t.Fatalf("OriginalBytes = %q, want nil", got.OriginalBytes)
	}
	if !reflect.DeepEqual(got.Items, seeds) {
		t.Fatalf("Items = %#v, want seeds %#v", got.Items, seeds)
	}
	if got.Collection != "records" {
		t.Fatalf("Collection = %q, want records", got.Collection)
	}
}

func TestPreflightJSONSnapshotRequiresExistingDirectory(t *testing.T) {
	seeds := []testRecord{{ID: "seed-1"}}

	t.Run("missing directory", func(t *testing.T) {
		dataDir := filepath.Join(t.TempDir(), "missing")
		if _, err := PreflightJSONSnapshot(dataDir, "records.json", "records", seeds, migrationRecordID); err == nil {
			t.Fatal("PreflightJSONSnapshot returned nil error for missing data directory")
		}
	})

	t.Run("not a directory", func(t *testing.T) {
		dataDir := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(dataDir, []byte("file"), 0o600); err != nil {
			t.Fatalf("write dataDir file: %v", err)
		}
		if _, err := PreflightJSONSnapshot(dataDir, "records.json", "records", seeds, migrationRecordID); err == nil {
			t.Fatal("PreflightJSONSnapshot returned nil error for non-directory dataDir")
		}
	})
}

func TestPreflightJSONSnapshotRejectsFinalComponentSymlinks(t *testing.T) {
	for _, tt := range []struct {
		name       string
		targetName string
		create     bool
	}{
		{name: "existing target", targetName: "target.json", create: true},
		{name: "dangling target", targetName: "missing.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.create {
				if err := os.WriteFile(filepath.Join(root, tt.targetName), []byte(`[{"id":"target"}]`), 0o600); err != nil {
					t.Fatalf("write symlink target: %v", err)
				}
			}
			if err := os.Symlink(tt.targetName, filepath.Join(root, "records.json")); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			if _, err := PreflightJSONSnapshot(root, "records.json", "records", []testRecord{{ID: "seed"}}, migrationRecordID); err == nil {
				t.Fatal("PreflightJSONSnapshot returned nil error for final-component symlink")
			}
		})
	}
}

func TestPreflightJSONSnapshotRejectsNestedDuplicateObjectKeys(t *testing.T) {
	type nestedRecord struct {
		ID      string `json:"id"`
		Details struct {
			Value string `json:"value"`
		} `json:"details"`
	}
	root := t.TempDir()
	raw := []byte(`[{"id":"one","details":{"value":"first","value":"second"}}]`)
	if err := os.WriteFile(filepath.Join(root, "records.json"), raw, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := PreflightJSONSnapshot(root, "records.json", "records", nil, func(record nestedRecord) string { return record.ID })
	if err == nil {
		t.Fatal("PreflightJSONSnapshot returned nil error for nested duplicate object key")
	}
}

func TestPreflightJSONSnapshotValidEmptyDoesNotSeedAndRetainsBytes(t *testing.T) {
	root := t.TempDir()
	original := []byte("[  ]\n")
	if err := os.WriteFile(filepath.Join(root, "records.json"), original, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	got, err := PreflightJSONSnapshot(root, "records.json", "records", []testRecord{{ID: "seed-1"}}, migrationRecordID)
	if err != nil {
		t.Fatalf("PreflightJSONSnapshot returned error: %v", err)
	}
	if !got.FileExisted {
		t.Fatal("FileExisted = false, want true")
	}
	if len(got.Items) != 0 {
		t.Fatalf("Items = %#v, want empty", got.Items)
	}
	if !bytes.Equal(got.OriginalBytes, original) {
		t.Fatalf("OriginalBytes = %q, want exact source %q", got.OriginalBytes, original)
	}
}

func TestPreflightJSONSnapshotStrictDecodeAndRetainsOriginalOnFailure(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "whitespace only", raw: " \n\t"},
		{name: "malformed", raw: "[{"},
		{name: "top level null", raw: "null"},
		{name: "top level object", raw: `{ "id": "one" }`},
		{name: "unknown field", raw: `[{"id":"one","name":"One","extra":true}]`},
		{name: "duplicate object key", raw: `[{"id":"one","id":"two","name":"Two"}]`},
		{name: "trailing value", raw: `[{"id":"one","name":"One"}] []`},
		{name: "empty ID", raw: `[{"id":"  ","name":"No ID"}]`},
		{name: "duplicate IDs", raw: `[{"id":"same"},{"id":"same"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			original := []byte(tt.raw)
			if err := os.WriteFile(filepath.Join(root, "records.json"), original, 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}

			got, err := PreflightJSONSnapshot(root, "records.json", "records", nil, migrationRecordID)
			if err == nil {
				t.Fatal("PreflightJSONSnapshot returned nil error, want fail-closed error")
			}
			if !got.FileExisted {
				t.Fatal("FileExisted = false on failure, want true")
			}
			if !bytes.Equal(got.OriginalBytes, original) {
				t.Fatalf("OriginalBytes = %q, want exact source %q", got.OriginalBytes, original)
			}
		})
	}
}

func TestPreflightJSONSnapshotLoadsTypedRecords(t *testing.T) {
	root := t.TempDir()
	original := []byte(`[ {"id":"file-1","name":"From file"} ]`)
	if err := os.WriteFile(filepath.Join(root, "records.json"), original, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	got, err := PreflightJSONSnapshot(root, "records.json", "records", []testRecord{{ID: "seed-1"}}, migrationRecordID)
	if err != nil {
		t.Fatalf("PreflightJSONSnapshot returned error: %v", err)
	}
	want := []testRecord{{ID: "file-1", Name: "From file"}}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("Items = %#v, want %#v", got.Items, want)
	}
	if !bytes.Equal(got.OriginalBytes, original) {
		t.Fatalf("OriginalBytes = %q, want %q", got.OriginalBytes, original)
	}
}

func TestPreflightJSONSnapshotRejectsUnsafeArgumentsAndInvalidSeeds(t *testing.T) {
	validSelector := migrationRecordID
	tests := []struct {
		name       string
		filename   string
		collection string
		seeds      []testRecord
		selector   func(testRecord) string
	}{
		{name: "empty filename", filename: "", collection: "records", selector: validSelector},
		{name: "filename traversal", filename: "../records.json", collection: "records", selector: validSelector},
		{name: "filename slash", filename: "nested/records.json", collection: "records", selector: validSelector},
		{name: "filename backslash", filename: `nested\records.json`, collection: "records", selector: validSelector},
		{name: "empty collection", filename: "records.json", collection: "", selector: validSelector},
		{name: "collection traversal", filename: "records.json", collection: "../records", selector: validSelector},
		{name: "nil selector", filename: "records.json", collection: "records"},
		{name: "empty seed ID", filename: "records.json", collection: "records", seeds: []testRecord{{ID: "\t"}}, selector: validSelector},
		{name: "duplicate seed IDs", filename: "records.json", collection: "records", seeds: []testRecord{{ID: "same"}, {ID: "same"}}, selector: validSelector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PreflightJSONSnapshot(t.TempDir(), tt.filename, tt.collection, tt.seeds, tt.selector)
			if err == nil {
				t.Fatal("PreflightJSONSnapshot returned nil error, want validation error")
			}
		})
	}
}

func TestCreateImmutableBackupPreservesBytesAndWritesDeterministicPrivateManifest(t *testing.T) {
	parent := t.TempDir()
	backupDir := filepath.Join(parent, "backup")
	sources := []RawBackupSource{
		{Name: "sessions.json", Data: []byte("session bytes\x00\n"), Exists: true},
		{Name: "missing.json", Exists: false},
		{Name: "records.json", Data: []byte("[ { \"id\": \"x\" } ]\n"), Exists: true},
	}

	if err := CreateImmutableBackup(backupDir, sources); err != nil {
		t.Fatalf("CreateImmutableBackup returned error: %v", err)
	}
	for _, source := range sources {
		path := filepath.Join(backupDir, source.Name)
		if !source.Exists {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("missing source content file exists or stat failed: %v", err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read backup %s: %v", source.Name, err)
		}
		if !bytes.Equal(got, source.Data) {
			t.Fatalf("backup %s = %q, want exact bytes %q", source.Name, got, source.Data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat backup %s: %v", source.Name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode for %s = %04o, want 0600", source.Name, info.Mode().Perm())
		}
	}

	manifestPath := filepath.Join(backupDir, "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded struct {
		Files []struct {
			Name   string `json:"name"`
			Exists bool   `json:"exists"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(decoded.Files) != 3 {
		t.Fatalf("manifest entries = %d, want 3: %s", len(decoded.Files), manifest)
	}
	if got, want := []string{decoded.Files[0].Name, decoded.Files[1].Name, decoded.Files[2].Name}, []string{"missing.json", "records.json", "sessions.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest names = %#v, want sorted %#v", got, want)
	}
	if decoded.Files[0].Exists || decoded.Files[0].SHA256 != "" {
		t.Fatalf("missing entry = %#v, want absent with no digest", decoded.Files[0])
	}
	for index, source := range []RawBackupSource{sources[2], sources[0]} {
		digest := sha256.Sum256(source.Data)
		if !decoded.Files[index+1].Exists || decoded.Files[index+1].SHA256 != hex.EncodeToString(digest[:]) {
			t.Errorf("manifest entry for %s = %#v, want its SHA-256", source.Name, decoded.Files[index+1])
		}
	}

	dirInfo, err := os.Stat(backupDir)
	if err != nil {
		t.Fatalf("stat backup directory: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("backup directory mode = %04o, want 0700", dirInfo.Mode().Perm())
	}
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	if manifestInfo.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode = %04o, want 0600", manifestInfo.Mode().Perm())
	}

	reordered := []RawBackupSource{sources[2], sources[1], sources[0]}
	if err := CreateImmutableBackup(backupDir, reordered); err != nil {
		t.Fatalf("identical reordered repeat did not safely reuse backup: %v", err)
	}
	reusedManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read reused manifest: %v", err)
	}
	if !bytes.Equal(reusedManifest, manifest) {
		t.Fatalf("manifest changed on identical repeat: before %q after %q", manifest, reusedManifest)
	}
}

func TestCreateImmutableBackupRequiresExistingParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing-parent")
	backupDir := filepath.Join(parent, "backup")
	if err := CreateImmutableBackup(backupDir, []RawBackupSource{{Name: "records.json", Data: []byte("records"), Exists: true}}); err == nil {
		t.Fatal("CreateImmutableBackup returned nil error with missing parent directory")
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatalf("parent directory was created or inspection failed: %v", err)
	}
}

func TestCreateImmutableBackupConcurrentIdenticalPublication(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backup")
	payload := bytes.Repeat([]byte("payload-"), 128*1024)
	sources := []RawBackupSource{
		{Name: "one.json", Data: payload, Exists: true},
		{Name: "two.json", Data: payload, Exists: true},
		{Name: "three.json", Data: payload, Exists: true},
		{Name: "four.json", Data: payload, Exists: true},
	}

	const publishers = 8
	start := make(chan struct{})
	errs := make(chan error, publishers)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(publishers)
	done.Add(publishers)
	for i := 0; i < publishers; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			errs <- CreateImmutableBackup(backupDir, sources)
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent identical publisher returned error: %v", err)
		}
	}
	for _, source := range sources {
		got, err := os.ReadFile(filepath.Join(backupDir, source.Name))
		if err != nil {
			t.Fatalf("read concurrently published %s: %v", source.Name, err)
		}
		if !bytes.Equal(got, source.Data) {
			t.Errorf("concurrently published %s does not match source bytes", source.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(backupDir, BackupManifestFilename)); err != nil {
		t.Fatalf("stat concurrently published manifest: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(backupDir))
	if err != nil {
		t.Fatalf("read backup parent: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(backupDir) {
		t.Fatalf("backup parent entries = %#v, want only the published destination", entries)
	}
}

func TestCreateImmutableBackupRejectsConflictsAndPartialBackupsWithoutOverwrite(t *testing.T) {
	t.Run("conflicting backup", func(t *testing.T) {
		backupDir := filepath.Join(t.TempDir(), "backup")
		original := []byte("original")
		if err := CreateImmutableBackup(backupDir, []RawBackupSource{{Name: "records.json", Data: original, Exists: true}}); err != nil {
			t.Fatalf("create initial backup: %v", err)
		}
		if err := CreateImmutableBackup(backupDir, []RawBackupSource{{Name: "records.json", Data: []byte("changed"), Exists: true}}); err == nil {
			t.Fatal("conflicting backup returned nil error")
		}
		got, err := os.ReadFile(filepath.Join(backupDir, "records.json"))
		if err != nil {
			t.Fatalf("read original backup after conflict: %v", err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("conflicting attempt overwrote backup: got %q, want %q", got, original)
		}
	})

	t.Run("partial backup", func(t *testing.T) {
		backupDir := filepath.Join(t.TempDir(), "partial")
		if err := os.Mkdir(backupDir, 0o700); err != nil {
			t.Fatalf("create partial directory: %v", err)
		}
		partialPath := filepath.Join(backupDir, "records.json")
		if err := os.WriteFile(partialPath, []byte("partial"), 0o600); err != nil {
			t.Fatalf("write partial content: %v", err)
		}
		if err := CreateImmutableBackup(backupDir, []RawBackupSource{{Name: "records.json", Data: []byte("complete"), Exists: true}}); err == nil {
			t.Fatal("partial backup returned nil error")
		}
		got, err := os.ReadFile(partialPath)
		if err != nil {
			t.Fatalf("read partial content after attempt: %v", err)
		}
		if string(got) != "partial" {
			t.Fatalf("partial backup was overwritten: got %q", got)
		}
	})
}

func TestCreateImmutableBackupRejectsUnsafeAndAmbiguousSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []RawBackupSource
	}{
		{name: "empty name", sources: []RawBackupSource{{Name: "", Exists: true}}},
		{name: "traversal", sources: []RawBackupSource{{Name: "../outside.json", Exists: true}}},
		{name: "slash", sources: []RawBackupSource{{Name: "nested/file.json", Exists: true}}},
		{name: "backslash", sources: []RawBackupSource{{Name: `nested\\file.json`, Exists: true}}},
		{name: "reserved manifest", sources: []RawBackupSource{{Name: "manifest.json", Exists: true}}},
		{name: "duplicate names", sources: []RawBackupSource{{Name: "records.json", Exists: true}, {Name: "records.json", Exists: false}}},
		{name: "missing with bytes", sources: []RawBackupSource{{Name: "records.json", Data: []byte("ignored"), Exists: false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CreateImmutableBackup(filepath.Join(t.TempDir(), "backup"), tt.sources); err == nil {
				t.Fatal("CreateImmutableBackup returned nil error, want validation error")
			}
		})
	}

	if err := CreateImmutableBackup("", nil); err == nil || !strings.Contains(strings.ToLower(err.Error()), "directory") {
		t.Fatalf("empty backup directory error = %v, want directory validation error", err)
	}
}
