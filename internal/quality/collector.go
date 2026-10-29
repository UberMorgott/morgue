// Package quality collects recipe evidence without depending on execution state.
package quality

import (
	"context"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

type Input struct {
	Target runmodel.TargetRecord
	Output string
	// Events contains bounded terminal step diagnostics, never a full journal.
	Events []runmodel.Event
}
type Collector interface {
	Collect(context.Context, Input) (runmodel.QualityReport, error)
}

// Register is used at package initialization by disjoint family implementations.
func Register(family string, collector Collector) { collectors[family] = collector }

var collectors = map[string]Collector{}

// ForFamily returns nil when no collector is registered; callers must expose
// missing evidence explicitly rather than interpreting it as passing quality.
func ForFamily(family string) Collector { return collectors[family] }
