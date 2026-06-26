package gamedata

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// manifest is the machine-readable summary written to manifest.json.
type manifest struct {
	UnityVersion      string            `json:"unityVersion"`
	ToolVersions      map[string]string `json:"toolVersions"`
	ExtractionDate    string            `json:"extractionDate"`
	TotalDefs         int               `json:"totalDefs"`
	DefCountsByType   map[string]int    `json:"defCountsByType"`
	ActionCount       int               `json:"actionCount"`
	LocKeyCounts      map[string]int    `json:"locKeyCounts"`
	AssetCountsByType map[string]int    `json:"assetCountsByType"`
	Failed            []string          `json:"failed"`
}

// writeIndex writes manifest.json, INDEX.md, and README.md.
func writeIndex(opts Options, rep Report) error {
	date := time.Now().UTC().Format("2006-01-02")
	total := 0
	for _, c := range rep.DefCountsByType {
		total += c
	}

	failed := rep.Failed
	if failed == nil {
		failed = []string{}
	}
	m := manifest{
		UnityVersion:      opts.UnityVersion,
		ToolVersions:      opts.ToolVersions,
		ExtractionDate:    date,
		TotalDefs:         total,
		DefCountsByType:   rep.DefCountsByType,
		ActionCount:       rep.ActionCount,
		LocKeyCounts:      rep.LocKeyCounts,
		AssetCountsByType: rep.AssetCountsByType,
		Failed:            failed,
	}
	if err := writeJSON(filepath.Join(opts.OutDir, "manifest.json"), m); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("# GameData Index\n\n")
	md.WriteString(fmt.Sprintf("- Unity version: %s\n", orNA(opts.UnityVersion)))
	md.WriteString(fmt.Sprintf("- Extraction date: %s\n", date))
	md.WriteString(fmt.Sprintf("- Total defs: %d\n", total))
	md.WriteString(fmt.Sprintf("- Actions: %d\n", rep.ActionCount))
	md.WriteString(fmt.Sprintf("- Failed: %d\n", len(rep.Failed)))
	if len(opts.ToolVersions) > 0 {
		md.WriteString("\n## Tool versions\n\n")
		for _, k := range sortedKeysStr(opts.ToolVersions) {
			md.WriteString(fmt.Sprintf("- %s: %s\n", k, opts.ToolVersions[k]))
		}
	}
	writeCountSection(&md, "Defs by type", rep.DefCountsByType)
	writeCountSection(&md, "Localization keys by language", rep.LocKeyCounts)
	writeCountSection(&md, "Assets by type", rep.AssetCountsByType)
	if len(rep.Failed) > 0 {
		md.WriteString("\n## Failed\n\n")
		for _, f := range rep.Failed {
			md.WriteString("- " + f + "\n")
		}
	}
	if err := writeFile(filepath.Join(opts.OutDir, "INDEX.md"), md.String()); err != nil {
		return err
	}

	readme := strings.Join([]string{
		"# GameData",
		"",
		"Generated game-data tree extracted from a Unity Mono build.",
		"",
		"- `defs/<TypeName>/<DefName>.json` - one file per ScriptableObject def (named fields).",
		"- `input/inputmap.md` / `inputmap.json` - input actions -> chords -> keys.",
		"- `prefabs/<name>.json` - GameObject + MonoBehaviour serialized fields.",
		"- `loc/<lang>.json` - localization key -> string per language.",
		"- `inventory/assets.csv` - asset inventory (Name,Type,Container,PathID,SourceBundle).",
		"- `INDEX.md` - counts + versions; `manifest.json` - machine-readable summary.",
		"",
		"Def-to-def references appear as `{\"$ref\": \"<id>\", \"$name\": \"<DefName|null>\"}`.",
		"",
	}, "\n")
	return writeFile(filepath.Join(opts.OutDir, "README.md"), readme)
}

func writeCountSection(md *strings.Builder, title string, counts map[string]int) {
	md.WriteString("\n## " + title + "\n\n")
	if len(counts) == 0 {
		md.WriteString("(none)\n")
		return
	}
	for _, k := range sortedKeysInt(counts) {
		md.WriteString(fmt.Sprintf("- %s: %d\n", k, counts[k]))
	}
}

func orNA(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func sortedKeysInt(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedKeysStr(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
