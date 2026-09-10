package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVersionValid(t *testing.T) {
	// magic 0xFAB11BAF LE + version 39 LE
	blob := []byte{0xAF, 0x1B, 0xB1, 0xFA, 0x27, 0x00, 0x00, 0x00, 0xDE, 0xAD}
	p := filepath.Join(t.TempDir(), "global-metadata.dat")
	_ = os.WriteFile(p, blob, 0644)
	v, err := ReadVersion(p)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if v != 39 {
		t.Fatalf("version = %d, want 39", v)
	}
}

func TestReadVersionV24(t *testing.T) {
	// magic 0xFAB11BAF LE + version 24 LE (older Unity)
	blob := []byte{0xAF, 0x1B, 0xB1, 0xFA, 0x18, 0x00, 0x00, 0x00}
	p := filepath.Join(t.TempDir(), "v24.dat")
	_ = os.WriteFile(p, blob, 0644)
	v, err := ReadVersion(p)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if v != 24 {
		t.Fatalf("version = %d, want 24", v)
	}
}

func TestReadVersionBadMagic(t *testing.T) {
	blob := []byte{0x00, 0x11, 0x22, 0x33, 0x27, 0x00, 0x00, 0x00}
	p := filepath.Join(t.TempDir(), "bad.dat")
	_ = os.WriteFile(p, blob, 0644)
	if _, err := ReadVersion(p); err == nil {
		t.Fatal("expected error on bad magic")
	}
}

func TestReadVersionTooShort(t *testing.T) {
	p := filepath.Join(t.TempDir(), "short.dat")
	_ = os.WriteFile(p, []byte{0xAF, 0x1B}, 0644)
	if _, err := ReadVersion(p); err == nil {
		t.Fatal("expected error on short file")
	}
}

// TestReadVersionRealLC2 reads the real Lost Castle 2 global-metadata.dat and
// expects metadata version 39. Skipped if the game is not installed locally so
// CI/other machines stay green.
func TestReadVersionRealLC2(t *testing.T) {
	const lc2 = `D:\Steam\steamapps\common\Lost Castle 2\LostCastle2_Data\il2cpp_data\Metadata\global-metadata.dat`
	if _, err := os.Stat(lc2); err != nil {
		t.Skipf("LC2 metadata not present: %v", err)
	}
	v, err := ReadVersion(lc2)
	if err != nil {
		t.Fatalf("ReadVersion(LC2): %v", err)
	}
	if v != 39 {
		t.Fatalf("LC2 metadata version = %d, want 39", v)
	}
}
