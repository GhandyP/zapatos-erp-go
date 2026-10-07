package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Event struct {
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Entity string `json:"entity"`
	At     string `json:"at"`
}

// Access provides context-aware audit reads and writes for HTTP handlers.
type Access interface {
	RecordContext(ctx context.Context, actor, action, entity string) (Event, error)
	ListContext(ctx context.Context) ([]Event, error)
}

type Store struct {
	path      string
	mu        sync.RWMutex
	events    []Event
	maxEvents int
}

func NewStore(path string) *Store {
	s := &Store{path: path, maxEvents: 500}
	_ = s.load()
	return s
}

func (s *Store) Record(actor, action, entity string) Event {
	event := Event{Actor: actor, Action: action, Entity: entity, At: time.Now().UTC().Format(time.RFC3339Nano)}
	s.mu.Lock()
	s.events = append(s.events, event)
	if len(s.events) > s.maxEvents {
		s.events = append([]Event(nil), s.events[len(s.events)-s.maxEvents:]...)
	}
	_ = s.persistLocked()
	s.mu.Unlock()
	return event
}

// RecordContext records an event and reports persistence failures. The legacy
// Record method intentionally retains its historical best-effort behavior.
func (s *Store) RecordContext(ctx context.Context, actor, action, entity string) (Event, error) {
	if ctx == nil {
		return Event{}, fmt.Errorf("record audit event: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	event := Event{Actor: actor, Action: action, Entity: entity, At: time.Now().UTC().Format(time.RFC3339Nano)}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := append([]Event(nil), s.events...)
	s.events = append(s.events, event)
	if len(s.events) > s.maxEvents {
		s.events = append([]Event(nil), s.events[len(s.events)-s.maxEvents:]...)
	}
	if err := s.persistLocked(); err != nil {
		s.events = previous
		return Event{}, fmt.Errorf("persist audit event: %w", err)
	}
	return event, nil
}

func (s *Store) List() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// ListContext returns the current in-memory or JSON-backed snapshot.
func (s *Store) ListContext(ctx context.Context) ([]Event, error) {
	if ctx == nil {
		return nil, fmt.Errorf("list audit events: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *Store) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) == 0 {
		return nil
	}
	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		return nil
	}
	if len(events) == 0 {
		return nil
	}
	sort.SliceStable(events, func(i, j int) bool {
		iTime, iErr := time.Parse(time.RFC3339Nano, events[i].At)
		jTime, jErr := time.Parse(time.RFC3339Nano, events[j].At)
		if iErr != nil || jErr != nil {
			return events[i].At < events[j].At
		}
		return iTime.Before(jTime)
	})
	if len(events) > s.maxEvents {
		events = append([]Event(nil), events[len(events)-s.maxEvents:]...)
	}
	s.mu.Lock()
	s.events = append([]Event(nil), events...)
	s.mu.Unlock()
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.events, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
