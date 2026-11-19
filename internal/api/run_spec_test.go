package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/services"
)

func newSpecServer(t *testing.T, last *runmodel.RunRequest) (*Server, *httptest.Server, chan error) {
	t.Helper()
	pipeline := services.NewPipelineServiceWithPrepare(fakePrepare(func(context.Context) error { return nil }, last))
	s := NewServer(pipeline, services.NewToolsService(""), &services.ConfigService{}, &services.ReconService{}, nil)
	done := make(chan error, 4)
	// NewServer installed its own hook; chain ours for test synchronisation.
	services.SetRunFinishedHook(pipeline, func(err error) { done <- err })
	ts := httptest.NewServer(s.http.Handler)
	t.Cleanup(ts.Close)
	return s, ts, done
}

func TestRunAcceptsFullRunRequest(t *testing.T) {
	yes, no := true, false
	want := runmodel.RunRequest{
		Path: "in.exe", Output: "out", Recipe: "dotnet", NoSkip: &yes,
		Exclude: []string{"*.pdb"}, ReferencePaths: []string{"refs"},
		AllowDynamic: &yes, Cflow: &yes, CodeOnly: &no,
		GameDataOut: "gd", ToolsMirror: "https://mirror.example",
	}
	var got runmodel.RunRequest
	s, ts, done := newSpecServer(t, &got)
	body, _ := json.Marshal(want)
	code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body)
	if code != http.StatusAccepted {
		t.Fatalf("got %d %s", code, b)
	}
	if err := <-done; err != nil {
		t.Fatalf("run finished with error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request lost fields:\n got %+v\nwant %+v", got, want)
	}
	var resp struct {
		Status    string    `json:"status"`
		RunID     string    `json:"runId"`
		StartedAt time.Time `json:"startedAt"`
		Output    string    `json:"output"`
	}
	if err := json.Unmarshal([]byte(b), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "started" || len(resp.RunID) != 32 || resp.StartedAt.IsZero() || resp.Output != "out" {
		t.Fatalf("202 body %s", b)
	}

	// Status exposes the run identity, backend start time, lastSeq and outcome.
	code, sb := doReq(t, http.MethodGet, ts.URL+"/api/run/status", s.Token(), "", "", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var st map[string]any
	_ = json.Unmarshal([]byte(sb), &st)
	if st["runId"] != resp.RunID || st["outcome"] != "success" || st["lastSeq"] != float64(2) || st["phase"] != "done" {
		t.Fatalf("status %s", sb)
	}
	started, _ := st["startedAt"].(string)
	if ts, _ := time.Parse(time.RFC3339Nano, started); !ts.Equal(resp.StartedAt) {
		t.Fatalf("startedAt %v != %v", st["startedAt"], resp.StartedAt)
	}
}

func TestRunLegacyBodyStillValid(t *testing.T) {
	var got runmodel.RunRequest
	s, ts, done := newSpecServer(t, &got)
	code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", []byte(`{"path":"a.exe","output":"o"}`))
	if code != http.StatusAccepted {
		t.Fatalf("got %d %s", code, b)
	}
	if err := <-done; err != nil {
		t.Fatalf("run finished with error: %v", err)
	}
	if !reflect.DeepEqual(got, runmodel.RunRequest{Path: "a.exe", Output: "o"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestRunRejectsMalformedBodies(t *testing.T) {
	s, ts, _ := newSpecServer(t, nil)
	for name, body := range map[string]string{
		"unknown field": `{"path":"a.exe","bogus":1}`,
		"extra value":   `{"path":"a.exe"} {"path":"b.exe"}`,
		"wrong type":    `{"path":"a.exe","allowDynamic":"yes"}`,
		"array exclude": `{"path":"a.exe","exclude":"*.pdb"}`,
		"missing path":  `{"output":"o"}`,
		"not json":      `path=a.exe`,
	} {
		t.Run(name, func(t *testing.T) {
			code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", []byte(body))
			if code != http.StatusBadRequest {
				t.Fatalf("got %d %s, want 400", code, b)
			}
		})
	}
	if st := s.pipeline.GetStatus(); st.Running {
		t.Fatal("rejected body admitted a run")
	}
}

// The real engine rejects an unavailable input before admission: 400, and the
// slot stays free.
func TestRunEngineValidationIs400(t *testing.T) {
	s := newTestServer()
	ts := httptest.NewServer(s.http.Handler)
	defer ts.Close()
	body, _ := json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "missing.exe")})
	code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body)
	if code != http.StatusBadRequest || !strings.Contains(b, "invalid run request") {
		t.Fatalf("got %d %s, want 400", code, b)
	}
	if st := s.pipeline.GetStatus(); st.Running {
		t.Fatal("slot held after failed preparation")
	}
}

func TestSSERelaysEventsV2WithID(t *testing.T) {
	s, _, _ := newSpecServer(t, nil)
	ch := make(chan sseMsg, 8)
	s.events.mu.Lock()
	s.events.clients[ch] = struct{}{}
	s.events.mu.Unlock()

	h, err := runmodel.NewHandle("o")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := runmodel.NewEvent(h.ID, 7, time.Now(), "", "", runmodel.ScanCompleted{FilesTotal: 3, GroupsTotal: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.broadcastRunEvent(ev)
	msg := <-ch
	data, _ := json.Marshal(ev)
	want := "event: pipeline:event:v2\nid: " + h.ID + ":7\ndata: " + string(data) + "\n\n"
	if msg.text != want || msg.run != h.ID || msg.seq != 7 {
		t.Fatalf("sse frame:\n%q\nwant\n%q", msg.text, want)
	}
}
