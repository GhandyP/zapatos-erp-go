package store

import (
	"sync"
)

type Repository[T any] interface {
	List() ([]T, error)
	FindByID(id string) (T, bool, error)
	Save(entity T) (T, error)
	Delete(id string) (bool, error)
}

type memoryRepository[T any] struct {
	mu    sync.RWMutex
	items map[string]T
	idFn  func(T) string
}

func NewMemoryRepository[T any](seed []T, idFn func(T) string) Repository[T] {
	items := make(map[string]T, len(seed))
	for _, item := range seed {
		items[idFn(item)] = item
	}
	return &memoryRepository[T]{items: items, idFn: idFn}
}

func (r *memoryRepository[T]) List() ([]T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out, nil
}

func (r *memoryRepository[T]) FindByID(id string) (T, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	return item, ok, nil
}

func (r *memoryRepository[T]) Save(entity T) (T, error) {
	r.mu.Lock()
	r.items[r.idFn(entity)] = entity
	r.mu.Unlock()
	return entity, nil
}

func (r *memoryRepository[T]) Delete(id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return false, nil
	}
	delete(r.items, id)
	return true, nil
}
