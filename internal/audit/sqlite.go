package audit

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const sqliteAuditLimit = 500

// SQLiteWriter stores events in the generic records table through the caller's
// transaction, so audit and business writes can commit or roll back together.
type SQLiteWriter struct {
	tx         *sql.Tx
	collection string
}

// NewSQLiteWriter creates an audit writer bound to tx and collection.
func NewSQLiteWriter(tx *sql.Tx, collection string) *SQLiteWriter {
	return &SQLiteWriter{tx: tx, collection: collection}
}

// Write inserts event with the caller-selected id and retains at most the 500
// newest events in the configured collection, ordered by event timestamp.
func (w *SQLiteWriter) Write(id string, event Event) error {
	if w == nil || w.tx == nil {
		return errors.New("SQLite audit writer has no transaction")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode SQLite audit event: %w", err)
	}
	if _, err := w.tx.Exec(
		"INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)",
		w.collection,
		id,
		string(payload),
	); err != nil {
		return fmt.Errorf("insert SQLite audit event: %w", err)
	}
	if err := w.retainNewest(); err != nil {
		return fmt.Errorf("retain SQLite audit events: %w", err)
	}
	return nil
}

// Record creates a timestamped event with a collision-resistant ID and inserts
// it without upsert using the writer's transaction.
func (w *SQLiteWriter) Record(actor, action, entity string) (Event, error) {
	id, err := generateSQLiteAuditID()
	if err != nil {
		return Event{}, fmt.Errorf("generate SQLite audit ID: %w", err)
	}
	event := Event{
		Actor: actor, Action: action, Entity: entity,
		At: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := w.Write(id, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func generateSQLiteAuditID() (string, error) {
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return "", err
	}
	return "aud_" + hex.EncodeToString(idBytes[:]), nil
}

type sqliteAuditRecord struct {
	id      string
	at      string
	time    time.Time
	hasTime bool
}

func (w *SQLiteWriter) retainNewest() error {
	rows, err := w.tx.Query(
		"SELECT id, payload FROM store_records WHERE collection = ?",
		w.collection,
	)
	if err != nil {
		return fmt.Errorf("query SQLite audit events: %w", err)
	}

	records := make([]sqliteAuditRecord, 0, sqliteAuditLimit+1)
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan SQLite audit event: %w", err)
		}
		var event Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			_ = rows.Close()
			return fmt.Errorf("decode SQLite audit event %q: %w", id, err)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, event.At)
		records = append(records, sqliteAuditRecord{
			id:      id,
			at:      event.At,
			time:    timestamp,
			hasTime: err == nil,
		})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate SQLite audit events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close SQLite audit event query: %w", err)
	}
	if len(records) <= sqliteAuditLimit {
		return nil
	}

	// Invalid timestamps are ordered before valid timestamps so malformed old
	// records cannot displace well-formed, newer events from the retained set.
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i], records[j]
		if left.hasTime != right.hasTime {
			return !left.hasTime
		}
		if left.hasTime && !left.time.Equal(right.time) {
			return left.time.Before(right.time)
		}
		if left.at != right.at {
			return left.at < right.at
		}
		return left.id < right.id
	})

	for _, record := range records[:len(records)-sqliteAuditLimit] {
		if _, err := w.tx.Exec(
			"DELETE FROM store_records WHERE collection = ? AND id = ?",
			w.collection,
			record.id,
		); err != nil {
			return fmt.Errorf("delete expired SQLite audit event %q: %w", record.id, err)
		}
	}
	return nil
}
