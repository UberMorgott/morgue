package api

import (
	"encoding/json"
	"net/http"
)

// --- Pipeline handlers ---

type runRequest struct {
	Path   string `json:"path"`
	Output string `json:"output"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	go func() {
		s.events.Broadcast("pipeline:started", nil)
		if err := s.pipeline.Run(req.Path, req.Output); err != nil {
			errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			s.events.Broadcast("pipeline:error", errMsg)
			return
		}
		s.events.Broadcast("pipeline:complete", nil)
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleGetPipelineStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.pipeline.GetStatus())
}

// --- Tools handlers ---

func (s *Server) handleGetTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.tools.CheckAll())
}

type toolRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleInstallTool(w http.ResponseWriter, r *http.Request) {
	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Name == "" {
		go func() {
			if err := s.tools.InstallAll(); err != nil {
				errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
				s.events.Broadcast("tools:install:error", errMsg)
			}
		}()
		writeJSON(w, http.StatusOK, map[string]string{"status": "installing all"})
		return
	}

	go func() {
		if err := s.tools.Install(req.Name); err != nil {
			errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			s.events.Broadcast("tools:install:error", errMsg)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]string{"status": "installing", "name": req.Name})
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

	if err := s.tools.Delete(req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
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
	writeJSON(w, http.StatusOK, cfg)
}

type updateSettingsRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
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

	writeJSON(w, http.StatusOK, cfg)
}

// writeJSON is a helper that encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
