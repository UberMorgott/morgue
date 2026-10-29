// Package quality collects recipe evidence without depending on execution state.
package quality

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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

// QualityFile is the per-target report the engine writes next to the output.
const QualityFile = "quality.json"

// recipeFamilies maps every registered recipe to its collector families. The
// first family measures code and owns the report's code metrics; later
// families (unity data export) are merged in as clearly named sections.
var recipeFamilies = map[string][]string{
	"dotnet-generic":    {"managed"},
	"dotnet-confuserex": {"managed"},
	"unity-mono":        {"managed", "unitydata"},
	"unity-il2cpp":      {"il2cpp", "unitydata"},
	"native":            {"native"},
	"delphi":            {"delphi"},
	"ue5":               {"ue5"},
	"java":              {"java"},
	"nsis":              {"container"},
}

// FamiliesForRecipe returns the collector families of a recipe (nil if the
// recipe has none).
func FamiliesForRecipe(recipe string) []string {
	return append([]string(nil), recipeFamilies[recipe]...)
}

// CollectTarget measures one finished target. It never fails: a missing
// collector, a collector error or an invalid report becomes an explicit
// unknown report with a warning, never a reassuring pass or zero.
func CollectTarget(ctx context.Context, recipe string, in Input) runmodel.QualityReport {
	families := recipeFamilies[recipe]
	if len(families) == 0 {
		return UnknownReport("unknown", fmt.Sprintf("no quality collector is registered for recipe %q", recipe))
	}
	if recipe == "unity-il2cpp" && strings.EqualFold(filepath.Base(in.Target.InputPath), "global-metadata.dat") {
		return NotApplicableReport("il2cpp", "companion global-metadata.dat: it is consumed by the GameAssembly target, whose report measures IL2CPP quality; this target produces no output of its own")
	}
	var merged runmodel.QualityReport
	for i, family := range families {
		r := collectFamily(ctx, family, in)
		if i == 0 {
			merged = r
			continue
		}
		merged = mergeSection(merged, r)
	}
	return merged
}

func collectFamily(ctx context.Context, family string, in Input) runmodel.QualityReport {
	c := ForFamily(family)
	if c == nil {
		return UnknownReport(family, "no collector registered for family "+family)
	}
	r, err := c.Collect(ctx, in)
	if err != nil {
		if verr := r.Validate(); verr != nil {
			r = UnknownReport(family, "collector failed: "+err.Error())
		} else {
			r.Warnings = append(r.Warnings, "collector error (report may be incomplete): "+err.Error())
			if r.Status == "pass" {
				r.Status = "unknown"
			}
		}
	}
	if err := r.Validate(); err != nil {
		r = UnknownReport(family, "collector produced an invalid report: "+err.Error())
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	if r.Checks == nil {
		r.Checks = []runmodel.QualityCheck{}
	}
	return r
}

// mergeSection folds a secondary family into the code report: its checks keep
// their stable IDs under a "<family>/" prefix, its warnings are prefixed, and
// the combined status is the worst applicable one. Code metrics stay the
// primary family's; the secondary family's metrics are data metrics.
func mergeSection(primary, section runmodel.QualityReport) runmodel.QualityReport {
	out := primary
	out.Family = primary.Family + "+" + section.Family
	out.Warnings = append(append([]string{}, primary.Warnings...), prefixAll(section.Family+": ", section.Warnings)...)
	out.Checks = append([]runmodel.QualityCheck{}, primary.Checks...)
	for _, c := range section.Checks {
		c.ID = section.Family + "/" + c.ID
		out.Checks = append(out.Checks, c)
	}
	out.Status = worstStatus(primary.Status, section.Status)
	return out
}

func prefixAll(p string, in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, p+s)
	}
	return out
}

// worstStatus: degraded > unknown > pass > not-applicable.
func worstStatus(a, b string) string {
	rank := map[string]int{"not-applicable": 0, "pass": 1, "unknown": 2, "degraded": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func blockMetric(status, basis string) runmodel.Metric {
	return runmodel.Metric{Status: status, Basis: basis, Evidence: []runmodel.Evidence{}}
}

func blockReport(family, status, metricStatus, basis string) runmodel.QualityReport {
	m := blockMetric(metricStatus, basis)
	c := runmodel.Coverage{Found: m, Emitted: m}
	names := runmodel.NamesUnknown(basis)
	if metricStatus == "not-applicable" {
		names = runmodel.NamesNotApplicable(basis)
	}
	return runmodel.QualityReport{
		SchemaVersion: runmodel.QualityVersion, Family: family, Status: status,
		Functions: c, Types: c, Names: names, DecompilerErrors: m, UnknownResultTypes: m,
		UnresolvedReferences: m, ResidualObfuscation: m, StringDecryption: c,
		Warnings: []string{},
		Checks:   []runmodel.QualityCheck{{ID: "output.present", Status: metricStatus, Message: basis, Evidence: []runmodel.Evidence{}}},
	}
}

// UnknownReport is the explicit "evidence missing" report.
func UnknownReport(family, basis string) runmodel.QualityReport {
	r := blockReport(family, "unknown", "unknown", basis)
	r.Warnings = []string{basis}
	return r
}

// NotApplicableReport is a report for targets whose quality is measured
// elsewhere (companion inputs) or that carry no code of their own.
func NotApplicableReport(family, basis string) runmodel.QualityReport {
	return blockReport(family, "not-applicable", "not-applicable", basis)
}
