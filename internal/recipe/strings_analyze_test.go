package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAnalyzeStringsURLTokenization verifies that URL categorization extracts
// bounded URL tokens rather than storing a whole (possibly glued) line. This is
// the Go string-table case where adjacent strings concatenate without a NUL
// separator, producing a line like
// "https://example.com/apifoobarbaz https://other.org/x junk".
func TestAnalyzeStringsURLTokenization(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "strings.txt")
	out := filepath.Join(dir, "strings.json")

	lines := "" +
		"https://example.com/api?k=1&v=2 trailingGarbageGluedHere moreStuff\n" + // glued: one URL + garbage
		"prefix https://first.test/a https://second.test/b suffix\n" + // two URLs in one line
		"visit http://plain.example/path)now\n" + // closing paren bounds the token
		"clean https://solo.example/ok\n"

	if err := os.WriteFile(in, []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}

	analyzeStrings(in, out)

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("strings.json not written: %v", err)
	}
	var a StringsAnalysis
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	want := map[string]bool{
		"https://example.com/api?k=1&v=2": true,
		"https://first.test/a":            true,
		"https://second.test/b":           true,
		"http://plain.example/path":       true,
		"https://solo.example/ok":         true,
	}

	got := map[string]bool{}
	for _, u := range a.URLs {
		got[u] = true
		// No stored URL may contain a space (would indicate a glued multi-string blob).
		if containsSpace(u) {
			t.Errorf("URL entry contains a space (glued blob not tokenized): %q", u)
		}
	}

	for w := range want {
		if !got[w] {
			t.Errorf("expected URL %q not found; got %v", w, a.URLs)
		}
	}
	// The glued garbage tail must NOT be part of any URL.
	for _, u := range a.URLs {
		if u == "https://example.com/api?k=1&v=2 trailingGarbageGluedHere moreStuff" {
			t.Errorf("URL stored as glued blob: %q", u)
		}
	}
}

func containsSpace(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' {
			return true
		}
	}
	return false
}
