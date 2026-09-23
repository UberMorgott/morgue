package recon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	peparser "github.com/saferwall/pe"
)

func TestClassifyInvalidFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage.exe")
	if err := os.WriteFile(p, []byte("not a PE file"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Classify(context.Background(), p)
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

	result, err := Classify(context.Background(), p)
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
		{"dll", ".dll", Unknown},
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
	result, err := Classify(context.Background(), "/nonexistent/path/binary.exe")
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

func TestIsNativeAOT(t *testing.T) {
	aot := &peparser.File{Export: peparser.Export{Functions: []peparser.ExportFunction{{Name: "DotNetRuntimeDebugHeader"}}}}
	if !isNativeAOT(aot) {
		t.Error("isNativeAOT = false for image exporting DotNetRuntimeDebugHeader")
	}
	plain := &peparser.File{Export: peparser.Export{Functions: []peparser.ExportFunction{{Name: "SDL_Init"}}}}
	if isNativeAOT(plain) {
		t.Error("isNativeAOT = true for plain native DLL")
	}
}

func TestClassifyJava(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
		want Kind
	}{
		{"app.jar", []byte("PK\x03\x04rest"), Java},
		{"app.war", []byte("PK\x03\x04rest"), Java},
		{"Main.class", []byte{0xCA, 0xFE, 0xBA, 0xBE, 0, 0}, Java},
		{"notes.jar", []byte("plain text"), Unknown},
		{"fat.bin", []byte{0xCA, 0xFE, 0xBA, 0xBE, 0, 0}, Unknown},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		if err := os.WriteFile(p, c.data, 0644); err != nil {
			t.Fatal(err)
		}
		r, err := Classify(context.Background(), p)
		if err != nil {
			t.Fatalf("Classify(%s): %v", c.name, err)
		}
		if r.Kind != c.want {
			t.Errorf("Classify(%s).Kind = %v, want %v", c.name, r.Kind, c.want)
		}
	}
}
