package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/api/apitoken"
)

// fakeGUI serves /api/run and /api/run/status, reporting the given terminal
// phase/error once polled, and records the token header it saw.
func fakeGUI(t *testing.T, runCode int, phase, errMsg string) (*string, func()) {
	t.Helper()
	seen := new(string)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Get(apitoken.Header)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/run":
			w.WriteHeader(runCode)
			if runCode >= 400 {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "busy"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
		case "/api/run/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"running": false, "phase": phase, "error": errMsg})
		}
	}))
	old := apiBase
	apiBase = ts.URL + "/api"
	tokFile := filepath.Join(t.TempDir(), "api-token")
	if err := os.WriteFile(tokFile, []byte("tok123"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apitoken.EnvPath, tokFile)
	return seen, func() { apiBase = old; ts.Close() }
}

func TestAPIRunWaitExitStatus(t *testing.T) {
	cases := []struct {
		name, phase, errMsg string
		runCode             int
		wantErr             string
	}{
		{"done", "done", "", http.StatusAccepted, ""},
		{"error", "error", "boom", http.StatusAccepted, "boom"},
		{"cancelled", "cancelled", "context canceled", http.StatusAccepted, "cancelled"},
		{"busy", "", "", http.StatusConflict, "409"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, done := fakeGUI(t, tc.runCode, tc.phase, tc.errMsg)
			defer done()
			err := APIRunWait("x.exe", "")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
			if *seen != "tok123" {
				t.Fatalf("token header = %q, want tok123", *seen)
			}
		})
	}
}
