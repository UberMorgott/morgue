package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
)

// trickleServer serves a never-finishing "binary": it advertises a large size
// and then dribbles bytes out, so a download against it is always in flight.
func trickleServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000000")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for {
			if _, err := w.Write([]byte("morgue")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
}

// TestInstallCancelledMidDownload proves the pipeline can abort a tool download:
// cancelling the context must make Install return promptly with a context error
// and must not leave a truncated binary behind for Resolve() to pick up.
func TestInstallCancelledMidDownload(t *testing.T) {
	srv := trickleServer(t)
	defer srv.Close()

	orig := Registry
	Registry = append(append([]ToolDef(nil), Registry...), ToolDef{
		Name:        "test-slow-tool",
		Description: "test-only trickling download",
		Category:    CategoryExtractor,
		Method:      MethodDirectURL,
		URL:         srv.URL + "/slowtool.exe",
		Binary:      "slowtool.exe",
		Optional:    true,
	})
	t.Cleanup(func() { Registry = orig })

	baseDir := t.TempDir()
	mgr := NewManager(baseDir, config.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := mgr.Install(ctx, "test-slow-tool", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Install returned nil error for a cancelled download")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Install took %v after cancel — cancellation did not propagate", elapsed)
	}

	partial := filepath.Join(baseDir, "test-slow-tool", "slowtool.exe")
	if _, statErr := os.Stat(partial); statErr == nil {
		t.Fatalf("half-written binary left behind at %s", partial)
	}
	if mgr.IsInstalled("test-slow-tool") {
		t.Fatal("cancelled install reports the tool as installed")
	}
}

// TestDownloadFileCancelledRemovesPartial covers the same guarantee one layer
// down, where every install method funnels through.
func TestDownloadFileCancelledRemovesPartial(t *testing.T) {
	srv := trickleServer(t)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "partial.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := downloadFile(ctx, srv.URL+"/partial.bin", dest, nil)
	if err == nil {
		t.Fatal("downloadFile returned nil error for a cancelled download")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatalf("partial file left behind at %s", dest)
	}
}
