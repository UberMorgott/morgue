package recon

import (
	"bytes"
	"strings"
	"unicode"
)

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
func EnrichWithHeuristics(r *Result, sectionNames, importNames, strs []string, fileData []byte) {
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

	if r.Compiler == "" && hasDelphiMarkers(sectionNames, importNames) {
		r.Compiler = "Delphi"
	}
}
