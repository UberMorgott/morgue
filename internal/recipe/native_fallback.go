package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	peparser "github.com/saferwall/pe"
)

// native_fallback.go provides pure-Go, tool-free artifacts for the native
// recipe so it NEVER yields zero output — even when the strings tool and Ghidra
// are unavailable (offline / no download). All extraction is read-only PE
// parsing via the already-vendored github.com/saferwall/pe; the target is never
// executed.

// importEntry is one imported DLL and the symbols pulled from it.
type importEntry struct {
	DLL       string   `json:"dll"`
	Functions []string `json:"functions"`
}

// writeImports parses the target PE and writes the import table as
// <outDir>/imports.txt (one `DLL!Func` line per symbol) and
// <outDir>/imports.json (structured). Returns the number of imported symbols.
func writeImports(target, outDir string) (int, error) {
	f, err := peparser.New(target, nil)
	if err != nil {
		return 0, fmt.Errorf("pe.New: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Parse(); err != nil {
		return 0, fmt.Errorf("pe.Parse: %w", err)
	}

	var txt strings.Builder
	entries := make([]importEntry, 0, len(f.Imports))
	count := 0
	for _, imp := range f.Imports {
		funcs := make([]string, 0, len(imp.Functions))
		for _, fn := range imp.Functions {
			name := fn.Name
			if fn.ByOrdinal || name == "" {
				name = fmt.Sprintf("#%d", fn.Ordinal)
			}
			funcs = append(funcs, name)
			txt.WriteString(imp.Name)
			txt.WriteByte('!')
			txt.WriteString(name)
			txt.WriteByte('\n')
			count++
		}
		entries = append(entries, importEntry{DLL: imp.Name, Functions: funcs})
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "imports.txt"), []byte(txt.String()), 0644); err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "imports.json"), data, 0644); err != nil {
		return 0, err
	}
	return count, nil
}

// writeStringsFallback extracts printable ASCII and UTF-16LE strings from the
// target's raw bytes (mirroring recon.extractStrings) and writes them to
// outPath, one per line. Returns the number of strings written. This is the
// pure-Go replacement for the Sysinternals strings tool so strings.txt always
// exists.
func writeStringsFallback(target, outPath string, minLen int) (int, error) {
	if minLen <= 0 {
		minLen = 4
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filepath.Base(target), err)
	}

	var out strings.Builder
	count := 0
	emit := func(s string) {
		out.WriteString(s)
		out.WriteByte('\n')
		count++
	}

	// Printable ASCII runs.
	var cur []byte
	for _, b := range data {
		if b >= 0x20 && b < 0x7f {
			cur = append(cur, b)
			continue
		}
		if len(cur) >= minLen {
			emit(string(cur))
		}
		cur = cur[:0]
	}
	if len(cur) >= minLen {
		emit(string(cur))
	}

	// UTF-16LE runs (ASCII code unit followed by a zero high byte).
	cur = cur[:0]
	for i := 0; i+1 < len(data); i += 2 {
		lo, hi := data[i], data[i+1]
		if hi == 0x00 && lo >= 0x20 && lo < 0x7f {
			cur = append(cur, lo)
			continue
		}
		if len(cur) >= minLen {
			emit(string(cur))
		}
		cur = cur[:0]
	}
	if len(cur) >= minLen {
		emit(string(cur))
	}

	if err := os.WriteFile(outPath, []byte(out.String()), 0644); err != nil {
		return 0, err
	}
	return count, nil
}

// writeSectionSummary writes <outPath> (sections.txt) listing each PE section's
// name, raw size and Shannon entropy. Returns the number of sections.
func writeSectionSummary(target, outPath string) (int, error) {
	f, err := peparser.New(target, nil)
	if err != nil {
		return 0, fmt.Errorf("pe.New: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Parse(); err != nil {
		return 0, fmt.Errorf("pe.Parse: %w", err)
	}

	var b strings.Builder
	b.WriteString("# name    size(raw)    entropy(0-8)\n")
	for i := range f.Sections {
		sec := &f.Sections[i]
		name := strings.TrimRight(string(sec.Header.Name[:]), "\x00")
		entropy := sec.CalculateEntropy(f)
		fmt.Fprintf(&b, "%-10s %-12d entropy=%.3f\n", name, sec.Header.SizeOfRawData, entropy)
	}
	if err := os.WriteFile(outPath, []byte(b.String()), 0644); err != nil {
		return 0, err
	}
	return len(f.Sections), nil
}
