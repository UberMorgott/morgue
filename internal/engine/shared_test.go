package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSharedProductsOnlyCacheSuccessfulLiveWork(t *testing.T) {
	s := newSharedProducts(context.Background())
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Do("game1|settings1", func() error { calls.Add(1); return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("shared extraction repeated %d times", calls.Load())
	}
	if reused, err := s.Do("game1|settings2", func() error { calls.Add(1); return nil }); err != nil || reused {
		t.Fatal("different settings reused work")
	}
	want := errors.New("export failed")
	if _, err := s.Do("failure", func() error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if reused, err := s.Do("failure", func() error { return nil }); err != nil || reused {
		t.Fatal("failed extraction was cached")
	}
	if reused, err := newSharedProducts(context.Background()).Do("game1|settings1", func() error { return nil }); err != nil || reused {
		t.Fatal("separate run reused old work")
	}
}
