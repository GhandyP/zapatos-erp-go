package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SQLiteJSONImportPreparation contains the validated import plan and the exact
// source bytes to publish in the immutable backup before opening the database.
type SQLiteJSONImportPreparation struct {
	Plan          JSONImportPlan
	BackupSources []RawBackupSource
}

// SQLiteJSONImportGateOptions configures the ordered, host-locked JSON import
// gate. Prepare is called only when the requested migration is not complete.
type SQLiteJSONImportGateOptions struct {
	Context         context.Context
	DatabasePath    string
	LockFilePath    string
	MigrationID     string
	BackupDirectory string
	Prepare         func() (SQLiteJSONImportPreparation, error)
}

// SQLiteJSONImportGateResult contains the import outcome and the still-open
// database handle for the caller. A completed migration skips preparation but
// still returns an open Database for serving requests.
type SQLiteJSONImportGateResult struct {
	Database *sql.DB
	Import   JSONImportResult
}

// RunSQLiteJSONImportGate probes, prepares, backs up, and imports in that
// order while holding the host-local migration lock. The database is opened
// only after the exact backup has been published. The caller owns Database on
// success and must close it.
func RunSQLiteJSONImportGate(options SQLiteJSONImportGateOptions) (result SQLiteJSONImportGateResult, err error) {
	err = WithMigrationLock(options.Context, options.LockFilePath, func() error {
		state, err := ProbeSQLiteJSONImportState(options.DatabasePath, options.MigrationID)
		if err != nil {
			return fmt.Errorf("probe SQLite JSON import state: %w", err)
		}
		if state.Completed {
			db, err := OpenSQLite(options.DatabasePath)
			if err != nil {
				return fmt.Errorf("open SQLite database after completed import: %w", err)
			}
			result.Database = db
			result.Import = JSONImportResult{Skipped: true}
			return nil
		}
		if options.Prepare == nil {
			return errors.New("SQLite JSON import preparation callback must not be nil")
		}

		preparation, err := options.Prepare()
		if err != nil {
			return fmt.Errorf("prepare SQLite JSON import: %w", err)
		}
		if _, err := validateJSONImportPlan(preparation.Plan); err != nil {
			return fmt.Errorf("validate SQLite JSON import plan: %w", err)
		}
		if err := CreateImmutableBackup(options.BackupDirectory, preparation.BackupSources); err != nil {
			return fmt.Errorf("publish immutable JSON import backup: %w", err)
		}

		db, err := OpenSQLite(options.DatabasePath)
		if err != nil {
			return fmt.Errorf("open SQLite database after backup publication: %w", err)
		}
		closeAfterFailure := func(cause error) error {
			if closeErr := db.Close(); closeErr != nil {
				return fmt.Errorf("%w (close SQLite database after failure: %v)", cause, closeErr)
			}
			return cause
		}

		importResult, err := ImportJSONOnce(db, options.MigrationID, func() (JSONImportPlan, error) {
			return preparation.Plan, nil
		})
		if err != nil {
			return closeAfterFailure(fmt.Errorf("import prepared SQLite JSON plan: %w", err))
		}
		result.Database = db
		result.Import = importResult
		return nil
	})
	if err != nil {
		if result.Database != nil {
			if closeErr := result.Database.Close(); closeErr != nil {
				err = fmt.Errorf("%w (close SQLite database after migration-lock failure: %v)", err, closeErr)
			}
		}
		result = SQLiteJSONImportGateResult{}
	}
	return result, err
}
