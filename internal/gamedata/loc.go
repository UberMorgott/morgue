package gamedata

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// organizeLoc writes loc/<lang>.json (key->string per language) from two
// sources, in priority order:
//
//  1. I2 Localization source ScriptableObjects (i2Sources): mSource.mLanguages
//     defines the ordered language columns and mSource.mTerms[i].Languages[j]
//     aligns with mLanguages[j]. This is the Phoenix Point / I2 path.
//  2. CSV/TXT localization TextAssets under ExportDir (header
//     "Keys,<Lang1>,..."). Fallback for non-I2 games.
//
// Returns per-language key counts.
func organizeLoc(opts Options, i2Sources []map[string]any, rep *Report) map[string]int {
	langs := map[string]map[string]string{} // lang -> key -> string

	for _, src := range i2Sources {
		parseI2Source(src, langs)
	}

	if opts.ExportDir != "" {
		_ = filepath.WalkDir(opts.ExportDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(d.Name())) {
			case ".csv", ".txt":
				parseLocCSV(path, langs, rep)
			}
			return nil
		})
	}

	counts := map[string]int{}
	for lang, kv := range langs {
		if err := writeJSON(filepath.Join(opts.OutDir, "loc", sanitizeName(lang)+".json"), kv); err != nil {
			if rep != nil {
				rep.Failed = append(rep.Failed, "loc/"+lang+": "+err.Error())
			}
			continue
		}
		counts[lang] = len(kv)
	}
	return counts
}

// parseI2Source merges one I2 LanguageSource (its fields map, containing
// mSource) into langs. Terms are keyed by their fully-qualified Term (falling
// back to Key); each term's Languages[] aligns index-wise with mLanguages[].
func parseI2Source(fields map[string]any, langs map[string]map[string]string) {
	ms := asMap(fields["mSource"])
	if ms == nil {
		return
	}
	langDefs := asList(ms["mLanguages"])
	if len(langDefs) == 0 {
		return
	}
	labels := make([]string, len(langDefs))
	for i, ld := range langDefs {
		lm := asMap(ld)
		code := strings.TrimSpace(asString(lm["Code"]))
		name := strings.TrimSpace(asString(lm["Name"]))
		switch {
		case code != "":
			labels[i] = code
		case name != "":
			labels[i] = name
		default:
			labels[i] = fmt.Sprintf("lang%d", i)
		}
	}
	for _, t := range asList(ms["mTerms"]) {
		tm := asMap(t)
		if tm == nil {
			continue
		}
		key := asString(firstField(tm, "Term", "Key"))
		if key == "" {
			continue
		}
		for i, v := range asList(tm["Languages"]) {
			if i >= len(labels) {
				break
			}
			lang := labels[i]
			if langs[lang] == nil {
				langs[lang] = map[string]string{}
			}
			langs[lang][key] = asString(v)
		}
	}
}

// parseLocCSV merges one localization CSV into langs. Files whose first column
// is not "Key"/"Keys" are ignored (not a localization table).
func parseLocCSV(path string, langs map[string]map[string]string, rep *Report) {
	defer func() {
		if r := recover(); r != nil && rep != nil {
			rep.Failed = append(rep.Failed, path+": loc parse panic")
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil || len(header) < 2 || !isKeyHeader(header[0]) {
		return
	}
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		key := row[0]
		for j := 1; j < len(row) && j < len(header); j++ {
			lang := strings.TrimSpace(header[j])
			if lang == "" {
				continue
			}
			if langs[lang] == nil {
				langs[lang] = map[string]string{}
			}
			langs[lang][key] = row[j]
		}
	}
}

// isKeyHeader reports whether a header cell names the key column (BOM tolerant).
func isKeyHeader(h string) bool {
	h = strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")
	return strings.EqualFold(h, "key") || strings.EqualFold(h, "keys")
}
