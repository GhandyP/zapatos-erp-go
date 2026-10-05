package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS store_records (
	collection TEXT NOT NULL,
	id TEXT NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY (collection, id)
)`

// SQLiteExecutor is implemented by both *sql.DB and *sql.Tx.
type SQLiteExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// OpenSQLite opens a SQLite database, configures its connection, and ensures
// the generic repository schema exists.
func OpenSQLite(path string) (*sql.DB, error) {
	// modernc applies _pragma to every new connection, unlike a PRAGMA Exec
	// through database/sql, which only configures the connection it checks out.
	dsn := path
	if strings.Contains(dsn, "?") {
		dsn += "&"
	} else {
		dsn += "?"
	}
	dsn += "_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	// A single connection serializes work within this process. SQLite still
	// coordinates other processes through its database locks.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure SQLite WAL mode: %w", err)
	}
	if err := EnsureSQLiteSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// EnsureSQLiteSchema creates the shared table used by typed repositories.
func EnsureSQLiteSchema(executor SQLiteExecutor) error {
	if _, err := executor.Exec(sqliteSchema); err != nil {
		return fmt.Errorf("ensure SQLite schema: %w", err)
	}
	return nil
}

var _ Repository[any] = (*sqliteRepository[any])(nil)

type sqliteRepository[T any] struct {
	executor   SQLiteExecutor
	collection string
	idFn       func(T) string
}

// NewSQLiteRepository creates a typed view over one collection in the shared
// SQLite records table. Its executor can be either a *sql.DB or a *sql.Tx.
func NewSQLiteRepository[T any](executor SQLiteExecutor, collection string, idFn func(T) string) Repository[T] {
	return &sqliteRepository[T]{executor: executor, collection: collection, idFn: idFn}
}

func (r *sqliteRepository[T]) List() ([]T, error) {
	rows, err := r.executor.Query(
		"SELECT payload FROM store_records WHERE collection = ? ORDER BY id",
		r.collection,
	)
	if err != nil {
		return nil, fmt.Errorf("list SQLite records: %w", err)
	}
	defer rows.Close()

	items := make([]T, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan SQLite record: %w", err)
		}
		var item T
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, fmt.Errorf("decode SQLite record: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SQLite records: %w", err)
	}
	return items, nil
}

func (r *sqliteRepository[T]) FindByID(id string) (T, bool, error) {
	var zero T
	var payload string
	err := r.executor.QueryRow(
		"SELECT payload FROM store_records WHERE collection = ? AND id = ?",
		r.collection,
		id,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("find SQLite record: %w", err)
	}

	var item T
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return zero, false, fmt.Errorf("decode SQLite record: %w", err)
	}
	return item, true, nil
}

func (r *sqliteRepository[T]) Save(entity T) (T, error) {
	payload, err := json.Marshal(entity)
	if err != nil {
		return entity, fmt.Errorf("encode SQLite record: %w", err)
	}
	_, err = r.executor.Exec(
		`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)
		ON CONFLICT (collection, id) DO UPDATE SET payload = excluded.payload`,
		r.collection,
		r.idFn(entity),
		string(payload),
	)
	if err != nil {
		return entity, fmt.Errorf("save SQLite record: %w", err)
	}
	return entity, nil
}

func (r *sqliteRepository[T]) Delete(id string) (bool, error) {
	result, err := r.executor.Exec(
		"DELETE FROM store_records WHERE collection = ? AND id = ?",
		r.collection,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("delete SQLite record: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count deleted SQLite records: %w", err)
	}
	return deleted > 0, nil
}
