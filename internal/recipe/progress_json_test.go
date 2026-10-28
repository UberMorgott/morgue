package recipe

import (
	"encoding/json"
	"errors"
	"testing"
)

// StepProgress crosses the Wails/SSE wire as JSON: Status as its name and
// Error as its message text, never as an empty "{}" object.
func TestStepProgressJSON(t *testing.T) {
	b, err := json.Marshal(StepProgress{Step: 1, Total: 3, Name: "decompile", Status: Failed, Error: errors.New("boom")})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["Status"] != "Failed" || got["Error"] != "boom" || got["Name"] != "decompile" || got["Step"] != float64(1) {
		t.Fatalf("StepProgress JSON = %s", b)
	}

	b, err = json.Marshal(StepProgress{Status: Running})
	if err != nil {
		t.Fatal(err)
	}
	var running map[string]any
	if err := json.Unmarshal(b, &running); err != nil {
		t.Fatal(err)
	}
	if _, ok := running["Error"]; ok {
		t.Fatalf("nil Error must be omitted: %s", b)
	}
}
