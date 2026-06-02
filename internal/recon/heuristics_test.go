package recon

import (
	"testing"
)

func TestPuaCharDensity(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want float64
	}{
		{"empty", "", 0},
		{"normal", "System.Runtime", 0},
		{"high pua", "\ue000\ue001\ue002abc", 0.5},
		{"all pua", "\ue000\ue001\ue002", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := puaCharDensity(tt.s)
			if got != tt.want {
				t.Errorf("puaCharDensity(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestDetectObfuscatorByStrings(t *testing.T) {
	tests := []struct {
		name    string
		strings []string
		want    string
	}{
		{"confuserex", []string{"ConfuserEx v1.0.0"}, "ConfuserEx"},
		{"dotfuscator", []string{"Dotfuscator Community Edition"}, "Dotfuscator"},
		{"obfuscar", []string{"Obfuscar"}, "Obfuscar"},
		{"net reactor", []string{".NET Reactor"}, ".NET Reactor"},
		{"none", []string{"System.Runtime", "mscorlib"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectObfuscatorByStrings(tt.strings)
			if got != tt.want {
				t.Errorf("detectObfuscatorByStrings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasDelphiMarkers(t *testing.T) {
	tests := []struct {
		name     string
		sections []string
		imports  []string
		want     bool
	}{
		{"delphi sections", []string{"CODE", ".data"}, nil, true},
		{"delphi imports", []string{".text"}, []string{"borlndmm.dll"}, true},
		{"no delphi", []string{".text", ".data"}, []string{"kernel32.dll"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDelphiMarkers(tt.sections, tt.imports)
			if got != tt.want {
				t.Errorf("hasDelphiMarkers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectEmbeddedSignals(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"costura", []byte("something costura. embedded"), 1},
		{"costura loader", []byte("costura.something and Costura.AssemblyLoader found"), 2}, // matches both costura. and Costura.AssemblyLoader
		{"ilmerge", []byte("ILMerge marker"), 1},
		{"none", []byte("normal binary data"), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectEmbeddedSignals(tt.data)
			if len(got) != tt.want {
				t.Errorf("detectEmbeddedSignals() len = %d, want %d; signals = %v", len(got), tt.want, got)
			}
		})
	}
}

func TestIsMangledName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"a", true},        // single lowercase — renamer
		{"ab", true},       // two lowercase — renamer
		{"a`1", true},        // mangled generic (arity stripped before check)
		{"", true}, // private-use-area unicode — renamer
		{"Id", false},      // two chars but not all-lowercase
		{"To", false},      // capitalized
		{"GetUserAsync", false},
		{"<Module>", false},
		{"WebMapController", false},
	}
	for _, tt := range tests {
		if got := isMangledName(tt.name); got != tt.want {
			t.Errorf("isMangledName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMangledTypeRatio(t *testing.T) {
	clean := []string{"<Module>", "UserService", "OrderController", "AuthHandler", "TokenStore"}
	if ratio, total := mangledTypeRatio(clean); ratio >= mangledTypeThreshold {
		t.Errorf("clean assembly flagged: ratio=%.2f total=%d", ratio, total)
	}
	obf := []string{"<Module>", "a", "b", "c", "aa", "ab", "ac", "ba", "UserService"}
	if ratio, total := mangledTypeRatio(obf); ratio < mangledTypeThreshold {
		t.Errorf("obfuscated assembly missed: ratio=%.2f total=%d", ratio, total)
	}
}

func TestEnrichConfuserExFromMangling(t *testing.T) {
	// Managed assembly with mostly-mangled type names and no name-string marker
	// must be flagged as generically obfuscated. Name mangling proves obfuscation
	// but does NOT identify the tool (the a/b/aa renaming is shared across the
	// renamer family), so the family-agnostic GenericObfuscated value is expected;
	// a family-specific signal (e.g. the ConfuserEx string-decrypter probe) refines
	// it later.
	r := &Result{Kind: Managed}
	typeNames := make([]string, 0, 40)
	typeNames = append(typeNames, "<Module>", "PublicApi")
	for c := 'a'; c <= 'z'; c++ {
		typeNames = append(typeNames, string(c), string(c)+"a")
	}
	EnrichWithHeuristics(r, nil, nil, nil, nil, typeNames)
	if r.Obfuscator != GenericObfuscated {
		t.Errorf("Obfuscator = %q, want %q", r.Obfuscator, GenericObfuscated)
	}
	if !r.NeedsDeobfuscation() {
		t.Errorf("NeedsDeobfuscation() = false, want true for mangled assembly")
	}

	// Clean managed assembly must not be flagged.
	clean := &Result{Kind: Managed}
	cleanNames := []string{"<Module>", "UserService", "OrderController", "AuthHandler",
		"TokenStore", "DbContext", "MapController", "ReportService", "BillingJob",
		"SchemaUploader", "AccountManager", "SessionStore", "CacheLayer", "HttpClientFactory",
		"ConfigLoader", "EventBus", "QueueWorker", "MetricsCollector", "HealthCheck", "Startup", "Program"}
	EnrichWithHeuristics(clean, nil, nil, nil, nil, cleanNames)
	if clean.Obfuscator != "" {
		t.Errorf("clean Obfuscator = %q, want empty", clean.Obfuscator)
	}
}

func TestDetectObfuscatorFeatures(t *testing.T) {
	tests := []struct {
		name    string
		strings []string
		want    int
	}{
		{"anti_tamper", []string{"AntiTamper"}, 1},
		{"anti_debug", []string{"AntiDebug"}, 1},
		{"ctrl_flow", []string{"ControlFlow"}, 1},
		{"ref_proxy", []string{"ReferenceProxy"}, 1},
		{"none", []string{"normal string"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectObfuscatorFeatures(tt.strings)
			if len(got) < tt.want {
				t.Errorf("detectObfuscatorFeatures() len = %d, want >= %d", len(got), tt.want)
			}
		})
	}
}
