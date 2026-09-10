package gamedata

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var prefabDocHeader = regexp.MustCompile(`^!u!(\d+)\s+&(\d+)`)

// organizePrefabs walks FullExportDir for *.prefab and writes each prefab's
// documents (GameObject + MonoBehaviour named fields) to prefabs/<name>.json.
func organizePrefabs(opts Options, rep *Report) {
	_ = filepath.WalkDir(opts.FullExportDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped so the rest of the export still yields prefabs
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".prefab") {
			return nil
		}
		docs, perr := parsePrefab(path)
		if perr != nil {
			rep.Failed = append(rep.Failed, relPath(opts.FullExportDir, path)+": "+perr.Error())
			return nil //nolint:nilerr // recorded in rep.Failed; one bad prefab must not abort the walk
		}
		name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		out := map[string]any{"name": name, "objects": docs}
		if err := writeJSON(filepath.Join(opts.OutDir, "prefabs", sanitizeName(name)+".json"), out); err != nil {
			rep.Failed = append(rep.Failed, relPath(opts.FullExportDir, path)+": "+err.Error())
		}
		return nil
	})
}

// parsePrefab splits a Unity multi-document YAML file and parses each document
// independently. Unity's "%TAG !u!" directive only applies to the first
// document, so a streaming decode fails after doc 1; we split on "--- "
// boundaries and strip the per-doc tag/anchor header before parsing.
func parsePrefab(path string) ([]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from WalkDir over the caller's local export dir
	if err != nil {
		return nil, err
	}
	var docs []any
	for _, ch := range splitUnityDocs(string(data)) {
		classID, pathID, body := stripDocHeader(ch)
		if strings.TrimSpace(body) == "" {
			continue
		}
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil {
			continue // tolerate a malformed document
		}
		doc := map[string]any{}
		for k, v := range parsed {
			doc["type"] = k
			doc["fields"] = v
		}
		if classID != "" {
			doc["classID"] = classID
		}
		if pathID != "" {
			doc["pathID"] = pathID
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// splitUnityDocs returns the per-document chunks (each starting with the
// "!u!N &M ..." header line), dropping the YAML directive preamble.
func splitUnityDocs(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var chunks []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for ln := range strings.SplitSeq(s, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- "):
			flush()
			cur = append(cur, strings.TrimPrefix(ln, "--- "))
		case strings.HasPrefix(ln, "%"):
			// YAML directive (%YAML / %TAG): skip
		case len(cur) == 0 && strings.TrimSpace(ln) == "":
			// leading blank line before first doc: skip
		default:
			cur = append(cur, ln)
		}
	}
	flush()
	return chunks
}

// stripDocHeader removes the leading "!u!N &M [stripped]" line from a document
// chunk and returns its classID, pathID, and the remaining YAML body.
func stripDocHeader(chunk string) (classID, pathID, body string) {
	head, rest, found := strings.Cut(chunk, "\n")
	if !found {
		return "", "", ""
	}
	first := strings.TrimSpace(head)
	body = rest
	if m := prefabDocHeader.FindStringSubmatch(first); m != nil {
		classID, pathID = m[1], m[2]
	}
	return classID, pathID, body
}
