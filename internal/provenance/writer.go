// Package provenance streams additive NDJSON identity records.
package provenance

import (
	"encoding/json"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"os"
	"path/filepath"
	"sync"
)

type Sink interface {
	Write(runmodel.ProvenanceRecord) error
}
type Writer struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

func Open(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Writer{file: f, encoder: json.NewEncoder(f)}, nil
}
func (w *Writer) Write(r runmodel.ProvenanceRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(r)
}
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}
