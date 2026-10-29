package engine

import (
	"context"
	"errors"
	"sync"
)

type sharedProduct struct {
	done chan struct{}
	err  error
}
type sharedProducts struct {
	mu       sync.Mutex
	ctx      context.Context
	products map[string]*sharedProduct
}

func newSharedProducts(ctx context.Context) *sharedProducts {
	return &sharedProducts{ctx: ctx, products: map[string]*sharedProduct{}}
}

func (s *sharedProducts) Do(key string, fn func() error) (bool, error) {
	if key == "" || fn == nil {
		return false, errors.New("invalid shared product")
	}
	if err := s.ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	if existing := s.products[key]; existing != nil {
		s.mu.Unlock()
		select {
		case <-existing.done:
			return existing.err == nil, existing.err
		case <-s.ctx.Done():
			return false, s.ctx.Err()
		}
	}
	entry := &sharedProduct{done: make(chan struct{})}
	s.products[key] = entry
	s.mu.Unlock()
	// Always release waiters, including a panic in a recipe callback. The engine
	// catches the panic at the recipe boundary; the failed product is not cached.
	completed := false
	defer func() {
		if !completed {
			s.mu.Lock()
			entry.err = errors.New("shared product interrupted")
			delete(s.products, key)
			close(entry.done)
			s.mu.Unlock()
		}
	}()
	err := fn()
	if err == nil {
		err = s.ctx.Err()
	}
	s.mu.Lock()
	entry.err = err
	if err != nil {
		delete(s.products, key)
	}
	close(entry.done)
	completed = true
	s.mu.Unlock()
	return false, err
}
