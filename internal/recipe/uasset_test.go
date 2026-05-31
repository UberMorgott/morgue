package recipe

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// putInt32 / putUint32 append LE values to a buffer.
func putInt32(b *bytes.Buffer, v int32)   { _ = binary.Write(b, binary.LittleEndian, v) }
func putUint32(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.LittleEndian, v) }

// putASCIIFString appends an FString: int32 len (incl. NUL) + bytes + NUL.
func putASCIIFString(b *bytes.Buffer, s string) {
	putInt32(b, int32(len(s)+1))
	b.WriteString(s)
	b.WriteByte(0)
}

// buildSyntheticUAsset builds a minimal valid legacy .uasset with the given
// name-table entries. LegacyFileVersion = -8 (so a UE5 version field is
// present). The name table immediately follows the summary; NameOffset points
// at it. No hash bytes are appended (the parser's plausibility check handles
// both cases).
func buildSyntheticUAsset(names []string) []byte {
	var summary bytes.Buffer
	putUint32(&summary, uassetTag)        // Tag
	putInt32(&summary, -8)                // LegacyFileVersion (<= -8)
	putInt32(&summary, 0)                 // LegacyUE3Version (present since != -4)
	putInt32(&summary, 522)               // FileVersionUE4
	putInt32(&summary, 1009)              // FileVersionUE5 (present since <= -8)
	putInt32(&summary, 0)                 // FileVersionLicenseeUE4
	putInt32(&summary, 0)                 // CustomVersions count (empty)
	putInt32(&summary, 0)                 // TotalHeaderSize (placeholder)
	putASCIIFString(&summary, "")         // FolderName (empty FString => len 1, "\0")
	putUint32(&summary, 0)                // PackageFlags
	putInt32(&summary, int32(len(names))) // NameCount

	// NameOffset = size of summary + 4 (the NameOffset field itself).
	nameOffset := summary.Len() + 4
	putInt32(&summary, int32(nameOffset)) // NameOffset

	// Name table.
	var table bytes.Buffer
	for _, n := range names {
		putASCIIFString(&table, n)
	}

	out := append(summary.Bytes(), table.Bytes()...)
	return out
}

func TestParseUAssetNames(t *testing.T) {
	names := []string{"MyClass", "MyProperty"}
	data := buildSyntheticUAsset(names)

	dir := t.TempDir()
	path := filepath.Join(dir, "Test.uasset")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := parseUAsset(path)
	if err != nil {
		t.Fatalf("parseUAsset: %v", err)
	}
	if info.NameCount != 2 {
		t.Errorf("NameCount = %d, want 2", info.NameCount)
	}
	if len(info.Names) != 2 {
		t.Fatalf("Names = %v, want 2 entries", info.Names)
	}
	if info.Names[0] != "MyClass" || info.Names[1] != "MyProperty" {
		t.Errorf("Names = %v, want [MyClass MyProperty]", info.Names)
	}
	if info.TotalNames != 2 {
		t.Errorf("TotalNames = %d, want 2", info.TotalNames)
	}
}

// buildRetocStubUAsset mimics a `retoc to-legacy` UE5 package: tag,
// LegacyFileVersion=-9, then version/size words, an EMPTY FolderName, and the
// FName table placed right after — with the summary's NameOffset/NameCount left
// as 0 (which is what defeats the textbook sequential parse). The parser must
// fall back to scanNameTableStart to recover the names. This pins the real-file
// regression so it can't silently come back.
func buildRetocStubUAsset(names []string) []byte {
	var b bytes.Buffer
	putUint32(&b, uassetTag) // Tag
	putInt32(&b, -9)         // LegacyFileVersion = -9 (retoc stub)
	// Version/size words the parser walks past — all zero in real stubs.
	putInt32(&b, 0) // LegacyUE3Version (present since LegacyFileVersion != -4)
	putInt32(&b, 0) // FileVersionUE4 (legacy<=-8 path also reads UE5 below)
	putInt32(&b, 0) // FileVersionUE5
	putInt32(&b, 0) // FileVersionLicenseeUE4
	putInt32(&b, 0) // CustomVersionCount (empty)
	putInt32(&b, 0) // TotalHeaderSize (stub leaves 0)
	putInt32(&b, 0) // FolderName FString len = 0 (empty)
	putInt32(&b, 0) // PackageFlags
	putInt32(&b, 0) // NameCount = 0 (not populated)
	putInt32(&b, 0) // NameOffset = 0 (not populated)
	// FName table: each entry = FString(len incl NUL) + bytes + NUL + 4 hash.
	for _, n := range names {
		putInt32(&b, int32(len(n)+1))
		b.WriteString(n)
		b.WriteByte(0)
		putUint32(&b, 0) // 4 hash bytes
	}
	return b.Bytes()
}

func TestParseUAssetRetocStub(t *testing.T) {
	names := []string{"StaticMesh", "BodySetup", "/Script/Engine"}
	data := buildRetocStubUAsset(names)

	dir := t.TempDir()
	path := filepath.Join(dir, "Stub.uasset")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := parseUAsset(path)
	if err != nil {
		t.Fatalf("parseUAsset: %v", err)
	}
	if info.TotalNames != 3 {
		t.Fatalf("TotalNames = %d, want 3 (scan fallback should recover names); got names=%v",
			info.TotalNames, info.Names)
	}
	if info.Names[0] != "StaticMesh" || info.Names[1] != "BodySetup" || info.Names[2] != "/Script/Engine" {
		t.Errorf("Names = %v, want [StaticMesh BodySetup /Script/Engine]", info.Names)
	}
}

func TestParseUAssetBadTag(t *testing.T) {
	data := make([]byte, 64)
	// Wrong tag.
	binary.LittleEndian.PutUint32(data, 0xDEADBEEF)
	dir := t.TempDir()
	path := filepath.Join(dir, "Bad.uasset")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseUAsset(path); err == nil {
		t.Error("expected error for bad tag, got nil")
	}
}

func TestParseUAssetTruncatedNoPanic(t *testing.T) {
	// Valid tag + legacy version, then truncated — must error, never panic.
	full := buildSyntheticUAsset([]string{"A", "B", "C"})
	for _, cut := range []int{8, 16, 24, len(full) - 3} {
		if cut < 0 || cut > len(full) {
			continue
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "Trunc.uasset")
		if err := os.WriteFile(path, full[:cut], 0644); err != nil {
			t.Fatal(err)
		}
		// Just must not panic; error is acceptable and expected for most cuts.
		_, _ = parseUAsset(path)
	}
}

func TestParseUAssetGarbageNoPanic(t *testing.T) {
	// Correct tag but garbage afterwards designed to drive bad lengths/offsets.
	var b bytes.Buffer
	putUint32(&b, uassetTag)
	putInt32(&b, -8)
	// Fill with 0xFF which decodes to large negative/positive values.
	for i := 0; i < 200; i++ {
		b.WriteByte(0xFF)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Garbage.uasset")
	if err := os.WriteFile(path, b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	// Must return an error without panicking (defer/recover proven).
	if _, err := parseUAsset(path); err == nil {
		t.Log("garbage parsed without error (acceptable as long as no panic)")
	}
}

func TestBuildAssetsIndex(t *testing.T) {
	extracted := t.TempDir()
	out := t.TempDir()

	// Two valid assets + one garbage (should be counted as failed).
	good1 := buildSyntheticUAsset([]string{"Alpha", "Beta"})
	good2 := buildSyntheticUAsset([]string{"Gamma"})
	if err := os.WriteFile(filepath.Join(extracted, "One.uasset"), good1, 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(extracted, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "Two.umap"), good2, 0644); err != nil {
		t.Fatal(err)
	}
	// Garbage with valid tag but unparseable body.
	var g bytes.Buffer
	putUint32(&g, uassetTag)
	putInt32(&g, -8)
	for i := 0; i < 100; i++ {
		g.WriteByte(0xFF)
	}
	if err := os.WriteFile(filepath.Join(extracted, "Bad.uasset"), g.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	// A non-package file that must be ignored.
	if err := os.WriteFile(filepath.Join(extracted, "readme.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := buildAssetsIndex(out, extracted)
	if err != nil {
		t.Fatalf("buildAssetsIndex: %v", err)
	}
	if idx.AssetsParsed != 2 {
		t.Errorf("AssetsParsed = %d, want 2", idx.AssetsParsed)
	}
	if idx.AssetsFailed != 1 {
		t.Errorf("AssetsFailed = %d, want 1 (the garbage asset)", idx.AssetsFailed)
	}
	if idx.TotalNames != 3 {
		t.Errorf("TotalNames = %d, want 3 (Alpha,Beta,Gamma)", idx.TotalNames)
	}

	// assets_index.json + assets.ndjson written.
	if !fileExists(filepath.Join(out, "assets_index.json")) {
		t.Error("assets_index.json not written")
	}
	if !fileExists(filepath.Join(out, "assets.ndjson")) {
		t.Error("assets.ndjson not written")
	}
}

func TestBuildAssetsIndexMissingDir(t *testing.T) {
	out := t.TempDir()
	idx, err := buildAssetsIndex(out, filepath.Join(out, "does-not-exist"))
	if err != nil {
		t.Fatalf("buildAssetsIndex on missing dir: %v", err)
	}
	if idx.AssetsParsed != 0 {
		t.Errorf("AssetsParsed = %d, want 0", idx.AssetsParsed)
	}
	if !fileExists(filepath.Join(out, "assets_index.json")) {
		t.Error("assets_index.json should still be written for missing dir")
	}
}
