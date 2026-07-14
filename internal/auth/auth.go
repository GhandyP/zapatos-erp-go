package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type UserAccount struct {
	Username     string
	PasswordHash string
	Role         string
}

func NewUserAccount(username, password, role string) UserAccount {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: bcrypt hash failed: " + err.Error())
	}
	return UserAccount{Username: username, PasswordHash: string(hash), Role: role}
}

type SessionUser struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type SessionStore struct {
	path     string
	users    []UserAccount
	mu       sync.RWMutex
	sessions map[string]SessionUser
}

func NewSessionStore(path string, users []UserAccount) *SessionStore {
	s := &SessionStore{path: path, users: users, sessions: map[string]SessionUser{}}
	_ = s.load()
	return s
}

func (s *SessionStore) Login(username, password string) (string, SessionUser, bool) {
	for _, user := range s.users {
		if user.Username == username && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
			token := "sess_" + randomHex(16)
			account := SessionUser{Username: user.Username, Role: user.Role}
			s.mu.Lock()
			s.sessions[token] = account
			if err := s.persistLocked(); err != nil {
				delete(s.sessions, token)
				s.mu.Unlock()
				return "", SessionUser{}, false
			}
			s.mu.Unlock()
			return token, account, true
		}
	}
	return "", SessionUser{}, false
}

func (s *SessionStore) Get(token string) (SessionUser, bool) {
	if token == "" {
		return SessionUser{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.sessions[token]
	return user, ok
}

func GetBearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

type sessionSnapshot struct {
	Token string      `json:"token"`
	User  SessionUser `json:"user"`
}

func (s *SessionStore) load() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var snapshots []sessionSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return nil
	}
	if len(snapshots) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.Token) == "" || strings.TrimSpace(snapshot.User.Username) == "" {
			continue
		}
		s.sessions[snapshot.Token] = snapshot.User
	}
	return nil
}

func (s *SessionStore) persistLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	snapshots := make([]sessionSnapshot, 0, len(s.sessions))
	for token, user := range s.sessions {
		snapshots = append(snapshots, sessionSnapshot{Token: token, User: user})
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Token < snapshots[j].Token })
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
