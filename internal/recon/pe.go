package recon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	peparser "github.com/saferwall/pe"

	"github.com/UberMorgott/morgue/internal/util"
)

// parsePE opens and parses a PE file, returning the parsed structure.
func parsePE(path string) (*peparser.File, error) {
	f, err := peparser.New(path, nil)
	if err != nil {
		return nil, fmt.Errorf("pe.New: %w", err)
	}
	if err := f.Parse(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("pe.Parse: %w", err)
	}
	return f, nil
}

// Classify performs reconnaissance on a binary file and returns a Result.
func Classify(path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{Path: path, Kind: Unknown, Fallback: true}, nil
	}

	sha, _ := util.SHA256File(path)

	r := Result{
		Path:   path,
		Size:   info.Size(),
		SHA256: sha,
	}

	f, err := parsePE(path)
	if err != nil {
		// Not a valid PE — fall back to extension-based classification
		r.Kind = classifyByExtension(filepath.Ext(path))
		r.Fallback = true
		return r, nil
	}
	defer func() { _ = f.Close() }()

	if f.HasCLR {
		r.Kind = Managed
		r.Runtime = clrVersion(f)
	} else {
		r.Kind = Native
		r.Compiler = classifyNativeCompiler(f)
	}

	// Extract section and import names for heuristics
	var sectionNames []string
	for _, sec := range f.Sections {
		sectionNames = append(sectionNames, strings.TrimRight(string(sec.Header.Name[:]), "\x00"))
	}
	importNames := importedDLLNames(f)

	// Run DiE if available (best-effort)
	diecPath := util.ToolPath("diec", "diec.exe")
	if _, statErr := os.Stat(diecPath); statErr == nil {
		RunDiE(&r, diecPath, path)
	}

	// Read capped file data for embedded signal detection (max 10MB to avoid OOM)
	const maxHeuristicScan = 10 * 1024 * 1024
	var fileData []byte
	if hf, err := os.Open(path); err == nil {
		buf := make([]byte, maxHeuristicScan)
		n, _ := hf.Read(buf)
		fileData = buf[:n]
		_ = hf.Close()
	}

	// Enrich with heuristics
	EnrichWithHeuristics(&r, sectionNames, importNames, nil, fileData)

	return r, nil
}

// classifyByExtension provides a best-guess Kind based on file extension.
func classifyByExtension(ext string) Kind {
	switch strings.ToLower(ext) {
	case ".dll":
		return Managed // optimistic — most DLLs we care about are .NET
	case ".so", ".dylib":
		return Native
	default:
		return Unknown
	}
}

// classifyNativeCompiler detects the compiler from PE characteristics.
func classifyNativeCompiler(f *peparser.File) string {
	importNames := importedDLLNames(f)

	// Delphi detection: imports from borlndmm.dll or cc32*.dll, or section named CODE
	for _, name := range importNames {
		lower := strings.ToLower(name)
		if lower == "borlndmm.dll" || strings.HasPrefix(lower, "cc32") {
			return "Delphi"
		}
	}
	for _, sec := range f.Sections {
		name := strings.TrimRight(string(sec.Header.Name[:]), "\x00")
		if name == "CODE" || name == ".idata" {
			return "Delphi"
		}
	}

	// Go detection: .symtab section or go.buildid section
	for _, sec := range f.Sections {
		name := strings.TrimRight(string(sec.Header.Name[:]), "\x00")
		if name == ".symtab" || name == ".go.buildid" {
			return "Go"
		}
	}

	return ""
}

// clrVersion extracts the CLR runtime version string.
func clrVersion(f *peparser.File) string {
	h := f.CLR.CLRHeader
	if h.MajorRuntimeVersion > 0 {
		return fmt.Sprintf("CLR %d.%d", h.MajorRuntimeVersion, h.MinorRuntimeVersion)
	}
	return ""
}

// importedDLLNames returns the list of imported DLL names from a PE file.
func importedDLLNames(f *peparser.File) []string {
	names := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		names = append(names, imp.Name)
	}
	return names
}
