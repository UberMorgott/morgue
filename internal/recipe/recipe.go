package recipe

import (
	"context"
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

// StepInfo describes a recipe step.
type StepInfo struct {
	Name     string
	Required bool
}

// StepProgress reports the progress of a single step.
type StepProgress struct {
	Step     int
	Total    int
	Name     string
	Tool     string
	Status   StepStatus
	Duration time.Duration
	Error    error
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
