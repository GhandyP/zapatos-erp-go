package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zapatos-erp-go/internal/auth"
	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/server"
	"zapatos-erp-go/internal/store"
)

func TestValidateStartupInputsRejectsFileDataDir(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.WriteFile(dataDir, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	err := validateStartupInputs(BootstrapConfig{BindAddr: ":3001", DataDir: dataDir})
	if err == nil {
		t.Fatal("expected startup validation to fail for a file path")
	}
}

func TestBuildSQLiteRuntimeMigratesTypedRecordsAndPublishesExactBackups(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	sources := map[string]string{
		"raw-materials.json":  "[ { \"id\": \"rm-1\", \"name\": \"Leather\", \"unit\": \"kg\", \"minStock\": 3 } ]\n",
		"finished-goods.json": "[ {\"id\":\"fg-1\",\"style\":\"Runner\",\"size\":\"40\",\"color\":\"blue\",\"stock\":4} ]",
		"packaging.json":      "[ { \"id\": \"pkg-x\", \"name\": \"Small box\", \"transportMode\": \"van\", \"maxUnits\": 8 } ]\n",
		"logistics.json":      "[ {\"id\":\"slot-x\",\"name\":\"Rack B\",\"zone\":\"Z2\",\"capacityUnits\":9} ]",
		"invoices.json":       "[ { \"id\": \"inv-x\", \"customerId\": \"cust-x\", \"total\": 12.5, \"tax\": 1, \"status\": \"draft\" } ]\n",
		"audit.json":          "[ {\"actor\":\"operator\", \"action\":\"create\", \"entity\":\"item\", \"at\":\"2025-01-01T00:00:00Z\"} ]\n",
		"sessions.json":       " legacy session bytes \n",
	}
	for name, contents := range sources {
		writeFile(t, filepath.Join(dataDir, name), contents)
	}

	runtime := buildRuntime(t, dataDir, nil)
	wantRecords := map[string]string{
		store.CollectionRawMaterials:  "rm-1",
		store.CollectionFinishedGoods: "fg-1",
		store.CollectionPackaging:     "pkg-x",
		store.CollectionLogistics:     "slot-x",
		store.CollectionInvoices:      "inv-x",
		store.CollectionAuditEvents:   "legacy-audit-000000",
	}
	for collection, id := range wantRecords {
		var count int
		if err := runtime.db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", collection, id).Scan(&count); err != nil {
			t.Fatalf("count imported %s/%s: %v", collection, id, err)
		}
		if count != 1 {
			t.Errorf("imported %s/%s count = %d, want 1", collection, id, count)
		}
	}
	auditEvents, err := runtime.auditStore.ListContext(context.Background())
	if err != nil {
		t.Fatalf("list audit events through SQLite audit adapter: %v", err)
	}
	if len(auditEvents) != 1 || auditEvents[0].Actor != "operator" {
		t.Fatalf("SQLite audit events = %#v, want imported operator event", auditEvents)
	}
	if runtime.transactionScope == nil {
		t.Fatal("SQLite transaction scope is nil")
	}
	var sessionCount int
	if err := runtime.db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", store.CollectionSessions).Scan(&sessionCount); err != nil {
		t.Fatalf("count imported sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("imported session count = %d, want 0", sessionCount)
	}

	for name, contents := range sources {
		backup, err := os.ReadFile(filepath.Join(runtime.backupDirectory, name))
		if err != nil {
			t.Fatalf("read backed-up %s: %v", name, err)
		}
		if string(backup) != contents {
			t.Errorf("backup %s = %q, want exact bytes %q", name, backup, contents)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(runtime.backupDirectory, store.BackupManifestFilename))
	if err != nil {
		t.Fatalf("read backup manifest: %v", err)
	}
	var entries struct {
		Files []struct {
			Name   string `json:"name"`
			Exists bool   `json:"exists"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifest, &entries); err != nil {
		t.Fatalf("decode backup manifest: %v", err)
	}
	if len(entries.Files) != len(sources) {
		t.Fatalf("manifest source count = %d, want %d", len(entries.Files), len(sources))
	}
}

func TestSQLiteRuntimeTransactionScopeCommitsMutationAndAudit(t *testing.T) {
	runtime := buildRuntime(t, filepath.Join(t.TempDir(), "data"), nil)
	item := packaging.PackagingSpec{
		ID: "pkg-transaction", Name: "Transactional box", TransportMode: "van", MaxUnits: 2,
		Dimensions: dimensionsForRuntimeTest(),
	}
	if err := runtime.transactionScope.Run(context.Background(), func(services server.TransactionServices) error {
		if _, err := services.Packaging.Upsert(item); err != nil {
			return err
		}
		return services.RecordAudit("operator", "create", item.ID)
	}); err != nil {
		t.Fatalf("run SQLite mutation and audit transaction: %v", err)
	}
	if got, err := runtime.modules.Packaging.Get(item.ID); err != nil || got.Name != item.Name {
		t.Fatalf("committed package = (%#v, %v), want saved item", got, err)
	}
	events, err := runtime.auditStore.ListContext(context.Background())
	if err != nil {
		t.Fatalf("list audit events after transaction: %v", err)
	}
	if len(events) != 1 || events[0].Entity != item.ID {
		t.Fatalf("committed audit events = %#v, want package audit event", events)
	}
}

func TestBuildSQLiteRuntimeUsesSeedsOnlyForMissingBusinessFiles(t *testing.T) {
	for _, test := range []struct {
		name       string
		filename   string
		collection string
		seedID     string
		writeEmpty bool
	}{
		{name: "missing packaging uses seed", filename: "packaging.json", collection: store.CollectionPackaging, seedID: "pkg-1"},
		{name: "empty packaging stays empty", filename: "packaging.json", collection: store.CollectionPackaging, writeEmpty: true},
		{name: "missing logistics uses seed", filename: "logistics.json", collection: store.CollectionLogistics, seedID: "slot-1"},
		{name: "empty logistics stays empty", filename: "logistics.json", collection: store.CollectionLogistics, writeEmpty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			if test.writeEmpty {
				writeFile(t, filepath.Join(dataDir, test.filename), "[]\n")
			}
			runtime := buildRuntime(t, dataDir, nil)
			var count int
			if err := runtime.db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", test.collection).Scan(&count); err != nil {
				t.Fatalf("count %s: %v", test.collection, err)
			}
			if test.seedID == "" {
				if count != 0 {
					t.Fatalf("%s count = %d, want 0 for valid empty array", test.collection, count)
				}
				return
			}
			if count != 1 {
				t.Fatalf("%s count = %d, want 1 missing-file seed", test.collection, count)
			}
			var seedCount int
			if err := runtime.db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ? AND id = ?", test.collection, test.seedID).Scan(&seedCount); err != nil {
				t.Fatalf("find seed %s: %v", test.seedID, err)
			}
			if seedCount != 1 {
				t.Fatalf("seed %s count = %d, want 1", test.seedID, seedCount)
			}
		})
	}
}

func TestBuildSQLiteRuntimeMalformedSnapshotLeavesDatabaseAbsent(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	writeFile(t, filepath.Join(dataDir, "raw-materials.json"), "[{\"id\":\"x\",\"unexpected\":true}]")

	if _, err := buildSQLiteRuntime(context.Background(), dataDir, nil); err == nil {
		t.Fatal("buildSQLiteRuntime returned nil error for a malformed legacy snapshot")
	}
	for _, path := range []string{
		filepath.Join(dataDir, sqliteDatabaseFilename),
		filepath.Join(dataDir, sqliteDatabaseFilename+"-wal"),
		filepath.Join(dataDir, sqliteDatabaseFilename+"-shm"),
		filepath.Join(dataDir, sqliteDatabaseFilename+"-journal"),
		filepath.Join(dataDir, sqliteBackupParentName, sqliteBackupDirectoryName),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("runtime artifact %q exists or stat failed before successful preflight: %v", path, err)
		}
	}
}

func TestBuildSQLiteRuntimeSkipsStaleJSONAfterCompletedMarker(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	writeFile(t, filepath.Join(dataDir, "raw-materials.json"), `[{"id":"rm-persist","name":"Leather","unit":"kg","minStock":1}]`)
	first := buildRuntime(t, dataDir, nil)
	if err := first.Close(); err != nil {
		t.Fatalf("close first runtime: %v", err)
	}
	writeFile(t, filepath.Join(dataDir, "raw-materials.json"), "not JSON")

	second := buildRuntime(t, dataDir, nil)
	items, err := second.modules.RawMaterials.List()
	if err != nil {
		t.Fatalf("list SQLite raw materials after completed migration: %v", err)
	}
	if len(items) != 1 || items[0].ID != "rm-persist" {
		t.Fatalf("SQLite data after completed migration = %#v, want persisted record", items)
	}
}

func TestBuildSQLiteRuntimeSharesSessionsAndDataAcrossInstancesAndRestart(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	legacySessions := `[{"token":"legacy-token","user":{"username":"old-user","role":"admin"}}]`
	writeFile(t, filepath.Join(dataDir, "sessions.json"), legacySessions)
	users := []auth.UserAccount{auth.NewUserAccount("admin", "secret", "operator")}

	first := buildRuntime(t, dataDir, users)
	if _, ok := first.sessions.Get("legacy-token"); ok {
		t.Fatal("legacy JSON session token remained valid after SQLite cutover")
	}
	token, user, ok := first.sessions.Login("admin", "secret")
	if !ok || token == "" || user.Username != "admin" {
		t.Fatalf("SQLite login = (%q, %#v, %v), want authenticated admin", token, user, ok)
	}
	item := packaging.PackagingSpec{
		ID: "pkg-shared", Name: "Shared box", TransportMode: "van", MaxUnits: 3,
		Dimensions: dimensionsForRuntimeTest(),
	}
	if _, err := first.modules.Packaging.Upsert(item); err != nil {
		t.Fatalf("save shared package: %v", err)
	}

	second := buildRuntime(t, dataDir, users)
	if got, ok := second.sessions.Get(token); !ok || got.Username != "admin" {
		t.Fatalf("second SQLite session store Get(%q) = (%#v, %v), want shared admin session", token, got, ok)
	}
	if got, err := second.modules.Packaging.Get("pkg-shared"); err != nil || got.Name != item.Name {
		t.Fatalf("second runtime package Get = (%#v, %v), want shared record", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first runtime: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second runtime: %v", err)
	}

	restarted := buildRuntime(t, dataDir, users)
	if got, err := restarted.modules.Packaging.Get("pkg-shared"); err != nil || got.Name != item.Name {
		t.Fatalf("restarted runtime package Get = (%#v, %v), want persisted record", got, err)
	}
	if got, ok := restarted.sessions.Get(token); !ok || got.Username != "admin" {
		t.Fatalf("restarted SQLite session Get(%q) = (%#v, %v), want persisted session", token, got, ok)
	}
}

func TestBuildSQLiteRuntimeUsesCanonicalPathsForDataDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "volume")
	alias := filepath.Join(root, "app-data")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("create canonical volume: %v", err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}

	runtime := buildRuntime(t, alias, nil)
	if runtime.dataDir != target {
		t.Fatalf("runtime data directory = %q, want resolved target %q", runtime.dataDir, target)
	}
	if runtime.databasePath != filepath.Join(target, sqliteDatabaseFilename) {
		t.Errorf("database path = %q, want canonical target path", runtime.databasePath)
	}
	if runtime.migrationLockPath != filepath.Join(target, sqliteMigrationLockFilename) {
		t.Errorf("migration lock path = %q, want canonical target path", runtime.migrationLockPath)
	}
	if runtime.backupDirectory != filepath.Join(target, sqliteBackupParentName, sqliteBackupDirectoryName) {
		t.Errorf("backup path = %q, want canonical target path", runtime.backupDirectory)
	}
	for _, path := range []string{runtime.databasePath, runtime.migrationLockPath, filepath.Join(runtime.backupDirectory, store.BackupManifestFilename)} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("canonical runtime artifact %q was not created: %v", path, err)
		}
	}
	info, err := os.Lstat(alias)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("configured directory alias was not preserved as a symlink: info=%v err=%v", info, err)
	}
}

func TestBuildSQLiteRuntimeRestrictsDataDatabaseBackupAndSidecarModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission modes are not supported on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDir, 0o755); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	writeFile(t, filepath.Join(dataDir, "raw-materials.json"), "[]\n")
	first := buildRuntime(t, dataDir, nil)
	item := packaging.PackagingSpec{
		ID: "pkg-permissions", Name: "Private box", TransportMode: "van", MaxUnits: 3,
		Dimensions: dimensionsForRuntimeTest(),
	}
	if _, err := first.modules.Packaging.Upsert(item); err != nil {
		t.Fatalf("write SQLite record to materialize WAL sidecars: %v", err)
	}
	assertPermissions(t, dataDir, 0o700)
	assertPermissions(t, first.databasePath, 0o600)
	assertPermissions(t, first.backupParent, 0o700)
	assertPermissions(t, first.backupDirectory, 0o700)
	assertPermissions(t, filepath.Join(first.backupDirectory, store.BackupManifestFilename), 0o600)
	assertPermissions(t, filepath.Join(first.backupDirectory, "raw-materials.json"), 0o600)
	assertPermissions(t, first.migrationLockPath, 0o600)

	for _, suffix := range []string{"-wal", "-shm"} {
		path := first.databasePath + suffix
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("SQLite sidecar %q was not created: %v", path, err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("make existing sidecar broadly accessible for test: %v", err)
		}
	}
	if err := os.Chmod(first.databasePath, 0o644); err != nil {
		t.Fatalf("make existing database broadly accessible for test: %v", err)
	}

	second := buildRuntime(t, dataDir, nil)
	assertPermissions(t, second.databasePath, 0o600)
	assertPermissions(t, second.databasePath+"-wal", 0o600)
	assertPermissions(t, second.databasePath+"-shm", 0o600)
}

func TestBuildSQLiteRuntimeRejectsSymlinkDatabasePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("database symlink protection test requires Unix symlinks")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	outside := filepath.Join(root, "outside.sqlite")
	writeFile(t, outside, "do not open")
	databasePath := filepath.Join(dataDir, sqliteDatabaseFilename)
	if err := os.Symlink(outside, databasePath); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}

	if _, err := buildSQLiteRuntime(context.Background(), dataDir, nil); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("buildSQLiteRuntime error = %v, want unsafe database symlink rejection", err)
	}
	contents, err := os.ReadFile(outside)
	if err != nil || string(contents) != "do not open" {
		t.Fatalf("external database target changed: contents=%q err=%v", contents, err)
	}
}

func buildRuntime(t *testing.T, dataDir string, users []auth.UserAccount) *sqliteRuntime {
	t.Helper()
	runtime, err := buildSQLiteRuntime(context.Background(), dataDir, users)
	if err != nil {
		t.Fatalf("build SQLite runtime: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close SQLite runtime: %v", err)
		}
	})
	return runtime
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("permissions for %s = %04o, want %04o", path, got, want)
	}
}

func dimensionsForRuntimeTest() domain.Dimensions {
	return domain.Dimensions{LengthCM: 10, WidthCM: 10, HeightCM: 10, WeightKG: 1}
}
