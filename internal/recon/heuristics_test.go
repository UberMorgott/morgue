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
		name     string
		sections []string
		want     int
	}{
		{"rsrc", []string{".rsrc", ".text"}, 1},
		{"none", []string{".text", ".data"}, 0},
		{"multiple", []string{".rsrc", ".themida"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectEmbeddedSignals(tt.sections)
			if len(got) != tt.want {
				t.Errorf("detectEmbeddedSignals() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestDetectObfuscatorFeatures(t *testing.T) {
	tests := []struct {
		name    string
		strings []string
		want    int
	}{
		{"anti-debug", []string{"IsDebuggerPresent"}, 1},
		{"proxy calls", []string{"something proxy_call somewhere"}, 1},
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
