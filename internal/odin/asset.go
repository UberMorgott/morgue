package odin

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const serializedMarker = "SerializedBytes:"

// getHex scans an Asset file for the first "SerializedBytes:" line and returns
// the decoded raw blob. Mirrors C# Program.GetHex.
func getHex(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if idx := strings.Index(line, serializedMarker); idx >= 0 {
			h := strings.TrimSpace(line[idx+len(serializedMarker):])
			b, err := hex.DecodeString(h)
			if err != nil {
				return nil, fmt.Errorf("decode hex in %s: %w", path, err)
			}
			return b, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no %s in %s", serializedMarker, path)
}

// DecodeAssetFile decodes one .asset file's Odin blob into the readable tree.
// Returns the rendered text and the blob byte count.
func DecodeAssetFile(path string) (string, int, error) {
	blob, err := getHex(path)
	if err != nil {
		return "", 0, err
	}
	text, err := decodeBlob(blob)
	if err != nil {
		return "", len(blob), err
	}
	return text, len(blob), nil
}

// decodeBlob runs the decoder + printer inside a recover boundary so a
// malformed/truncated blob (out-of-range reads, bad string lengths, oversized
// primarrays) is converted into a returned error instead of crashing the
// process. The happy path is unaffected: recover only fires on a panic.
func decodeBlob(blob []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if de, ok := r.(decodeError); ok {
				err = de
			} else {
				err = fmt.Errorf("malformed odin blob: %v", r)
			}
		}
	}()
	d := newDecoder(blob)
	d.run()
	return printTree(d.toks), nil
}

// DecodeDir decodes the named files (in order) under dir, emitting the same
// banner+tree layout as the C# odindec Main loop: a leading blank line, a
// 60-'#' rule, "## <file>  (<n> bytes)", another rule, then the tree.
// Files that cannot be decoded are reported inline and do not abort the rest.
func DecodeDir(dir string, files []string) string {
	var sb strings.Builder
	rule := strings.Repeat("#", 60)
	for _, fn := range files {
		path := filepath.Join(dir, fn)
		if _, statErr := os.Stat(path); statErr != nil {
			sb.WriteString(fmt.Sprintf("## %s: MISSING\n", fn))
			continue
		}
		text, n, err := DecodeAssetFile(path)
		sb.WriteString("\n" + rule + "\n")
		sb.WriteString(fmt.Sprintf("## %s  (%d bytes)\n", fn, n))
		sb.WriteString(rule + "\n")
		if err != nil {
			sb.WriteString("  DECODE ERROR: " + err.Error() + "\n")
			continue
		}
		sb.WriteString(text)
	}
	return sb.String()
}
