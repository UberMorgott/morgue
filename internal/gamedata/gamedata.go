// Package gamedata organizes a raw AssetRipper Unity export (plus an optional
// AssetStudio inventory) into a deterministic, greppable text tree of game
// DATA: ScriptableObject defs, the input map, prefabs, localization, and an
// inventory, with an index/manifest/readme.
//
// Every per-file failure is recovered and recorded in Report.Failed; a single
// bad asset never aborts the whole run.
package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options configures an Organize run. Log and Progress may be nil.
type Options struct {
	ExportDir     string            // AssetRipper export root (defs/SO/TextAsset/scripts)
	FullExportDir string            // optional full export root for prefabs/scenes ("" = none)
	OutDir        string            // GameData output root
	InventoryCSV  string            // AssetStudio inventory file ("" = skip)
	UnityVersion  string            // e.g. "2019.4.31f1"
	ToolVersions  map[string]string // tool name -> version, for the manifest
	Log           func(string)
	Progress      func(done, total int, unit string)
}

// Report summarizes an Organize run.
type Report struct {
	DefCountsByType   map[string]int
	ActionCount       int
	LocKeyCounts      map[string]int
	AssetCountsByType map[string]int
	Failed            []string
}

// Organize reads the export(s) + inventory and writes the GameData tree under
// OutDir. It returns a Report; per-item failures are non-fatal.
func Organize(opts Options) (Report, error) {
	rep := Report{
		DefCountsByType:   map[string]int{},
		LocKeyCounts:      map[string]int{},
		AssetCountsByType: map[string]int{},
	}
	if opts.OutDir == "" {
		return rep, fmt.Errorf("gamedata: OutDir is required")
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return rep, fmt.Errorf("gamedata: create out dir: %w", err)
	}

	// 1. guid -> ClassName from script .meta files.
	scriptMap := buildScriptMap(opts)
	logf(opts, fmt.Sprintf("scripts: %d guid->class mappings", len(scriptMap)))

	// 2. defs -> defs/<Type>/<Name>.json + name index.
	dr := organizeDefs(opts, scriptMap)
	rep.DefCountsByType = dr.Counts
	rep.Failed = append(rep.Failed, dr.Failed...)

	// 3. resolve $ref placeholders to $name (second pass over written defs).
	if err := resolveRefs(filepath.Join(opts.OutDir, "defs"), dr.Index); err != nil {
		logf(opts, "refs: "+err.Error())
	}

	// 4. input map.
	rep.ActionCount = organizeInput(opts, dr.InputMaps)

	// 5. localization (I2 sources from the defs pass + CSV fallback).
	rep.LocKeyCounts = organizeLoc(opts, dr.I2Sources, &rep)

	// 6. prefabs (optional).
	if opts.FullExportDir != "" {
		organizePrefabs(opts, &rep)
	}

	// 7. inventory (optional).
	if opts.InventoryCSV != "" {
		organizeInventory(opts, &rep)
	}

	// 8. index / manifest / readme.
	if err := writeIndex(opts, rep); err != nil {
		logf(opts, "index: "+err.Error())
	}

	return rep, nil
}

func logf(opts Options, msg string) {
	if opts.Log != nil {
		opts.Log(msg)
	}
}

func progress(opts Options, done, total int, unit string) {
	if opts.Progress != nil {
		opts.Progress(done, total, unit)
	}
}

// writeJSON marshals v indented and writes it, creating parent dirs.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// writeFile writes text content, creating parent dirs.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

var illegalFilename = strings.NewReplacer(
	"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
	"\"", "_", "<", "_", ">", "_", "|", "_", "\n", "_", "\r", "_",
)

// sanitizeName makes s safe as a single path component (Windows-conservative).
func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = illegalFilename.Replace(s)
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "_"
	}
	return s
}

// relPath returns p relative to base, or p unchanged when that is not possible.
func relPath(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}
