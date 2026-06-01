package recon

import (
	"bytes"
	"strings"
	"unicode"
)

// isMangledName reports whether a metadata type name looks renamer-obfuscated:
// non-ASCII / Private-Use-Area characters, or a 1-2 char all-lowercase token
// (the sequential a, b, c, ..., aa, ab naming used by ConfuserEx-family renamers).
// A leading backtick-arity suffix (generic types: "a`1") is ignored before the
// length check.
func isMangledName(s string) bool {
	if i := strings.IndexByte(s, '`'); i >= 0 {
		s = s[:i]
	}
	for _, r := range s {
		if r > 0x7e || unicode.In(r, unicode.Co) {
			return true
		}
	}
	if len(s) >= 1 && len(s) <= 2 {
		for _, r := range s {
			if r < 'a' || r > 'z' {
				return false
			}
		}
		return true
	}
	return false
}

// mangledTypeRatio returns the fraction of own type names that look
// renamer-mangled, plus the total count. Measured over TypeDef definitions only
// (not TypeRef references), so framework references don't dilute the score: a
// clean assembly scores ~0, a ConfuserEx-renamed one scores ~0.8+.
func mangledTypeRatio(typeNames []string) (ratio float64, total int) {
	mangled := 0
	for _, n := range typeNames {
		if n == "" || n == "<Module>" {
			continue
		}
		total++
		if isMangledName(n) {
			mangled++
		}
	}
	if total == 0 {
		return 0, 0
	}
	return float64(mangled) / float64(total), total
}

// mangledTypeThreshold is the fraction of mangled TypeDef names above which a
// managed assembly is considered renamer-obfuscated. Clean assemblies sit near
// 0; ConfuserEx-renamed ones measured ~0.83 — 0.40 cleanly separates them.
const mangledTypeThreshold = 0.40

// minTypesForObfCheck avoids flagging tiny assemblies where a couple of short
// helper-type names would skew the ratio.
const minTypesForObfCheck = 20

// puaCharDensity returns the ratio of Private Use Area unicode characters in a string.
func puaCharDensity(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	total := 0
	pua := 0
	for _, r := range s {
		total++
		if unicode.In(r, unicode.Co) {
			pua++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(pua) / float64(total)
}

// detectObfuscatorByStrings checks known obfuscator signatures in strings.
func detectObfuscatorByStrings(strs []string) string {
	patterns := []struct {
		needle string
		name   string
	}{
		{"ConfuserEx", "ConfuserEx"},
		{"Confuser", "Confuser"},
		{"Dotfuscator", "Dotfuscator"},
		{"Obfuscar", "Obfuscar"},
		{"Eazfuscator", "Eazfuscator"},
		{"SmartAssembly", "SmartAssembly"},
		{"Babel", "Babel"},
		{"Crypto Obfuscator", "CryptoObfuscator"},
		{".Protect", "DotNetProtect"},
		{".NET Reactor", ".NET Reactor"},
		{"Themida", "Themida"},
		{"VMProtect", "VMProtect"},
	}

	for _, s := range strs {
		for _, p := range patterns {
			if strings.Contains(s, p.needle) {
				return p.name
			}
		}
	}
	return ""
}

// hasDelphiMarkers checks sections and imports for Delphi indicators.
func hasDelphiMarkers(sectionNames, importNames []string) bool {
	for _, name := range sectionNames {
		if name == "CODE" {
			return true
		}
	}
	for _, name := range importNames {
		lower := strings.ToLower(name)
		if lower == "borlndmm.dll" || strings.HasPrefix(lower, "cc32") {
			return true
		}
	}
	return false
}

// detectEmbeddedSignals scans file data for byte patterns indicating embedded payloads.
func detectEmbeddedSignals(data []byte) []string {
	markers := []struct {
		needle string
		signal string
	}{
		{"costura.", "Costura.Fody resources"},
		{"Costura.AssemblyLoader", "Costura.Fody loader"},
		{"AssemblyResolve", "AssemblyResolve hook"},
		{"ILMerge", "ILMerge signature"},
		{"ILRepack", "ILRepack signature"},
	}

	var signals []string
	for _, m := range markers {
		if bytes.Contains(data, []byte(m.needle)) {
			signals = append(signals, m.signal)
		}
	}
	return signals
}

// detectObfuscatorFeatures detects specific ConfuserEx protection features from string patterns.
func detectObfuscatorFeatures(strs []string) []string {
	markers := []struct {
		pattern string
		feature string
	}{
		{"AntiTamper", "anti_tamper"},
		{"AntiDebug", "anti_debug"},
		{"Constants", "constants"},
		{"ControlFlow", "ctrl_flow"},
		{"ReferenceProxy", "ref_proxy"},
		{"Resources", "resources"},
	}

	seen := map[string]bool{}
	var features []string
	for _, s := range strs {
		for _, m := range markers {
			if !seen[m.feature] && strings.Contains(s, m.pattern) {
				features = append(features, m.feature)
				seen[m.feature] = true
			}
		}
	}
	return features
}

// EnrichWithHeuristics adds heuristic-based detections to a Result.
// fileData is the raw bytes of the file for embedded signal detection.
// typeNames are the .NET TypeDef names (nil for native binaries), used to
// detect renamer obfuscation from mangled identifier names.
func EnrichWithHeuristics(r *Result, sectionNames, importNames, strs []string, fileData []byte, typeNames []string) {
	if r.Obfuscator == "" {
		r.Obfuscator = detectObfuscatorByStrings(strs)
	}

	features := detectObfuscatorFeatures(strs)
	if len(features) > 0 {
		r.ObfuscatorFeatures = append(r.ObfuscatorFeatures, features...)
	}

	signals := detectEmbeddedSignals(fileData)
	if len(signals) > 0 {
		r.EmbeddedSignals = append(r.EmbeddedSignals, signals...)
		r.EmbeddedSuspected = true
	}

	// Renamer-obfuscation detection from metadata name mangling. ConfuserEx's
	// Resources protection strips its own name, so the string-based detection
	// above misses it; the mangled TypeDef names are the reliable tell.
	if r.Kind == Managed && r.Obfuscator == "" {
		if ratio, total := mangledTypeRatio(typeNames); total >= minTypesForObfCheck && ratio >= mangledTypeThreshold {
			// Heavy renaming plus resource packing (AssemblyResolve hook, not
			// Costura) is the ConfuserEx fingerprint; de4dot-cex is the right
			// pipeline for the whole renamer family regardless of exact tool.
			r.Obfuscator = "ConfuserEx"
		}
	}

	if r.Compiler == "" && hasDelphiMarkers(sectionNames, importNames) {
		r.Compiler = "Delphi"
	}
}
