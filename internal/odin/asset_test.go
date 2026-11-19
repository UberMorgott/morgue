package odin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeAssetFileSingle(t *testing.T) {
	blob := sampleBlob()
	p := writeOdinAsset(t, t.TempDir(), "Sample", "0123456789abcdef0123456789abcdef", blob)
	out, n, err := DecodeAssetFile(p)
	if err != nil {
		t.Fatalf("DecodeAssetFile: %v", err)
	}
	if n != len(blob) {
		t.Fatalf("blob size = %d, want %d", n, len(blob))
	}
	if out == "" {
		t.Fatalf("empty decode output")
	}
	// First non-empty line of the tree should be the root struct.
	if want := "_data { <Asset+Data>"; !contains(out, want) {
		t.Fatalf("decoded output missing %q", want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestGetHexMissingMarker(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "no-bytes.asset")
	_ = os.WriteFile(tmp, []byte("MonoBehaviour:\n  m_Name: x\n"), 0644)
	if _, err := getHex(tmp); err == nil {
		t.Fatalf("expected error for file with no SerializedBytes")
	}
}
