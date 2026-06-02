package recipe

import (
	"context"
	"encoding/json"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/tools"
)

// StepStatus represents the state of a pipeline step.
type StepStatus int

const (
	Pending StepStatus = iota
	Running
	Success
	Failed
	Skipped
)

var stepStatusNames = [...]string{
	"Pending",
	"Running",
	"Success",
	"Failed",
	"Skipped",
}

func (s StepStatus) String() string {
	if int(s) < len(stepStatusNames) {
		return stepStatusNames[s]
	}
	return "Unknown"
}

// MarshalJSON serializes StepStatus as its string name for frontend consumption.
func (s StepStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// StepInfo describes a recipe step.
type StepInfo struct {
	Name     string
	Required bool
}

// StepProgress reports the progress of a single step.
type StepProgress struct {
	Step       int
	Total      int
	Name       string
	Tool       string
	Count      int
	CountTotal int // total items expected (e.g. total DLLs to decompile); 0 = indeterminate
	Unit       string
	Status     StepStatus
	Duration   time.Duration
	Error      error
}

// PauseChecker allows recipes to check for pause between steps.
type PauseChecker interface {
	WaitIfPaused(ctx context.Context) error
}

// Context provides everything a recipe needs to execute.
type Context struct {
	Target   string
	Output   string
	Progress chan StepProgress
	Log      chan string
	Tools    *tools.Manager
	Ctx      context.Context
	Config   *config.Config
	Pause    PauseChecker
	// Obfuscator is the recon-detected obfuscator name (e.g. "ConfuserEx" or
	// the generic "Obfuscated"). Recipes that handle multiple obfuscators use
	// it to pick tool flags — e.g. de4dot forced `-p crx` for ConfuserEx vs
	// auto-detect for a generic match.
	Obfuscator string
	// StepFilter limits which steps a recipe executes in batch mode.
	// Empty string means run all steps. Recipes that support batching
	// check this field and skip steps that don't match.
	// Values: "strings", "ghidra", "" (all).
	StepFilter string
	// AllowDynamic opts into steps that execute target code (e.g. ConfuserEx
	// embedded-assembly extraction via in-process cctor + Harmony capture).
	// Off by default for safety.
	AllowDynamic bool
}

// Recipe is the interface that all decompilation recipes must implement.
type Recipe interface {
	Name() string
	Description() string
	Match(r *recon.Result) bool
	Steps() []StepInfo
	RequiredTools() []string
	Execute(ctx *Context) error
}
