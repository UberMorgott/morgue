package provenance

import (
	"bufio"
	"encoding/json"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriterStreamsConcurrentRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "provenance.ndjson")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			token := "0x06000001"
			if err := w.Write(runmodel.ProvenanceRecord{SchemaVersion: 1, Token: &token}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() {
		var r runmodel.ProvenanceRecord
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Token == nil || *r.Token != "0x06000001" {
			t.Fatal("identity lost")
		}
		n++
	}
	if s.Err() != nil || n != 50 {
		t.Fatalf("records=%d err=%v", n, s.Err())
	}
}
