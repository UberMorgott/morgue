package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/api/apitoken"
	"github.com/UberMorgott/morgue/internal/services"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const listenAddr = "127.0.0.1:19876"

// Server is an HTTP API server that runs alongside the Wails GUI,
// allowing AI agents and CLI tools to control the app.
type Server struct {
	pipeline *services.PipelineService
	tools    *services.ToolsService
	config   *services.ConfigService
	recon    *services.ReconService
	events   *EventBroadcaster
	http     *http.Server
	app      *application.App

	// token is the per-launch secret clients send in apitoken.Header.
	token string
	// tokenPath is where Start publishes token for the CLI / curl.
	tokenPath string
}

// NewServer creates a Server wired to the given services.
func NewServer(pipeline *services.PipelineService, tools *services.ToolsService, cfg *services.ConfigService, recon *services.ReconService) *Server {
	s := &Server{
		pipeline: pipeline,
		tools:    tools,
		config:   cfg,
		recon:    recon,
		events:   NewEventBroadcaster(),
	}
	// crypto/rand never fails on supported platforms (Go 1.24+ crashes instead).
	s.token, _ = apitoken.Generate()
	services.SetRunFinishedHook(pipeline, func(runErr error) {
		if runErr != nil {
			s.events.Broadcast("pipeline:error", marshalJSON(map[string]string{"error": runErr.Error()}))
		}
	})
	services.SetEventHook(pipeline, s.broadcastRunEvent)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/events", s.events.HandleSSE)

	// Pipeline
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("GET /api/run/status", s.handleGetPipelineStatus)
	mux.HandleFunc("POST /api/stop", s.handleStop)

	// Tools
	mux.HandleFunc("GET /api/tools", s.handleGetTools)
	mux.HandleFunc("POST /api/tools/install", s.handleInstallTool)
	mux.HandleFunc("POST /api/tools/delete", s.handleDeleteTool)

	// Settings
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)

	// Recon
	mux.HandleFunc("GET /api/info", s.handleInfo)

	// Instructions
	mux.HandleFunc("GET /api/instructions", s.handleInstructions)

	s.http = &http.Server{
		Addr:              listenAddr,
		Handler:           securityMiddleware(s.token, maxBodyMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout bounds ordinary responses; the SSE handler and the tools
		// long-poll lift it per request via http.ResponseController.
		WriteTimeout: 120 * time.Second,
	}

	return s
}

// Start begins listening on 127.0.0.1:19876. It returns once the listener
// is ready (or on error). The server runs in a background goroutine.
func (s *Server) Start() error {
	if s.tokenPath == "" {
		p, err := apitoken.Path()
		if err != nil {
			return fmt.Errorf("api: %w", err)
		}
		s.tokenPath = p
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
	}
	// Publish the token only once we own the port, so a second instance that
	// fails to bind cannot clobber the running instance's token.
	if err := apitoken.Write(s.tokenPath, s.token); err != nil {
		_ = ln.Close()
		return fmt.Errorf("api: %w", err)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("api server panic: %v", r)
			}
		}()
		_ = s.http.Serve(ln)
	}()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.events.Shutdown()
	// Remove the token file if it is still ours.
	if s.tokenPath != "" {
		if b, err := os.ReadFile(s.tokenPath); err == nil && string(b) == s.token {
			_ = os.Remove(s.tokenPath)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// HookEvents subscribes to Wails application events and rebroadcasts them via SSE.
func (s *Server) HookEvents(app *application.App) {
	s.app = app
	events := []string{
		"pipeline:progress",
		"update:progress",
		"tool:download:start",
		"tool:download:progress",
		"tool:download:complete",
		"tool:installed",
		"startup:progress",
	}
	for _, name := range events {
		eventName := name
		app.Event.On(eventName, func(e *application.CustomEvent) {
			data, _ := json.Marshal(e.Data)
			s.events.Broadcast(eventName, data)
		})
	}
}

// Token returns the per-launch API token (for tests and in-process callers).
func (s *Server) Token() string { return s.token }

// isLoopbackHost reports whether host (no port) names this machine's loopback.
func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// allowedOrigin reports whether a browser Origin may talk to the API: loopback
// pages and the Wails webview (wails://wails, http(s)://wails.localhost).
// Parsed, not prefix-matched, so http://localhost.evil.com is rejected.
func allowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "wails":
		return true
	case "http", "https":
		h := u.Hostname()
		return isLoopbackHost(h) || strings.EqualFold(h, "wails.localhost")
	}
	return false
}

// tokenExempt lists read-only endpoints that carry no sensitive data and are
// fetched by the GUI webview (which cannot know the token) or used as a
// liveness probe.
func tokenExempt(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/api/status" || r.URL.Path == "/api/instructions"
}

// securityMiddleware guards the localhost API against drive-by browser
// requests (CSRF) and DNS rebinding:
//   - Host must be a loopback name (defeats DNS rebinding);
//   - a present Origin must be loopback or the Wails webview (CLI/curl send none);
//   - POST/PUT bodies must be application/json (forces a CORS preflight);
//   - every non-exempt request must carry the per-launch token in X-Morgue-Token.
func securityMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")

		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopbackHost(strings.Trim(host, "[]")) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden host"})
			return
		}

		origin := r.Header.Get("Origin")
		if origin != "" {
			if !allowedOrigin(origin) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden origin"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+apitoken.Header)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
				return
			}
		}

		if !tokenExempt(r) {
			got := r.Header.Get(apitoken.Header)
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid " + apitoken.Header})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodyMiddleware limits request body size to 1MB for POST/PUT methods.
func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running": true,
		"port":    "19876",
	})
}
