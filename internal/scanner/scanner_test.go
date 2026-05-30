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

	groups := groupFiles(result.Files, nil)

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

func TestScanUnrealPaksOnly(t *testing.T) {
	root := t.TempDir()
	// Bare mod/paks folder: pak + IoStore containers, no game structure.
	for _, name := range []string{"Foo_P.pak", "Foo_P.utoc", "Foo_P.ucas"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fake"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	// Pak/IoStore files must NOT pollute the generic binary list.
	if len(result.Files) != 0 {
		t.Errorf("Scan() Files = %d, want 0 (paks are tracked separately)", len(result.Files))
	}

	// Exactly one Unreal group, collapsed to a single representative target.
	var unreal []TargetGroup
	for _, g := range result.Groups {
		if g.Kind == GroupUnreal {
			unreal = append(unreal, g)
		}
	}
	if len(unreal) != 1 {
		t.Fatalf("got %d GroupUnreal groups, want exactly 1", len(unreal))
	}
	if len(result.Groups) != 1 {
		t.Errorf("got %d total groups, want 1 (no standalone from paks)", len(result.Groups))
	}
	if got := len(unreal[0].Files); got != 1 {
		t.Fatalf("Unreal group has %d files, want exactly 1 (single-target collapse)", got)
	}
	// Representative prefers the .pak container.
	if rep := filepath.Base(unreal[0].Files[0]); rep != "Foo_P.pak" {
		t.Errorf("representative = %q, want Foo_P.pak", rep)
	}
}

func sameRootSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[filepath.Clean(g)] = true
	}
	for _, w := range want {
		if !seen[filepath.Clean(w)] {
			return false
		}
	}
	return true
}

func TestDedupeUnrealRoots(t *testing.T) {
	tests := []struct {
		name  string
		roots []string
		want  []string
	}{
		{
			name: "client paks + nested WindowsServer build under Builds -> keep client only",
			roots: []string{
				`X:\Games\R5\R5\Content\Paks`,
				`X:\Games\R5\R5\Builds\WindowsServer\R5\Content\Paks`,
			},
			want: []string{`X:\Games\R5\R5\Content\Paks`},
		},
		{
			name: "two genuinely separate games (no Builds, neither ancestor) -> keep both",
			roots: []string{
				`X:\Games\GameA\Content\Paks`,
				`X:\Games\GameB\Content\Paks`,
			},
			want: []string{
				`X:\Games\GameA\Content\Paks`,
				`X:\Games\GameB\Content\Paks`,
			},
		},
		{
			name: "ancestor + descendant -> keep ancestor only",
			roots: []string{
				`X:\Games\Foo`,
				`X:\Games\Foo\Content\Paks`,
			},
			want: []string{`X:\Games\Foo`},
		},
		{
			name:  "single root unchanged",
			roots: []string{`X:\Games\Foo\Content\Paks`},
			want:  []string{`X:\Games\Foo\Content\Paks`},
		},
		{
			name: "Builds segment matched as component, not substring (Rebuilds kept)",
			roots: []string{
				`X:\Games\Foo\Rebuilds\Content\Paks`,
				`X:\Games\Bar\Content\Paks`,
			},
			want: []string{
				`X:\Games\Foo\Rebuilds\Content\Paks`,
				`X:\Games\Bar\Content\Paks`,
			},
		},
		{
			name: "all roots under Builds (no non-Builds) -> keep all (conservative)",
			roots: []string{
				`X:\A\Builds\WindowsServer\Content\Paks`,
				`X:\B\Builds\WindowsServer\Content\Paks`,
			},
			want: []string{
				`X:\A\Builds\WindowsServer\Content\Paks`,
				`X:\B\Builds\WindowsServer\Content\Paks`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeUnrealRoots(tt.roots)
			if !sameRootSet(got, tt.want) {
				t.Errorf("dedupeUnrealRoots(%v) = %v, want %v", tt.roots, got, tt.want)
			}
		})
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
