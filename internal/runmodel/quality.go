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
	DecompilerErrors     Metric         `json:"decompilerErrors"`
	UnknownResultTypes   Metric         `json:"unknownResultTypes"`
	UnresolvedReferences Metric         `json:"unresolvedReferences"`
	ResidualObfuscation  Metric         `json:"residualObfuscation"`
	StringDecryption     Coverage       `json:"stringDecryption"`
	Warnings             []string       `json:"warnings"`
	Checks               []QualityCheck `json:"checks"`
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
