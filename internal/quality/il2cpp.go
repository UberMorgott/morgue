package quality

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// IL2CPPEvidenceFile is the target-relative file the unity-il2cpp recipe writes
// with facts only it observes (dumper attempts, per-assembly decompile results).
const IL2CPPEvidenceFile = "evidence/il2cpp.json"

// IL2CPPEvidence is the recipe-observed part of IL2CPP quality. Counts that the
// collector can measure from output files are deliberately not duplicated here.
type IL2CPPEvidence struct {
	SchemaVersion        int              `json:"schemaVersion"`
	Binary               string           `json:"binary"`
	Metadata             string           `json:"metadata"`
	MetadataVersion      *int64           `json:"metadataVersion"`
	MetadataVersionError string           `json:"metadataVersionError"`
	Dumper               string           `json:"dumper"`
	DumperVersion        string           `json:"dumperVersion"`
	Attempts             []DumperAttempt  `json:"attempts"`
	DumpCS               string           `json:"dumpCs"`
	Decompiler           string           `json:"decompiler"`
	DecompilerVersion    string           `json:"decompilerVersion"`
	Assemblies           []AssemblyResult `json:"assemblies"`
}

// DumperAttempt records one dumper invocation, successful or not.
type DumperAttempt struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Error   string `json:"error"`
}

// AssemblyResult is the decompile outcome of one DummyDLL.
type AssemblyResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // decompiled | partial | failed | system-skipped
	Mode   string `json:"mode"`   // project | flat
	Error  string `json:"error"`
	// Errors counts members ILSpy replaced by error text in a partial result.
	Errors int64 `json:"errors"`
}

// StubMember is one token-carrying declaration in DummyDLL-decompiled C#.
type StubMember struct {
	Module  string // DummyDLL file name, e.g. "LC2.Core.dll"
	Path    string // slash path relative to the scanned root's parent target
	Line    int64
	Token   string // normalized 0x%08x
	RVA     string // "" when the stub carries no native address
	VA      string
	Kind    string // type | method | field | property | event | other
	Name    string // declaration text as emitted
	Type    string // enclosing type name ("" for top-level types)
	Package string
}

// StubStats are measured over the decompiled stub tree, keyed by module+token.
type StubStats struct {
	Files                 int64
	Modules               int64
	Types                 int64
	Methods               int64
	MethodsWithAddress    int64
	MethodsWithoutAddress int64
	StubMarkers           int64 // files referencing the dumper's attribute namespace
	TypesByModule         map[string]int64
}

var (
	il2cppTokenAttr   = regexp.MustCompile(`^\[Token\(Token = "0x([0-9A-Fa-f]{1,8})"\)\]`)
	il2cppAddressAttr = regexp.MustCompile(`^\[Address\(RVA = "(0x[0-9A-Fa-f]+)"(?:, Offset = "0x[0-9A-Fa-f]+")?(?:, VA = "(0x[0-9A-Fa-f]+)")?`)
	il2cppTypeKeyword = regexp.MustCompile(`\b(?:class|struct|interface|enum|record|delegate)\s+(?:[\w.<>,\[\]]+\s+)*?([\pL_@][\w@]*)`)
	il2cppImageHeader = regexp.MustCompile(`^// Image (\d+): (.+?) - (?:Assembly: .*? - Types (\d+)-(\d+)|(\d+))\s*$`)
	il2cppTypeDefIdx  = regexp.MustCompile(`// TypeDefIndex: (\d+)`)
)

func il2cppHex(s string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("0x%x", v)
}

// ScanIL2CPPStubs streams every .cs file under srcRoot (one directory per
// DummyDLL) and reports each token-carrying declaration to visit. Nothing is
// accumulated per member, so arbitrarily large dumps stay bounded in memory.
// relBase is the directory member paths are made relative to.
func ScanIL2CPPStubs(ctx context.Context, srcRoot, relBase string, visit func(StubMember) error) (StubStats, error) {
	stats := StubStats{TypesByModule: map[string]int64{}}
	modules, err := os.ReadDir(srcRoot)
	if err != nil {
		return stats, err
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name() < modules[j].Name() })
	for _, m := range modules {
		if !m.IsDir() {
			continue
		}
		stats.Modules++
		module := m.Name() + ".dll"
		seen := map[string]bool{}
		err := filepath.WalkDir(filepath.Join(srcRoot, m.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cs") {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			stats.Files++
			rel, _ := filepath.Rel(relBase, path)
			return il2cppScanFile(path, filepath.ToSlash(rel), module, seen, &stats, visit)
		})
		if err != nil {
			return stats, err
		}
	}
	return stats, nil
}

type il2cppScope struct {
	name  string
	depth int
}

func il2cppScanFile(path, rel, module string, seen map[string]bool, stats *StubStats, visit func(StubMember) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var (
		line           int64
		token, rva, va string
		depth          int
		scopes         []il2cppScope
		pendingType    string
		pkg            string
		marker         bool
	)
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		switch {
		case text == "":
			continue
		case strings.HasPrefix(text, "using Il2CppInspector.DLL") || strings.HasPrefix(text, "using Il2CppDummyDll"):
			marker = true
			continue
		case strings.HasPrefix(text, "namespace "):
			pkg = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(text, "namespace ")), ";")
			continue
		case text == "{":
			depth++
			if pendingType != "" {
				scopes = append(scopes, il2cppScope{name: pendingType, depth: depth})
				pendingType = ""
			}
			continue
		case text == "}" || text == "};":
			depth--
			for len(scopes) > 0 && scopes[len(scopes)-1].depth > depth {
				scopes = scopes[:len(scopes)-1]
			}
			continue
		}
		pendingType = ""
		if m := il2cppTokenAttr.FindStringSubmatch(text); m != nil {
			token = il2cppHex(m[1])
			continue
		}
		if m := il2cppAddressAttr.FindStringSubmatch(text); m != nil {
			rva, va = il2cppHex(m[1]), il2cppHex(m[2])
			continue
		}
		if strings.HasPrefix(text, "[") {
			continue // other attributes keep the pending token
		}
		if token == "" {
			continue
		}
		member := StubMember{Module: module, Path: rel, Line: line, Token: token, RVA: rva, VA: va, Name: strings.TrimSuffix(text, "{"), Package: pkg}
		if len(scopes) > 0 {
			member.Type = scopes[len(scopes)-1].name
		}
		tv, _ := strconv.ParseUint(strings.TrimPrefix(token, "0x"), 16, 32)
		switch tv >> 24 {
		case 0x02:
			member.Kind = "type"
			if m := il2cppTypeKeyword.FindStringSubmatch(text); m != nil {
				pendingType = m[1]
			}
		case 0x06:
			member.Kind = "method"
		case 0x04:
			member.Kind = "field"
		case 0x17:
			member.Kind = "property"
		case 0x14:
			member.Kind = "event"
		default:
			member.Kind = "other"
		}
		key := token
		if !seen[key] {
			seen[key] = true
			switch member.Kind {
			case "type":
				stats.Types++
				stats.TypesByModule[module]++
			case "method":
				stats.Methods++
				if rva != "" {
					stats.MethodsWithAddress++
				} else {
					stats.MethodsWithoutAddress++
				}
			}
		}
		token, rva, va = "", "", ""
		if visit != nil {
			if err := visit(member); err != nil {
				return err
			}
		}
	}
	if marker {
		stats.StubMarkers++
	}
	return sc.Err()
}

// DumpImage is one metadata image listed in the dumper's C# header.
type DumpImage struct {
	Index     int64
	Name      string
	TypeStart int64
	TypeCount int64 // -1 when the format does not state it
}

// DumpHeader summarizes the dumper's merged C# file.
type DumpHeader struct {
	Format       string // il2cppinspector | il2cppdumper | unknown
	Images       []DumpImage
	TypeDefCount int64 // unique TypeDefIndex declarations
}

// ParseIL2CPPDump reads image headers and TypeDefIndex declarations from
// il2cpp.cs (InspectorRedux) or dump.cs (Il2CppDumper). Unrecognized text
// yields Format "unknown" so callers report unknown, never a measured zero.
func ParseIL2CPPDump(ctx context.Context, path string) (DumpHeader, error) {
	h := DumpHeader{Format: "unknown"}
	f, err := os.Open(path)
	if err != nil {
		return h, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	seen := map[int64]bool{}
	maxIdx := int64(-1)
	var n int
	for sc.Scan() {
		if n++; n%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return h, err
			}
		}
		text := sc.Text()
		if m := il2cppImageHeader.FindStringSubmatch(text); m != nil {
			idx, _ := strconv.ParseInt(m[1], 10, 64)
			img := DumpImage{Index: idx, Name: m[2], TypeCount: -1}
			if m[3] != "" {
				h.Format = "il2cppinspector"
				img.TypeStart, _ = strconv.ParseInt(m[3], 10, 64)
				end, _ := strconv.ParseInt(m[4], 10, 64)
				img.TypeCount = end - img.TypeStart + 1
			} else {
				h.Format = "il2cppdumper"
				img.TypeStart, _ = strconv.ParseInt(m[5], 10, 64)
			}
			h.Images = append(h.Images, img)
			continue
		}
		if m := il2cppTypeDefIdx.FindStringSubmatch(text); m != nil {
			v, _ := strconv.ParseInt(m[1], 10, 64)
			if !seen[v] {
				seen[v] = true
				h.TypeDefCount++
			}
			if v > maxIdx {
				maxIdx = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		return h, err
	}
	if h.Format == "il2cppdumper" && len(h.Images) > 0 {
		// Il2CppDumper states only each image's first TypeDefIndex; counts follow
		// from the next image's start (and the last declared index for the tail).
		imgs := append([]DumpImage(nil), h.Images...)
		sort.Slice(imgs, func(i, j int) bool { return imgs[i].TypeStart < imgs[j].TypeStart })
		counts := map[int64]int64{}
		for i, img := range imgs {
			end := maxIdx + 1
			if i+1 < len(imgs) {
				end = imgs[i+1].TypeStart
			}
			if end >= img.TypeStart && maxIdx >= 0 {
				counts[img.Index] = end - img.TypeStart
			}
		}
		for i := range h.Images {
			if c, ok := counts[h.Images[i].Index]; ok {
				h.Images[i].TypeCount = c
			}
		}
	}
	return h, nil
}

// il2cppHelperAssemblies are attribute-carrier DLLs the dumpers add themselves;
// they correspond to no metadata image.
func il2cppHelperAssembly(name string) bool {
	switch strings.ToLower(name) {
	case "il2cppinspector.dll", "il2cppdummydll.dll":
		return true
	}
	return false
}

func init() { Register("il2cpp", IL2CPP{}) }

// IL2CPP collects quality for unity-il2cpp targets. Its C# is metadata-derived
// stub declarations: no native method body is ever counted as recovered.
type IL2CPP struct{}

func unityMetric(status string, value *int64, basis string, ev ...runmodel.Evidence) runmodel.Metric {
	if ev == nil {
		ev = []runmodel.Evidence{}
	}
	return runmodel.Metric{Value: value, Status: status, Basis: basis, Evidence: ev}
}

func unityMeasured(n int64, basis string, ev ...runmodel.Evidence) runmodel.Metric {
	return unityMetric("measured", new(n), basis, ev...)
}
func unityUnknown(basis string, ev ...runmodel.Evidence) runmodel.Metric {
	return unityMetric("unknown", nil, basis, ev...)
}
func unityNA(basis string) runmodel.Metric { return unityMetric("not-applicable", nil, basis) }

func unityNACoverage(basis string) runmodel.Coverage {
	return runmodel.Coverage{Found: unityNA(basis), Emitted: unityNA(basis)}
}

func unityEv(path, detail string) runmodel.Evidence {
	return runmodel.Evidence{Path: path, Detail: detail}
}

func unityCheck(id, status, msg string, ev ...runmodel.Evidence) runmodel.QualityCheck {
	if ev == nil {
		ev = []runmodel.Evidence{}
	}
	return runmodel.QualityCheck{ID: id, Status: status, Message: msg, Evidence: ev}
}

// unityStatus derives the report status from mandatory checks only: any
// warn/fail => degraded, else any unknown => unknown, else pass.
func unityStatus(checks []runmodel.QualityCheck, mandatory map[string]bool) string {
	status := "pass"
	for _, c := range checks {
		if !mandatory[c.ID] {
			continue
		}
		switch c.Status {
		case "warn", "fail":
			return "degraded"
		case "unknown":
			status = "unknown"
		}
	}
	return status
}

// unityTargetDir resolves the absolute per-target output directory.
func unityTargetDir(in Input, evidenceFile string) string {
	if in.Target.Output != "" && !filepath.IsAbs(in.Target.Output) {
		joined := filepath.Join(in.Output, filepath.FromSlash(in.Target.Output))
		if _, err := os.Stat(filepath.Join(joined, filepath.FromSlash(evidenceFile))); err == nil {
			return joined
		}
	}
	return in.Output
}

func unityReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func unityFileSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, rerr := f.Read(buf)
		h.Write(buf[:n])
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func unityInputHash(in Input, role string) (string, bool) {
	for _, f := range in.Target.InputSet {
		if f.Role == role && f.HashStatus == "complete" && f.SHA256 != nil {
			return *f.SHA256, true
		}
	}
	if role == "primary" && in.Target.SHA256 != nil {
		return *in.Target.SHA256, true
	}
	return "", false
}

var il2cppMandatory = map[string]bool{
	"output.present": true, "artifact.fidelity": true, "functions.coverage": true,
	"decompiler.errors": true, "provenance.complete": true,
}

// Collect never fails on missing or drifted evidence: it degrades to unknown.
func (IL2CPP) Collect(ctx context.Context, in Input) (runmodel.QualityReport, error) {
	dir := unityTargetDir(in, IL2CPPEvidenceFile)
	r := runmodel.QualityReport{
		SchemaVersion:        runmodel.QualityVersion,
		Family:               "il2cpp",
		Names:                runmodel.NamesNotApplicable("IL2CPP type/method names come from global-metadata.dat (original metadata names, not tool defaults)"),
		UnknownResultTypes:   unityNA("stub assemblies carry no method bodies whose result types could be inferred"),
		UnresolvedReferences: unityUnknown("not collected for IL2CPP stub decompilation"),
		ResidualObfuscation:  unityUnknown("no IL2CPP metadata/binary obfuscation detector runs in this recipe"),
		StringDecryption:     unityNACoverage("IL2CPP pipeline performs no string decryption"),
		Warnings:             []string{},
		Checks:               []runmodel.QualityCheck{},
	}
	warn := func(s string) { r.Warnings = append(r.Warnings, s) }

	var ev IL2CPPEvidence
	evErr := unityReadJSON(filepath.Join(dir, filepath.FromSlash(IL2CPPEvidenceFile)), &ev)
	if evErr != nil {
		warn("recipe evidence unavailable: " + evErr.Error())
	}

	// Assembly accounting: metadata images vs DummyDLLs vs decompiled modules.
	var header DumpHeader
	headerErr := errors.New("dump C# not recorded")
	dumpRel := ev.DumpCS
	if dumpRel == "" {
		for _, c := range []string{"dump/cs/il2cpp.cs", "dump/cs/dump.cs"} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(c))); err == nil {
				dumpRel = c
				break
			}
		}
	}
	if dumpRel != "" {
		header, headerErr = ParseIL2CPPDump(ctx, filepath.Join(dir, filepath.FromSlash(dumpRel)))
		if headerErr == nil && header.Format == "unknown" {
			headerErr = errors.New("unrecognized dumper C# format (parser drift)")
		}
	}
	if headerErr != nil {
		warn("dump C# header unavailable: " + headerErr.Error())
	}
	dummies, emptyDummies := il2cppDummyDLLs(filepath.Join(dir, "dump", "dll"))

	srcDir := filepath.Join(dir, "src")
	stats, scanErr := ScanIL2CPPStubs(ctx, srcDir, dir, nil)
	if scanErr != nil && ctx.Err() != nil {
		return r, ctx.Err()
	}

	// output.present
	switch {
	case len(dummies) == 0:
		r.Checks = append(r.Checks, unityCheck("output.present", "fail", "no DummyDLL assemblies in dump/dll", unityEv("dump/dll", "")))
	case scanErr != nil || stats.Files == 0:
		msg := "no decompiled stub C# in src/"
		if scanErr != nil {
			msg += ": " + scanErr.Error()
		}
		r.Checks = append(r.Checks, unityCheck("output.present", "fail", msg, unityEv("src", "")))
	case headerErr == nil && len(il2cppMissingImages(header, dummies, emptyDummies)) > 0:
		missing := il2cppMissingImages(header, dummies, emptyDummies)
		r.Checks = append(r.Checks, unityCheck("output.present", "warn",
			fmt.Sprintf("dumper emitted %d of %d metadata images as DummyDLLs; the dump is partial (missing: %s)", len(header.Images)-len(missing), len(header.Images), strings.Join(il2cppFirst(missing, 10), ", ")),
			unityEv(dumpRel, "image headers"), unityEv("dump/dll", "")))
	case len(emptyDummies) > 0:
		r.Checks = append(r.Checks, unityCheck("output.present", "warn",
			fmt.Sprintf("%d DummyDLLs are empty (truncated dump): %s", len(emptyDummies), strings.Join(il2cppFirst(emptyDummies, 10), ", ")),
			unityEv("dump/dll", "")))
	default:
		msg := fmt.Sprintf("%d DummyDLLs, %d stub source files in %d modules", len(dummies), stats.Files, stats.Modules)
		if headerErr == nil {
			msg = fmt.Sprintf("all %d metadata images emitted as DummyDLLs; ", len(header.Images)) + msg
		}
		r.Checks = append(r.Checks, unityCheck("output.present", "pass", msg, unityEv("dump/dll", ""), unityEv("src", "")))
	}

	// artifact.fidelity: the C# is stub fidelity by construction; confirm the
	// dumper attribute markers so a mislabeled tree is visible.
	if stats.Files > 0 && stats.StubMarkers > 0 {
		r.Checks = append(r.Checks, unityCheck("artifact.fidelity", "pass",
			fmt.Sprintf("src/ and dump/ are stub C# (fidelity %q): signatures, tokens and native addresses only; method bodies are placeholders, not recovered code (%d/%d files carry dumper attribute markers)", runmodel.FidelityStub, stats.StubMarkers, stats.Files),
			unityEv("src", "Token/Address attributes")))
	} else if stats.Files > 0 {
		r.Checks = append(r.Checks, unityCheck("artifact.fidelity", "warn", "stub C# without dumper attribute markers; origin of declarations cannot be confirmed", unityEv("src", "")))
	} else {
		r.Checks = append(r.Checks, unityCheck("artifact.fidelity", "unknown", "no stub C# to classify"))
	}
	warn("IL2CPP C# under src/ and dump/ is stub fidelity: method bodies are NOT decompiled native code; GameAssembly.dll native bodies were not analyzed")

	// functions: true native body universe is not enumerated; zero recovered.
	if stats.Files > 0 {
		r.Functions = runmodel.Coverage{
			Found: unityUnknown(fmt.Sprintf("native method body universe of GameAssembly.dll is not enumerated; stubs declare %d methods (%d with native RVA, %d without: abstract/extern/generic-shared)", stats.Methods, stats.MethodsWithAddress, stats.MethodsWithoutAddress),
				unityEv("src", "method tokens 0x06")),
			Emitted: unityMeasured(0, "DummyDLL decompilation emits placeholder bodies only; no native method body is recovered"),
		}
		r.Checks = append(r.Checks, unityCheck("functions.coverage", "warn",
			fmt.Sprintf("0 native method bodies recovered; %d method stubs (%d with native RVA) are declarations only", stats.Methods, stats.MethodsWithAddress),
			unityEv("src", "")))
	} else {
		r.Functions = runmodel.Coverage{Found: unityUnknown("no stub output to inventory"), Emitted: unityUnknown("no stub output to inventory")}
		r.Checks = append(r.Checks, unityCheck("functions.coverage", "unknown", "no stub output to inventory"))
	}

	// types: metadata TypeDef ranges of decompiled modules vs emitted type tokens.
	r.Types = il2cppTypes(header, headerErr, dumpRel, stats, scanErr)
	switch {
	case r.Types.Found.Status == "measured" && r.Types.Emitted.Status == "measured":
		r.Checks = append(r.Checks, unityCheck("types.coverage", "pass",
			fmt.Sprintf("%d type tokens emitted for decompiled modules whose metadata declares %d TypeDefs; ILSpy folds compiler-generated types, so counts are not a ratio", *r.Types.Emitted.Value, *r.Types.Found.Value)))
	default:
		r.Checks = append(r.Checks, unityCheck("types.coverage", "unknown", r.Types.Found.Basis))
	}

	// decompiler.errors from recipe evidence.
	if evErr == nil {
		var failed, ok, part, partErrs, sys int64
		var failedNames, partNames []string
		evs := []runmodel.Evidence{}
		for _, a := range ev.Assemblies {
			switch a.Status {
			case "failed":
				failed++
				failedNames = append(failedNames, a.Name)
				evs = append(evs, unityEv("log/ilspycmd.log", a.Name+": "+a.Error))
			case "partial":
				part++
				partErrs += a.Errors
				partNames = append(partNames, a.Name)
				evs = append(evs, unityEv("log/ilspycmd.log", fmt.Sprintf("%s: partial, %d member errors replaced by error text", a.Name, a.Errors)))
			case "decompiled":
				ok++
			case "system-skipped":
				sys++
			}
		}
		r.DecompilerErrors = unityMeasured(failed+partErrs, "failed assemblies (no usable output) plus members ILSpy replaced by error text in partial assemblies", evs...)
		status := "pass"
		if failed+part > 0 {
			status = "warn"
		}
		r.Checks = append(r.Checks, unityCheck("decompiler.errors", status,
			fmt.Sprintf("selected %d DummyDLLs: %d decompiled fully, %d partial (%d member errors)%s, %d failed%s; %d system/engine assemblies skipped by policy",
				ok+part+failed, ok, part, partErrs, il2cppList(partNames), failed, il2cppList(failedNames), sys), evs...))
		if sys > 0 {
			warn(fmt.Sprintf("%d system/engine DummyDLLs (System.*, UnityEngine.*, mscorlib ...) were not decompiled", sys))
		}
	} else {
		r.DecompilerErrors = unityUnknown("recipe evidence missing: " + evErr.Error())
		r.Checks = append(r.Checks, unityCheck("decompiler.errors", "unknown", "recipe evidence missing"))
	}

	r.Checks = append(r.Checks, il2cppProvenanceCheck(ctx, in, dir, ev, evErr, &r))
	r.Checks = append(r.Checks,
		unityCheck("types.unknown-result", "not-applicable", r.UnknownResultTypes.Basis),
		unityCheck("references.unresolved", "unknown", r.UnresolvedReferences.Basis),
		unityCheck("obfuscation.residual", "unknown", r.ResidualObfuscation.Basis),
		unityCheck("strings.decryption", "not-applicable", "IL2CPP pipeline performs no string decryption"),
	)
	r.Status = unityStatus(r.Checks, il2cppMandatory)
	return r, nil
}

func il2cppTypes(h DumpHeader, herr error, dumpRel string, st StubStats, serr error) runmodel.Coverage {
	c := runmodel.Coverage{}
	if serr == nil && st.Files > 0 {
		c.Emitted = unityMeasured(st.Types, "unique type tokens (0x02) declared in decompiled stub C#", unityEv("src", ""))
	} else {
		c.Emitted = unityUnknown("no decompiled stub C# to inventory")
	}
	if herr != nil {
		c.Found = unityUnknown("metadata TypeDef ranges unavailable: " + herr.Error())
		return c
	}
	var found int64
	matched := 0
	for _, img := range h.Images {
		if _, ok := st.TypesByModule[img.Name]; !ok {
			continue
		}
		if img.TypeCount < 0 {
			c.Found = unityUnknown("dump header does not state type counts for " + img.Name)
			return c
		}
		found += img.TypeCount
		matched++
	}
	if matched == 0 {
		c.Found = unityUnknown("no decompiled module matches a metadata image header")
		return c
	}
	c.Found = unityMeasured(found, fmt.Sprintf("metadata TypeDef ranges of the %d decompiled images (%s header); compiler-generated and nested types included", matched, h.Format), unityEv(dumpRel, "// Image headers"))
	return c
}

func il2cppProvenanceCheck(ctx context.Context, in Input, dir string, ev IL2CPPEvidence, evErr error, r *runmodel.QualityReport) runmodel.QualityCheck {
	if evErr != nil {
		return unityCheck("provenance.complete", "unknown", "recipe evidence missing; binary/metadata pair and dumper identity unknown")
	}
	evs := []runmodel.Evidence{}
	status := "pass"
	var notes []string
	degrade := func(s string) {
		if status == "pass" {
			status = "warn"
		}
		notes = append(notes, s)
	}
	pair := []struct{ role, rel string }{{"primary", ev.Binary}, {"il2cpp-metadata", ev.Metadata}}
	for _, p := range pair {
		want, ok := unityInputHash(in, p.role)
		if p.rel == "" {
			degrade(p.role + " copy not recorded")
			continue
		}
		got, err := unityFileSHA256(ctx, filepath.Join(dir, filepath.FromSlash(p.rel)))
		if err != nil {
			if ctx.Err() != nil {
				return unityCheck("provenance.complete", "unknown", "cancelled")
			}
			degrade(p.role + " copy unreadable: " + err.Error())
			continue
		}
		evs = append(evs, unityEv(p.rel, p.role+" sha256 "+got))
		switch {
		case !ok:
			degrade(p.role + " input hash missing from manifest; pair reproducibility unproven")
		case got != want:
			status = "fail"
			notes = append(notes, fmt.Sprintf("%s copy sha256 %s differs from admitted input %s", p.role, got, want))
		}
	}
	if ev.MetadataVersion == nil {
		status = "fail"
		notes = append(notes, "global-metadata.dat version unreadable ("+ev.MetadataVersionError+"); metadata may not belong to this binary")
	} else {
		notes = append(notes, fmt.Sprintf("metadata version %d", *ev.MetadataVersion))
	}
	if ev.Dumper == "" {
		status = "fail"
		notes = append(notes, "dumper identity missing")
	} else {
		if ev.DumperVersion == "" {
			degrade("dumper " + ev.Dumper + " version unknown")
		}
		notes = append(notes, "dumper "+ev.Dumper+" "+ev.DumperVersion)
	}
	for _, a := range ev.Attempts {
		if a.Error != "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf("dumper %s failed before fallback: %s", a.Tool, a.Error))
		}
	}
	return unityCheck("provenance.complete", status, strings.Join(notes, "; "), evs...)
}

func il2cppDummyDLLs(dir string) (ok, empty []string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		// Images such as "__Generated" are emitted without a .dll extension.
		if e.IsDir() || il2cppHelperAssembly(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() == 0 {
			empty = append(empty, e.Name())
			continue
		}
		ok = append(ok, e.Name())
	}
	return ok, empty
}

func il2cppFirst(s []string, n int) []string {
	if len(s) > n {
		return append(append([]string(nil), s[:n]...), fmt.Sprintf("... +%d", len(s)-n))
	}
	return s
}

func il2cppList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(il2cppFirst(names, 10), ", ") + ")"
}

// il2cppMissingImages lists metadata images with no (non-empty) DummyDLL.
func il2cppMissingImages(h DumpHeader, dummies, empty []string) []string {
	have := map[string]bool{}
	for _, d := range dummies {
		have[strings.ToLower(d)] = true
	}
	var missing []string
	for _, img := range h.Images {
		n := strings.ToLower(img.Name)
		if !have[n] && !have[n+".dll"] {
			missing = append(missing, img.Name)
		}
	}
	return missing
}
