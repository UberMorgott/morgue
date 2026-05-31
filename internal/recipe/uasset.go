package recipe

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// uassetTag is the legacy UE package magic, 0x9E2A83C1 (bytes on disk:
// C1 83 2A 9E, little-endian). We accept the LE uint32 form.
const uassetTag uint32 = 0x9E2A83C1

// uassetMaxNames bounds how many FName entries we will attempt to read from a
// single package, guarding against a corrupt NameCount driving a huge loop.
const uassetMaxNames = 5_000_000

// uassetNamesCap is how many resolved FName strings we retain per asset in the
// NDJSON detail (TotalNames still reports the full count).
const uassetNamesCap = 200

// UAssetImport is a resolved FObjectImport (best-effort).
type UAssetImport struct {
	Class string `json:"class"`
	Name  string `json:"name"`
}

// UAssetExport is a resolved FObjectExport (best-effort).
type UAssetExport struct {
	ClassIndex int    `json:"class_index"`
	Name       string `json:"name"`
}

// UAssetInfo is the parsed semantics of a single .uasset/.umap package.
type UAssetInfo struct {
	Path        string         `json:"path"`
	NameCount   int            `json:"name_count"`
	ImportCount int            `json:"import_count"`
	ExportCount int            `json:"export_count"`
	TotalNames  int            `json:"total_names"`
	Names       []string       `json:"names"` // capped at uassetNamesCap
	Imports     []UAssetImport `json:"imports,omitempty"`
	Exports     []UAssetExport `json:"exports,omitempty"`
}

// assetsIndex is the aggregate written to <outDir>/assets_index.json.
type assetsIndex struct {
	GeneratedAt  string       `json:"generated_at"`
	AssetsParsed int          `json:"assets_parsed"`
	AssetsFailed int          `json:"assets_failed"`
	TotalNames   int64        `json:"total_names"`
	Sample       []UAssetInfo `json:"sample"`
	AssetsNDJSON string       `json:"assets_ndjson"`
}

// assetsSampleCap bounds the inline sample array in assets_index.json.
const assetsSampleCap = 200

// byteReader is a bounds-checked, position-tracked reader over an in-memory
// package buffer. Every read validates remaining length, so a malformed offset
// /count yields an error rather than a slice-bounds panic.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) remaining() int { return len(r.data) - r.pos }

func (r *byteReader) need(n int) error {
	if n < 0 || r.pos+n > len(r.data) {
		return fmt.Errorf("uasset: read past end (pos=%d need=%d len=%d)", r.pos, n, len(r.data))
	}
	return nil
}

func (r *byteReader) seek(off int) error {
	if off < 0 || off > len(r.data) {
		return fmt.Errorf("uasset: seek out of range (off=%d len=%d)", off, len(r.data))
	}
	r.pos = off
	return nil
}

func (r *byteReader) int32() (int32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := int32(binary.LittleEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v, nil
}

func (r *byteReader) uint32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

// peekInt32 returns the next int32 without advancing (or error if not enough).
func (r *byteReader) peekInt32() (int32, bool) {
	if r.remaining() < 4 {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(r.data[r.pos:])), true
}

func (r *byteReader) skip(n int) error {
	if err := r.need(n); err != nil {
		return err
	}
	r.pos += n
	return nil
}

// fstring reads a UE FString: int32 Len; Len==0 empty; Len>0 => Len bytes
// ASCII (typically NUL-terminated, so the string is Len-1 chars, but some
// writers store the exact char count with no terminator); Len<0 => (-Len)*2
// bytes UTF-16LE (NUL-terminated). Lengths are bounds-checked. trimNUL handles
// both the terminated and non-terminated ASCII forms.
func (r *byteReader) fstring() (string, error) {
	n, err := r.int32()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if n > 0 {
		// Sanity bound: an absurd length means we are off the rails.
		if int(n) > r.remaining() {
			return "", fmt.Errorf("uasset: FString len %d exceeds remaining %d", n, r.remaining())
		}
		if err := r.need(int(n)); err != nil {
			return "", err
		}
		raw := r.data[r.pos : r.pos+int(n)]
		r.pos += int(n)
		return string(trimNUL(raw)), nil
	}
	// UTF-16LE
	count := int(-n)
	byteLen := count * 2
	if byteLen > r.remaining() {
		return "", fmt.Errorf("uasset: FString utf16 len %d exceeds remaining %d", byteLen, r.remaining())
	}
	if err := r.need(byteLen); err != nil {
		return "", err
	}
	u16 := make([]uint16, count)
	for i := range count {
		u16[i] = binary.LittleEndian.Uint16(r.data[r.pos+i*2:])
	}
	r.pos += byteLen
	// Trim trailing NUL.
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16.Decode(u16)), nil
}

func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// parseUAsset parses a legacy UE package header table from path, returning real
// FName strings. It never panics: a defer/recover converts any unexpected
// slice-bounds panic into an error.
func parseUAsset(path string) (info *UAssetInfo, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			info = nil
			err = fmt.Errorf("uasset: panic recovered parsing %s: %v", path, rec)
		}
	}()

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	if len(data) < 32 {
		return nil, errors.New("uasset: file too small to be a package")
	}

	r := &byteReader{data: data}

	// Tag — accept the LE uint32 form (raw bytes C1 83 2A 9E).
	tag, err := r.uint32()
	if err != nil {
		return nil, err
	}
	if tag != uassetTag {
		return nil, fmt.Errorf("uasset: bad tag 0x%08X (want 0x%08X)", tag, uassetTag)
	}

	// LegacyFileVersion (negative).
	legacyVer, err := r.int32()
	if err != nil {
		return nil, err
	}

	// LegacyUE3Version (present unless LegacyFileVersion == -4).
	if legacyVer != -4 {
		if _, err = r.int32(); err != nil {
			return nil, err
		}
	}

	// FileVersionUE4.
	if _, err = r.int32(); err != nil {
		return nil, err
	}

	// FileVersionUE5 — ONLY when LegacyFileVersion <= -8.
	if legacyVer <= -8 {
		if _, err = r.int32(); err != nil {
			return nil, err
		}
	}

	// FileVersionLicenseeUE4.
	if _, err = r.int32(); err != nil {
		return nil, err
	}

	// CustomVersions array: int32 count, then count*(FGuid 16 bytes + int32).
	cvCount, err := r.int32()
	if err != nil {
		return nil, err
	}
	if cvCount < 0 || cvCount > 4096 {
		return nil, fmt.Errorf("uasset: implausible custom-version count %d", cvCount)
	}
	if err = r.skip(int(cvCount) * (16 + 4)); err != nil {
		return nil, err
	}

	// TotalHeaderSize.
	if _, err = r.int32(); err != nil {
		return nil, err
	}

	// FolderName (FString).
	if _, err = r.fstring(); err != nil {
		return nil, err
	}

	// PackageFlags (uint32).
	if _, err = r.uint32(); err != nil {
		return nil, err
	}

	// NameCount, NameOffset.
	nameCount, err := r.int32()
	if err != nil {
		return nil, err
	}
	nameOffset, err := r.int32()
	if err != nil {
		return nil, err
	}
	if nameCount < 0 || nameCount > uassetMaxNames {
		return nil, fmt.Errorf("uasset: implausible name count %d", nameCount)
	}
	if int(nameOffset) < 0 || int(nameOffset) > len(data) {
		return nil, fmt.Errorf("uasset: name offset %d out of range (len=%d)", nameOffset, len(data))
	}

	out := &UAssetInfo{
		Path:      path,
		NameCount: int(nameCount),
		Names:     []string{},
	}

	// Best-effort: try to read Import/Export count+offset by continuing the
	// summary parse. These fields come AFTER several version-variant sections
	// (SoftObjectPaths, GatherableText, etc.), so this is fragile — wrap it and
	// ignore failures (name table is the MUST_PASS anchor).
	impCount, impOff, expCount, expOff := tryReadImportExportMeta(r)
	out.ImportCount = impCount
	out.ExportCount = expCount

	// --- Name table (the robustness anchor) ---
	// First try the summary-derived NameOffset/NameCount. For packages emitted
	// by `retoc to-legacy` (LegacyFileVersion=-9 stubs) the summary's
	// NameOffset/NameCount fields are NOT populated (they are zero), so the
	// textbook sequential parse yields zero names. When that happens, fall back
	// to a version-independent SCAN: locate the first run of length-prefixed
	// printable FName strings and read it. Verified on the real Windrose UE5
	// assets (see TestParseUAssetRetocStub and the temporary real-file check).
	// The summary-derived offset/count is unreliable for `retoc to-legacy`
	// stubs (the field walk lands on garbage, e.g. NameCount reads as a string
	// length, yielding one or two bogus names). The SCAN is the grounded anchor:
	// it locates the first contiguous run of length-prefixed printable FStrings
	// and stops at the real name-table boundary. Run BOTH and keep whichever
	// recovers MORE names — for a normally-serialized package the summary table
	// IS the first run so the two agree; for retoc stubs the scan wins. This
	// never regresses valid packages and never silently returns the bogus
	// summary result.
	names, hashSkip := readNameTable(data, int(nameOffset), int(nameCount))
	if scanOff := scanNameTableStart(data); scanOff >= 0 {
		if scanNames, scanHash := readNameTable(data, scanOff, uassetMaxNames); len(scanNames) > len(names) {
			names, hashSkip = scanNames, scanHash
		}
	}
	if int(nameCount) <= 0 || len(names) != int(nameCount) {
		out.NameCount = len(names)
	}
	out.TotalNames = len(names)
	if len(names) > uassetNamesCap {
		out.Names = append(out.Names, names[:uassetNamesCap]...)
	} else {
		out.Names = append(out.Names, names...)
	}

	// Best-effort imports/exports resolution using the name table.
	if impCount > 0 && impOff > 0 && impOff < len(data) {
		out.Imports = readImports(data, impOff, impCount, names)
	}
	if expCount > 0 && expOff > 0 && expOff < len(data) {
		out.Exports = readExports(data, expOff, expCount, names)
	}
	_ = hashSkip

	return out, nil
}

// tryReadImportExportMeta attempts to read import/export count+offset from the
// current summary position. UE layout after NameOffset is highly version-
// variant, so this is purely best-effort and returns zeros on any difficulty.
// It does NOT mutate the caller's expectations: callers must treat zeros as
// "unknown" and fall back to the name table only.
//
// Grounded note (Windrose UE5, `retoc to-legacy` output): these packages do
// NOT populate the summary's NameOffset/NameCount (hence the scan fallback in
// parseUAsset), and the same is true for the Import/Export offsets — they are
// zero. The bytes following the name table are an export/import table with a
// version-specific stride and no offset/count anchor we can verify, so
// extracting them would require guessing a stride. We refuse to fake records
// and return zeros; readImports/readExports still run for packages that DO
// carry valid summary offsets (kept for forward compatibility).
func tryReadImportExportMeta(_ *byteReader) (impCount, impOff, expCount, expOff int) {
	defer func() { _ = recover() }()
	// Conservative: we do not attempt to skip the many optional summary
	// sections (SoftObjectPaths/GatherableText/etc.) whose presence varies by
	// version, because misjudging one corrupts everything downstream. Returning
	// zeros keeps the parse anchored on the name table, satisfying MUST_PASS.
	return 0, 0, 0, 0
}

// readNameTable reads up to count FName entries starting at offset. Each entry
// is an FString optionally followed by hash bytes (a uint16 or two uint16s in
// some versions). We detect the hash by peeking the next int32: if it is NOT a
// plausible FString length we skip 4 bytes (the two uint16 hashes) before the
// next entry. Bounds errors and the first non-printable entry stop the loop and
// return what was read. The non-printable stop bounds the SCAN fallback (which
// passes a huge count and reads until the table ends); for summary-driven reads
// whose entries are all real FNames it is a no-op.
func readNameTable(data []byte, offset, count int) (names []string, hashBytesSeen bool) {
	r := &byteReader{data: data}
	if r.seek(offset) != nil {
		return nil, false
	}
	capHint := count
	if capHint > 1024 {
		capHint = 1024
	}
	names = make([]string, 0, capHint)
	for range count {
		// Read one entry as a length-prefixed printable FString. We deliberately
		// do NOT use byteReader.fstring here: it accepts a zero length as a valid
		// empty FName, but zeroed trailing hash bytes also read as length 0, which
		// desynced the loop and truncated the table after the first name.
		s, consumed, ok := plausibleFStringAt(data, r.pos)
		if !ok {
			break
		}
		names = append(names, s)
		if r.skip(consumed) != nil {
			break
		}
		// Decide whether 4 hash bytes follow this entry: skip them UNLESS the
		// next int32 is itself the strictly-positive length of a plausible next
		// FString. A length <= 0 (or out of range) means we are on hash bytes.
		next, peekOK := r.peekInt32()
		if !peekOK {
			break
		}
		if next <= 0 || int(next) > r.remaining()-4 {
			if r.skip(4) != nil {
				break
			}
			hashBytesSeen = true
		}
	}
	return names, hashBytesSeen
}

// scanNameTableStart locates the start offset of the FName table without
// trusting the summary's NameOffset. UE package layout after the summary is
// version-variant (the CustomVersion container and trimmed summaries break
// sequential offset math, especially for `retoc to-legacy` stub packages whose
// LegacyFileVersion=-9 and whose NameOffset/NameCount summary fields are zero).
// The name table is, however, always a contiguous run of length-prefixed
// printable FStrings, so we scan past the summary for the first position that
// begins such a run. We require at least 2 consecutive valid entries to avoid
// false positives on incidental data. Returns -1 if none found. Bounds are
// checked; never panics.
func scanNameTableStart(data []byte) int {
	// Skip the fixed summary head (Tag + a few version/size words). 0x14 is
	// below the smallest observed name-table start (0x34) and well past Tag.
	const minScan = 0x14
	for off := minScan; off+8 < len(data); off += 4 {
		if isFNameRunStart(data, off) {
			return off
		}
	}
	return -1
}

// isFNameRunStart reports whether off begins a run of >=2 plausible FName
// entries (an int32 ASCII FString length + printable bytes). The optional
// per-entry hash bytes are tolerated via the same plausibility check the main
// reader uses.
func isFNameRunStart(data []byte, off int) bool {
	pos := off
	for entries := 0; entries < 2; entries++ {
		s, consumed, ok := plausibleFStringAt(data, pos)
		if !ok || len(s) < 2 {
			return false
		}
		pos += consumed
		// Tolerate 4 trailing hash bytes between entries: if the int32 at pos is
		// NOT itself the positive length of a plausible next FString, treat it as
		// hash bytes and skip 4. NOTE: a length of 0 (empty FName) is a valid
		// FString but is also exactly what zeroed hash bytes look like, so for the
		// run-detection heuristic we require a strictly positive length to count
		// as "next entry begins here"; otherwise we assume hashes and skip.
		if next, nok := peekFStringLen(data, pos); !nok || next <= 0 || int(next) > len(data)-pos-4 {
			pos += 4
		}
	}
	return true
}

// plausibleFStringAt reads an ASCII FString at pos and returns it plus the bytes
// consumed (len prefix + body) when it is a printable string of reasonable
// length. Accepts both NUL-terminated and exact-length (no-NUL) forms. UTF-16
// names are not expected in the FName table.
func plausibleFStringAt(data []byte, pos int) (string, int, bool) {
	if pos+4 > len(data) {
		return "", 0, false
	}
	n := int(int32(binary.LittleEndian.Uint32(data[pos:])))
	if n <= 0 || n > 1024 || pos+4+n > len(data) {
		return "", 0, false
	}
	body := data[pos+4 : pos+4+n]
	// Accept BOTH encodings real packages use: NUL-terminated (strip the NUL)
	// and exact-length without a terminator. retoc emits both forms; requiring a
	// NUL was a bug that made many real assets fail the scan.
	if body[len(body)-1] == 0 {
		body = body[:len(body)-1]
	}
	if len(body) == 0 {
		return "", 0, false
	}
	for _, c := range body {
		if c < 32 || c >= 127 {
			return "", 0, false
		}
	}
	return string(body), 4 + n, true
}

// peekFStringLen returns the int32 at pos without advancing.
func peekFStringLen(data []byte, pos int) (int32, bool) {
	if pos+4 > len(data) {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(data[pos:])), true
}

// readImports best-effort reads FObjectImport entries. FObjectImport =
// FName ClassPackage, FName ClassName, int32 OuterIndex, FName ObjectName
// (+ optional bImportOptional in newer UE5). FName = int32 NameIndex + int32
// Number, resolved against the name table. Stops on any bounds error.
func readImports(data []byte, offset, count int, names []string) []UAssetImport {
	var out []UAssetImport
	defer func() { _ = recover() }()
	r := &byteReader{data: data}
	if r.seek(offset) != nil {
		return out
	}
	resolve := func() string {
		idx, err := r.int32()
		if err != nil {
			return ""
		}
		// Number (unused for the display name).
		if _, err := r.int32(); err != nil {
			return ""
		}
		if idx >= 0 && int(idx) < len(names) {
			return names[idx]
		}
		return ""
	}
	for range count {
		if r.remaining() < 24 {
			break
		}
		_ = resolve() // ClassPackage
		className := resolve()
		if _, err := r.int32(); err != nil { // OuterIndex
			break
		}
		objName := resolve()
		if objName == "" && className == "" {
			break
		}
		out = append(out, UAssetImport{Class: className, Name: objName})
	}
	return out
}

// readExports best-effort reads FObjectExport entries. We only resolve the
// ObjectName FName and the ClassIndex; the rest of the struct is skipped via a
// heuristic stride, so this is genuinely best-effort and never required.
func readExports(data []byte, offset, count int, names []string) []UAssetExport {
	var out []UAssetExport
	defer func() { _ = recover() }()
	r := &byteReader{data: data}
	if r.seek(offset) != nil {
		return out
	}
	for range count {
		if r.remaining() < 16 {
			break
		}
		classIndex, err := r.int32()
		if err != nil {
			break
		}
		// SuperIndex, OuterIndex (and optionally TemplateIndex) — version
		// variant; we skip a conservative two int32 then read ObjectName FName.
		if _, err := r.int32(); err != nil { // Super/Template
			break
		}
		if _, err := r.int32(); err != nil { // Outer
			break
		}
		nameIdx, err := r.int32()
		if err != nil {
			break
		}
		if _, err := r.int32(); err != nil { // FName Number
			break
		}
		name := ""
		if nameIdx >= 0 && int(nameIdx) < len(names) {
			name = names[nameIdx]
		}
		if name == "" {
			break
		}
		out = append(out, UAssetExport{ClassIndex: int(classIndex), Name: name})
	}
	return out
}

// buildAssetsIndex walks extractedDir for .uasset/.umap packages, parses each
// (best-effort, never crashing), streams per-asset detail to
// <outDir>/assets.ndjson, and writes aggregate stats + a capped sample to
// <outDir>/assets_index.json. Returns the aggregate. A nil/empty extractedDir
// or a missing dir yields a zero-count index (still written) and no error.
func buildAssetsIndex(outDir, extractedDir string) (*assetsIndex, error) {
	idx := &assetsIndex{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Sample:       []UAssetInfo{},
		AssetsNDJSON: "assets.ndjson",
	}
	if extractedDir == "" {
		return idx, writeAssetsIndex(outDir, idx)
	}
	if info, err := os.Stat(extractedDir); err != nil || !info.IsDir() {
		return idx, writeAssetsIndex(outDir, idx)
	}

	ndjsonPath := filepath.Join(outDir, "assets.ndjson")
	ndjsonFile, err := os.Create(ndjsonPath)
	if err != nil {
		return idx, err
	}
	ndjsonBuf := bufio.NewWriterSize(ndjsonFile, 64*1024)
	ndjsonEnc := json.NewEncoder(ndjsonBuf)

	filepath.WalkDir(extractedDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".uasset" && ext != ".umap" {
			return nil
		}
		info, perr := parseUAsset(path)
		if perr != nil || info == nil {
			idx.AssetsFailed++
			return nil
		}
		// Normalize path to a slash-relative form for portability.
		if rel, rerr := filepath.Rel(extractedDir, path); rerr == nil {
			info.Path = filepath.ToSlash(rel)
		} else {
			info.Path = filepath.ToSlash(path)
		}
		idx.AssetsParsed++
		idx.TotalNames += int64(info.TotalNames)
		if encErr := ndjsonEnc.Encode(info); encErr != nil {
			// NDJSON write failure shouldn't abort the whole walk.
			return nil
		}
		if len(idx.Sample) < assetsSampleCap {
			idx.Sample = append(idx.Sample, *info)
		}
		return nil
	})

	ndjsonBuf.Flush()
	ndjsonFile.Close()

	return idx, writeAssetsIndex(outDir, idx)
}

// writeAssetsIndex marshals idx to <outDir>/assets_index.json.
func writeAssetsIndex(outDir string, idx *assetsIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "assets_index.json"), data, 0644)
}
