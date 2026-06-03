package recipe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeSyntheticMonolith writes a Ghidra-style combined .c into srcDir for the
// binary base name "game". The format mirrors the real 1.4GB Windrose output
// verified by the team: a 3-line banner, then per-function records of the form
//
//	// 1400013a0            (lowercase hex header line, 4-16 chars)
//	void <sig>(void)        (signature; named iff it is not FUN_/DAT_/...)
//	{ ...body... }
//
// Every `namedEvery`-th function gets a real C++-style symbol so NamedCount>0
// and class recovery has something to find; the rest stay anonymous (FUN_).
// Returns the binary path to pass to splitAndIndexDecompiledC.
func writeSyntheticMonolith(t *testing.T, srcDir string, n, bodyLines, namedEvery int) string {
	t.Helper()
	binPath := filepath.Join(srcDir, "game.exe")
	combined := filepath.Join(srcDir, "game.c")
	f, err := os.Create(combined)
	if err != nil {
		t.Fatalf("create combined .c: %v", err)
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 256*1024)
	bw.WriteString("// Decompiled by Ghidra via Morgue\n")
	bw.WriteString("// Binary: game.exe\n")
	bw.WriteString("// Architecture: x86:LE:64:default\n")

	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("%09x", 0x140001000+i*0x20) // lowercase, >=4 hex
		bw.WriteString("// " + addr + "\n")
		if namedEvery > 0 && i%namedEvery == 0 {
			// Real symbol form: class-qualified so cppClassOwner finds a class.
			fmt.Fprintf(bw, "void Engine::Widget%d::Tick(void)\n", i)
		} else {
			fmt.Fprintf(bw, "void FUN_%s(void)\n", addr)
		}
		bw.WriteString("{\n")
		for j := 0; j < bodyLines; j++ {
			fmt.Fprintf(bw, "  local_%d = local_%d + %d;\n", j, j, i)
		}
		bw.WriteString("}\n\n")
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush combined .c: %v", err)
	}
	return binPath
}

// sampleHeap launches a goroutine that polls runtime HeapAlloc and records the
// peak observed while fn runs. A before/after snapshot would miss the transient
// peak caused by json.MarshalIndent materializing the whole indented JSON, so we
// must sample continuously. Returns the peak HeapAlloc in bytes.
func sampleHeap(fn func()) uint64 {
	var peak uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ms runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
				runtime.ReadMemStats(&ms)
				for {
					cur := atomic.LoadUint64(&peak)
					if ms.HeapAlloc <= cur || atomic.CompareAndSwapUint64(&peak, cur, ms.HeapAlloc) {
						break
					}
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	fn()
	close(stop)
	wg.Wait()
	// One final read in case the peak landed between the last poll and fn return.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapAlloc > peak {
		peak = ms.HeapAlloc
	}
	return peak
}

// countBucketFiles walks funcsDir and counts .c bucket files. It also asserts
// that each file's parent directory name is a valid bucketFor() 2-hex prefix
// (bucketWriter nests as functions/<2hex>/<2hex>_NN.c).
func countBucketFiles(t *testing.T, funcsDir string) int {
	t.Helper()
	var n int
	err := filepath.WalkDir(funcsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".c") {
			return nil
		}
		dir := filepath.Base(filepath.Dir(p))
		if len(dir) != 2 {
			t.Fatalf("bucket file %q not under a 2-hex prefix dir (got %q)", p, dir)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("walk functions dir: %v", err)
	}
	return n
}

// TestSplitManyRecordsBoundedMemory is the core regression test for the OOM
// root cause: on a monolith with N functions, the old code accumulates an
// in-RAM symbols map + classSet and then json.MarshalIndent's the entire thing,
// which is O(n) memory and on the real 1.4GB/43.9M-line input exhausts RAM and
// aborts the process before symbols.json is written.
//
// Assertions:
//   - functions/ dir created with >=1 bucket file, named by bucketFor() prefix.
//   - symbols.json exists, parses, FunctionCount==N, NamedCount>0.
//   - peak HeapAlloc stays under a sane bound while splitting N functions.
//
// Scale rationale: the OOM is LINEAR in function count (measured: ~400 MiB of
// peak heap per 1M functions with the old in-RAM symbols map + MarshalIndent —
// 500k->179, 1M->436, 2M->816 MiB). It only crosses a dangerous threshold at
// the real file's scale (millions of functions / 43.9M lines). N=2M therefore
// reproduces the blowup deterministically: the old code peaks ~816 MiB (FAIL),
// while a streaming fix stays roughly constant in function count.
func TestSplitManyRecordsBoundedMemory(t *testing.T) {
	const (
		n          = 2_000_000
		bodyLines  = 3
		namedEvery = 5 // 20% named
		heapCap    = 512 << 20
	)
	srcDir := t.TempDir()
	binPath := writeSyntheticMonolith(t, srcDir, n, bodyLines, namedEvery)

	var res *splitResult
	var splitErr error
	peak := sampleHeap(func() {
		res, splitErr = splitAndIndexDecompiledC(srcDir, binPath)
	})
	if splitErr != nil {
		t.Fatalf("splitAndIndexDecompiledC: %v", splitErr)
	}
	if res == nil {
		t.Fatalf("splitAndIndexDecompiledC returned nil result for non-empty input")
	}

	// functions/ buckets. bucketWriter nests files one level deep as
	// functions/<2hex>/<2hex>_NN.c, so walk recursively and confirm the
	// directory name is the bucketFor() 2-hex prefix.
	funcsDir := filepath.Join(srcDir, "functions")
	buckets := countBucketFiles(t, funcsDir)
	if buckets < 1 {
		t.Fatalf("expected >=1 bucket .c file, got %d", buckets)
	}

	// symbols.json parses with expected counts.
	symPath := filepath.Join(srcDir, "symbols.json")
	data, err := os.ReadFile(symPath)
	if err != nil {
		t.Fatalf("read symbols.json: %v", err)
	}
	var sm symbolMap
	if err := json.Unmarshal(data, &sm); err != nil {
		t.Fatalf("parse symbols.json: %v", err)
	}
	if sm.Counts.Total != n {
		t.Fatalf("symbols.json counts.total = %d, want %d", sm.Counts.Total, n)
	}
	if res.FunctionCount != n {
		t.Fatalf("res.FunctionCount = %d, want %d", res.FunctionCount, n)
	}
	if res.NamedCount == 0 {
		t.Fatalf("res.NamedCount = 0, want > 0")
	}

	t.Logf("n=%d functions, buckets=%d, named=%d, peakHeap=%d MiB",
		n, buckets, res.NamedCount, peak>>20)

	if peak > heapCap {
		t.Fatalf("peak HeapAlloc = %d MiB exceeds bound %d MiB (O(n) in-RAM symbol map + MarshalIndent)",
			peak>>20, heapCap>>20)
	}
}

// recordTruncationMarker is the exact text appendLine writes once a single
// record's in-memory body crosses maxRecordBytes. The test asserts on it to
// PROVE the per-record cap actually fires (not merely that streaming works).
const recordTruncationMarker = "[record body truncated by Morgue"

// TestSplitPathologicalHugeRecord proves the maxRecordBytes per-record cap is
// actually enforced. It writes ONE record whose body comfortably exceeds the
// 64MB in-memory cap, then asserts:
//   - the split still completes with FunctionCount==1 and bounded peak heap, and
//   - the produced bucket file CONTAINS the truncation marker — i.e. the cap
//     fired and recBuilder stopped growing, rather than the record just
//     happening to stream through under the limit.
func TestSplitPathologicalHugeRecord(t *testing.T) {
	const (
		// Each body line is ~28 bytes; >2.4M lines guarantees the accumulated
		// record body crosses maxRecordBytes (64MiB) so the cap MUST fire.
		bodyLines = 4_000_000
		heapCap   = 512 << 20
	)
	if int64(bodyLines)*28 <= maxRecordBytes {
		t.Fatalf("test misconfigured: bodyLines*~28 (%d) must exceed maxRecordBytes (%d)",
			int64(bodyLines)*28, maxRecordBytes)
	}
	srcDir := t.TempDir()
	binPath := filepath.Join(srcDir, "game.exe")
	combined := filepath.Join(srcDir, "game.c")
	f, err := os.Create(combined)
	if err != nil {
		t.Fatalf("create combined .c: %v", err)
	}
	bw := bufio.NewWriterSize(f, 256*1024)
	bw.WriteString("// Decompiled by Ghidra via Morgue\n")
	bw.WriteString("// Binary: game.exe\n")
	bw.WriteString("// Architecture: x86:LE:64:default\n")
	bw.WriteString("// 140001000\n")
	bw.WriteString("void FUN_140001000(void)\n")
	bw.WriteString("{\n")
	for j := 0; j < bodyLines; j++ {
		fmt.Fprintf(bw, "  local_%d = local_%d + 1;\n", j, j)
	}
	bw.WriteString("}\n")
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush combined .c: %v", err)
	}
	f.Close()

	var res *splitResult
	var splitErr error
	peak := sampleHeap(func() {
		res, splitErr = splitAndIndexDecompiledC(srcDir, binPath)
	})
	if splitErr != nil {
		t.Fatalf("splitAndIndexDecompiledC (huge record): %v", splitErr)
	}
	if res == nil || res.FunctionCount != 1 {
		t.Fatalf("expected FunctionCount==1 for single huge record, got %+v", res)
	}

	// PROVE the cap fired: the single bucket file must contain the marker, and
	// it must be far smaller than the raw body (~112MB) — capped near 64MiB.
	funcsDir := filepath.Join(srcDir, "functions")
	var capped bool
	var bucketSize int64
	werr := filepath.WalkDir(funcsDir, func(p string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".c") {
			return e
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		bucketSize += int64(len(data))
		if strings.Contains(string(data), recordTruncationMarker) {
			capped = true
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk functions dir: %v", werr)
	}
	if !capped {
		t.Fatalf("per-record cap did NOT fire: truncation marker %q absent from bucket files "+
			"(maxRecordBytes=%d not enforced)", recordTruncationMarker, maxRecordBytes)
	}
	// The written record must be bounded near the cap, not the full ~112MB body.
	if bucketSize > maxRecordBytes+(8<<20) {
		t.Fatalf("bucket file size %d exceeds cap+8MiB (%d) — body not truncated",
			bucketSize, maxRecordBytes+(8<<20))
	}

	t.Logf("huge record bodyLines=%d peakHeap=%d MiB capFired=%v bucketBytes=%d (cap=%d)",
		bodyLines, peak>>20, capped, bucketSize, maxRecordBytes)
	if peak > heapCap {
		t.Fatalf("peak HeapAlloc = %d MiB exceeds bound %d MiB for single huge record",
			peak>>20, heapCap>>20)
	}
}

// TestSplitNonEmptyZeroFunctionsErrors is the regression guard for header-format
// drift: a non-empty combined .c that yields zero functions (e.g. Ghidra output
// whose header convention changed) must surface as an ERROR, not a silent
// logged-success. Current code returns success with FunctionCount==0.
func TestSplitNonEmptyZeroFunctionsErrors(t *testing.T) {
	srcDir := t.TempDir()
	binPath := filepath.Join(srcDir, "game.exe")
	combined := filepath.Join(srcDir, "game.c")
	// Non-empty content with NO header line matching reFuncHeader.
	content := "// Decompiled by Ghidra via Morgue\n" +
		"// Binary: game.exe\n" +
		"this file has body text but no // <hexaddr> headers at all\n" +
		"void something(void) { return; }\n"
	if err := os.WriteFile(combined, []byte(content), 0644); err != nil {
		t.Fatalf("write combined .c: %v", err)
	}

	res, err := splitAndIndexDecompiledC(srcDir, binPath)
	if err == nil {
		t.Fatalf("expected error for non-empty .c yielding 0 functions, got nil (res=%+v)", res)
	}
	t.Logf("got expected error: %v", err)
}

// TestSplitIdempotent proves the split is idempotent: running it twice into the
// same srcDir must NOT accumulate stale bucket files. The bucket writer only
// rolls to higher-numbered files, so without the os.RemoveAll(funcsDir) guard a
// re-run would leave the first run's buckets in place and append more.
func TestSplitIdempotent(t *testing.T) {
	srcDir := t.TempDir()
	binPath := writeSyntheticMonolith(t, srcDir, 5000, 4, 5)

	res1, err := splitAndIndexDecompiledC(srcDir, binPath)
	if err != nil {
		t.Fatalf("first split: %v", err)
	}
	funcsDir := filepath.Join(srcDir, "functions")
	files1 := countBucketFiles(t, funcsDir)

	res2, err := splitAndIndexDecompiledC(srcDir, binPath)
	if err != nil {
		t.Fatalf("second split: %v", err)
	}
	files2 := countBucketFiles(t, funcsDir)

	if res1.FunctionCount != res2.FunctionCount {
		t.Fatalf("function count drifted across runs: %d -> %d", res1.FunctionCount, res2.FunctionCount)
	}
	if files1 != files2 {
		t.Fatalf("bucket files accumulated across runs: %d -> %d (functions/ not cleared)", files1, files2)
	}
	t.Logf("idempotent: functions=%d bucketFiles=%d (stable across 2 runs)", res2.FunctionCount, files2)
}

// TestSplitRealWindroseMonolith validates the streaming split against the REAL
// ~1.4GB Ghidra monolith for Windrose (the file whose old O(n) split OOM-froze
// the machine). It is skip-gated on the file existing so CI without the game
// tree stays green (same pattern as uasset_realassets_test.go).
//
// It does NOT use t.TempDir(): the lead wants the produced functions/ +
// symbols.json/.ndjson left in the real output dir for downstream RE, so the
// split runs in place against the real srcDir and outputs are NOT cleaned up.
//
// Memory method: a goroutine samples runtime.HeapAlloc every 50ms during the
// split and tracks the peak (a before/after snapshot would miss transient
// spikes). `go test` does NOT run main()'s Job Object cap, so bounded HeapAlloc
// here is the proof the algorithm itself is O(1) in function count; the OS-level
// cap enforcement was proven separately in task #3.
func TestSplitRealWindroseMonolith(t *testing.T) {
	const heapCap = 768 << 20 // generous ceiling; expect a few hundred MiB, not GB
	srcDir := `E:\DEV\Windrose\decompiled\shipping-ghidra2\Windrose-Win64-Shipping\src`
	// binaryPath only needs the right base name: splitAndIndexDecompiledC derives
	// the combined .c as <srcDir>/<base>.c, i.e. Windrose-Win64-Shipping.c.
	binPath := filepath.Join(srcDir, "Windrose-Win64-Shipping.exe")
	combined := filepath.Join(srcDir, "Windrose-Win64-Shipping.c")

	info, err := os.Stat(combined)
	if err != nil {
		t.Skipf("real Windrose monolith not present: %v", err)
	}
	t.Logf("real combined .c: %d bytes", info.Size())

	var res *splitResult
	var splitErr error
	start := time.Now()
	peak := sampleHeap(func() {
		res, splitErr = splitAndIndexDecompiledC(srcDir, binPath)
	})
	elapsed := time.Since(start)

	if splitErr != nil {
		t.Fatalf("splitAndIndexDecompiledC on real monolith: %v", splitErr)
	}
	if res == nil {
		t.Fatalf("nil result for real monolith")
	}

	// functions/ buckets: count 2-hex dirs, total .c files, total bytes on disk.
	funcsDir := filepath.Join(srcDir, "functions")
	var bucketDirs, bucketFiles int
	var bucketBytes int64
	dirSet := map[string]bool{}
	werr := filepath.WalkDir(funcsDir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if name := filepath.Base(p); len(name) == 2 && p != funcsDir {
				dirSet[name] = true
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".c") {
			bucketFiles++
			if fi, ferr := d.Info(); ferr == nil {
				bucketBytes += fi.Size()
			}
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk functions dir: %v", werr)
	}
	bucketDirs = len(dirSet)
	if bucketFiles < 1 || bucketDirs < 1 {
		t.Fatalf("expected >=1 bucket dir and file, got dirs=%d files=%d", bucketDirs, bucketFiles)
	}

	// symbols.json summary parses; symbols.ndjson exists with a line count.
	symData, err := os.ReadFile(filepath.Join(srcDir, "symbols.json"))
	if err != nil {
		t.Fatalf("read symbols.json: %v", err)
	}
	var sm symbolMap
	if err := json.Unmarshal(symData, &sm); err != nil {
		t.Fatalf("parse symbols.json: %v", err)
	}
	if sm.Counts.Total != res.FunctionCount {
		t.Fatalf("symbols.json total=%d != res.FunctionCount=%d", sm.Counts.Total, res.FunctionCount)
	}

	ndjsonLines := 0
	sf, err := os.Open(filepath.Join(srcDir, "symbols.ndjson"))
	if err != nil {
		t.Fatalf("open symbols.ndjson: %v", err)
	}
	defer sf.Close()
	ssc := bufio.NewScanner(sf)
	ssc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for ssc.Scan() {
		if strings.TrimSpace(ssc.Text()) != "" {
			ndjsonLines++
		}
	}
	if err := ssc.Err(); err != nil {
		t.Fatalf("scan symbols.ndjson: %v", err)
	}
	if ndjsonLines != res.FunctionCount {
		t.Fatalf("symbols.ndjson lines=%d != FunctionCount=%d", ndjsonLines, res.FunctionCount)
	}

	t.Logf("REAL SPLIT OK: functions=%d named=%d namedPct=%.2f%% | buckets: dirs=%d files=%d bytes=%d (%d MiB) | symbols.ndjson lines=%d | peakHeap=%d MiB | wall=%s",
		res.FunctionCount, res.NamedCount, res.NamedPct,
		bucketDirs, bucketFiles, bucketBytes, bucketBytes>>20,
		ndjsonLines, peak>>20, elapsed.Round(time.Millisecond))

	if peak > heapCap {
		t.Fatalf("peak HeapAlloc = %d MiB exceeds bound %d MiB on the real monolith",
			peak>>20, heapCap>>20)
	}
}
