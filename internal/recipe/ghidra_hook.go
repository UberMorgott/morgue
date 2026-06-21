package recipe

import (
	"context"
	"errors"
)

// ErrGhidraHookNotImplemented marks the native-body Ghidra path as a stub.
//
// The IL2CPP dump yields class/field/enum signatures + per-method RVA offsets
// but NOT method bodies. Recovering bodies means feeding GameAssembly.dll plus
// the dumped RVA map into Ghidra (morgue already integrates Ghidra elsewhere).
// This is intentionally NOT built yet — see
// docs/specs/2026-06-21-il2cpp-modern-unity-pipeline-design.md §5.9. The typed
// entry point exists so a caller can gate it behind an opt-in flag without
// crashing the pipeline.
var ErrGhidraHookNotImplemented = errors.New("ghidra native-body hook not implemented (stub)")

// GhidraBodyRequest describes the inputs a future Ghidra body-recovery pass would
// need: the native image, the dumped RVA/signature map, and an output dir.
type GhidraBodyRequest struct {
	GameAssembly string // path to GameAssembly.dll
	RVAMapPath   string // path to the dumped C# (carries RVA offsets per method)
	OutDir       string // where Ghidra output would be written
}

// RunGhidraBodyHook is the documented entry point for the (future) native-body
// recovery path. It currently returns ErrGhidraHookNotImplemented so a caller can
// gate it behind an opt-in flag without crashing the pipeline.
func RunGhidraBodyHook(ctx context.Context, req GhidraBodyRequest) error {
	_ = ctx
	_ = req
	return ErrGhidraHookNotImplemented
}
