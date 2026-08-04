package recipe

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func u16(s string) []byte {
	b := make([]byte, 0, len(s)*2)
	for _, r := range s {
		var c [2]byte
		binary.LittleEndian.PutUint16(c[:], uint16(r))
		b = append(b, c[:]...)
	}
	return b
}

// varRef encodes a NS_VAR_CODE reference the way a Unicode NSIS build does:
// the code char, then one char holding the index 7 bits per byte-half.
func varRef(idx int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b[0:2], nsVarCode)
	binary.LittleEndian.PutUint16(b[2:4], uint16((idx&0x7F)|0x80|((idx>>7)&0x7F)<<8|0x8000))
	return b
}

// TestExtractStructured_UnicodePrefixed covers the real NSIS-3 solid layout that
// a real signed commercial installer uses: a uint32 header-size prefix ahead of the header
// (all block offsets relative to the byte AFTER it), UTF-16LE strings addressed
// by CHARACTER index, and a $VAR escape inside the path.
func TestExtractStructured_UnicodePrefixed(t *testing.T) {
	const content = "Hello NSIS!"

	entriesOff := 68 // relative to the header start
	stringsOff := entriesOff + nsisEntrySize

	var strTab []byte
	strTab = append(strTab, 0, 0) // char 0: empty string
	nameChar := len(strTab) / 2
	strTab = append(strTab, varRef(21)...) // $INSTDIR
	strTab = append(strTab, u16("\\sub\\hello.txt")...)
	strTab = append(strTab, 0, 0)

	langOff := stringsOff + len(strTab)
	hdrSize := langOff

	header := make([]byte, hdrSize)
	putBlock := func(idx, off, num int) {
		base := 4 + idx*8
		binary.LittleEndian.PutUint32(header[base:base+4], uint32(off))
		binary.LittleEndian.PutUint32(header[base+4:base+8], uint32(num))
	}
	putBlock(nbEntries, entriesOff, 1)
	putBlock(nbStrings, stringsOff, 0)
	putBlock(nbLangtables, langOff, 0)

	e := header[entriesOff:]
	binary.LittleEndian.PutUint32(e[0:4], ewExtractFile)
	binary.LittleEndian.PutUint32(e[8:12], uint32(nameChar)) // param1 = name (char index)
	binary.LittleEndian.PutUint32(e[12:16], 0)               // param2 = data position
	copy(header[stringsOff:], strTab)

	// Solid stream: [uint32 hdrSize][header][data records].
	stream := make([]byte, 4)
	binary.LittleEndian.PutUint32(stream, uint32(hdrSize))
	stream = append(stream, header...)
	rec := make([]byte, 4)
	binary.LittleEndian.PutUint32(rec, uint32(len(content)))
	stream = append(stream, append(rec, []byte(content)...)...)

	out := t.TempDir()
	r := extractStructured(stream, hdrSize, "LZMA", out, func(string) {})
	if !r.Unicode {
		t.Error("string table should be detected as Unicode")
	}
	if r.Entries != 1 || r.Wanted != 1 || r.Files != 1 {
		t.Fatalf("entries=%d wanted=%d files=%d, want 1/1/1", r.Entries, r.Wanted, r.Files)
	}
	// $INSTDIR is the installation root, so it collapses onto the output root.
	got, err := os.ReadFile(filepath.Join(out, "sub", "hello.txt"))
	if err != nil {
		t.Fatalf("expected sub/hello.txt: %v", err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// TestSanitizeRel_Vars pins the variable substitution: known roots collapse or
// map to a fixed safe segment, unknown ones become _var*, and nothing a variable
// expands to can climb out of the output directory.
func TestSanitizeRel_Vars(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		in, want string
	}{
		{`$INSTDIR\sub\a.txt`, "sub" + sep + "a.txt"},
		{`$OUTDIR\a.txt`, "a.txt"},
		{`$PLUGINSDIR\nsis7z.dll`, "_plugins" + sep + "nsis7z.dll"},
		{`$TEMP\x.log`, "_temp" + sep + "x.log"},
		{`$VAR25\y.bin`, "_var25" + sep + "y.bin"},
		{`$R3\z.bin`, "_var_R3" + sep + "z.bin"},
		// path traversal attempts through a variable
		{`$INSTDIR\..\..\evil.exe`, "evil.exe"},
		{`$VAR9\../../evil.exe`, "_var9" + sep + "evil.exe"},
		{`$..\evil.exe`, "_var_" + sep + "evil.exe"},
		{`C:\$INSTDIR\a`, "a"},
	}
	for _, tt := range tests {
		got := sanitizeRel(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeRel(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if safeJoin("base", got) == "" {
			t.Errorf("sanitizeRel(%q) = %q escapes the output dir", tt.in, got)
		}
	}
}

// buildANSIStream builds a minimal ANSI solid stream with a single
// EW_EXTRACTFILE record whose data block is raw or individually compressed.
func buildANSIStream(t *testing.T, name string, payload []byte, compressed bool) ([]byte, int) {
	t.Helper()
	entriesOff := 68
	stringsOff := entriesOff + nsisEntrySize

	strTab := []byte{0}
	fileOff := len(strTab)
	strTab = append(strTab, []byte(name)...)
	strTab = append(strTab, 0)

	langOff := stringsOff + len(strTab)
	hdrSize := langOff
	header := make([]byte, hdrSize)
	putBlock := func(idx, off, num int) {
		base := 4 + idx*8
		binary.LittleEndian.PutUint32(header[base:base+4], uint32(off))
		binary.LittleEndian.PutUint32(header[base+4:base+8], uint32(num))
	}
	putBlock(nbEntries, entriesOff, 1)
	putBlock(nbStrings, stringsOff, 0)
	putBlock(nbLangtables, langOff, 0)

	e := header[entriesOff:]
	binary.LittleEndian.PutUint32(e[0:4], ewExtractFile)
	binary.LittleEndian.PutUint32(e[8:12], uint32(fileOff))
	binary.LittleEndian.PutUint32(e[12:16], 0)
	copy(header[stringsOff:], strTab)

	size := uint32(len(payload))
	if compressed {
		size |= 0x80000000
	}
	rec := make([]byte, 4)
	binary.LittleEndian.PutUint32(rec, size)
	return append(append(header, rec...), payload...), hdrSize
}

func deflateBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractStructured_NonSolidBlock: a data block with the compression bit set
// must be decompressed with the archive's method instead of being dropped.
func TestExtractStructured_NonSolidBlock(t *testing.T) {
	content := []byte("non-solid block payload")
	stream, hdrSize := buildANSIStream(t, "ns.txt", deflateBytes(t, content), true)

	out := t.TempDir()
	r := extractStructured(stream, hdrSize, "deflate", out, func(string) {})
	if r.Wanted != 1 || r.Files != 1 {
		t.Fatalf("files=%d wanted=%d, want 1/1", r.Files, r.Wanted)
	}
	got, err := os.ReadFile(filepath.Join(out, "ns.txt"))
	if err != nil {
		t.Fatalf("expected ns.txt: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	// Same block, but the method cannot decode it -> skipped, not written.
	out2 := t.TempDir()
	r2 := extractStructured(stream, hdrSize, "bzip2", out2, func(string) {})
	if r2.Wanted != 1 || r2.Files != 0 {
		t.Fatalf("undecodable block: files=%d wanted=%d, want 0/1", r2.Files, r2.Wanted)
	}
}

// TestExtractRawRecords_Resync: one corrupt record header in the middle must not
// cost the records behind it.
func TestExtractRawRecords_Resync(t *testing.T) {
	hdrSize := 16
	dec := make([]byte, hdrSize)
	rec := func(s string) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(len(s)))
		return append(b, []byte(s)...)
	}
	dec = append(dec, rec("first record")...)
	dec = append(dec, 0xF0, 0xFF, 0xFF, 0xFF) // bogus size -> resync
	dec = append(dec, rec("second record")...)

	out := t.TempDir()
	n, skipped := extractRawRecords(dec, hdrSize, out, func(string) {})
	if n != 2 || skipped != 1 {
		t.Fatalf("n=%d skipped=%d, want 2/1", n, skipped)
	}
	got, err := os.ReadFile(filepath.Join(out, "_raw", "file_0001.bin"))
	if err != nil || string(got) != "second record" {
		t.Fatalf("file_0001.bin = %q err=%v, want %q", got, err, "second record")
	}
}

func TestLooksUnicode(t *testing.T) {
	tests := []struct {
		name string
		tab  []byte
		want bool
	}{
		{"unicode", append(u16(`C:\Program Files\App\readme.txt`), 0, 0), true},
		{"ansi", append([]byte(`C:\Program Files\App\readme.txt`), 0), false},
		{"too short", []byte{1, 2, 3}, false},
	}
	for _, tt := range tests {
		if got := looksUnicode(tt.tab); got != tt.want {
			t.Errorf("%s: looksUnicode = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// ---- opcode walk ----

type tEntry struct {
	op int
	p  [6]int
}

// varRefA encodes a NS_VAR_CODE reference the way an ANSI NSIS build does.
func varRefA(idx int) []byte {
	return []byte{nsVarCode, byte(idx&0x7F) | 0x80, byte((idx>>7)&0x7F) | 0x80}
}

// nsisStrTab builds an ANSI string table (leading empty string) and returns the
// byte offset of each entry.
func nsisStrTab(strs [][]byte) ([]byte, []int) {
	tab := []byte{0}
	offs := make([]int, len(strs))
	for i, s := range strs {
		offs[i] = len(tab)
		tab = append(tab, s...)
		tab = append(tab, 0)
	}
	return tab, offs
}

// buildOpStream assembles an ANSI solid stream from an entry list and a set of
// data records. Returns the stream, its header size, and each record's position
// relative to the data base.
func buildOpStream(t *testing.T, strTab []byte, entries []tEntry, data [][]byte) ([]byte, int, []int) {
	t.Helper()
	entriesOff := 68
	stringsOff := entriesOff + len(entries)*nsisEntrySize
	langOff := stringsOff + len(strTab)
	hdrSize := langOff

	header := make([]byte, hdrSize)
	putBlock := func(idx, off, num int) {
		base := 4 + idx*8
		binary.LittleEndian.PutUint32(header[base:base+4], uint32(off))
		binary.LittleEndian.PutUint32(header[base+4:base+8], uint32(num))
	}
	putBlock(nbEntries, entriesOff, len(entries))
	putBlock(nbStrings, stringsOff, 0)
	putBlock(nbLangtables, langOff, 0)
	for i, en := range entries {
		e := header[entriesOff+i*nsisEntrySize:]
		binary.LittleEndian.PutUint32(e[0:4], uint32(en.op))
		for k, v := range en.p {
			binary.LittleEndian.PutUint32(e[4+k*4:8+k*4], uint32(v))
		}
	}
	copy(header[stringsOff:], strTab)

	stream := header
	var pos []int
	for _, d := range data {
		pos = append(pos, len(stream)-hdrSize)
		rec := make([]byte, 4)
		binary.LittleEndian.PutUint32(rec, uint32(len(d)))
		stream = append(append(stream, rec...), d...)
	}
	return stream, hdrSize, pos
}

// shellRefA encodes a NS_SHELL_CODE reference the way an ANSI NSIS build does:
// the code byte then the CSIDL pair, seven bits per byte (low = per-user folder,
// high = its all-users twin).
func shellRefA(cur, all int) []byte {
	return []byte{nsShellCode, byte(cur&0x7F) | 0x80, byte(all&0x7F) | 0x80}
}

// shellRefU is the same reference in a Unicode build: one char holding both.
func shellRefU(cur, all int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b[0:2], nsShellCode)
	binary.LittleEndian.PutUint16(b[2:4], uint16(cur&0x7F)|0x80|uint16(all&0x7F)<<8|0x8000)
	return b
}

// TestShellCode_ResolvesToFolderName pins the CSIDL pair -> NSIS constant
// mapping in both encodings. 0x17/0x02 is the pair that used to surface as the
// unreadable "$SHELL2946".
func TestShellCode_ResolvesToFolderName(t *testing.T) {
	tests := []struct {
		cur, all int
		want     string
	}{
		{0x02, 0x17, "$SMPROGRAMS"},
		{0x1A, 0x23, "$APPDATA"},
		{0x1C, 0x1C, "$LOCALAPPDATA"},
		{0x26, 0x26, "$PROGRAMFILES"},
		{0x10, 0x19, "$DESKTOP"},
		{0x0B, 0x16, "$STARTMENU"},
		{0x3B, 0x3B, "$CDBURN_AREA"},
		{0x7E, 0x7D, "$SHELL126_125"}, // neither id known -> honest fallback
	}
	for _, tt := range tests {
		ansi := append(shellRefA(tt.cur, tt.all), 0)
		if got := resolveNSISString(ansi, 0, false); got != tt.want {
			t.Errorf("ansi shell(%#x,%#x) = %q, want %q", tt.cur, tt.all, got, tt.want)
		}
		uni := append(shellRefU(tt.cur, tt.all), 0, 0)
		if got := resolveNSISString(uni, 0, true); got != tt.want {
			t.Errorf("unicode shell(%#x,%#x) = %q, want %q", tt.cur, tt.all, got, tt.want)
		}
	}
}

// TestSanitizeRel_ShellSegments: a shell folder becomes a readable directory,
// and it still cannot be used to climb out of the output directory.
func TestSanitizeRel_ShellSegments(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct{ in, want string }{
		{`$SMPROGRAMS\App\App.lnk`, "_shell_SMPROGRAMS" + sep + "App" + sep + "App.lnk"},
		{`$APPDATA\App\cfg.ini`, "_shell_APPDATA" + sep + "App" + sep + "cfg.ini"},
		{`$SHELL126_125\x.bin`, "_var_SHELL126_125" + sep + "x.bin"},
		// traversal attempts routed through a shell segment
		{`$SMPROGRAMS\..\..\..\evil.exe`, "_shell_SMPROGRAMS" + sep + "evil.exe"},
		{`$SMPROGRAMS\..\..\Windows\System32\evil.dll`,
			"_shell_SMPROGRAMS" + sep + "Windows" + sep + "System32" + sep + "evil.dll"},
	}
	for _, tt := range tests {
		got := sanitizeRel(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeRel(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if safeJoin("base", got) == "" {
			t.Errorf("sanitizeRel(%q) = %q escapes the output dir", tt.in, got)
		}
	}
}

// TestWalk_ShellPathExtracts: a file whose path starts with a shell constant
// lands under the readable _shell_* directory, inside the output root.
func TestWalk_ShellPathExtracts(t *testing.T) {
	strTab, off := nsisStrTab([][]byte{
		append(shellRefA(0x02, 0x17), []byte(`\App\readme.txt`)...),
	})
	entries := []tEntry{{op: ewExtractFile, p: [6]int{0, off[0], 0}}}
	stream, hdrSize, _ := buildOpStream(t, strTab, entries, [][]byte{[]byte("shell payload")})

	out := t.TempDir()
	if r := extractStructured(stream, hdrSize, "LZMA", out, func(string) {}); r.Files != 1 {
		t.Fatalf("files=%d, want 1", r.Files)
	}
	got, err := os.ReadFile(filepath.Join(out, "_shell_SMPROGRAMS", "App", "readme.txt"))
	if err != nil || string(got) != "shell payload" {
		t.Fatalf("_shell_SMPROGRAMS/App/readme.txt = %q err=%v", got, err)
	}
}

// TestWalk_NewOpcodes: the added opcodes are identified (so they leave the
// unknown bucket) and the recon-worthy ones reach nsis-actions.txt.
func TestWalk_NewOpcodes(t *testing.T) {
	strTab, off := nsisStrTab([][]byte{
		[]byte(`$INSTDIR\app.exe`), []byte(`Software\Morgue`), []byte("Path"),
		[]byte(`C:\src`), []byte(`C:\dst`), []byte("Install failed"),
		[]byte(`$INSTDIR\cfg.ini`), []byte("Setup"), []byte("Mode"),
	})
	entries := []tEntry{
		{op: ewSetFileAttributes, p: [6]int{off[0], 0x2}},
		{op: ewReadRegStr, p: [6]int{26, 2, off[1], off[2]}},
		{op: ewCopyFiles, p: [6]int{off[3], off[4]}},
		{op: ewMessageBox, p: [6]int{0x10, off[5]}},
		{op: ewReadINIStr, p: [6]int{27, off[7], off[8], off[6]}},
		{op: ewReboot},
		{op: ewIntOp, p: [6]int{0, 1, 2}},
		{op: ewPushPop},
		{op: ewGetTempFileName},
		{op: ewSendMessage},
		{op: ewLockWindow},
	}
	stream, hdrSize, _ := buildOpStream(t, strTab, entries, nil)

	r := extractStructured(stream, hdrSize, "LZMA", t.TempDir(), func(string) {})
	if r.Unknown != 0 {
		t.Errorf("Unknown = %d, want 0 (every opcode above is now identified)", r.Unknown)
	}
	if r.Known != len(entries) {
		t.Errorf("Known = %d, want %d", r.Known, len(entries))
	}
	joined := strings.Join(r.Meta, "\n")
	for _, want := range []string{
		`attributes: $INSTDIR\app.exe = 0x2`,
		`reg read: HKLM\Software\Morgue [Path] -> $VAR26`,
		`copy files: C:\src -> C:\dst`,
		"messagebox: Install failed",
		`ini read: $INSTDIR\cfg.ini [Setup] Mode -> $VAR27`,
		"reboot requested",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("nsis-actions missing %q, got:\n%s", want, joined)
		}
	}
	// The noop opcodes must record nothing at all.
	if len(r.Meta) != 6 {
		t.Errorf("Meta = %d lines, want 6 (noop opcodes must stay silent)", len(r.Meta))
	}
}

// TestWalk_AssignVarResolvesPath: EW_ASSIGNVAR must make a later $INSTDIR
// reference resolve to the real value instead of the $VAR fallback segment.
func TestWalk_AssignVarResolvesPath(t *testing.T) {
	strTab, off := nsisStrTab([][]byte{
		[]byte(`C:\Target\App`),
		append(varRefA(21), []byte(`\x.txt`)...), // $INSTDIR\x.txt
	})
	entries := []tEntry{
		{op: ewAssignVar, p: [6]int{21, off[0]}},
		{op: ewExtractFile, p: [6]int{0, off[1], 0}},
	}
	stream, hdrSize, _ := buildOpStream(t, strTab, entries, [][]byte{[]byte("payload")})

	out := t.TempDir()
	r := extractStructured(stream, hdrSize, "LZMA", out, func(string) {})
	if r.Files != 1 {
		t.Fatalf("files=%d, want 1", r.Files)
	}
	got, err := os.ReadFile(filepath.Join(out, "Target", "App", "x.txt"))
	if err != nil {
		t.Fatalf("expected Target/App/x.txt (resolved $INSTDIR): %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("content = %q", got)
	}
	// Without the assignment the same path must fall back to a safe segment.
	out2 := t.TempDir()
	stream2, hdr2, _ := buildOpStream(t, strTab, entries[1:], [][]byte{[]byte("payload")})
	extractStructured(stream2, hdr2, "LZMA", out2, func(string) {})
	if _, err := os.Stat(filepath.Join(out2, "x.txt")); err != nil {
		t.Errorf("unresolved $INSTDIR should collapse to the output root: %v", err)
	}
}

// TestWalk_RenameDelete: EW_RENAME and EW_DELETEFILE must show up in the tree.
func TestWalk_RenameDelete(t *testing.T) {
	strTab, off := nsisStrTab([][]byte{
		[]byte("a.txt"), []byte("b.txt"), []byte("c.txt"),
	})
	entries := []tEntry{
		{op: ewExtractFile, p: [6]int{0, off[0], 0}},
		{op: ewExtractFile, p: [6]int{0, off[1], 0}}, // pos filled below
		{op: ewRename, p: [6]int{off[0], off[2]}},
		{op: ewDeleteFile, p: [6]int{off[1]}},
	}
	stream, hdrSize, pos := buildOpStream(t, strTab, entries, [][]byte{[]byte("AAA"), []byte("BBB")})
	// patch entry 1's data position now that we know it
	binary.LittleEndian.PutUint32(stream[68+nsisEntrySize+12:68+nsisEntrySize+16], uint32(pos[1]))

	out := t.TempDir()
	r := extractStructured(stream, hdrSize, "LZMA", out, func(string) {})
	if r.Files != 2 || r.Wanted != 2 {
		t.Fatalf("files=%d wanted=%d, want 2/2", r.Files, r.Wanted)
	}
	got, err := os.ReadFile(filepath.Join(out, "c.txt"))
	if err != nil || string(got) != "AAA" {
		t.Fatalf("renamed c.txt = %q err=%v, want AAA", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "a.txt")); err == nil {
		t.Error("a.txt should have been renamed away")
	}
	if _, err := os.Stat(filepath.Join(out, "b.txt")); err == nil {
		t.Error("b.txt should have been deleted")
	}
}

// TestWalk_MetadataAndUnknownOpcode: metadata opcodes are recorded not executed,
// control flow is counted, and an unknown opcode does not break the walk.
func TestWalk_MetadataAndUnknownOpcode(t *testing.T) {
	strTab, off := nsisStrTab([][]byte{
		[]byte(`Software\Morgue`), []byte("Version"), []byte("1.0"),
		[]byte("Morgue.lnk"), []byte("app.exe"), []byte("plug.dll"), []byte("Init"),
		[]byte("done.txt"),
	})
	entries := []tEntry{
		{op: ewWriteReg, p: [6]int{2, off[0], off[1], off[2], 1}}, // HKLM, REG_SZ
		{op: ewCreateShortcut, p: [6]int{off[3], off[4]}},
		{op: ewRegisterDLL, p: [6]int{off[5], off[6]}},
		{op: 0x7FFF, p: [6]int{}}, // unknown opcode
		{op: ewJmp, p: [6]int{1}},
		{op: ewExtractFile, p: [6]int{0, off[7], 0}}, // must still run
	}
	stream, hdrSize, _ := buildOpStream(t, strTab, entries, [][]byte{[]byte("ok")})

	out := t.TempDir()
	r := extractStructured(stream, hdrSize, "LZMA", out, func(string) {})
	if r.Unknown != 1 {
		t.Errorf("Unknown = %d, want 1", r.Unknown)
	}
	if r.Control != 1 {
		t.Errorf("Control = %d, want 1", r.Control)
	}
	if r.Known != 5 {
		t.Errorf("Known = %d, want 5", r.Known)
	}
	if r.Files != 1 {
		t.Errorf("Files = %d, want 1 (walk must survive the unknown opcode)", r.Files)
	}
	if len(r.Meta) != 3 {
		t.Fatalf("Meta = %v, want 3 lines", r.Meta)
	}
	joined := strings.Join(r.Meta, "\n")
	for _, want := range []string{`HKLM\Software\Morgue`, "Morgue.lnk", "plug.dll!Init"} {
		if !strings.Contains(joined, want) {
			t.Errorf("actions artifact missing %q, got:\n%s", want, joined)
		}
	}
	// Registry writes must NOT touch the extracted tree.
	if _, err := os.Stat(filepath.Join(out, "Software")); err == nil {
		t.Error("EW_WRITEREG must not create directories in the extracted tree")
	}
}
