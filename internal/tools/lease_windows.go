//go:build windows

package tools

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"strings"
)

func lockToolFile(f *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return ErrInUse
	}
	return err
}
func unlockToolFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
func reparseEntry(info os.FileInfo) bool {
	data, ok := info.Sys().(*windows.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// Resolve junctions too; filepath.EvalSymlinks is insufficient on Windows.
func physicalToolPath(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	size := uint32(windows.MAX_PATH)
	buf := make([]uint16, size)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], size, 0)
		if err != nil {
			return "", err
		}
		if n < size {
			value := windows.UTF16ToString(buf[:n])
			if after, ok := strings.CutPrefix(value, `\\?\UNC\`); ok {
				return `\\` + after, nil
			}
			return strings.TrimPrefix(value, `\\?\`), nil
		}
		size = n + 1
		buf = make([]uint16, size)
	}
}
