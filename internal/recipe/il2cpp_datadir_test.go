package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindGameDataDirSteamRoot mirrors the real Lost Castle 2 Steam layout:
// GameAssembly.dll at the game root, with LostCastle2_Data as a CHILD directory.
func TestFindGameDataDirSteamRoot(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "Lost Castle 2")
	dataDir := filepath.Join(gameRoot, "LostCastle2_Data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	ga := filepath.Join(gameRoot, "GameAssembly.dll")
	if err := os.WriteFile(ga, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findGameDataDir(ga); got != dataDir {
		t.Fatalf("findGameDataDir = %q, want %q", got, dataDir)
	}
}

// TestFindGameDataDirInsideData covers a binary that already lives inside *_Data.
func TestFindGameDataDirInsideData(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "Game_Data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	ga := filepath.Join(dataDir, "GameAssembly.dll")
	if err := os.WriteFile(ga, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findGameDataDir(ga); got != dataDir {
		t.Fatalf("findGameDataDir = %q, want %q", got, dataDir)
	}
}

// TestFindGameDataDirSibling covers the binary sitting beside (not inside) *_Data.
func TestFindGameDataDirSibling(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	dataDir := filepath.Join(root, "Game_Data")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	ga := filepath.Join(binDir, "GameAssembly.dll")
	if err := os.WriteFile(ga, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findGameDataDir(ga); got != dataDir {
		t.Fatalf("findGameDataDir = %q, want %q", got, dataDir)
	}
}

// TestFindGameDataDirNone returns "" when no *_Data dir exists anywhere relevant.
func TestFindGameDataDirNone(t *testing.T) {
	root := t.TempDir()
	ga := filepath.Join(root, "GameAssembly.dll")
	if err := os.WriteFile(ga, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findGameDataDir(ga); got != "" {
		t.Fatalf("findGameDataDir = %q, want empty", got)
	}
}
