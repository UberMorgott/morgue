package recon

import (
	"encoding/json"
	"fmt"
)

// Kind represents the classification of a binary target.
type Kind int

const (
	Unknown      Kind = iota
	Managed           // .NET / Mono
	Native            // C/C++, Delphi, Go, Rust
	UnityMono         // Unity with Mono scripting backend
	UnityIL2CPP       // Unity with IL2CPP scripting backend
	UnrealEngine      // Unreal Engine 4/5
	Mixed             // Contains both managed and native components
)

var kindNames = [...]string{
	"Unknown",
	"Managed",
	"Native",
	"UnityMono",
	"UnityIL2CPP",
	"UnrealEngine",
	"Mixed",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

func (k Kind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

func (k *Kind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for i, name := range kindNames {
		if name == s {
			*k = Kind(i)
			return nil
		}
	}
	return fmt.Errorf("unknown Kind: %q", s)
}

// Result holds the output of binary reconnaissance.
type Result struct {
	Path               string   `json:"path"`
	Size               int64    `json:"size"`
	SHA256             string   `json:"sha256"`
	Kind               Kind     `json:"kind"`
	Runtime            string   `json:"runtime,omitempty"`
	Compiler           string   `json:"compiler,omitempty"`
	Obfuscator         string   `json:"obfuscator,omitempty"`
	ObfuscatorFeatures []string `json:"obfuscator_features,omitempty"`
	Packed             bool     `json:"packed,omitempty"`
	EmbeddedSuspected  bool     `json:"embedded_suspected,omitempty"`
	EmbeddedSignals    []string `json:"embedded_signals,omitempty"`
	EmbeddedParts      []string `json:"embedded_parts,omitempty"`
	Fallback           bool     `json:"fallback,omitempty"`
}

// NeedsDeobfuscation returns true if the binary appears to be obfuscated or packed.
func (r Result) NeedsDeobfuscation() bool {
	return r.Obfuscator != "" || r.Packed
}
