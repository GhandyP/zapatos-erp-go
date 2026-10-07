package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"zapatos-erp-go/internal/store"
)

// SQLiteStore provides context-aware audit access through the shared records
// table. Standalone writes use their own unit of work.
type SQLiteStore struct {
	db          *sql.DB
	idGenerator func() (string, error)
}

var _ Access = (*SQLiteStore)(nil)

// NewSQLiteStore creates audit access backed by db.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db, idGenerator: generateSQLiteAuditID}
}

// RecordContext inserts an audit event in its own SQLite transaction. When
// called after an external operation, that operation is not part of this unit
// of work.
func (s *SQLiteStore) RecordContext(ctx context.Context, actor, action, entity string) (Event, error) {
	if s == nil || s.db == nil {
		return Event{}, errors.New("SQLite audit store has no database")
	}
	if ctx == nil {
		return Event{}, errors.New("record SQLite audit event: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	newID := s.idGenerator
	if newID == nil {
		newID = generateSQLiteAuditID
	}
	id, err := newID()
	if err != nil {
		return Event{}, fmt.Errorf("generate SQLite audit ID: %w", err)
	}
	event := Event{
		Actor: actor, Action: action, Entity: entity,
		At: time.Now().UTC().Format(time.RFC3339Nano),
	}
	err = store.NewSQLiteUnitOfWork(s.db).Run(ctx, func(tx *sql.Tx) error {
		return NewSQLiteWriter(tx, store.CollectionAuditEvents).Write(id, event)
	})
	if err != nil {
		return Event{}, fmt.Errorf("record SQLite audit event: %w", err)
	}
	return event, nil
}

// ListContext returns events in chronological order. Equal timestamps are
// ordered by timestamp text and then record ID for stable results.
func (s *SQLiteStore) ListContext(ctx context.Context) ([]Event, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("SQLite audit store has no database")
	}
	if ctx == nil {
		return nil, errors.New("list SQLite audit events: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, payload FROM store_records WHERE collection = ?",
		store.CollectionAuditEvents,
	)
	if err != nil {
		return nil, fmt.Errorf("query SQLite audit events: %w", err)
	}
	defer rows.Close()

	records := make([]sqliteListedAuditRecord, 0)
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, fmt.Errorf("scan SQLite audit event: %w", err)
		}
		var event Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("decode SQLite audit event %q: %w", id, err)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, event.At)
		records = append(records, sqliteListedAuditRecord{
			id: id, event: event, timestamp: timestamp, hasTimestamp: err == nil,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SQLite audit events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close SQLite audit event query: %w", err)
	}

	sort.Slice(records, func(i, j int) bool {
		left, right := records[i], records[j]
		if left.hasTimestamp != right.hasTimestamp {
			return !left.hasTimestamp
		}
		if left.hasTimestamp && !left.timestamp.Equal(right.timestamp) {
			return left.timestamp.Before(right.timestamp)
		}
		if left.event.At != right.event.At {
			return left.event.At < right.event.At
		}
		return left.id < right.id
	})

	events := make([]Event, len(records))
	for i := range records {
		events[i] = records[i].event
	}
	return events, nil
}

type sqliteListedAuditRecord struct {
	id           string
	event        Event
	timestamp    time.Time
	hasTimestamp bool
}
