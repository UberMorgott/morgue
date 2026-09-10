package gamedata

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// buildScriptMap walks the export dir for *.cs.meta files and maps each
// script's guid to its class name (file base without the ".cs.meta" suffix).
func buildScriptMap(opts Options) map[string]string {
	m := map[string]string{}
	if opts.ExportDir == "" {
		return m
	}
	const suffix = ".cs.meta"
	_ = filepath.WalkDir(opts.ExportDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped so the rest of the script map still builds
		}
		name := d.Name()
		if !strings.HasSuffix(strings.ToLower(name), suffix) {
			return nil
		}
		guid := parseGuidFromMeta(path)
		if guid == "" {
			return nil
		}
		m[guid] = name[:len(name)-len(suffix)]
		return nil
	})
	return m
}

// parseGuidFromMeta reads a Unity .meta file and returns its "guid:" value, or
// "" when absent/unreadable.
func parseGuidFromMeta(path string) string {
	f, err := os.Open(path) //nolint:gosec // path is a local .meta file from the caller's export dir
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "guid:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
