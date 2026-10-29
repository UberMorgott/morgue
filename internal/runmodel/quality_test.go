package runmodel

import "testing"

func TestCoverageValidation(t *testing.T) {
	n, e, ratio := int64(10), int64(8), .8
	c := Coverage{Found: Metric{Value: &n, Status: "measured"}, Emitted: Metric{Value: &e, Status: "measured"}, Ratio: &ratio, Comparable: true}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Emitted.Status = "estimated"
	if c.Validate() == nil {
		t.Fatal("estimated declarations must not produce a coverage ratio")
	}
	c.Emitted.Status = "measured"
	n = 0
	if c.Validate() == nil {
		t.Fatal("zero denominator must not produce a ratio")
	}
}

func TestNameQualityValidation(t *testing.T) {
	total, auto, inferred, ratio := int64(10), int64(6), int64(2), .6
	q := NameQuality{Total: Metric{Value: &total, Status: "measured"}, AutoNamed: Metric{Value: &auto, Status: "measured"}, AutoRatio: &ratio, Inferred: Metric{Value: &inferred, Status: "measured"}}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := .5
	q.AutoRatio = &bad
	if q.Validate() == nil {
		t.Fatal("ratio disagreeing with counts accepted")
	}
	if NamesNotApplicable("metadata names").Validate() != nil || NamesUnknown("no evidence").Validate() != nil {
		t.Fatal("helper blocks must validate")
	}
	if (NameQuality{}).Validate() == nil {
		t.Fatal("zero-value name block must not validate")
	}
}
