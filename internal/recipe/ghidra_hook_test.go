package recipe

import (
	"context"
	"errors"
	"testing"
)

func TestGhidraHookNotImplemented(t *testing.T) {
	req := GhidraBodyRequest{
		GameAssembly: "D:/g/GameAssembly.dll",
		RVAMapPath:   "D:/out/dump/cs/il2cpp.cs",
		OutDir:       "D:/out/ghidra",
	}
	err := RunGhidraBodyHook(context.Background(), req)
	if !errors.Is(err, ErrGhidraHookNotImplemented) {
		t.Fatalf("expected ErrGhidraHookNotImplemented, got %v", err)
	}
}
