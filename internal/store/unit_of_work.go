package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SQLiteUnitOfWork runs a complete operation inside one SQLite transaction.
type SQLiteUnitOfWork struct {
	db *sql.DB
}

// NewSQLiteUnitOfWork creates a transaction runner for db.
func NewSQLiteUnitOfWork(db *sql.DB) *SQLiteUnitOfWork {
	return &SQLiteUnitOfWork{db: db}
}

// Run executes operation in a context-bound transaction. Callback errors are
// returned after rolling back; commit errors are returned to the caller.
func (u *SQLiteUnitOfWork) Run(ctx context.Context, operation func(*sql.Tx) error) (runErr error) {
	if u == nil || u.db == nil {
		return errors.New("SQLite unit of work has no database")
	}
	if operation == nil {
		return errors.New("SQLite unit of work has no operation")
	}

	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			runErr = errors.Join(runErr, fmt.Errorf("rollback SQLite transaction: %w", rollbackErr))
		}
	}()

	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite transaction: %w", err)
	}
	return nil
}
