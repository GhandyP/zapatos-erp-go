package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLiteUnitOfWorkRollsBackWhenCallbackFails(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	callbackErr := errors.New("business operation failed")
	uow := NewSQLiteUnitOfWork(db)
	err = uow.Run(context.Background(), func(tx *sql.Tx) error {
		repo := NewSQLiteRepository(tx, "widgets", func(v testRecord) string { return v.ID })
		if _, err := repo.Save(testRecord{ID: "rolled-back", Name: "Temporary"}); err != nil {
			return err
		}
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("Run error = %v, want callback error %v", err, callbackErr)
	}

	repo := NewSQLiteRepository(db, "widgets", func(v testRecord) string { return v.ID })
	if _, found, err := repo.FindByID("rolled-back"); err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	} else if found {
		t.Fatal("repository write persisted after callback failure")
	}
}

func TestSQLiteUnitOfWorkPropagatesDeferredConstraintCommitFailureAndRollsBack(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE required_parent (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create parent table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE deferred_child (
		parent_id INTEGER NOT NULL,
		FOREIGN KEY (parent_id) REFERENCES required_parent(id) DEFERRABLE INITIALLY DEFERRED
	)`); err != nil {
		t.Fatalf("create child table: %v", err)
	}

	uow := NewSQLiteUnitOfWork(db)
	callbackCompleted := false
	err = uow.Run(context.Background(), func(tx *sql.Tx) error {
		repo := NewSQLiteRepository(tx, "widgets", func(v testRecord) string { return v.ID })
		if _, err := repo.Save(testRecord{ID: "commit-failure", Name: "Temporary"}); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO deferred_child (parent_id) VALUES (?)", 404); err != nil {
			return err
		}
		callbackCompleted = true
		return nil
	})
	if !callbackCompleted {
		t.Fatal("callback did not complete; expected the deferred constraint to fail at commit")
	}
	if err == nil {
		t.Fatal("Run returned nil, want deferred-constraint commit error")
	}

	repo := NewSQLiteRepository(db, "widgets", func(v testRecord) string { return v.ID })
	if _, found, err := repo.FindByID("commit-failure"); err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	} else if found {
		t.Fatal("business write persisted after commit failure")
	}
	var childCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM deferred_child").Scan(&childCount); err != nil {
		t.Fatalf("count deferred child rows: %v", err)
	}
	if childCount != 0 {
		t.Fatalf("deferred_child row count = %d, want 0 after failed commit", childCount)
	}
}

func TestSQLiteUnitOfWorkDoesNotRunCallbackWithCanceledContext(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err = NewSQLiteUnitOfWork(db).Run(ctx, func(*sql.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("Run invoked callback with a canceled context")
	}
}

const rollbackErrorDriverName = "store-unit-of-work-rollback-error-test"

var (
	rollbackErrorDriverOnce sync.Once
	rollbackErrorSentinel   = errors.New("driver rollback failed")
)

type rollbackErrorDriver struct{}

func (rollbackErrorDriver) Open(string) (driver.Conn, error) {
	return rollbackErrorConn{}, nil
}

type rollbackErrorConn struct{}

func (rollbackErrorConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare is not supported")
}

func (rollbackErrorConn) Close() error { return nil }

func (rollbackErrorConn) Begin() (driver.Tx, error) {
	return rollbackErrorTx{}, nil
}

func (rollbackErrorConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return rollbackErrorTx{}, nil
}

type rollbackErrorTx struct{}

func (rollbackErrorTx) Commit() error   { return nil }
func (rollbackErrorTx) Rollback() error { return rollbackErrorSentinel }

func TestSQLiteUnitOfWorkReturnsCallbackAndRollbackErrors(t *testing.T) {
	rollbackErrorDriverOnce.Do(func() {
		sql.Register(rollbackErrorDriverName, rollbackErrorDriver{})
	})
	db, err := sql.Open(rollbackErrorDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	callbackErr := errors.New("callback failed")
	err = NewSQLiteUnitOfWork(db).Run(context.Background(), func(*sql.Tx) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Errorf("Run error = %v, want callback error %v", err, callbackErr)
	}
	if !errors.Is(err, rollbackErrorSentinel) {
		t.Errorf("Run error = %v, want rollback error %v", err, rollbackErrorSentinel)
	}
}
