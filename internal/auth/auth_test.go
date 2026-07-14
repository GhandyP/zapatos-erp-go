package auth

import (
	"path/filepath"
	"testing"
)

func TestNewUserAccountHashesPassword(t *testing.T) {
	acc := NewUserAccount("admin", "secret123", "administrador")
	if acc.PasswordHash == "" {
		t.Fatal("expected password hash to be set")
	}
	if acc.PasswordHash == "secret123" {
		t.Fatal("password should be hashed, not plaintext")
	}
	if acc.Username != "admin" || acc.Role != "administrador" {
		t.Fatalf("unexpected account: %+v", acc)
	}
}

func TestLoginSuccess(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	token, user, ok := sessions.Login("admin", "admin123")
	if !ok {
		t.Fatal("expected login to succeed")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if user.Username != "admin" || user.Role != "administrador" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	_, _, ok := sessions.Login("admin", "wrong")
	if ok {
		t.Fatal("expected login to fail with wrong password")
	}
}

func TestLoginUnknownUser(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	_, _, ok := sessions.Login("nobody", "admin123")
	if ok {
		t.Fatal("expected login to fail with unknown user")
	}
}

func TestGetSession(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	token, _, _ := sessions.Login("admin", "admin123")
	user, ok := sessions.Get(token)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if user.Username != "admin" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestGetSessionInvalidToken(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), nil)
	_, ok := sessions.Get("invalid-token")
	if ok {
		t.Fatal("expected session not found")
	}
}

func TestGetSessionEmptyToken(t *testing.T) {
	sessions := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), nil)
	_, ok := sessions.Get("")
	if ok {
		t.Fatal("expected session not found for empty token")
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"BEARER   abc123  ", "abc123"},
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer ", ""},
	}
	for _, tt := range tests {
		got := GetBearerToken(tt.input)
		if got != tt.want {
			t.Errorf("GetBearerToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRandomHexLength(t *testing.T) {
	for n := 1; n <= 32; n++ {
		got := randomHex(n)
		if len(got) != n*2 { // hex encoding doubles the length
			t.Fatalf("randomHex(%d) returned length %d, want %d", n, len(got), n*2)
		}
	}
}

func TestSessionStorePersistsAndReloadsSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	seeded := NewSessionStore(path, []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	token, original, ok := seeded.Login("admin", "admin123")
	if !ok {
		t.Fatal("expected login to succeed")
	}

	reloaded := NewSessionStore(path, []UserAccount{
		NewUserAccount("admin", "admin123", "administrador"),
	})
	user, ok := reloaded.Get(token)
	if !ok {
		t.Fatal("expected persisted session to reload")
	}
	if user != original {
		t.Fatalf("reloaded session = %+v, want %+v", user, original)
	}
}
