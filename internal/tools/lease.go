package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type toolLease struct{ f *os.File }

func (l *toolLease) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockToolFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return cerr
}

// Stable lock files live outside payload directories and are never unlinked.
func acquireToolLock(ctx context.Context, base, name string, exclusive, wait bool) (*toolLease, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid lock name %q", name)
	}
	dir := filepath.Join(base, ".locks")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, strings.ReplaceAll(name, ":", "_")+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err = lockToolFile(f, exclusive)
		if err == nil {
			return &toolLease{f: f}, nil
		}
		if !errors.Is(err, ErrInUse) || !wait {
			_ = f.Close()
			return nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func toolInUse(base, name string) bool {
	l, err := acquireToolLock(context.Background(), base, name, true, false)
	if err != nil {
		return errors.Is(err, ErrInUse)
	}
	_ = l.Close()
	return false
}
