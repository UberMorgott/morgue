package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/util"
)

// Frozen M4 producer step IDs used by the Unity recipes.
const (
	unityStepIL2CPPStubs   = "il2cpp.stubs"
	unityStepExtract       = "unity.extract"
	unityStepManagedDecomp = "managed.decompile"
	unityProvNameMeta      = "metadata"
	unityProvNameOrig      = "original"
	unityProvConfHigh      = "high"
	unityProvConfUnkn      = "unknown"
	unityProvNameUnkn      = "unknown"
	unityProvKindStub      = runmodel.FidelityStub
	unityProvKindAsset     = "asset"
)

// unityWriteEvidence atomically writes a target-relative evidence file.
func unityWriteEvidence(outDir, rel string, v any) error {
	path := filepath.Join(outDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// toolVersion returns the run-frozen version of a tool, or "" when unknown.
func unityToolVersion(ctx *Context, name string) string {
	if ctx == nil || ctx.Tools == nil {
		return ""
	}
	return ctx.Tools.Check(name).Version
}

func unityStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func unityProvID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "p" + hex.EncodeToString(sum[:8])
}

func unityRelSlash(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// artifactProvenance writes one artifact-level row (no per-member identity).
func unityArtifactProv(ctx *Context, inputSHA, artifactRel, kind, module, tool, stepID string, args []string, detail string) error {
	if ctx.Provenance == nil {
		return nil
	}
	if args == nil {
		args = []string{}
	}
	nameSource, conf := unityProvNameUnkn, unityProvConfUnkn
	if kind == "original" {
		nameSource, conf = unityProvNameOrig, unityProvConfHigh
	}
	return ctx.Provenance.Write(runmodel.ProvenanceRecord{
		SchemaVersion: runmodel.ProvenanceVersion,
		ID:            unityProvID(inputSHA, module, artifactRel, kind),
		InputSHA256:   inputSHA,
		ArtifactPath:  artifactRel,
		ArtifactKind:  kind,
		Module:        module,
		NameSource:    nameSource,
		Confidence:    conf,
		Evidence:      []runmodel.Evidence{{Path: artifactRel, Detail: detail}},
		Producer:      runmodel.ProvenanceProducer{Tool: tool, Version: unityToolVersion(ctx, tool), StepID: stepID, Args: args},
	})
}

// emitIL2CPPProvenance streams provenance for the stub products: artifact rows
// for the dump and every DummyDLL, then one row per type/method stub carrying
// its metadata token and native RVA/VA, linked to the exact src file:line.
// Returns the number of member rows written.
func emitIL2CPPProvenance(ctx *Context, layout il2cppLayout, srcDir, inputSHA, dumper, dumpRel string) (int64, error) {
	if ctx.Provenance == nil {
		return 0, nil
	}
	if dumpRel != "" {
		if err := unityArtifactProv(ctx, inputSHA, dumpRel, unityProvKindStub, "GameAssembly.dll", dumper, unityStepIL2CPPStubs, nil,
			"merged C# declarations from metadata; bodies absent"); err != nil {
			return 0, err
		}
	}
	dlls, _ := filepath.Glob(filepath.Join(layout.DllDir, "*.dll"))
	sort.Strings(dlls)
	for _, dll := range dlls {
		if err := unityArtifactProv(ctx, inputSHA, unityRelSlash(ctx.Output, dll), unityProvKindStub, filepath.Base(dll), dumper, unityStepIL2CPPStubs, nil,
			"DummyDLL: metadata-only assembly with placeholder IL bodies"); err != nil {
			return 0, err
		}
	}
	dumperVer := unityToolVersion(ctx, dumper)
	ilspyVer := unityToolVersion(ctx, "ilspycmd")
	var rows int64
	_, err := quality.ScanIL2CPPStubs(ctx.Ctx, srcDir, ctx.Output, func(m quality.StubMember) error {
		if m.Kind != "method" && m.Kind != "type" {
			return nil
		}
		name := m.Name
		if m.Type != "" {
			name = m.Type + "::" + m.Name
		}
		if m.Package != "" {
			name = m.Package + "." + name
		}
		line := m.Line
		detail := fmt.Sprintf("%s stub; token/address from %s %s DummyDLL attributes", m.Kind, dumper, dumperVer)
		if m.Kind == "method" && m.RVA == "" {
			detail += "; no native body address (abstract/extern/generic-shared)"
		}
		rows++
		return ctx.Provenance.Write(runmodel.ProvenanceRecord{
			SchemaVersion: runmodel.ProvenanceVersion,
			ID:            unityProvID(inputSHA, m.Module, m.Token),
			InputSHA256:   inputSHA,
			ArtifactPath:  m.Path,
			ArtifactKind:  unityProvKindStub,
			Module:        m.Module,
			Token:         unityStrPtr(m.Token),
			RVA:           unityStrPtr(m.RVA),
			Address:       unityStrPtr(m.VA),
			OriginalName:  &name,
			NameSource:    unityProvNameMeta,
			Confidence:    unityProvConfHigh,
			Evidence:      []runmodel.Evidence{{Path: m.Path, Line: &line, Detail: detail}},
			Producer: runmodel.ProvenanceProducer{Tool: "ilspycmd", Version: ilspyVer, StepID: unityStepIL2CPPStubs,
				Args: []string{"-o", "src/" + strings.TrimSuffix(m.Module, ".dll")}},
		})
	})
	return rows, err
}

// ilspyStderr renders bounded diagnostics for the raw decompiler log.
func ilspyStderr(res *util.CmdResult, err error) string {
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
		b.WriteByte('\n')
	}
	if res != nil {
		s := strings.TrimSpace(res.Stderr)
		if len(s) > 16*1024 {
			s = s[:16*1024] + "\n[truncated]"
		}
		b.WriteString(s)
	}
	return b.String()
}

var ilspyPartialMarker = regexp.MustCompile(`(\d+) error\(s\) occurred; the affected code was replaced by the error text`)

// ilspyPartialErrors reports whether a failed ilspycmd project run still wrote
// its project with only some members replaced by error text, and how many.
func ilspyPartialErrors(res *util.CmdResult, runErr error, outDir string) (int64, bool) {
	if runErr != nil || res == nil || res.ExitCode == 0 {
		return 0, false
	}
	m := ilspyPartialMarker.FindStringSubmatch(res.Stderr + "\n" + res.Stdout)
	if m == nil || !hasNonEmptySource(outDir) {
		return 0, false
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n, true
}

// hasNonEmptySource reports whether dir holds at least one non-empty .cs file.
func hasNonEmptySource(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cs") {
			return nil //nolint:nilerr // unreadable entries do not count as output
		}
		if info, ierr := d.Info(); ierr == nil && info.Size() > 0 {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
