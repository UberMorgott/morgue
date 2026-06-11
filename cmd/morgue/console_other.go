//go:build !windows

package main

// attachParentConsole is a no-op on non-Windows platforms: there is no
// windowsgui subsystem to compensate for. Morgue ships Windows-only, but this
// keeps the CLI path buildable for `go build ./...` / `go vet` on any host.
func attachParentConsole() {}
