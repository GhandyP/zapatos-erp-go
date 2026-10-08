package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/auth"
	"zapatos-erp-go/internal/integrations/foxpro"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/server"
	"zapatos-erp-go/internal/store"
)

const (
	sqliteDatabaseFilename      = "zapatos.sqlite"
	sqliteMigrationID           = "legacy-json-v1"
	sqliteMigrationLockFilename = "migration.lock"
	sqliteBackupParentName      = "migration-backups"
	sqliteBackupDirectoryName   = sqliteMigrationID
)

type sqliteRuntime struct {
	db               *sql.DB
	modules          *server.Modules
	sessions         auth.SessionService
	auditStore       audit.Access
	transactionScope server.TransactionScope

	dataDir           string
	databasePath      string
	migrationLockPath string
	backupParent      string
	backupDirectory   string
}

// buildSQLiteRuntime completes the one-time JSON cutover before constructing
// the adapters used by the HTTP server. The caller owns and must close runtime.
func buildSQLiteRuntime(ctx context.Context, configuredDataDir string, users []auth.UserAccount) (*sqliteRuntime, error) {
	if ctx == nil {
		return nil, errors.New("SQLite runtime context must not be nil")
	}
	if strings.TrimSpace(configuredDataDir) == "" {
		return nil, errors.New("data directory is required")
	}
	if err := setPrivateFileCreationMask(); err != nil {
		return nil, fmt.Errorf("enable private SQLite file creation: %w", err)
	}

	dataDir, err := canonicalPrivateDataDirectory(configuredDataDir)
	if err != nil {
		return nil, err
	}
	backupParent := filepath.Join(dataDir, sqliteBackupParentName)
	if err := preparePrivateDirectory(backupParent); err != nil {
		return nil, fmt.Errorf("prepare SQLite migration backup parent: %w", err)
	}

	databasePath := filepath.Join(dataDir, sqliteDatabaseFilename)
	migrationLockPath := filepath.Join(dataDir, sqliteMigrationLockFilename)
	if err := validateSQLiteRuntimePaths(databasePath, migrationLockPath); err != nil {
		return nil, err
	}
	if err := secureOptionalPrivateFile(migrationLockPath); err != nil {
		return nil, fmt.Errorf("secure SQLite migration lock: %w", err)
	}

	backupDirectory := filepath.Join(backupParent, sqliteBackupDirectoryName)
	gate, err := store.RunSQLiteJSONImportGate(store.SQLiteJSONImportGateOptions{
		Context:         ctx,
		DatabasePath:    databasePath,
		LockFilePath:    migrationLockPath,
		MigrationID:     sqliteMigrationID,
		BackupDirectory: backupDirectory,
		Prepare: func() (store.SQLiteJSONImportPreparation, error) {
			return prepareSQLiteJSONImport(dataDir)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("run SQLite JSON cutover: %w", err)
	}
	if gate.Database == nil {
		return nil, errors.New("SQLite JSON cutover returned no database")
	}
	if err := secureSQLiteRuntimeFiles(databasePath); err != nil {
		closeErr := gate.Database.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SQLite database after permission failure: %w", closeErr))
		}
		return nil, fmt.Errorf("secure SQLite runtime files: %w", err)
	}
	if err := secureOptionalPrivateFile(migrationLockPath); err != nil {
		closeErr := gate.Database.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SQLite database after lock permission failure: %w", closeErr))
		}
		return nil, fmt.Errorf("secure SQLite migration lock: %w", err)
	}

	modules := buildSQLiteModules(gate.Database)
	return &sqliteRuntime{
		db:                gate.Database,
		modules:           modules,
		sessions:          auth.NewSQLiteSessionStore(gate.Database, users),
		auditStore:        audit.NewSQLiteStore(gate.Database),
		transactionScope:  server.NewSQLiteTransactionScope(gate.Database),
		dataDir:           dataDir,
		databasePath:      databasePath,
		migrationLockPath: migrationLockPath,
		backupParent:      backupParent,
		backupDirectory:   backupDirectory,
	}, nil
}

func (r *sqliteRuntime) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func canonicalPrivateDataDirectory(configuredDataDir string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(configuredDataDir))
	if err != nil {
		return "", fmt.Errorf("resolve data directory path: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve canonical data directory: %w", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("make canonical data directory absolute: %w", err)
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect canonical data directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("canonical data path %q is not a directory", canonical)
	}
	if err := os.Chmod(canonical, 0o700); err != nil {
		return "", fmt.Errorf("secure canonical data directory: %w", err)
	}
	return filepath.Clean(canonical), nil
}

func preparePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create directory %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("path %q is not a regular directory", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set directory %q permissions: %w", path, err)
	}
	return nil
}

func validateSQLiteRuntimePaths(databasePath, migrationLockPath string) error {
	for _, path := range []string{
		databasePath,
		databasePath + "-wal",
		databasePath + "-shm",
		databasePath + "-journal",
		migrationLockPath,
	} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect SQLite runtime path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("SQLite runtime path %q must not be a symbolic link", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("SQLite runtime path %q is not a regular file", path)
		}
	}
	return nil
}

func secureOptionalPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("file %q is not a regular non-symlink file", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set file %q permissions: %w", path, err)
	}
	return nil
}

func secureSQLiteRuntimeFiles(databasePath string) error {
	if err := secureOptionalPrivateFile(databasePath); err != nil {
		return fmt.Errorf("secure SQLite database: %w", err)
	}
	if _, err := os.Lstat(databasePath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("SQLite database %q was not created", databasePath)
	} else if err != nil {
		return fmt.Errorf("inspect SQLite database %q: %w", databasePath, err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := secureOptionalPrivateFile(databasePath + suffix); err != nil {
			return fmt.Errorf("secure SQLite %s file: %w", suffix, err)
		}
	}
	return nil
}

func prepareSQLiteJSONImport(dataDir string) (store.SQLiteJSONImportPreparation, error) {
	rawMaterials, err := store.PreflightJSONSnapshot(dataDir, "raw-materials.json", store.CollectionRawMaterials,
		[]rawmaterials.RawMaterial{}, func(item rawmaterials.RawMaterial) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	finishedGoods, err := store.PreflightJSONSnapshot(dataDir, "finished-goods.json", store.CollectionFinishedGoods,
		[]finishedgoods.FinishedProductVariant{}, func(item finishedgoods.FinishedProductVariant) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	packagingItems, err := store.PreflightJSONSnapshot(dataDir, "packaging.json", store.CollectionPackaging,
		defaultPackagingSeeds(), func(item packaging.PackagingSpec) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	logisticsItems, err := store.PreflightJSONSnapshot(dataDir, "logistics.json", store.CollectionLogistics,
		defaultLogisticsSeeds(), func(item logistics.StorageSlot) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	invoices, err := store.PreflightJSONSnapshot(dataDir, "invoices.json", store.CollectionInvoices,
		[]billing.Invoice{}, func(item billing.Invoice) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	auditIndex := 0
	auditEvents, err := store.PreflightJSONSnapshot(dataDir, "audit.json", store.CollectionAuditEvents,
		[]audit.Event{}, func(audit.Event) string {
			id := legacyAuditID(auditIndex)
			auditIndex++
			return id
		})
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}

	rawRecords, err := encodeJSONImportRecords(rawMaterials.Items, func(item rawmaterials.RawMaterial, _ int) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	finishedRecords, err := encodeJSONImportRecords(finishedGoods.Items, func(item finishedgoods.FinishedProductVariant, _ int) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	packagingRecords, err := encodeJSONImportRecords(packagingItems.Items, func(item packaging.PackagingSpec, _ int) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	logisticsRecords, err := encodeJSONImportRecords(logisticsItems.Items, func(item logistics.StorageSlot, _ int) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	invoiceRecords, err := encodeJSONImportRecords(invoices.Items, func(item billing.Invoice, _ int) string { return item.ID })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	auditRecords, err := encodeJSONImportRecords(auditEvents.Items, func(_ audit.Event, index int) string { return legacyAuditID(index) })
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}

	plan := store.JSONImportPlan{Collections: []store.JSONImportCollection{
		{Name: store.CollectionRawMaterials, Records: rawRecords},
		{Name: store.CollectionFinishedGoods, Records: finishedRecords},
		{Name: store.CollectionPackaging, Records: packagingRecords},
		{Name: store.CollectionLogistics, Records: logisticsRecords},
		{Name: store.CollectionInvoices, Records: invoiceRecords},
		{Name: store.CollectionAuditEvents, Records: auditRecords},
	}}
	backupSources := []store.RawBackupSource{
		snapshotBackupSource("raw-materials.json", rawMaterials),
		snapshotBackupSource("finished-goods.json", finishedGoods),
		snapshotBackupSource("packaging.json", packagingItems),
		snapshotBackupSource("logistics.json", logisticsItems),
		snapshotBackupSource("invoices.json", invoices),
		snapshotBackupSource("audit.json", auditEvents),
	}
	sessionsSource, exists, err := readOptionalJSONSource(dataDir, "sessions.json")
	if err != nil {
		return store.SQLiteJSONImportPreparation{}, err
	}
	if exists {
		backupSources = append(backupSources, sessionsSource)
	}
	return store.SQLiteJSONImportPreparation{
		Plan:          plan,
		BackupSources: backupSources,
	}, nil
}

func encodeJSONImportRecords[T any](items []T, idSelector func(T, int) string) ([]store.JSONImportRecord, error) {
	records := make([]store.JSONImportRecord, 0, len(items))
	for index, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("encode imported record %d: %w", index, err)
		}
		records = append(records, store.JSONImportRecord{
			ID:      idSelector(item, index),
			Payload: json.RawMessage(payload),
		})
	}
	return records, nil
}

func snapshotBackupSource[T any](name string, snapshot store.JSONSnapshot[T]) store.RawBackupSource {
	return store.RawBackupSource{
		Name:   name,
		Data:   append([]byte(nil), snapshot.OriginalBytes...),
		Exists: snapshot.FileExisted,
	}
}

func readOptionalJSONSource(dataDir, name string) (store.RawBackupSource, bool, error) {
	path := filepath.Join(dataDir, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return store.RawBackupSource{Name: name}, false, nil
	}
	if err != nil {
		return store.RawBackupSource{}, false, fmt.Errorf("inspect legacy source %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return store.RawBackupSource{}, false, fmt.Errorf("legacy source %q is not a regular non-symlink file", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store.RawBackupSource{}, false, fmt.Errorf("read legacy source %q: %w", name, err)
	}
	return store.RawBackupSource{Name: name, Data: data, Exists: true}, true, nil
}

func legacyAuditID(index int) string {
	return fmt.Sprintf("legacy-audit-%06d", index)
}

func buildSQLiteModules(db *sql.DB) *server.Modules {
	rawRepo := store.NewSQLiteRepository(db, store.CollectionRawMaterials,
		func(item rawmaterials.RawMaterial) string { return item.ID })
	finishedRepo := store.NewSQLiteRepository(db, store.CollectionFinishedGoods,
		func(item finishedgoods.FinishedProductVariant) string { return item.ID })
	packagingRepo := store.NewSQLiteRepository(db, store.CollectionPackaging,
		func(item packaging.PackagingSpec) string { return item.ID })
	logisticsRepo := store.NewSQLiteRepository(db, store.CollectionLogistics,
		func(item logistics.StorageSlot) string { return item.ID })
	invoiceRepo := store.NewSQLiteRepository(db, store.CollectionInvoices,
		func(item billing.Invoice) string { return item.ID })

	rawService := rawmaterials.New(rawRepo)
	finishedService := finishedgoods.New(finishedRepo)
	packagingService := packaging.New(packagingRepo)
	logisticsService := logistics.New(logisticsRepo)
	billingService := billing.New(invoiceRepo)
	foxproAdapter := foxpro.New(billingService)
	return &server.Modules{
		RawMaterials:  rawService,
		FinishedGoods: finishedService,
		Packaging:     packagingService,
		Logistics:     logisticsService,
		Billing:       billingService,
		Foxpro:        foxproAdapter,
	}
}
