package tools

import (
	"errors"
	"testing"
)

// TestIsBenignCertNoise verifies Issue 4: the Windows cert-store
// "loadSystemRoots / ERROR_ALREADY_EXISTS" line is classified as benign noise,
// while a genuine network/TLS timeout (even one wrapped in a loadSystemRoots
// line) is NOT benign.
func TestIsBenignCertNoise(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"already exists code", errors.New("failed to loadSystemRoots: exit status 0x800700b7"), true},
		{"already exists text", errors.New("crypto/x509: certificate already exists in store"), true},
		{"bare loadSystemRoots", errors.New("failed to loadSystemRoots"), true},
		{"real timeout wrapped in cert line", errors.New("failed to loadSystemRoots: exit status 0x80072ee2"), false},
		{"plain timeout", errors.New("dial tcp: i/o timeout — timed out"), false},
		{"deadline exceeded", errors.New("context deadline exceeded"), false},
		{"unrelated failure", errors.New("404 not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBenignCertNoise(tt.err); got != tt.want {
				t.Errorf("isBenignCertNoise(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
