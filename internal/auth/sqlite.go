package auth

import (
	"database/sql"
	"encoding/json"

	"golang.org/x/crypto/bcrypt"

	"zapatos-erp-go/internal/store"
)

// SQLiteSessionStore stores session users in the shared generic SQLite records
// table. It does not create schema or import the legacy JSON session file.
type SQLiteSessionStore struct {
	db    *sql.DB
	users []UserAccount
}

var _ SessionService = (*SQLiteSessionStore)(nil)

// NewSQLiteSessionStore creates a session store backed by the caller's existing
// generic SQLite records table.
func NewSQLiteSessionStore(db *sql.DB, users []UserAccount) *SQLiteSessionStore {
	return &SQLiteSessionStore{db: db, users: users}
}

func (s *SQLiteSessionStore) Login(username, password string) (string, SessionUser, bool) {
	if s == nil || s.db == nil {
		return "", SessionUser{}, false
	}
	for _, user := range s.users {
		if user.Username != username || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			continue
		}

		token := "sess_" + randomHex(16)
		account := SessionUser{Username: user.Username, Role: user.Role}
		payload, err := json.Marshal(account)
		if err != nil {
			return "", SessionUser{}, false
		}
		_, err = s.db.Exec(
			`INSERT INTO store_records (collection, id, payload) VALUES (?, ?, ?)
			ON CONFLICT (collection, id) DO UPDATE SET payload = excluded.payload`,
			store.CollectionSessions,
			token,
			string(payload),
		)
		if err != nil {
			return "", SessionUser{}, false
		}
		return token, account, true
	}
	return "", SessionUser{}, false
}

func (s *SQLiteSessionStore) Get(token string) (SessionUser, bool) {
	if s == nil || s.db == nil || token == "" {
		return SessionUser{}, false
	}

	var payload string
	if err := s.db.QueryRow(
		"SELECT payload FROM store_records WHERE collection = ? AND id = ?",
		store.CollectionSessions,
		token,
	).Scan(&payload); err != nil {
		return SessionUser{}, false
	}

	var user SessionUser
	if err := json.Unmarshal([]byte(payload), &user); err != nil || user.Username == "" {
		return SessionUser{}, false
	}
	return user, true
}
