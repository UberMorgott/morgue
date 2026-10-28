// Package apitoken manages the per-launch secret that authenticates clients
// of the GUI's localhost HTTP API. The GUI writes a fresh random token on
// every start; the CLI (and curl users) read it back and send it in the
// X-Morgue-Token header. It is a leaf package so both internal/api and
// internal/cli can use it without import cycles.
package apitoken

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Header is the HTTP request header carrying the token.
const Header = "X-Morgue-Token"

// EnvPath overrides the token file location (used by tests).
const EnvPath = "MORGUE_API_TOKEN_FILE"

// Path returns the token file location: $MORGUE_API_TOKEN_FILE if set, else
// <user cache dir>/morgue/api-token (e.g. %LOCALAPPDATA%\morgue\api-token on
// Windows). The user cache dir is per-user and shared by every copy of the
// executable, matching the single fixed API port.
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("apitoken: user cache dir: %w", err)
	}
	return filepath.Join(dir, "morgue", "api-token"), nil
}

// Generate returns a new random 256-bit token as hex.
func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("apitoken: random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Write stores token at path with owner-only permissions (0600; on Windows
// the per-user profile ACL is what actually protects it).
func Write(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("apitoken: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token), 0o600); err != nil {
		return fmt.Errorf("apitoken: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("apitoken: rename: %w", err)
	}
	return nil
}

// Read returns the token stored at the default Path.
func Read() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p) //nolint:gosec // p is the fixed token path from Path(), not caller input
	if err != nil {
		return "", fmt.Errorf("apitoken: read %s: %w", p, err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", errors.New("apitoken: token file is empty")
	}
	return tok, nil
}
