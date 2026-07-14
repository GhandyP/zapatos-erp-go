package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type JSONFileRepository[T any] struct {
	path    string
	seed    []T
	idFn    func(T) string
	loadMu  sync.Mutex
	loaded  bool
	loadErr error
	mu      sync.RWMutex
	items   map[string]T
}

func NewJSONFileRepository[T any](path string, seed []T, idFn func(T) string) *JSONFileRepository[T] {
	return &JSONFileRepository[T]{path: path, seed: seed, idFn: idFn, items: map[string]T{}}
}

func (r *JSONFileRepository[T]) ensureLoaded() error {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	if r.loaded && r.loadErr == nil {
		return nil
	}

	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		r.replaceItems(r.seedItems())
		r.loaded = true
		r.loadErr = nil
		return nil
	}
	if err != nil {
		return r.degrade(fmt.Errorf("read repository %s: %w", r.path, err))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return r.degrade(fmt.Errorf("repository %s is empty", r.path))
	}

	var loaded []T
	if err := json.Unmarshal(data, &loaded); err != nil {
		return r.degrade(fmt.Errorf("decode repository %s: %w", r.path, err))
	}
	if loaded == nil && bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return r.degrade(fmt.Errorf("repository %s contains null instead of an array", r.path))
	}
	items := make(map[string]T, len(loaded))
	for _, item := range loaded {
		items[r.idFn(item)] = item
	}
	r.replaceItems(items)
	r.loaded = true
	r.loadErr = nil
	return nil
}

func (r *JSONFileRepository[T]) degrade(err error) error {
	r.replaceItems(r.seedItems())
	r.loaded = true
	r.loadErr = err
	return err
}

func (r *JSONFileRepository[T]) seedItems() map[string]T {
	items := make(map[string]T, len(r.seed))
	for _, item := range r.seed {
		items[r.idFn(item)] = item
	}
	return items
}

func (r *JSONFileRepository[T]) replaceItems(items map[string]T) {
	r.mu.Lock()
	r.items = items
	r.mu.Unlock()
}

// LoadError reports the latest file-load problem while keeping seed data readable.
// A nil error means the repository is backed by a valid file or a missing file.
func (r *JSONFileRepository[T]) LoadError() error {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	return r.loadErr
}

func (r *JSONFileRepository[T]) persistLocked() error {
	items := r.orderedItemsLocked()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, r.path)
}

func (r *JSONFileRepository[T]) List() ([]T, error) {
	if err := r.ensureLoaded(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.orderedItemsLocked(), nil
}

func (r *JSONFileRepository[T]) orderedItemsLocked() []T {
	keys := make([]string, 0, len(r.items))
	for key := range r.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]T, 0, len(keys))
	for _, key := range keys {
		items = append(items, r.items[key])
	}
	return items
}

func (r *JSONFileRepository[T]) FindByID(id string) (T, bool, error) {
	var zero T
	if err := r.ensureLoaded(); err != nil {
		return zero, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	return item, ok, nil
}

func (r *JSONFileRepository[T]) Save(entity T) (T, error) {
	if err := r.ensureLoaded(); err != nil {
		return entity, err
	}
	if err := r.LoadError(); err != nil {
		return entity, fmt.Errorf("repository %s is read-only until repaired: %w", r.path, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.idFn(entity)
	previous, existed := r.items[key]
	r.items[key] = entity
	if err := r.persistLocked(); err != nil {
		if existed {
			r.items[key] = previous
		} else {
			delete(r.items, key)
		}
		return entity, err
	}
	return entity, nil
}

func (r *JSONFileRepository[T]) Delete(id string) (bool, error) {
	if err := r.ensureLoaded(); err != nil {
		return false, err
	}
	if err := r.LoadError(); err != nil {
		return false, fmt.Errorf("repository %s is read-only until repaired: %w", r.path, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, ok := r.items[id]
	if !ok {
		return false, nil
	}
	delete(r.items, id)
	if err := r.persistLocked(); err != nil {
		r.items[id] = previous
		return false, err
	}
	return true, nil
}

func (r *JSONFileRepository[T]) String() string { return fmt.Sprintf("json repo %s", r.path) }
