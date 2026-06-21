package odin

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustNoPanic runs fn and fails the test if it panics (instead of crashing the
// whole test binary). Used to assert the decoder never panics on bad input.
func mustNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked on malformed input: %v", name, r)
		}
	}()
	fn()
}

// writeAssetWithBytes writes a minimal MonoBehaviour .asset whose
// "SerializedBytes:" line carries the given raw blob (hex-encoded).
func writeAssetWithBytes(t *testing.T, dir, name string, blob []byte) {
	t.Helper()
	body := "MonoBehaviour:\n  m_Name: " + name + "\n  SerializedBytes: " + hex.EncodeToString(blob) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDecodeTruncatedBlobNoPanic(t *testing.T) {
	// A NamedStartOfStructNode (0x03) followed by a string flag+length claiming
	// 100 chars but no payload — readString runs past the buffer.
	blob := []byte{0x03, 0x01, 0x64, 0x00, 0x00, 0x00}
	var err error
	mustNoPanic(t, "decodeBlob/truncated", func() {
		_, err = decodeBlob(blob)
	})
	if err == nil {
		t.Fatalf("expected decode error for truncated blob, got nil")
	}
}

func TestDecodeTruncatedAtReadNoPanic(t *testing.T) {
	// NamedInt (0x17) with a name but the int32 value truncated mid-read.
	blob := []byte{0x17, 0x01, 0x01, 0x00, 0x00, 0x00, 'x', 0x00, 0x01, 0x02}
	var err error
	mustNoPanic(t, "decodeBlob/truncated-read", func() {
		_, err = decodeBlob(blob)
	})
	if err == nil {
		t.Fatalf("expected decode error for truncated int read, got nil")
	}
}

func TestDecodeOversizedPrimArrayNoOOM(t *testing.T) {
	// PrimitiveArray (0x08) claiming INT32_MAX elements of 2 bytes each — a naive
	// make() would try to allocate ~4 GB. The guard must reject it as an error
	// instead of allocating or panicking with OOM.
	blob := []byte{
		0x08,
		0xFF, 0xFF, 0xFF, 0x7F, // count = 2147483647
		0x02, 0x00, 0x00, 0x00, // bytesPer = 2
		// no payload follows
	}
	var err error
	mustNoPanic(t, "decodeBlob/oversized-primarray", func() {
		_, err = decodeBlob(blob)
	})
	if err == nil {
		t.Fatalf("expected decode error for oversized primarray, got nil")
	}
	if !strings.Contains(err.Error(), "primarray") {
		t.Fatalf("error should mention primarray, got: %v", err)
	}
}

func TestDecodeNegativeStringLenNoPanic(t *testing.T) {
	// NamedString-ish: a string with a negative length must be rejected, not panic.
	blob := []byte{0x28, 0x01, 0xFF, 0xFF, 0xFF, 0xFF} // unnamed string, count = -1
	var err error
	mustNoPanic(t, "decodeBlob/negative-strlen", func() {
		_, err = decodeBlob(blob)
	})
	if err == nil {
		t.Fatalf("expected decode error for negative string length, got nil")
	}
}

func TestDecodeDirSkipsBadFileContinues(t *testing.T) {
	dir := t.TempDir()
	// good.asset: a valid struct/int/end blob (buildBlob from decoder_test.go).
	writeAssetWithBytes(t, dir, "good.asset", buildBlob())
	// bad.asset: truncated string payload -> decode error.
	writeAssetWithBytes(t, dir, "bad.asset", []byte{0x03, 0x01, 0x64, 0x00, 0x00, 0x00})

	var out string
	mustNoPanic(t, "DecodeDir/with-bad-file", func() {
		out = DecodeDir(dir, []string{"good.asset", "bad.asset", "missing.asset"})
	})

	// The good file must still decode (its root struct appears).
	if !strings.Contains(out, "root { ") {
		t.Fatalf("good file not decoded; output:\n%s", out)
	}
	// The bad file must be logged as a decode error, not abort the batch.
	if !strings.Contains(out, "## bad.asset") || !strings.Contains(out, "DECODE ERROR") {
		t.Fatalf("bad file not logged as decode error; output:\n%s", out)
	}
	// The missing file must be reported and not crash.
	if !strings.Contains(out, "## missing.asset: MISSING") {
		t.Fatalf("missing file not reported; output:\n%s", out)
	}
}
