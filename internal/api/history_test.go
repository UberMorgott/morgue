package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/api/apitoken"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/services"
	"github.com/UberMorgott/morgue/internal/workspace"
)

const histRun = "0123456789abcdef0123456789abcdef"

func historyFixture(t *testing.T, n uint64) (*HistoryHandlers, http.Handler) {
	t.Helper()
	reader := services.WorkspaceReader{
		Read: func(string) (workspace.Recovery, error) { return workspace.Recovery{LastSeq: n}, nil },
		ReadEvents: func(root, runID string, after uint64, limit int) (workspace.EventPage, error) {
			p := workspace.EventPage{RunID: runID, LastSeq: n, Events: []runmodel.Event{}}
			for s := after + 1; s <= n && len(p.Events) < limit; s++ {
				p.Events = append(p.Events, runmodel.Event{Version: 2, Run: runID, Seq: s, Type: runmodel.EventLog, Data: json.RawMessage(`{}`)})
			}
			p.HasMore = after+uint64(len(p.Events)) < n
			return p, nil
		},
	}
	ws := services.NewWorkspaceService(reader, workspace.NewRecentStore(filepath.Join(t.TempDir(), "r.json")))
	ts := time.Now().UTC()
	ev, err := runmodel.NewEvent(histRun, 1, ts, "", "", runmodel.RunStarted{
		Handle: runmodel.RunHandle{ID: histRun, StartedAt: ts, Output: t.TempDir()},
		Spec:   runmodel.RunSpec{Version: 1, Exclude: []string{}, ReferencePaths: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	services.ObserveWorkspaceEvent(ws, ev)
	h := NewHistoryHandlers(ws)
	mux := http.NewServeMux()
	h.Register(mux)
	return h, securityMiddleware("tok", mux)
}

func get(t *testing.T, srv http.Handler, url string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	req.Host = "127.0.0.1:19876"
	if auth {
		req.Header.Set(apitoken.Header, "tok")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestHistoryEventsAuthenticatedBoundedPages(t *testing.T) {
	_, srv := historyFixture(t, 2500)
	if rec := get(t, srv, "/api/runs/current/events?run="+histRun, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	rec := get(t, srv, "/api/runs/current/events?run="+histRun+"&after=1000&limit=9999", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	var page struct {
		RunID   string           `json:"runId"`
		Events  []runmodel.Event `json:"events"`
		LastSeq uint64           `json:"lastSeq"`
		HasMore bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.RunID != histRun || len(page.Events) != 1000 || page.Events[0].Seq != 1001 || !page.HasMore || page.LastSeq != 2500 {
		t.Fatalf("page run=%s n=%d first=%d more=%v last=%d", page.RunID, len(page.Events), page.Events[0].Seq, page.HasMore, page.LastSeq)
	}
	for url, want := range map[string]int{
		"/api/runs/current/events?run=" + strings.Repeat("f", 32): http.StatusConflict,
		"/api/runs/current/events?run=bad":                        http.StatusBadRequest,
		"/api/runs/current/events?run=" + histRun + "&after=-1":   http.StatusBadRequest,
		"/api/runs/current": http.StatusOK,
	} {
		if rec := get(t, srv, url, true); rec.Code != want {
			t.Errorf("%s: %d want %d", url, rec.Code, want)
		}
	}
}

func TestHistoryReplaySSEFramingAndStaleRun(t *testing.T) {
	h, _ := historyFixture(t, 3)
	rec := httptest.NewRecorder()
	if _, _, ok := h.CheckReplay(rec, strings.Repeat("e", 32)+":2"); ok || rec.Code != http.StatusConflict {
		t.Fatalf("stale run: ok=%v code=%d", ok, rec.Code)
	}
	rec = httptest.NewRecorder()
	run, after, ok := h.CheckReplay(rec, histRun+":1")
	if !ok || run != histRun || after != 1 {
		t.Fatalf("check: %v %s %d", ok, run, after)
	}
	var buf bytes.Buffer
	last, err := h.WriteReplay(&buf, run, after)
	if err != nil || last != 3 {
		t.Fatalf("replay last=%d err=%v", last, err)
	}
	s := buf.String()
	if strings.Count(s, "event: "+runmodel.EventName+"\n") != 2 || !strings.Contains(s, "id: "+histRun+":2\n") || strings.Contains(s, ":1\n") {
		t.Fatalf("framing:\n%s", s)
	}
}

// SSE Last-Event-ID reconnect: journal replay after the given seq, live
// duplicates dropped, a dropped live notification filled from the journal,
// stale run 409 and malformed id 400 before streaming.
func TestSSELastEventIDReplaysJournal(t *testing.T) {
	var mu sync.Mutex
	n := uint64(1500)
	mk := func(s uint64) runmodel.Event {
		return runmodel.Event{Version: 2, Run: histRun, Seq: s, Type: runmodel.EventLog, Data: json.RawMessage(`{}`)}
	}
	reader := services.WorkspaceReader{
		Read: func(string) (workspace.Recovery, error) { return workspace.Recovery{}, nil },
		ReadEvents: func(root, runID string, after uint64, limit int) (workspace.EventPage, error) {
			mu.Lock()
			last := n
			mu.Unlock()
			p := workspace.EventPage{RunID: runID, LastSeq: last, Events: []runmodel.Event{}}
			for s := after + 1; s <= last && len(p.Events) < limit; s++ {
				p.Events = append(p.Events, mk(s))
			}
			p.HasMore = after+uint64(len(p.Events)) < last
			return p, nil
		},
	}
	ws := services.NewWorkspaceService(reader, workspace.NewRecentStore(filepath.Join(t.TempDir(), "r.json")))
	ts := time.Now().UTC()
	started, err := runmodel.NewEvent(histRun, 1, ts, "", "", runmodel.RunStarted{
		Handle: runmodel.RunHandle{ID: histRun, StartedAt: ts, Output: t.TempDir()},
		Spec:   runmodel.RunSpec{Version: 1, Exclude: []string{}, ReferencePaths: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	services.ObserveWorkspaceEvent(ws, started)
	s := NewServer(services.NewPipelineService(), services.NewToolsService(""), &services.ConfigService{}, &services.ReconService{}, ws)
	srv := httptest.NewServer(s.http.Handler)
	defer srv.Close()
	defer s.events.Shutdown()

	open := func(lastID string) *http.Response {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/events", nil)
		req.Header.Set(apitoken.Header, s.Token())
		req.Header.Set("Last-Event-ID", lastID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for id, code := range map[string]int{strings.Repeat("e", 32) + ":2": http.StatusConflict, "garbage": http.StatusBadRequest} {
		resp := open(id)
		_ = resp.Body.Close()
		if resp.StatusCode != code {
			t.Fatalf("Last-Event-ID %q: %d, want %d", id, resp.StatusCode, code)
		}
	}

	resp := open(histRun + ":3")
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	next := func() uint64 {
		for sc.Scan() {
			if id, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
				_, seq, ok := ParseLastEventID(id)
				if !ok {
					t.Fatalf("bad id %q", id)
				}
				return seq
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return 0
	}
	for want := uint64(4); want <= 1500; want++ {
		if got := next(); got != want {
			t.Fatalf("replayed seq %d, want %d", got, want)
		}
	}
	s.broadcastRunEvent(mk(1500)) // duplicate of a replayed record
	mu.Lock()
	n = 1502
	mu.Unlock()
	s.broadcastRunEvent(mk(1502)) // 1501's notification was "dropped"
	for _, want := range []uint64{1501, 1502} {
		if got := next(); got != want {
			t.Fatalf("live seq %d, want %d", got, want)
		}
	}
}
