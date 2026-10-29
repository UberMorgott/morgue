package runmodel

import (
	"fmt"
	"math"
)

const QualityVersion = 1

type Evidence struct {
	Path   string `json:"path"`
	Line   *int64 `json:"line"`
	Detail string `json:"detail"`
}
type Metric struct {
	Value    *int64     `json:"value"`
	Status   string     `json:"status"`
	Basis    string     `json:"basis"`
	Evidence []Evidence `json:"evidence"`
}
type Coverage struct {
	Found      Metric   `json:"found"`
	Emitted    Metric   `json:"emitted"`
	Ratio      *float64 `json:"ratio"`
	Comparable bool     `json:"comparable"`
}
type QualityCheck struct {
	ID       string     `json:"id"`
	Status   string     `json:"status"`
	Message  string     `json:"message"`
	Evidence []Evidence `json:"evidence"`
}
type QualityReport struct {
	SchemaVersion        int            `json:"schemaVersion"`
	Family               string         `json:"family"`
	Status               string         `json:"status"`
	Functions            Coverage       `json:"functions"`
	Types                Coverage       `json:"types"`
	Names                NameQuality    `json:"names"`
	DecompilerErrors     Metric         `json:"decompilerErrors"`
	UnknownResultTypes   Metric         `json:"unknownResultTypes"`
	UnresolvedReferences Metric         `json:"unresolvedReferences"`
	ResidualObfuscation  Metric         `json:"residualObfuscation"`
	StringDecryption     Coverage       `json:"stringDecryption"`
	Warnings             []string       `json:"warnings"`
	Checks               []QualityCheck `json:"checks"`
}

// NameQuality measures how many recovered symbol names are tool-generated
// defaults (e.g. Ghidra FUN_<addr>) rather than original, metadata or
// inferred names. AutoRatio is the default-name share of Total; it is null
// unless both counts are measured over the same identity universe and
// Total > 0. Inferred counts default names replaced by Morgue's inference
// (still inference, not recovered originals).
type NameQuality struct {
	Total     Metric   `json:"total"`
	AutoNamed Metric   `json:"autoNamed"`
	AutoRatio *float64 `json:"autoRatio"`
	Inferred  Metric   `json:"inferred"`
}

// NamesNotApplicable is the name-quality block for families whose names come
// from metadata (managed/JVM/IL2CPP) or that emit no symbol names.
func NamesNotApplicable(basis string) NameQuality {
	m := Metric{Status: "not-applicable", Basis: basis, Evidence: []Evidence{}}
	return NameQuality{Total: m, AutoNamed: m, Inferred: m}
}

// NamesUnknown is the name-quality block when evidence is missing.
func NamesUnknown(basis string) NameQuality {
	m := Metric{Status: "unknown", Basis: basis, Evidence: []Evidence{}}
	return NameQuality{Total: m, AutoNamed: m, Inferred: m}
}

// Validate rejects absent metrics and fabricated default-name ratios.
func (n NameQuality) Validate() error {
	for _, m := range []Metric{n.Total, n.AutoNamed, n.Inferred} {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	if n.AutoRatio != nil {
		if n.Total.Status != "measured" || n.AutoNamed.Status != "measured" || *n.Total.Value == 0 || *n.AutoNamed.Value > *n.Total.Value {
			return fmt.Errorf("name ratio requires measured counts with total > 0")
		}
		if math.IsNaN(*n.AutoRatio) || math.Abs(*n.AutoRatio-float64(*n.AutoNamed.Value)/float64(*n.Total.Value)) > 1e-9 {
			return fmt.Errorf("name ratio disagrees with counts")
		}
	}
	return nil
}

// Validate checks every metric and coverage block of the report, so a
// collector cannot serialize an absent count as a reassuring zero.
func (r QualityReport) Validate() error {
	if r.SchemaVersion != QualityVersion {
		return fmt.Errorf("quality schemaVersion %d, want %d", r.SchemaVersion, QualityVersion)
	}
	switch r.Status {
	case "pass", "degraded", "unknown", "not-applicable":
	default:
		return fmt.Errorf("invalid quality status %q", r.Status)
	}
	for name, c := range map[string]Coverage{"functions": r.Functions, "types": r.Types, "stringDecryption": r.StringDecryption} {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name, m := range map[string]Metric{"decompilerErrors": r.DecompilerErrors, "unknownResultTypes": r.UnknownResultTypes,
		"unresolvedReferences": r.UnresolvedReferences, "residualObfuscation": r.ResidualObfuscation} {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := r.Names.Validate(); err != nil {
		return fmt.Errorf("names: %w", err)
	}
	return nil
}

func (m Metric) Validate() error {
	switch m.Status {
	case "measured", "estimated":
		if m.Value == nil || *m.Value < 0 {
			return fmt.Errorf("%s metric requires nonnegative value", m.Status)
		}
	case "unknown", "not-applicable":
		if m.Value != nil || m.Basis == "" {
			return fmt.Errorf("%s metric requires null value and basis", m.Status)
		}
	default:
		return fmt.Errorf("invalid metric status %q", m.Status)
	}
	return nil
}
func (c Coverage) Validate() error {
	if err := c.Found.Validate(); err != nil {
		return err
	}
	if err := c.Emitted.Validate(); err != nil {
		return err
	}
	if c.Ratio != nil {
		if !c.Comparable || c.Found.Status != "measured" || c.Emitted.Status != "measured" || *c.Found.Value == 0 || *c.Emitted.Value > *c.Found.Value {
			return fmt.Errorf("coverage ratio requires comparable measured identity counts")
		}
		if math.IsNaN(*c.Ratio) || math.Abs(*c.Ratio-float64(*c.Emitted.Value)/float64(*c.Found.Value)) > 1e-9 {
			return fmt.Errorf("coverage ratio disagrees with counts")
		}
	}
	return nil
}
