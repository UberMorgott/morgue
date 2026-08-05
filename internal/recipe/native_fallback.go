package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	peparser "github.com/saferwall/pe"

	"github.com/UberMorgott/morgue/internal/util"
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
	f, err := peparser.New(util.LongPath(target), nil)
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

// writePEExtras parses the target PE once and writes four sibling artifacts
// next to imports.txt: exports.txt, resources.txt, tls.txt and debug.txt.
// It never fails hard on a non-PE input or a broken/absent data directory —
// the artifact is still written with a `# ...` note explaining why it is empty
// and the same note is returned so the caller can log it. Returns the number
// of exported symbols found (0 for a non-PE / no export directory).
func writePEExtras(target, outDir string) (int, []string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, nil, err
	}
	write := func(name, body string) error {
		return os.WriteFile(filepath.Join(outDir, name), []byte(body), 0644)
	}

	f, err := peparser.New(util.LongPath(target), nil)
	if err == nil {
		defer func() { _ = f.Close() }()
		err = f.Parse()
	}
	if err != nil {
		note := fmt.Sprintf("not a parsable PE (%v)", err)
		for _, name := range []string{"exports.txt", "resources.txt", "tls.txt", "debug.txt"} {
			if werr := write(name, "# "+note+"\n"); werr != nil {
				return 0, nil, werr
			}
		}
		return 0, []string{note}, nil
	}

	var notes []string
	note := func(s string) string { notes = append(notes, s); return "# " + s + "\n" }

	// Exports: name + ordinal (+ forwarder target when the export is a forward).
	var b strings.Builder
	if len(f.Export.Functions) == 0 {
		b.WriteString(note("no export directory"))
	} else {
		fmt.Fprintf(&b, "# module=%s\n# ordinal  name\n", f.Export.Name)
		for _, fn := range f.Export.Functions {
			name := fn.Name
			if name == "" {
				name = fmt.Sprintf("#%d", fn.Ordinal)
			}
			fmt.Fprintf(&b, "%-8d %s", fn.Ordinal, name)
			if fn.Forwarder != "" {
				fmt.Fprintf(&b, " -> %s", fn.Forwarder)
			}
			b.WriteByte('\n')
		}
	}
	if err := write("exports.txt", b.String()); err != nil {
		return 0, notes, err
	}

	// Resources: top-level type + per-leaf id/lang/size. Contents are not decoded.
	b.Reset()
	if len(f.Resources.Entries) == 0 {
		b.WriteString(note("no resource directory"))
	} else {
		b.WriteString("# type  id  lang  size(bytes)\n")
		for _, typeEntry := range f.Resources.Entries {
			typeName := typeEntry.Name
			if typeName == "" {
				typeName = peparser.ResourceType(typeEntry.ID).String()
			}
			for _, idEntry := range typeEntry.Directory.Entries {
				for _, langEntry := range idEntry.Directory.Entries {
					fmt.Fprintf(&b, "%-16s %-8d %-8s %d\n",
						typeName, idEntry.ID, langEntry.Data.Lang.String(), langEntry.Data.Struct.Size)
				}
			}
		}
	}
	if err := write("resources.txt", b.String()); err != nil {
		return 0, notes, err
	}

	// TLS: presence + callback count (Callbacks is []uint32 or []uint64).
	b.Reset()
	switch cb := f.TLS.Callbacks.(type) {
	case []uint32:
		fmt.Fprintf(&b, "tls=present callbacks=%d\n", len(cb))
		for _, a := range cb {
			fmt.Fprintf(&b, "callback 0x%x\n", a)
		}
	case []uint64:
		fmt.Fprintf(&b, "tls=present callbacks=%d\n", len(cb))
		for _, a := range cb {
			fmt.Fprintf(&b, "callback 0x%x\n", a)
		}
	default:
		if f.TLS.Struct != nil {
			b.WriteString("tls=present callbacks=0\n")
		} else {
			b.WriteString(note("no TLS directory"))
		}
	}
	if err := write("tls.txt", b.String()); err != nil {
		return 0, notes, err
	}

	// Debug: entry type + the PDB path when the entry is CodeView.
	b.Reset()
	if len(f.Debugs) == 0 {
		b.WriteString(note("no debug directory"))
	} else {
		b.WriteString("# type  pdb\n")
		for _, d := range f.Debugs {
			pdb := ""
			switch info := d.Info.(type) {
			case peparser.CVInfoPDB70:
				pdb = info.PDBFileName
			case peparser.CVInfoPDB20:
				pdb = info.PDBFileName
			}
			fmt.Fprintf(&b, "%-16s %s\n", d.Type, pdb)
		}
	}
	if err := write("debug.txt", b.String()); err != nil {
		return 0, notes, err
	}

	return len(f.Export.Functions), notes, nil
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
	data, err := os.ReadFile(util.LongPath(target))
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
	f, err := peparser.New(util.LongPath(target), nil)
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
