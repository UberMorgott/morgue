package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/UberMorgott/morgue/internal/api/apitoken"
	"github.com/UberMorgott/morgue/internal/runmodel"
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
			err := APIRunWait(runmodel.RunRequest{Path: "x.exe"})
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

// `morgue api run` must forward every run flag, not just path/output, and
// resolve local paths against the caller's working directory.
func TestAPIRunForwardsFullRequest(t *testing.T) {
	var got runmodel.RunRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	}))
	defer ts.Close()
	old := apiBase
	apiBase = ts.URL + "/api"
	defer func() { apiBase = old }()
	t.Setenv(apitoken.EnvPath, filepath.Join(t.TempDir(), "none"))

	cmd := &cobra.Command{Use: "run"}
	AddRunFlags(cmd)
	if err := cmd.ParseFlags([]string{"-o", "out", "--recipe", "native", "--no-skip", "--exclude", "a,b",
		"--ref", "refs", "--allow-dynamic=false", "--cflow", "--code-only", "--gamedata-out", "gd",
		"--tools-mirror", "https://mirror.example"}); err != nil {
		t.Fatal(err)
	}
	if err := APIRun(RequestFromFlags(cmd, "game.exe")); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	abs := func(p string) string { return filepath.Join(cwd, p) }
	if got.Path != abs("game.exe") || got.Output != abs("out") || got.GameDataOut != abs("gd") ||
		len(got.ReferencePaths) != 1 || got.ReferencePaths[0] != abs("refs") {
		t.Fatalf("paths not absolutized: %+v", got)
	}
	if got.Recipe != "native" || got.ToolsMirror != "https://mirror.example" || strings.Join(got.Exclude, ",") != "a,b" {
		t.Fatalf("string fields lost: %+v", got)
	}
	if got.NoSkip == nil || !*got.NoSkip || got.AllowDynamic == nil || *got.AllowDynamic ||
		got.Cflow == nil || !*got.Cflow || got.CodeOnly == nil || !*got.CodeOnly {
		t.Fatalf("bool flags lost: %+v", got)
	}
}

// Omitted bool flags stay absent so the GUI applies its config defaults.
func TestAPIRunOmitsUnsetBools(t *testing.T) {
	cmd := &cobra.Command{Use: "run"}
	AddRunFlags(cmd)
	req := RequestFromFlags(cmd, "x.exe")
	b, _ := json.Marshal(req)
	for _, k := range []string{"noSkip", "allowDynamic", "cflow", "codeOnly"} {
		if strings.Contains(string(b), k) {
			t.Fatalf("%s present in %s", k, b)
		}
	}
}
