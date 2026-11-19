//go:build windows

package workspace

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive, non-blocking OS lock on f. Windows releases it
// when the handle closes or the process dies, so a crash never leaves a stale
// lock behind (unlike a marker/PID file).
func lockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}

// syncDir is a no-op: NTFS persists a MoveFileEx rename with the file metadata
// and directories cannot be flushed through a regular handle.
func syncDir(string) error { return nil }

// volumeNameDOS is VOLUME_NAME_DOS|FILE_NAME_NORMALIZED (both 0); x/sys does
// not export the constant.
const volumeNameDOS = 0

// realPath resolves p through symlinks, junctions and other reparse points
// via the final path of an opened handle. filepath.EvalSymlinks does not
// follow junctions (mount points), so it cannot be used for containment.
func realPath(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	size := uint32(windows.MAX_PATH)
	buf := make([]uint16, size)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], size, volumeNameDOS)
		if err != nil {
			return "", err
		}
		if n < size {
			s := windows.UTF16ToString(buf[:n])
			if strings.HasPrefix(s, `\\?\UNC\`) {
				return `\\` + s[len(`\\?\UNC\`):], nil
			}
			return strings.TrimPrefix(s, `\\?\`), nil
		}
		size = n + 1
		buf = make([]uint16, size)
	}
}
