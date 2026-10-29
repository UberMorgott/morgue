//go:build !windows

package tools

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockToolFile(f *os.File, exclusive bool) error {
	flags := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		flags = syscall.LOCK_EX | syscall.LOCK_NB
	}
	err := syscall.Flock(int(f.Fd()), flags)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrInUse
	}
	return err
}
func unlockToolFile(f *os.File) error              { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func reparseEntry(info os.FileInfo) bool           { return info.Mode()&os.ModeSymlink != 0 }
func physicalToolPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
