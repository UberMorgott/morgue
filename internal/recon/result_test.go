package recon

import (
	"encoding/json"
	"testing"
)

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{Unknown, "Unknown"},
		{Managed, "Managed"},
		{Native, "Native"},
		{UnityMono, "UnityMono"},
		{UnityIL2CPP, "UnityIL2CPP"},
		{Mixed, "Mixed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestKindJSON(t *testing.T) {
	for _, k := range []Kind{Unknown, Managed, Native, UnityMono, UnityIL2CPP, Mixed} {
		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("MarshalJSON(%v) error: %v", k, err)
		}

		var got Kind
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("UnmarshalJSON(%s) error: %v", data, err)
		}

		if got != k {
			t.Errorf("JSON roundtrip: got %v, want %v", got, k)
		}
	}
}

func TestResultJSONRoundtrip(t *testing.T) {
	r := Result{
		Path:       "test.dll",
		Size:       1024,
		SHA256:     "abc123",
		Kind:       Managed,
		Runtime:    ".NET 6.0",
		Compiler:   "C#",
		Obfuscator: "ConfuserEx",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Result
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Path != r.Path || got.Kind != r.Kind || got.Obfuscator != r.Obfuscator {
		t.Errorf("Roundtrip mismatch: got %+v", got)
	}
}

func TestNeedsDeobfuscation(t *testing.T) {
	tests := []struct {
		name string
		r    Result
		want bool
	}{
		{"with obfuscator", Result{Obfuscator: "ConfuserEx"}, true},
		{"without obfuscator", Result{Obfuscator: ""}, false},
		{"packed", Result{Packed: true}, true},
		{"clean", Result{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.NeedsDeobfuscation(); got != tt.want {
				t.Errorf("NeedsDeobfuscation() = %v, want %v", got, tt.want)
			}
		})
	}
}
