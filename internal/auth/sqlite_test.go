package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zapatos-erp-go/internal/store"
)

func TestSQLiteSessionStoreSharesSessionsAcrossDatabaseHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.sqlite")
	firstDB, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open first SQLite handle: %v", err)
	}
	defer firstDB.Close()

	secondDB, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open second SQLite handle: %v", err)
	}
	defer secondDB.Close()

	users := []UserAccount{NewUserAccount("admin", "admin123", "administrador")}
	first := NewSQLiteSessionStore(firstDB, users)
	second := NewSQLiteSessionStore(secondDB, users)
	token, wantUser, ok := first.Login("admin", "admin123")
	if !ok {
		t.Fatal("expected SQLite login to succeed")
	}
	if !strings.HasPrefix(token, "sess_") || len(token) != len("sess_")+32 {
		t.Fatalf("token = %q, want sess_ followed by 32 hex characters", token)
	}

	gotUser, ok := second.Get(token)
	if !ok {
		t.Fatal("session created through the first handle was not visible through the second handle")
	}
	if gotUser != wantUser {
		t.Fatalf("session user = %+v, want %+v", gotUser, wantUser)
	}
}

func TestSQLiteSessionStoreRejectsUnknownTokens(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "sessions.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db, nil)
	for _, token := range []string{"unknown-token", ""} {
		if user, ok := sessions.Get(token); ok {
			t.Errorf("Get(%q) = (%+v, true), want an unknown session", token, user)
		}
	}
}

func TestSQLiteSessionStoreDoesNotImportLegacySessionFile(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "sessions.json")
	users := []UserAccount{NewUserAccount("admin", "admin123", "administrador")}
	legacy := NewSessionStore(legacyPath, users)
	token, legacyUser, ok := legacy.Login("admin", "admin123")
	if !ok {
		t.Fatal("expected legacy JSON login to succeed")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy sessions file was not created: %v", err)
	}

	db, err := store.OpenSQLite(filepath.Join(root, "sessions.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	legacyReload := NewSessionStore(legacyPath, users)
	if got, ok := legacyReload.Get(token); !ok || got != legacyUser {
		t.Fatalf("legacy JSON session = (%+v, %v), want (%+v, true)", got, ok, legacyUser)
	}
	if got, ok := NewSQLiteSessionStore(db, users).Get(token); ok {
		t.Fatalf("SQLite store imported legacy token for %+v", got)
	}
}

func TestSQLiteSessionStoreFailsClosedOnStorageErrors(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "sessions.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db, []UserAccount{NewUserAccount("admin", "admin123", "administrador")})
	if _, err := db.Exec(`CREATE TRIGGER reject_session_writes BEFORE INSERT ON store_records
		WHEN NEW.collection = 'sessions'
		BEGIN SELECT RAISE(ABORT, 'injected session storage error'); END`); err != nil {
		t.Fatalf("create write-failure trigger: %v", err)
	}
	if token, user, ok := sessions.Login("admin", "admin123"); ok || token != "" || user != (SessionUser{}) {
		t.Fatalf("Login on a failing store = (%q, %+v, %v), want empty token, zero user, false", token, user, ok)
	}

	if _, err := db.Exec("DROP TRIGGER reject_session_writes"); err != nil {
		t.Fatalf("drop write-failure trigger: %v", err)
	}
	token, _, ok := sessions.Login("admin", "admin123")
	if !ok {
		t.Fatal("expected login to succeed after removing the injected write failure")
	}
	if _, err := db.Exec("DROP TABLE store_records"); err != nil {
		t.Fatalf("drop generic records table to inject read failure: %v", err)
	}
	if user, ok := sessions.Get(token); ok {
		t.Fatalf("Get after a storage error = (%+v, true), want fail closed", user)
	}
}
