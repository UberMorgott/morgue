package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/UberMorgott/morgue/internal/services"
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

	s.http = &http.Server{
		Addr:    listenAddr,
		Handler: mux,
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
		_ = s.http.Serve(ln)
	}()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running": true,
		"port":    "19876",
	})
}
