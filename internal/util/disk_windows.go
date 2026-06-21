//go:build windows

package util

import (
	"fmt"
	"syscall"
	"unsafe"
)

// modkernel32 is declared in mem_windows.go (same build tag); reuse it here.
var (
	procGetDiskFreeSpace = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetLogicalDrives = modkernel32.NewProc("GetLogicalDrives")
)

// EnumerateDrives lists fixed-drive letters with their free space (Windows).
// Free space is the caller-available bytes (GetDiskFreeSpaceExW first arg), so a
// quota-limited volume reports the usable figure, not the raw total.
func EnumerateDrives() []DriveInfo {
	mask, _, _ := procGetLogicalDrives.Call()
	var out []DriveInfo
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := fmt.Sprintf("%c:", 'A'+i)
		root := letter + `\`
		var freeAvail, totalBytes, totalFree uint64
		rp, _ := syscall.UTF16PtrFromString(root)
		ret, _, _ := procGetDiskFreeSpace.Call(
			uintptr(unsafe.Pointer(rp)),
			uintptr(unsafe.Pointer(&freeAvail)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFree)),
		)
		if ret == 0 {
			continue // not ready / not a usable volume (e.g. empty optical drive)
		}
		out = append(out, DriveInfo{Letter: letter, FreeBytes: freeAvail})
	}
	return out
}
