package services

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
)

// GUI/API mutations obey run leases: a tool selected by an active run can be
// neither updated nor deleted, and the refusal is visible (ErrInUse).
func TestToolsServiceRespectsRunLease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2023 15:04:05 GMT")
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()
	orig := tools.Registry
	tools.Registry = []tools.ToolDef{{Name: "m3svc", Method: tools.MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"}}
	t.Cleanup(func() { tools.Registry = orig })
	base := t.TempDir()
	dir := filepath.Join(base, "m3svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"tool.exe": "payload", ".version": "2023.01.02"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := tools.NewManager(base, config.Config{})
	sel, err := run.NewRunSelection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sel.Ensure(t.Context(), []string{"m3svc"}, nil); err != nil {
		t.Fatal(err)
	}
	svc := &ToolsService{manager: tools.NewManager(base, config.Config{}), activeOps: map[string]*OpState{}, changeCh: make(chan struct{}), installing: map[string]struct{}{}}
	if err := svc.StartInstallVersion("m3svc", "2022.01.01"); !errors.Is(err, tools.ErrInUse) {
		t.Fatalf("install during lease: %v", err)
	}
	if err := svc.InstallVersion(t.Context(), "m3svc", ""); !errors.Is(err, tools.ErrInUse) {
		t.Fatalf("direct install during lease: %v", err)
	}
	if err := svc.Delete("m3svc"); !errors.Is(err, tools.ErrInUse) {
		t.Fatalf("delete during lease: %v", err)
	}
	if err := sel.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete("m3svc"); err != nil {
		t.Fatalf("delete after lease: %v", err)
	}
	if err := svc.StartInstallVersion("", "1.0"); !errors.Is(err, ErrInvalidToolRequest) {
		t.Fatalf("version without name: %v", err)
	}
}
