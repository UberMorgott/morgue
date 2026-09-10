package gamedata

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// resolveRefs walks every written def JSON and annotates each {"$ref": "<id>"}
// object with {"$name": <DefName|null>} resolved from the global name index.
// Internal (intra-blob) refs are prefixed with "#" and resolve to null.
func resolveRefs(defsDir string, index map[string]string) error {
	if _, err := os.Stat(defsDir); err != nil {
		return nil //nolint:nilerr // no defs dir means the defs pass wrote nothing; there is nothing to resolve
	}
	return filepath.WalkDir(defsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil //nolint:nilerr // an unreadable entry is skipped so the rest of the tree still resolves
		}
		data, e := os.ReadFile(path) //nolint:gosec // path comes from WalkDir over our own output dir
		if e != nil {
			return nil //nolint:nilerr // best effort: an unreadable def keeps its unresolved $ref placeholders
		}
		var obj any
		if json.Unmarshal(data, &obj) != nil {
			return nil //nolint:nilerr // best effort: a malformed def keeps its unresolved $ref placeholders
		}
		if walkRefs(obj, index) {
			// A write failure here is systemic (permissions/disk), not per-file:
			// abort the pass so Organize logs it instead of silently dropping it.
			if werr := writeJSON(path, obj); werr != nil {
				return werr
			}
		}
		return nil
	})
}

// walkRefs recursively annotates $ref maps; returns true if anything changed.
func walkRefs(v any, index map[string]string) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"]; ok {
			// Odin-path placeholder: always annotate (null when unresolved).
			id, _ := ref.(string)
			if name, found := index[strings.TrimPrefix(id, "#")]; found {
				t["$name"] = name
			} else {
				t["$name"] = nil
			}
			changed = true
		} else if g, ok := t["guid"].(string); ok && g != "" {
			// Plain Unity object reference {fileID, guid, type}: annotate only
			// when the guid resolves, to avoid churning every unresolved ref.
			if _, hasFID := t["fileID"]; hasFID {
				if name, found := index[g]; found && t["$name"] != name {
					t["$name"] = name
					changed = true
				}
			}
		}
		for _, vv := range t {
			if walkRefs(vv, index) {
				changed = true
			}
		}
	case []any:
		for _, vv := range t {
			if walkRefs(vv, index) {
				changed = true
			}
		}
	}
	return changed
}
