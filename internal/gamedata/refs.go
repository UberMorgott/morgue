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
		return nil // no defs written; nothing to resolve
	}
	return filepath.WalkDir(defsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		var obj any
		if json.Unmarshal(data, &obj) != nil {
			return nil
		}
		if walkRefs(obj, index) {
			_ = writeJSON(path, obj)
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
