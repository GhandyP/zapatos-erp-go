package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type JSONFileRepository[T any] struct {
	path  string
	seed  []T
	idFn  func(T) string
	once  sync.Once
	mu    sync.RWMutex
	items map[string]T
}

func NewJSONFileRepository[T any](path string, seed []T, idFn func(T) string) *JSONFileRepository[T] {
	return &JSONFileRepository[T]{path: path, seed: seed, idFn: idFn, items: map[string]T{}}
}

func (r *JSONFileRepository[T]) ensureLoaded() error {
	r.once.Do(func() {
		data, err := os.ReadFile(r.path)
		if err != nil || len(bytes.TrimSpace(data)) == 0 {
			for _, item := range r.seed {
				r.items[r.idFn(item)] = item
			}
			return
		}
		var loaded []T
		if err := json.Unmarshal(data, &loaded); err != nil {
			for _, item := range r.seed {
				r.items[r.idFn(item)] = item
			}
			return
		}
		if len(loaded) == 0 {
			for _, item := range r.seed {
				r.items[r.idFn(item)] = item
			}
			return
		}
		for _, item := range loaded {
			r.items[r.idFn(item)] = item
		}
	})
	return nil
}

func (r *JSONFileRepository[T]) persistLocked() error {
	items := make([]T, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
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
	out := make([]T, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out, nil
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
	r.mu.Lock()
	r.items[r.idFn(entity)] = entity
	err := r.persistLocked()
	r.mu.Unlock()
	return entity, err
}

func (r *JSONFileRepository[T]) Delete(id string) (bool, error) {
	if err := r.ensureLoaded(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return false, nil
	}
	delete(r.items, id)
	return true, r.persistLocked()
}

func (r *JSONFileRepository[T]) String() string { return fmt.Sprintf("json repo %s", r.path) }
