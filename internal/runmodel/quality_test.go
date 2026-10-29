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
