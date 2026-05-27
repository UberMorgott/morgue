package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/events", s.events.HandleSSE)

	// Pipeline
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("GET /api/run/status", s.handleGetPipelineStatus)

	// Tools
	mux.HandleFunc("GET /api/tools", s.handleGetTools)
	mux.HandleFunc("POST /api/tools/install", s.handleInstallTool)
	mux.HandleFunc("POST /api/tools/delete", s.handleDeleteTool)

	// Settings
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)

	// Instructions
	mux.HandleFunc("GET /api/instructions", s.handleInstructions)

	s.http = &http.Server{
		Addr:    listenAddr,
		Handler: corsMiddleware(maxBodyMiddleware(mux)),
	}

	return s
}

// Start begins listening on 127.0.0.1:19876. It returns once the listener
// is ready (or on error). The server runs in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// HookEvents subscribes to Wails application events and rebroadcasts them via SSE.
func (s *Server) HookEvents(app *application.App) {
	s.app = app
	events := []string{
		"pipeline:progress",
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

// EmitToGUI emits a Wails event so the frontend receives it directly.
// Safe to call when app is nil (headless / CLI mode).
func (s *Server) EmitToGUI(event string, data ...any) {
	if s.app != nil {
		s.app.Event.Emit(event, data...)
	}
}

// corsMiddleware adds permissive CORS headers so the Wails webview
// (which runs on a different origin) can call the local API.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
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
