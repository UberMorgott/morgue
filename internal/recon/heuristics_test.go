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
