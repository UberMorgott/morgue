package recipe

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz/lzma"

	"github.com/UberMorgott/morgue/internal/recon"
)

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// buildDecompressedNSIS constructs a minimal but structurally-valid NSIS
// decompressed stream: header (flags + 8 block headers + entries + strings) then
// one file-data record. Returns the stream and its header size.
func buildDecompressedNSIS(t *testing.T) (stream []byte, hdrSize int) {
	t.Helper()
	const fileContent = "Hello NSIS!"

	// Offsets (absolute in the decompressed stream).
	entriesOff := 68
	stringsOff := entriesOff + 2*nsisEntrySize // 68 + 56 = 124

	// String table (ANSI, null-terminated). Offsets are relative to stringsOff.
	var strTab []byte
	strTab = append(strTab, 0x00) // rel 0: empty string
	dirOff := len(strTab)         // rel 1
	strTab = append(strTab, []byte("OUT")...)
	strTab = append(strTab, 0x00)
	fileOff := len(strTab) // rel 5
	strTab = append(strTab, []byte("hello.txt")...)
	strTab = append(strTab, 0x00)

	langOff := stringsOff + len(strTab)
	hdrSize = langOff // header ends where strings end; langtables empty

	header := make([]byte, hdrSize)
	// flags = 0 (header[0:4] already zero)
	putBlock := func(idx, off, num int) {
		base := 4 + idx*8
		copy(header[base:base+4], le32(uint32(off)))   //nolint:gosec // G115: test fixture values are small constants that cannot overflow
		copy(header[base+4:base+8], le32(uint32(num))) //nolint:gosec // G115: test fixture values are small constants that cannot overflow
	}
	putBlock(nbEntries, entriesOff, 2)
	putBlock(nbStrings, stringsOff, 0)
	putBlock(nbLangtables, langOff, 0)
	putBlock(nbData, hdrSize, 0)

	// Entry 0: SetOutPath (EW_CREATEDIR, param1 != 0) -> dir "OUT".
	e0 := header[entriesOff:]
	copy(e0[0:4], le32(ewCreateDir))
	copy(e0[4:8], le32(uint32(dirOff))) //nolint:gosec // G115: param0 = path string, a small test fixture offset
	copy(e0[8:12], le32(1))             // param1 = 1 -> SetOutPath

	// Entry 1: EW_EXTRACTFILE -> "hello.txt" at data position 0.
	e1 := header[entriesOff+nsisEntrySize:]
	copy(e1[0:4], le32(ewExtractFile))
	copy(e1[4:8], le32(0))                // param0 = overwrite
	copy(e1[8:12], le32(uint32(fileOff))) //nolint:gosec // G115: param1 = name string, a small test fixture offset
	copy(e1[12:16], le32(0))              // param2 = position in data block

	// Copy the string table into the header.
	copy(header[stringsOff:stringsOff+len(strTab)], strTab)

	// File data record after the header: [int32 size][bytes].
	rec := append(le32(uint32(len(fileContent))), []byte(fileContent)...)

	out := append([]byte(nil), header...)
	return append(out, rec...), hdrSize
}

// nsisLZMA compresses stream into NSIS-style LZMA: a 5-byte header
// (props + dict size) followed by the raw LZMA1 body (with EOS marker). This
// mirrors what NSIS writes and what the recipe reconstructs.
func nsisLZMA(t *testing.T, stream []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := lzma.NewWriter(&buf)
	if err != nil {
		t.Fatalf("lzma.NewWriter: %v", err)
	}
	if _, err := w.Write(stream); err != nil {
		t.Fatalf("lzma write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("lzma close: %v", err)
	}
	full := buf.Bytes() // classic .lzma: 13-byte header + body
	if len(full) < 13 {
		t.Fatal("lzma output too short")
	}
	// Strip the 8-byte uncompressed-size field (bytes 5:13); keep props+dict.
	return append(append([]byte{}, full[:5]...), full[13:]...)
}

// buildNSISArchive assembles firstheader + NSIS-LZMA into a file, optionally
// flipping one signature byte to exercise the repair path.
func buildNSISArchive(t *testing.T, flipSig bool) string {
	t.Helper()
	stream, hdrSize := buildDecompressedNSIS(t)
	return wrapNSISArchive(t, stream, hdrSize, flipSig)
}

// wrapNSISArchive compresses a decompressed stream NSIS-style and writes it
// behind a firstheader, optionally flipping one signature byte.
func wrapNSISArchive(t *testing.T, stream []byte, hdrSize int, flipSig bool) string {
	t.Helper()
	comp := nsisLZMA(t, stream)

	sig := append([]byte{}, nsisSignature...)
	if flipSig {
		sig[10] ^= 0xFF // flip a byte in "NullsoftInst"
	}
	var fhr []byte
	fhr = append(fhr, 0, 0, 0, 0) // flags
	fhr = append(fhr, sig...)
	fhr = append(fhr, le32(uint32(hdrSize))...) //nolint:gosec // G115: test fixture values are small constants that cannot overflow
	archiveSize := nsisFirstHeaderSize + len(comp)
	fhr = append(fhr, le32(uint32(archiveSize))...) //nolint:gosec // G115: test fixture values are small constants that cannot overflow

	path := filepath.Join(t.TempDir(), "installer.exe")
	if err := os.WriteFile(path, append(fhr, comp...), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNSISUnpack_ExtractSeverity: a run that fell back to the raw dump is
// DEGRADED output and must be reported Warn, not a silent success — the full
// structured run right above it stays Success.
func TestNSISUnpack_ExtractSeverity(t *testing.T) {
	runExtract := func(t *testing.T, target string) (StepProgress, nsisManifest) {
		t.Helper()
		outDir := t.TempDir()
		prog := make(chan StepProgress, 64)
		ctx := &Context{Target: target, Output: outDir, Progress: prog, Ctx: context.Background()}
		if err := (&NSISUnpack{}).Execute(ctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		close(prog)
		var last StepProgress
		for p := range prog {
			if p.Step == 3 && p.Status != Running {
				last = p
			}
		}
		var man nsisManifest
		mdata, err := os.ReadFile(filepath.Join(outDir, "nsis-manifest.json")) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
		if err != nil {
			t.Fatalf("manifest: %v", err)
		}
		if err := json.Unmarshal(mdata, &man); err != nil {
			t.Fatalf("manifest json: %v", err)
		}
		return last, man
	}

	t.Run("structured-complete", func(t *testing.T) {
		p, man := runExtract(t, buildNSISArchive(t, false))
		if man.ExtractMode != "structured" || man.FilesExtracted != man.FilesWanted {
			t.Fatalf("setup: mode=%q %d/%d", man.ExtractMode, man.FilesExtracted, man.FilesWanted)
		}
		if p.Status != Success {
			t.Errorf("status = %v (%v), want Success", p.Status, p.Error)
		}
	})

	t.Run("raw-fallback", func(t *testing.T) {
		// Same stream, but the entries block claims zero instructions: the walk
		// finds no files and the recipe dumps raw records instead.
		stream, hdrSize := buildDecompressedNSIS(t)
		binary.LittleEndian.PutUint32(stream[4+nbEntries*8+4:], 0)

		p, man := runExtract(t, wrapNSISArchive(t, stream, hdrSize, false))
		if man.ExtractMode != "raw-fallback" {
			t.Fatalf("ExtractMode = %q, want raw-fallback", man.ExtractMode)
		}
		if p.Status != Warn {
			t.Fatalf("status = %v, want Warn (raw fallback is not a success)", p.Status)
		}
		if p.Error == nil || !strings.Contains(p.Error.Error(), "raw-fallback") {
			t.Errorf("warn message = %v, want it to name the fallback mode", p.Error)
		}
	})
}

func TestNSISUnpack_Match(t *testing.T) {
	n := &NSISUnpack{}
	if !n.Match(&recon.Result{Kind: recon.NSIS}) {
		t.Error("NSISUnpack should match Kind == NSIS")
	}
	if n.Match(&recon.Result{Kind: recon.Native}) {
		t.Error("NSISUnpack should NOT match Kind == Native")
	}
}

func TestNSISUnpack_Execute(t *testing.T) {
	for _, flip := range []bool{false, true} {
		name := "clean"
		if flip {
			name = "flipped-signature"
		}
		t.Run(name, func(t *testing.T) {
			target := buildNSISArchive(t, flip)
			outDir := t.TempDir()

			ctx := &Context{
				Target: target,
				Output: outDir,
				Ctx:    context.Background(),
			}
			n := &NSISUnpack{}
			if err := n.Execute(ctx); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			// The extracted file must exist with the right bytes.
			got, err := os.ReadFile(filepath.Join(outDir, "extracted", "OUT", "hello.txt")) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
			if err != nil {
				t.Fatalf("expected extracted/OUT/hello.txt: %v", err)
			}
			if string(got) != "Hello NSIS!" {
				t.Errorf("extracted content = %q, want %q", got, "Hello NSIS!")
			}

			// Manifest sanity.
			mdata, err := os.ReadFile(filepath.Join(outDir, "nsis-manifest.json")) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
			if err != nil {
				t.Fatalf("expected nsis-manifest.json: %v", err)
			}
			var man nsisManifest
			if err := json.Unmarshal(mdata, &man); err != nil {
				t.Fatalf("manifest json: %v", err)
			}
			if man.ExtractMode != "structured" {
				t.Errorf("ExtractMode = %q, want structured", man.ExtractMode)
			}
			if man.FilesExtracted != 1 {
				t.Errorf("FilesExtracted = %d, want 1", man.FilesExtracted)
			}
			if man.Compression != "LZMA" {
				t.Errorf("Compression = %q, want LZMA", man.Compression)
			}
			if flip && !man.SignatureRepaired {
				t.Error("expected SignatureRepaired = true for the flipped archive")
			}
		})
	}
}
