package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestDir creates a test directory structure mimicking various binary layouts.
func setupTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Unity IL2CPP layout
	il2cpp := filepath.Join(root, "UnityIL2CPP")
	os.MkdirAll(filepath.Join(il2cpp, "UnityIL2CPP_Data", "il2cpp_data", "Metadata"), 0755)
	os.WriteFile(filepath.Join(il2cpp, "GameAssembly.dll"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(il2cpp, "UnityIL2CPP_Data", "il2cpp_data", "Metadata", "global-metadata.dat"), []byte("fake"), 0644)

	// Unity Mono layout
	mono := filepath.Join(root, "UnityMono")
	os.MkdirAll(filepath.Join(mono, "UnityMono_Data", "Managed"), 0755)
	os.WriteFile(filepath.Join(mono, "UnityMono_Data", "Managed", "Assembly-CSharp.dll"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(mono, "UnityMono_Data", "Managed", "UnityEngine.dll"), []byte("fake"), 0644)

	// Standalone DLLs
	standalone := filepath.Join(root, "standalone")
	os.MkdirAll(standalone, 0755)
	os.WriteFile(filepath.Join(standalone, "app.exe"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(standalone, "lib.dll"), []byte("fake"), 0644)

	return root
}

func TestScan(t *testing.T) {
	root := setupTestDir(t)
	result, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if len(result.Files) == 0 {
		t.Error("Scan() found no files")
	}

	// Should find at least: GameAssembly.dll, global-metadata.dat, Assembly-CSharp.dll, UnityEngine.dll, app.exe, lib.dll
	if len(result.Files) < 5 {
		t.Errorf("Scan() found %d files, want >= 5", len(result.Files))
	}
}

func TestGroupFiles(t *testing.T) {
	root := setupTestDir(t)
	result, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	groups := groupFiles(result.Files)

	var hasIL2CPP, hasMono, hasStandalone bool
	for _, g := range groups {
		switch g.Kind {
		case GroupUnityIL2CPP:
			hasIL2CPP = true
		case GroupUnityMono:
			hasMono = true
		case GroupStandalone:
			hasStandalone = true
		}
	}

	if !hasIL2CPP {
		t.Error("Missing Unity IL2CPP group")
	}
	if !hasMono {
		t.Error("Missing Unity Mono group")
	}
	if !hasStandalone {
		t.Error("Missing Standalone group")
	}
}

func TestScanEmpty(t *testing.T) {
	dir := t.TempDir()
	result, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(result.Files) != 0 {
		t.Errorf("Scan() found %d files in empty dir, want 0", len(result.Files))
	}
}
