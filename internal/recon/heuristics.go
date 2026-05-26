package recon

import (
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

// detectEmbeddedSignals looks for section names that suggest embedded payloads or packers.
func detectEmbeddedSignals(sectionNames []string) []string {
	suspicious := map[string]string{
		".rsrc":    "resource section (may contain embedded binaries)",
		".themida": "Themida packer section",
		".vmp":     "VMProtect section",
		".upx":     "UPX packer section",
		"UPX0":     "UPX packer section",
		"UPX1":     "UPX packer section",
		".aspack":  "ASPack section",
		".adata":   "ASPack data section",
	}

	var signals []string
	for _, name := range sectionNames {
		if sig, ok := suspicious[name]; ok {
			signals = append(signals, sig)
		}
	}
	return signals
}

// detectObfuscatorFeatures detects specific obfuscation techniques from string patterns.
func detectObfuscatorFeatures(strs []string) []string {
	markers := []struct {
		pattern string
		feature string
	}{
		{"IsDebuggerPresent", "anti-debug"},
		{"CheckRemoteDebuggerPresent", "anti-debug"},
		{"proxy_call", "proxy calls"},
		{"ProxyCall", "proxy calls"},
		{"<Module>", "module initializer"},
		{"ConfuserEx.Runtime", "confuserex runtime"},
		{"cctor", "static constructor injection"},
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
func EnrichWithHeuristics(r *Result, sectionNames, importNames, strings []string) {
	if r.Obfuscator == "" {
		r.Obfuscator = detectObfuscatorByStrings(strings)
	}

	features := detectObfuscatorFeatures(strings)
	if len(features) > 0 {
		r.ObfuscatorFeatures = append(r.ObfuscatorFeatures, features...)
	}

	signals := detectEmbeddedSignals(sectionNames)
	if len(signals) > 0 {
		r.EmbeddedSignals = append(r.EmbeddedSignals, signals...)
		r.EmbeddedSuspected = true
	}

	if r.Compiler == "" && hasDelphiMarkers(sectionNames, importNames) {
		r.Compiler = "Delphi"
	}
}
