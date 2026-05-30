package api

import (
	"net/http"

	"github.com/UberMorgott/morgue/internal/instructions"
)

// AIInstructions contains Markdown instructions for AI agents
// that control Morgue via the CLI/API. The canonical text lives in the
// neutral leaf package internal/instructions so the Wails service can
// reference it without an import cycle.
const AIInstructions = instructions.Text

func (s *Server) handleInstructions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(AIInstructions))
}
