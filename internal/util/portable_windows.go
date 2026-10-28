//go:build windows

package util

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// processAlive reports whether a process with this PID is still running.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // G115: PIDs come from our own p<pid> dir names, always small positive ints
	if err != nil {
		// Access denied means it exists (another user's process); anything
		// else (ERROR_INVALID_PARAMETER) means it is gone.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == 259 // STILL_ACTIVE
}

// Sysinternals tools persist -accepteula as HKCU\Software\Sysinternals\<Tool>
// (EulaAccepted=1). That cannot be redirected, so morgue removes the key again
// once its last concurrent spawn of the tool exits — but only when the key
// did not exist before morgue first ran the tool (the user's own acceptance
// is left alone).
const sysinternalsKey = `Software\Sysinternals`

var sysinternalsTools = map[string]string{
	"strings.exe":   "Strings",
	"strings64.exe": "Strings",
}

type eulaState struct {
	checked       bool
	keyExisted    bool
	parentExisted bool
	active        int
}

var (
	eulaMu sync.Mutex
	eulas  = map[string]*eulaState{}
)

func keyExists(path string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_ = k.Close()
	return true
}

// sysinternalsEulaKey returns the tool's subkey name when name is a known
// Sysinternals binary invoked with -accepteula, else "".
func sysinternalsEulaKey(name string, args []string) string {
	tool, ok := sysinternalsTools[strings.ToLower(filepath.Base(name))]
	if !ok || !slices.ContainsFunc(args, func(a string) bool { return strings.EqualFold(a, "-accepteula") }) {
		return ""
	}
	return tool
}

// trackEula must wrap every spawn; the returned func runs after it exits.
func trackEula(name string, args []string) func() {
	tool := sysinternalsEulaKey(name, args)
	if tool == "" {
		return func() {}
	}
	path := sysinternalsKey + `\` + tool
	eulaMu.Lock()
	st := eulas[tool]
	if st == nil {
		st = &eulaState{}
		eulas[tool] = st
	}
	if !st.checked {
		st.checked = true
		st.keyExisted = keyExists(path)
		st.parentExisted = keyExists(sysinternalsKey)
	}
	st.active++
	eulaMu.Unlock()

	return func() {
		eulaMu.Lock()
		defer eulaMu.Unlock()
		st.active--
		if st.active > 0 || st.keyExisted {
			return
		}
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
		if !st.parentExisted {
			// Fails (as intended) if anything else now lives under it.
			_ = registry.DeleteKey(registry.CURRENT_USER, sysinternalsKey)
		}
	}
}
