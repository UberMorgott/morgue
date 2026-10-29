//go:build !windows

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockFile takes an exclusive, non-blocking flock on f; the kernel releases it
// when the descriptor closes or the process dies.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// syncDir flushes a directory so a rename into it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// realPath resolves p through symlinks.
func realPath(p string) (string, error) { return filepath.EvalSymlinks(p) }
