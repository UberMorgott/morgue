package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/api/apitoken"
	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/services"
)

// blockingRunner returns an exec func that blocks until its context is
// cancelled (or release is closed), reporting how many runs were started.
func blockingRunner(started *int32Counter, release <-chan struct{}) func(context.Context) error {
	return func(ctx context.Context) error {
		started.inc()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
}

// fakeRun is an admitted run whose execution is exec; it emits a valid
// run.started / run.finished pair so transports see a real v2 stream.
type fakeRun struct {
	h    runmodel.RunHandle
	spec runmodel.RunSpec
	exec func(context.Context) error
}

func (f *fakeRun) Handle() runmodel.RunHandle { return f.h }
func (f *fakeRun) Close() error               { return nil }
func (f *fakeRun) Execute(ctx context.Context, _ *engine.PauseGate, events chan<- runmodel.Event) (runmodel.RunResult, error) {
	if ev, err := runmodel.NewEvent(f.h.ID, 1, f.h.StartedAt, "", "", runmodel.RunStarted{Handle: f.h, Spec: f.spec}); err == nil {
		events <- ev
	}
	err := f.exec(ctx)
	outcome := runmodel.OutcomeSuccess
	switch {
	case errors.Is(err, context.Canceled):
		outcome = runmodel.OutcomeCancelled
	case err != nil:
		outcome = runmodel.OutcomeError
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	now := time.Now().UTC()
	if ev, e := runmodel.NewEvent(f.h.ID, 2, now, "", "", runmodel.RunFinished{Outcome: outcome, FinishedAt: now, Error: errText}); e == nil {
		events <- ev
	}
	return runmodel.RunResult{Handle: f.h, Outcome: outcome, Error: errText}, err
}

// fakePrepare admits every request, recording the last one in *last (if non-nil).
func fakePrepare(exec func(context.Context) error, last *runmodel.RunRequest) services.PrepareFunc {
	var mu sync.Mutex
	return func(_ context.Context, req runmodel.RunRequest) (services.AdmittedRun, error) {
		if last != nil {
			mu.Lock()
			*last = req
			mu.Unlock()
		}
		h, err := runmodel.NewHandle(req.Output)
		if err != nil {
			return nil, err
		}
		return &fakeRun{h: h, spec: runmodel.RunSpec{Version: runmodel.SpecVersion, Path: req.Path, Output: req.Output}, exec: exec}, nil
	}
}

type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *int32Counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// newGuardedServer returns an httptest server running the full middleware
// chain around a Server whose pipeline uses runner.
func newGuardedServer(t *testing.T, runner func(context.Context) error) (*Server, *httptest.Server) {
	t.Helper()
	pipeline := services.NewPipelineServiceWithPrepare(fakePrepare(runner, nil))
	s := NewServer(pipeline, services.NewToolsService(""), &services.ConfigService{}, &services.ReconService{}, nil)
	ts := httptest.NewServer(s.http.Handler)
	t.Cleanup(ts.Close)
	return s, ts
}

func doReq(t *testing.T, method, url, token, contentType, origin string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set(apitoken.Header, token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(b)
}

func TestRunAdmissionConcurrent(t *testing.T) {
	var started int32Counter
	release := make(chan struct{})
	s, ts := newGuardedServer(t, blockingRunner(&started, release))
	defer close(release)

	body, _ := json.Marshal(map[string]string{"path": "x.exe"})
	const n = 16
	codes := make([]int, n)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-gate
			code, _ := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body)
			codes[i] = code
		})
	}
	close(gate)
	wg.Wait()

	accepted, busy := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			busy++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if accepted != 1 || busy != n-1 {
		t.Fatalf("accepted=%d busy=%d, want 1/%d", accepted, busy, n-1)
	}
	deadline := time.Now().Add(2 * time.Second)
	for started.get() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := started.get(); got != 1 {
		t.Fatalf("runner started %d times, want 1", got)
	}
	if st := s.pipeline.GetStatus(); !st.Running {
		t.Fatalf("status not running after admission: %+v", st)
	}
}

func TestStopEndpoint(t *testing.T) {
	var started int32Counter
	release := make(chan struct{})
	defer close(release)
	s, ts := newGuardedServer(t, blockingRunner(&started, release))

	code, _ := doReq(t, http.MethodPost, ts.URL+"/api/stop", s.Token(), "application/json", "", []byte("{}"))
	if code != http.StatusConflict {
		t.Fatalf("stop while idle: got %d, want 409", code)
	}

	body, _ := json.Marshal(map[string]string{"path": "x.exe"})
	if code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body); code != http.StatusAccepted {
		t.Fatalf("run: got %d %s", code, b)
	}
	if code, b := doReq(t, http.MethodPost, ts.URL+"/api/stop", s.Token(), "application/json", "", []byte("{}")); code != http.StatusAccepted {
		t.Fatalf("stop: got %d %s", code, b)
	}

	deadline := time.Now().Add(2 * time.Second)
	for s.pipeline.GetStatus().Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	st := s.pipeline.GetStatus()
	if st.Running || st.Phase != "cancelled" || st.Error == "" {
		t.Fatalf("after stop: %+v, want running=false phase=cancelled error set", st)
	}

	// The slot is free again: a new run is admitted.
	if code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body); code != http.StatusAccepted {
		t.Fatalf("rerun: got %d %s", code, b)
	}
}

func TestRunFailureStatus(t *testing.T) {
	boom := errors.New("boom")
	s, ts := newGuardedServer(t, func(context.Context) error { return boom })
	body, _ := json.Marshal(map[string]string{"path": "x.exe"})
	if code, b := doReq(t, http.MethodPost, ts.URL+"/api/run", s.Token(), "application/json", "", body); code != http.StatusAccepted {
		t.Fatalf("run: got %d %s", code, b)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.pipeline.GetStatus().Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := s.pipeline.GetStatus(); st.Phase != "error" || st.Error != "boom" {
		t.Fatalf("status %+v, want phase=error error=boom", st)
	}
}

func TestSecurityMiddleware(t *testing.T) {
	s, ts := newGuardedServer(t, func(context.Context) error { return nil })
	tok := s.Token()
	stop := ts.URL + "/api/stop"

	cases := []struct {
		name, method, url, token, ct, origin string
		want                                 int
	}{
		{"no token", http.MethodPost, stop, "", "application/json", "", http.StatusUnauthorized},
		{"bad token", http.MethodPost, stop, "nope", "application/json", "", http.StatusUnauthorized},
		{"form post", http.MethodPost, stop, tok, "application/x-www-form-urlencoded", "", http.StatusUnsupportedMediaType},
		{"text post", http.MethodPost, stop, tok, "text/plain", "", http.StatusUnsupportedMediaType},
		{"evil origin", http.MethodPost, stop, tok, "application/json", "https://evil.example", http.StatusForbidden},
		{"prefix origin", http.MethodPost, stop, tok, "application/json", "http://localhost.evil.com", http.StatusForbidden},
		{"get needs token", http.MethodGet, ts.URL + "/api/run/status", "", "", "", http.StatusUnauthorized},
		{"ok cli", http.MethodPost, stop, tok, "application/json; charset=utf-8", "", http.StatusConflict},
		{"ok localhost origin", http.MethodPost, stop, tok, "application/json", "http://localhost:5173", http.StatusConflict},
		{"ok wails origin", http.MethodGet, ts.URL + "/api/instructions", "", "", "http://wails.localhost", http.StatusOK},
		{"status exempt", http.MethodGet, ts.URL + "/api/status", "", "", "", http.StatusOK},
		{"preflight", http.MethodOptions, stop, "", "", "http://127.0.0.1:3000", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, b := doReq(t, tc.method, tc.url, tc.token, tc.ct, tc.origin, []byte("{}"))
			if code != tc.want {
				t.Fatalf("got %d (%s), want %d", code, b, tc.want)
			}
		})
	}

	// DNS rebinding: a foreign Host header is rejected even with a valid token.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/api/run/status", nil)
	req.Host = "attacker.example:19876"
	req.Header.Set(apitoken.Header, tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host: got %d, want 403", resp.StatusCode)
	}
}
