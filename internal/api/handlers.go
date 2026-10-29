package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/services"
	"github.com/UberMorgott/morgue/internal/tools"
)

// --- Pipeline handlers ---

// decodeRunRequest strictly decodes a runmodel.RunRequest: unknown fields,
// wrong types and trailing JSON values are rejected. The legacy {path,output}
// body is a valid subset.
func decodeRunRequest(r *http.Request) (runmodel.RunRequest, error) {
	var req runmodel.RunRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return req, errors.New("request body must contain a single JSON object")
	}
	return req, nil
}

// runStarted is the 202 body of POST /api/run.
type runStarted struct {
	Status    string    `json:"status"`
	RunID     string    `json:"runId"`
	StartedAt time.Time `json:"startedAt"`
	Output    string    `json:"output"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRunRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	// StartSpec reserves the single run slot and prepares the run synchronously
	// (validation + output reservation), so concurrent POSTs get exactly one
	// admission and the client learns the handle; execution continues in a
	// background goroutine. CLI clients follow GET /api/run/status or SSE
	// /api/events (pipeline:event:v2). Run errors are broadcast as
	// pipeline:error via the hook set in NewServer.
	//nolint:contextcheck // the run must outlive this request; StartSpec gives it its own cancellable ctx (POST /api/stop)
	h, err := s.pipeline.StartSpec(req)
	switch {
	case errors.Is(err, services.ErrBusy), errors.Is(err, engine.ErrOutputBusy):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	case errors.Is(err, engine.ErrInvalidRun):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, runStarted{Status: "started", RunID: h.ID, StartedAt: h.StartedAt.UTC(), Output: h.Output})
}
func (s *Server) handleStop(w http.ResponseWriter, _ *http.Request) {
	if err := s.pipeline.StopActive(); errors.Is(err, services.ErrNotRunning) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not running"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleGetPipelineStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.pipeline.GetStatus())
}

// --- Tools handlers ---

func (s *Server) handleGetTools(w http.ResponseWriter, r *http.Request) {
	waitStr := r.URL.Query().Get("wait")
	if waitStr != "" {
		waitSec, err := strconv.Atoi(waitStr)
		if err != nil || waitSec < 1 {
			waitSec = 30
		}
		if waitSec > 120 {
			waitSec = 120
		}
		// Lift the server WriteTimeout for this long-poll (120 s max wait).
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(waitSec)*time.Second + 30*time.Second))
		changed := s.tools.WaitForChange(time.Duration(waitSec) * time.Second)
		resp := s.tools.GetToolsEnriched()
		resp.Changed = changed
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, s.tools.GetToolsEnriched())
}

// toolRequest: optional exact version for install (operator override); an
// install without version clears an existing override on success.
type toolRequest struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

func (s *Server) handleInstallTool(w http.ResponseWriter, r *http.Request) {
	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	//nolint:contextcheck // the install job must outlive this request, so it runs detached from r.Context()
	switch err := s.tools.StartInstallVersion(req.Name, req.Version); {
	case errors.Is(err, services.ErrBusy):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	case errors.Is(err, tools.ErrInUse):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "in use"})
		return
	case errors.Is(err, services.ErrInvalidToolRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	tool := req.Name
	if tool == "" {
		tool = "all"
	}
	s.events.Broadcast("tool:install:start", marshalJSON(map[string]string{"tool": tool}))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "installing", "name": tool})
}
func (s *Server) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	// Synchronous so an active run lease is reported (409) instead of being
	// silently ignored; a leased payload is never evicted.
	if err := s.tools.Delete(req.Name); err != nil {
		if errors.Is(err, tools.ErrInUse) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "in use"})
			return
		}
		log.Printf("api: delete %s: %v", req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": req.Name})
}

// --- Settings handlers ---

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.config.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Redact sensitive fields before returning.
	redacted := *cfg
	if redacted.GitHubToken != "" {
		redacted.GitHubToken = "***"
	}
	writeJSON(w, http.StatusOK, &redacted)
}

type updateSettingsRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// settingsAllowlist defines which config keys may be modified via the API.
var settingsAllowlist = map[string]bool{
	"MaxFileSizeMB":         true,
	"SkipSystemLibs":        true,
	"GeneratePDB":           true,
	"AutoUpdateCheck":       true,
	"DefaultOutputDir":      true,
	"UE5GhidraDecompile":    true,
	"NativeGhidraDecompile": true,
	"DelphiIDRAnalysis":     true,
	"DelphiGhidraDecompile": true,
	"AllowDynamicExecution": true,
	"UnityExtractAssets":    true,
	"GhidraMaxMemGB":        true,
	"GhidraMaxCPU":          true,
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	if !settingsAllowlist[req.Key] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "setting not modifiable via API"})
		return
	}

	cfg, err := s.config.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Marshal config to a map, update the key, unmarshal back.
	data, err := json.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if _, ok := m[req.Key]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown setting key"})
		return
	}
	m[req.Key] = req.Value

	data, err = json.Marshal(m)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if err := s.config.Save(*cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Redact sensitive fields before returning.
	if cfg.GitHubToken != "" {
		cfg.GitHubToken = "***"
	}
	writeJSON(w, http.StatusOK, cfg)
}

// marshalJSON is a convenience wrapper around json.Marshal that swallows errors.
// It is safe for fire-and-forget SSE payloads where encoding cannot realistically fail.
func marshalJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

// writeJSON is a helper that encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- Info handler ---

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query parameter is required"})
		return
	}

	path = filepath.Clean(path)

	if _, err := os.Stat(path); err != nil { //nolint:gosec // the caller names the file to inspect; the API is token-guarded and loopback-only
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	result, err := recon.Classify(r.Context(), path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	recipeName := ""
	if rec := recipe.Match(&result); rec != nil {
		recipeName = rec.Name()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":                path,
		"size":                result.Size,
		"sha256":              result.SHA256,
		"kind":                result.Kind.String(),
		"runtime":             result.Runtime,
		"compiler":            result.Compiler,
		"obfuscator":          result.Obfuscator,
		"obfuscator_features": result.ObfuscatorFeatures,
		"packed":              result.Packed,
		"embedded_suspected":  result.EmbeddedSuspected,
		"embedded_signals":    result.EmbeddedSignals,
		"recipe":              recipeName,
	})
}
