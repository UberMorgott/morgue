package services

import (
	"github.com/UberMorgott/morgue/internal/instructions"
)

// InstructionsService exposes the AI control instructions text to the
// frontend via a native Wails binding (no HTTP dependency).
type InstructionsService struct{}

// Get returns the AI control instructions Markdown text.
func (s *InstructionsService) Get() string {
	return instructions.Text
}
