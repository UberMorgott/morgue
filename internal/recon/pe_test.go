package recon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyInvalidFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage.exe")
	if err := os.WriteFile(p, []byte("not a PE file"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Classify(p)
	if err != nil {
		t.Fatalf("Classify() should not error on invalid PE, got: %v", err)
	}

	if result.Kind != Unknown {
		t.Errorf("Kind = %v, want Unknown for invalid PE", result.Kind)
	}
	if result.Fallback != true {
		t.Error("Fallback should be true for invalid PE")
	}
	if result.Path != p {
		t.Errorf("Path = %v, want %v", result.Path, p)
	}
}

func TestClassifyMZStub(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stub.exe")
	// Minimal MZ header (DOS stub only, no PE signature)
	mz := make([]byte, 64)
	mz[0] = 'M'
	mz[1] = 'Z'
	if err := os.WriteFile(p, mz, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Classify(p)
	if err != nil {
		t.Fatalf("Classify() should not error on MZ stub, got: %v", err)
	}

	if result.Kind != Unknown {
		t.Errorf("Kind = %v, want Unknown for MZ-only stub", result.Kind)
	}
}

func TestClassifyByExtension(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		want Kind
	}{
		{"dll", ".dll", Managed},
		{"exe", ".exe", Unknown},
		{"so", ".so", Native},
		{"dylib", ".dylib", Native},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyByExtension(tt.ext)
			if got != tt.want {
				t.Errorf("classifyByExtension(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func TestClassifyNonexistent(t *testing.T) {
	result, err := Classify("/nonexistent/path/binary.exe")
	if err != nil {
		t.Fatalf("Classify() should not error on missing file, got: %v", err)
	}
	if result.Kind != Unknown {
		t.Errorf("Kind = %v, want Unknown", result.Kind)
	}
	if !result.Fallback {
		t.Error("Fallback should be true")
	}
}
