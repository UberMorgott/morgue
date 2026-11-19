package quality

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// Native-family collectors (native incl. NativeAOT/Go, delphi, ue5). All
// numbers come from files the recipe wrote: the per-function status NDJSON
// emitted by MorgueExport.java inside Ghidra, the split catalog
// (functions.ndjson), name_map.csv and the recipe's quality_evidence.json.
// Anything not backed by such a file is unknown with a basis, never zero.

// EvidenceFile is the per-target sidecar native-family recipes write so the
// collector can tell "did not run (why)" from "ran and produced nothing".
const EvidenceFile = "quality_evidence.json"

// Status values for StepEvidence.Status.
const (
	StepSucceeded   = "success"
	StepFailed      = "failed"
	StepSkipped     = "skipped"
	StepDisabled    = "disabled"
	StepUnavailable = "unavailable"
)

// NativeEvidence records what the native-family recipe actually did.
type NativeEvidence struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Family        string              `json:"family"`
	Decompile     *StepEvidence       `json:"decompile,omitempty"`
	IDR           *StepEvidence       `json:"idr,omitempty"`
	Extraction    *ExtractionEvidence `json:"extraction,omitempty"`
	Usmap         *UsmapEvidence      `json:"usmap,omitempty"`
}

// StepEvidence describes one optional recipe step.
type StepEvidence struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	Tool   string `json:"tool"`
	Binary string `json:"binary"` // base name of the decompiled module
	Output string `json:"output"` // target-relative output dir/file
}

// ExtractionEvidence describes UE container extraction (retoc to-legacy).
type ExtractionEvidence struct {
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	Containers    int    `json:"containers"` // pak/utoc/ucas files discovered
	Dirs          int    `json:"dirs"`       // IoStore dirs attempted
	FailedDirs    int    `json:"failedDirs"`
	FilesProduced int    `json:"filesProduced"`
	LastError     string `json:"lastError"`
}

// UsmapEvidence describes the UE .usmap mapping file used for type layout.
type UsmapEvidence struct {
	Found      bool   `json:"found"`
	Path       string `json:"path"`
	Parsed     bool   `json:"parsed"`
	Error      string `json:"error"`
	Classes    int    `json:"classes"`
	SDKClasses int    `json:"sdkClasses"` // classes written to sdk/ from this usmap
}

// WriteNativeEvidence writes ev to <targetDir>/quality_evidence.json.
func WriteNativeEvidence(targetDir string, ev NativeEvidence) error {
	ev.SchemaVersion = 1
	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, EvidenceFile), data, 0644)
}

// GhidraStatusFile / GhidraLogFile mirror the recipe's file names (recipe
// cannot be imported here).
const (
	GhidraStatusFile = "ghidra_functions.ndjson"
	GhidraLogFile    = "ghidra.log"
	ProvenanceFile   = "provenance.ndjson"
)

// GhidraFunction is one function line of ghidra_functions.ndjson.
type GhidraFunction struct {
	Kind            string `json:"kind"` // "" for a function, else program|summary
	Address         string `json:"address"`
	Name            string `json:"name"`
	Source          string `json:"source"` // Ghidra SourceType: DEFAULT|ANALYSIS|IMPORTED|USER_DEFINED
	Thunk           bool   `json:"thunk"`
	Status          string `json:"status"` // decompiled|failed|error|empty
	Warnings        int64  `json:"warnings"`
	UndefinedReturn bool   `json:"undefinedReturn"`
	Message         string `json:"message"`

	// program header fields
	GhidraVersion string `json:"ghidraVersion"`
	ImageBase     string `json:"imageBase"`
	PDBLoaded     bool   `json:"pdbLoaded"`
	FunctionCount int64  `json:"functionCount"`

	// summary trailer fields
	Found      int64 `json:"found"`
	Decompiled int64 `json:"decompiled"`
	Errors     int64 `json:"errors"`
}

// NormalizeAddr returns a canonical lowercase 0x-hex address ("" if unparsable).
func NormalizeAddr(s string) string {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		h = h[i+1:]
	}
	v, err := strconv.ParseUint(h, 16, 64)
	if err != nil {
		return ""
	}
	return "0x" + strconv.FormatUint(v, 16)
}

// GhidraStats is the streamed tally of ghidra_functions.ndjson.
type GhidraStats struct {
	Present        bool
	Program        *GhidraFunction
	Summary        *GhidraFunction
	Found          int64
	Decompiled     int64
	Failed         int64 // failed + error + empty
	Thunks         int64
	Warnings       int64 // Ghidra "WARNING:" comments in decompiled bodies
	UndefinedRet   int64
	BySource       map[string]int64
	FailedSamples  []runmodel.Evidence
	MalformedLines int64
}

const natSampleCap = 10

// ReadGhidraStats streams path. A missing file returns Present=false, nil.
func ReadGhidraStats(path, relPath string) (GhidraStats, error) {
	st := GhidraStats{BySource: map[string]int64{}}
	f, err := os.Open(path) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	defer func() { _ = f.Close() }()
	st.Present = true
	var line int64
	err = natStreamLines(f, func(b []byte) error {
		line++
		if len(b) == 0 {
			return nil
		}
		var g GhidraFunction
		if json.Unmarshal(b, &g) != nil {
			st.MalformedLines++
			return nil //nolint:nilerr // malformed line is counted above; the stream goes on
		}
		switch g.Kind {
		case "program":
			p := g
			st.Program = &p
			return nil
		case "summary":
			s := g
			st.Summary = &s
			return nil
		}
		st.Found++
		st.BySource[g.Source]++
		if g.Thunk {
			st.Thunks++
		}
		if g.Status == "decompiled" {
			st.Decompiled++
			st.Warnings += g.Warnings
			if g.UndefinedReturn {
				st.UndefinedRet++
			}
		} else {
			st.Failed++
			if len(st.FailedSamples) < natSampleCap {
				l := line
				st.FailedSamples = append(st.FailedSamples, runmodel.Evidence{Path: relPath, Line: &l,
					Detail: fmt.Sprintf("%s %s @ %s: %s", g.Status, g.Name, g.Address, g.Message)})
			}
		}
		return nil
	})
	return st, err
}

// natStreamLines calls fn for each line without a size cap on a single line.
func natStreamLines(r io.Reader, fn func([]byte) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		b, err := br.ReadBytes('\n')
		if len(b) > 0 {
			if ferr := fn([]byte(strings.TrimRight(string(b), "\r\n"))); ferr != nil {
				return ferr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// splitStats streams functions.ndjson (the split catalog) and counts records
// whose address matches the next decompiled function of the status file, so
// emitted counts are tied to the same address identity universe.
type splitStats struct {
	Present  bool
	Records  int64
	Named    int64
	Matched  int64 // records whose address equals the next decompiled status address
	Mismatch int64
}

func readSplitStats(srcDir string) (splitStats, error) {
	var st splitStats
	f, err := os.Open(filepath.Join(srcDir, "functions.ndjson")) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	defer func() { _ = f.Close() }()
	st.Present = true

	var next func() (string, bool)
	if sf, serr := os.Open(filepath.Join(srcDir, GhidraStatusFile)); serr == nil { //nolint:gosec // G304: fixed file inside the target output
		defer func() { _ = sf.Close() }()
		br := bufio.NewReaderSize(sf, 1<<20)
		next = func() (string, bool) {
			for {
				b, rerr := br.ReadBytes('\n')
				if len(b) > 0 {
					var g GhidraFunction
					if json.Unmarshal(b, &g) == nil && g.Kind == "" && g.Status == "decompiled" {
						return NormalizeAddr(g.Address), true
					}
				}
				if rerr != nil {
					return "", false
				}
			}
		}
	}
	err = natStreamLines(f, func(b []byte) error {
		if len(b) == 0 {
			return nil
		}
		var e struct {
			Address string `json:"address"`
			IsNamed bool   `json:"is_named"`
		}
		if json.Unmarshal(b, &e) != nil {
			return nil //nolint:nilerr // malformed line is skipped; the stream goes on
		}
		st.Records++
		if e.IsNamed {
			st.Named++
		}
		if next != nil {
			if a, ok := next(); ok && a != "" && a == NormalizeAddr(e.Address) {
				st.Matched++
			} else {
				st.Mismatch++
			}
		}
		return nil
	})
	return st, err
}

// countCSVRows returns data rows (excluding header) of a CSV, -1 if absent.
func countCSVRows(path string) (int64, error) {
	f, err := os.Open(path) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		if os.IsNotExist(err) {
			return -1, nil
		}
		return -1, err
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(bufio.NewReader(f))
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	var n int64 = -1
	for {
		_, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return -1, rerr
		}
		n++
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// provenanceStats tallies the target's provenance.ndjson.
type provenanceStats struct {
	Present    bool
	Records    int64
	Functions  int64 // native.decompile rows with an address
	Renames    int64 // native.rename rows
	Pseudocode int64
	BadKind    int64 // native.decompile rows not labelled pseudocode
}

func readProvenanceStats(path string) (provenanceStats, error) {
	var st provenanceStats
	f, err := os.Open(path) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	defer func() { _ = f.Close() }()
	st.Present = true
	err = natStreamLines(f, func(b []byte) error {
		if len(b) == 0 {
			return nil
		}
		var r runmodel.ProvenanceRecord
		if json.Unmarshal(b, &r) != nil {
			return nil //nolint:nilerr // malformed line is skipped; the stream goes on
		}
		st.Records++
		if r.ArtifactKind == runmodel.FidelityPseudocode {
			st.Pseudocode++
		}
		switch r.Producer.StepID {
		case "native.decompile":
			if r.Address != nil {
				st.Functions++
				if r.ArtifactKind != runmodel.FidelityPseudocode {
					st.BadKind++
				}
			}
		case "native.rename":
			st.Renames++
		}
		return nil
	})
	return st, err
}

// Metric/coverage helpers (prefixed: the package is shared with other tracks).

func natMeasured(n int64, basis string, ev ...runmodel.Evidence) runmodel.Metric {
	return runmodel.Metric{Value: &n, Status: "measured", Basis: basis, Evidence: natEv(ev)}
}
func natUnknown(basis string, ev ...runmodel.Evidence) runmodel.Metric {
	return runmodel.Metric{Status: "unknown", Basis: basis, Evidence: natEv(ev)}
}
func natNA(basis string) runmodel.Metric {
	return runmodel.Metric{Status: "not-applicable", Basis: basis, Evidence: []runmodel.Evidence{}}
}
func natEv(ev []runmodel.Evidence) []runmodel.Evidence {
	if ev == nil {
		return []runmodel.Evidence{}
	}
	return ev
}
func natCoverage(found, emitted runmodel.Metric, comparable bool) runmodel.Coverage {
	c := runmodel.Coverage{Found: found, Emitted: emitted, Comparable: comparable}
	if comparable && found.Status == "measured" && emitted.Status == "measured" &&
		*found.Value > 0 && *emitted.Value <= *found.Value {
		r := float64(*emitted.Value) / float64(*found.Value)
		c.Ratio = &r
	} else {
		c.Comparable = comparable && found.Status == "measured" && emitted.Status == "measured"
	}
	return c
}
func natCoverageAll(m runmodel.Metric) runmodel.Coverage {
	return runmodel.Coverage{Found: m, Emitted: m}
}
func natCheck(id, status, msg string, ev ...runmodel.Evidence) runmodel.QualityCheck {
	return runmodel.QualityCheck{ID: id, Status: status, Message: msg, Evidence: natEv(ev)}
}
func natFile(rel, detail string) runmodel.Evidence {
	return runmodel.Evidence{Path: rel, Detail: detail}
}

// natMandatory lists the check IDs whose status drives report status.
var natMandatory = map[string]bool{
	"output.present":      true,
	"functions.coverage":  true,
	"decompiler.errors":   true,
	"provenance.complete": true,
	"artifact.fidelity":   true,
}

// natFinish derives report status: any warn/fail mandatory check => degraded;
// else any unknown mandatory => unknown; all mandatory n/a => not-applicable.
func natFinish(r *runmodel.QualityReport) {
	degraded, unknown, applicable := false, false, false
	for _, c := range r.Checks {
		if !natMandatory[c.ID] {
			continue
		}
		switch c.Status {
		case "pass":
			applicable = true
		case "warn", "fail":
			applicable, degraded = true, true
		case "unknown":
			applicable, unknown = true, true
		}
	}
	switch {
	case degraded:
		r.Status = "degraded"
	case unknown:
		r.Status = "unknown"
	case applicable:
		r.Status = "pass"
	default:
		r.Status = "not-applicable"
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
}

func readEvidence(dir string) (*NativeEvidence, error) {
	data, err := os.ReadFile(filepath.Join(dir, EvidenceFile)) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ev NativeEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("parse %s: %w", EvidenceFile, err)
	}
	return &ev, nil
}

// combinedC returns the target-relative path of the combined Ghidra .c in
// srcDir and whether it carries the pseudocode fidelity banner.
func combinedC(out, srcDir string) (rel string, size int64, labelled bool) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", 0, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".c") {
			continue
		}
		p := filepath.Join(srcDir, e.Name())
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		r, _ := filepath.Rel(out, p)
		head := make([]byte, 512)
		if f, oerr := os.Open(p); oerr == nil { //nolint:gosec // G304: file inside the target output
			n, _ := f.Read(head)
			_ = f.Close()
			labelled = strings.Contains(string(head[:n]), "// PSEUDOCODE:")
		}
		return filepath.ToSlash(r), info.Size(), labelled
	}
	return "", 0, false
}

// ghidraSection fills functions/decompiler/type metrics and checks from the
// Ghidra outputs under <out>/src. notApplicable, when non-empty, marks the
// function metrics not-applicable (e.g. an assets-only UE target).
func ghidraSection(out string, ev *StepEvidence, notApplicable string, r *runmodel.QualityReport) {
	srcDir := filepath.Join(out, "src")
	statusRel := "src/" + GhidraStatusFile
	if notApplicable != "" {
		r.Functions = natCoverageAll(natNA(notApplicable))
		r.Names = runmodel.NamesNotApplicable(notApplicable)
		r.DecompilerErrors = natNA(notApplicable)
		r.UnknownResultTypes = natNA(notApplicable)
		r.Checks = append(r.Checks,
			natCheck("functions.coverage", "not-applicable", notApplicable),
			natCheck("decompiler.errors", "not-applicable", notApplicable),
			natCheck("types.unknown-result", "not-applicable", notApplicable))
		return
	}

	why := "Ghidra decompilation did not run"
	if ev != nil && ev.Status != "" && ev.Status != StepSucceeded {
		why = fmt.Sprintf("Ghidra decompilation %s: %s", ev.Status, ev.Reason)
	}
	gs, gerr := ReadGhidraStats(filepath.Join(srcDir, GhidraStatusFile), statusRel)
	if gerr != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("read %s: %v", statusRel, gerr))
	}
	if !gs.Present {
		basis := why
		if ev == nil || ev.Status == StepSucceeded {
			basis = statusRel + " missing (Ghidra output from an older Morgue or the export script died); function inventory not measured"
		}
		r.Functions = natCoverageAll(natUnknown(basis))
		r.Names = runmodel.NamesUnknown(basis)
		r.DecompilerErrors = natUnknown(basis)
		r.UnknownResultTypes = natUnknown(basis)
		r.Checks = append(r.Checks,
			natCheck("functions.coverage", "unknown", basis),
			natCheck("decompiler.errors", "unknown", basis),
			natCheck("types.unknown-result", "unknown", basis))
		return
	}
	if gs.MalformedLines > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%s: %d malformed lines ignored", statusRel, gs.MalformedLines))
	}
	if gs.Summary == nil {
		r.Warnings = append(r.Warnings, statusRel+" has no summary trailer: the Ghidra export stopped early (e.g. JVM OOM); counts cover only the functions it reached")
	}

	statusEv := natFile(statusRel, "per-function decompile status written by MorgueExport.java inside Ghidra")
	foundBasis := "functions in Ghidra's listing (getFunctions(true): internal functions incl. thunks; external/import-only symbols excluded)"
	if gs.Program != nil && gs.Program.FunctionCount > gs.Found {
		foundBasis += fmt.Sprintf("; Ghidra function manager total incl. %d external symbols: %d", gs.Program.FunctionCount-gs.Found, gs.Program.FunctionCount)
	}
	found := natMeasured(gs.Found, foundBasis, statusEv)

	// Emitted bodies: split catalog records tied to decompiled status addresses;
	// a target without a split (delphi) uses the decompiled status count.
	sp, serr := readSplitStats(srcDir)
	if serr != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("read src/functions.ndjson: %v", serr))
	}
	var emitted runmodel.Metric
	comparable := true
	switch {
	case sp.Present:
		emitted = natMeasured(sp.Matched, "function bodies written to src/functions/ (functions.ndjson records whose address matches a decompiled Ghidra function, before dedupe)",
			natFile("src/functions.ndjson", "split catalog"))
		if sp.Mismatch > 0 || sp.Records != gs.Decompiled {
			r.Warnings = append(r.Warnings, fmt.Sprintf("split catalog has %d records (%d address mismatches) vs %d decompiled by Ghidra", sp.Records, sp.Mismatch, gs.Decompiled))
		}
	default:
		emitted = natMeasured(gs.Decompiled, "functions Ghidra decompiled into the combined .c (no per-function split for this recipe)", statusEv)
	}
	r.Functions = natCoverage(found, emitted, comparable)

	failEv := append([]runmodel.Evidence{statusEv}, gs.FailedSamples...)
	r.DecompilerErrors = natMeasured(gs.Failed, "functions Ghidra failed to decompile (timeout/error/empty result)", failEv...)
	r.UnknownResultTypes = natMeasured(gs.UndefinedRet, "decompiled functions whose return type Ghidra left 'undefined*'", statusEv)

	def := gs.BySource["DEFAULT"]
	nameMsg := fmt.Sprintf("names: %d of %d default FUN_ (unknown, %.1f%%); imported %d, analysis %d, user/script %d",
		def, gs.Found, natPct(def, gs.Found), gs.BySource["IMPORTED"], gs.BySource["ANALYSIS"], gs.BySource["USER_DEFINED"])
	if gs.Program != nil && gs.Program.PDBLoaded {
		nameMsg += "; PDB loaded"
	}
	r.Names = runmodel.NameQuality{
		Total:     natMeasured(gs.Found, foundBasis, statusEv),
		AutoNamed: natMeasured(def, "functions whose Ghidra name source is DEFAULT (FUN_<addr> and similar auto-generated names): purpose unknown", statusEv),
		Inferred:  natUnknown("indexes/name_map.csv not produced: no single-string-candidate renames recorded"),
	}
	if gs.Found > 0 && def <= gs.Found {
		ratio := float64(def) / float64(gs.Found)
		r.Names.AutoRatio = &ratio
	}
	if renames, _ := countCSVRows(filepath.Join(srcDir, "indexes", "name_map.csv")); renames >= 0 {
		nameMsg += fmt.Sprintf("; %d default names renamed by single-string-candidate inference (indexes/name_map.csv, medium confidence)", renames)
		r.Names.Inferred = natMeasured(renames, "default names renamed by single-string-candidate inference (medium confidence; inference, not original names)",
			natFile("src/indexes/name_map.csv", "rename map"))
	}
	if gs.Found > 0 && def*2 > gs.Found {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%.1f%% of functions have Ghidra default names (FUN_<addr>): their purpose is unknown", natPct(def, gs.Found)))
	}

	switch {
	case r.Functions.Ratio != nil && *r.Functions.Emitted.Value == *r.Functions.Found.Value:
		r.Checks = append(r.Checks, natCheck("functions.coverage", "pass",
			fmt.Sprintf("%d/%d functions emitted as pseudocode; %s", *emitted.Value, gs.Found, nameMsg), statusEv))
	case r.Functions.Ratio != nil:
		r.Checks = append(r.Checks, natCheck("functions.coverage", "warn",
			fmt.Sprintf("%d/%d functions emitted (%.1f%%); %s", *emitted.Value, gs.Found, *r.Functions.Ratio*100, nameMsg), statusEv))
	default:
		r.Checks = append(r.Checks, natCheck("functions.coverage", "unknown", "Ghidra found 0 functions; "+nameMsg, statusEv))
	}
	if gs.Failed == 0 {
		r.Checks = append(r.Checks, natCheck("decompiler.errors", "pass",
			fmt.Sprintf("0 failed decompilations; %d Ghidra WARNING comments in bodies", gs.Warnings), statusEv))
	} else {
		r.Checks = append(r.Checks, natCheck("decompiler.errors", "warn",
			fmt.Sprintf("%d functions failed to decompile; %d Ghidra WARNING comments in bodies", gs.Failed, gs.Warnings), failEv...))
	}
	if gs.UndefinedRet == 0 {
		r.Checks = append(r.Checks, natCheck("types.unknown-result", "pass", "no undefined return types", statusEv))
	} else {
		r.Checks = append(r.Checks, natCheck("types.unknown-result", "warn",
			fmt.Sprintf("%d of %d decompiled functions have an undefined return type", gs.UndefinedRet, gs.Decompiled), statusEv))
	}
}

func natPct(n, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * 100 / float64(total)
}

// fidelityAndProvenance adds artifact.fidelity and provenance.complete checks
// for Ghidra pseudocode under <out>/src.
func fidelityAndProvenance(out string, r *runmodel.QualityReport, expectFunctions int64, extraWarn ...string) {
	rel, size, labelled := combinedC(out, filepath.Join(out, "src"))
	switch {
	case rel == "":
		r.Checks = append(r.Checks, natCheck("artifact.fidelity", "not-applicable", "no Ghidra pseudocode produced"))
	case labelled:
		r.Checks = append(r.Checks, natCheck("artifact.fidelity", "pass",
			fmt.Sprintf("Ghidra output is labelled pseudocode (%d bytes), not original source", size), natFile(rel, "PSEUDOCODE banner")))
	default:
		r.Checks = append(r.Checks, natCheck("artifact.fidelity", "warn",
			"Ghidra .c lacks the PSEUDOCODE banner: readers may mistake it for original source", natFile(rel, "no banner")))
	}

	ps, err := readProvenanceStats(filepath.Join(out, ProvenanceFile))
	if err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("read %s: %v", ProvenanceFile, err))
	}
	pev := natFile(ProvenanceFile, "per-target provenance stream")
	switch {
	case !ps.Present:
		r.Checks = append(r.Checks, natCheck("provenance.complete", "unknown", ProvenanceFile+" not found for this target"))
	case ps.BadKind > 0:
		r.Checks = append(r.Checks, natCheck("provenance.complete", "fail",
			fmt.Sprintf("%d decompiled-function records are not labelled pseudocode", ps.BadKind), pev))
	case expectFunctions >= 0 && ps.Functions < expectFunctions:
		r.Checks = append(r.Checks, natCheck("provenance.complete", "warn",
			fmt.Sprintf("%d function records for %d functions Ghidra found", ps.Functions, expectFunctions), pev))
	default:
		r.Checks = append(r.Checks, natCheck("provenance.complete", "pass",
			fmt.Sprintf("%d records (%d functions with address, %d renames)", ps.Records, ps.Functions, ps.Renames), pev))
	}
	r.Warnings = append(r.Warnings, extraWarn...)
}

func natBase(family string) runmodel.QualityReport {
	return runmodel.QualityReport{
		SchemaVersion:        runmodel.QualityVersion,
		Family:               family,
		Status:               "unknown",
		Types:                natCoverageAll(natUnknown("not measured")),
		Names:                runmodel.NamesUnknown("Ghidra function status (src/" + GhidraStatusFile + ") not available"),
		DecompilerErrors:     natUnknown("not measured"),
		UnknownResultTypes:   natUnknown("not measured"),
		UnresolvedReferences: natUnknown("native: unresolved call targets/imports are not measured"),
		ResidualObfuscation:  natUnknown("native: packer/obfuscation residue after decompilation is not measured"),
		StringDecryption:     natCoverageAll(natNA("native family has no string decryption step")),
		Warnings:             []string{},
		Checks:               []runmodel.QualityCheck{},
	}
}

func natInfoChecks(r *runmodel.QualityReport) {
	r.Checks = append(r.Checks,
		natCheck("references.unresolved", "unknown", r.UnresolvedReferences.Basis),
		natCheck("obfuscation.residual", "unknown", r.ResidualObfuscation.Basis),
		natCheck("strings.decryption", "not-applicable", r.StringDecryption.Found.Basis))
}

func expectedFunctions(out string) int64 {
	gs, err := ReadGhidraStats(filepath.Join(out, "src", GhidraStatusFile), "")
	if err != nil || !gs.Present {
		return -1
	}
	return gs.Found
}

// Native is the collector for the native recipe (C/C++, Go, NativeAOT).
type Native struct{}

// Delphi is the collector for the delphi recipe (IDR + Ghidra).
type Delphi struct{}

// UE5 is the collector for the ue5 recipe (containers, usmap, Ghidra).
type UE5 struct{}

func init() {
	Register("native", Native{})
	Register("delphi", Delphi{})
	Register("ue5", UE5{})
}

// Collect implements Collector.
func (Native) Collect(_ context.Context, in Input) (runmodel.QualityReport, error) {
	r := natBase("native")
	out := in.Output
	ev, err := readEvidence(out)
	if err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}
	var dec *StepEvidence
	if ev != nil {
		dec = ev.Decompile
	}
	r.Types = natCoverageAll(natNA("native pseudocode has no type inventory; C++ class owners in src/symbols.json are name-derived"))
	r.Checks = append(r.Checks, natCheck("types.coverage", "not-applicable", r.Types.Found.Basis))
	ghidraSection(out, dec, "", &r)

	rel, size, _ := combinedC(out, filepath.Join(out, "src"))
	switch {
	case rel != "" && size > 0:
		r.Checks = append(r.Checks, natCheck("output.present", "pass", "Ghidra pseudocode present", natFile(rel, "combined decompiled .c")))
	case natExists(filepath.Join(out, "imports.json")) || natExists(filepath.Join(out, "exports.txt")):
		r.Checks = append(r.Checks, natCheck("output.present", "warn",
			"no pseudocode: only PE metadata (imports/exports/strings/sections) was produced", natFile("imports.json", "metadata fallback")))
	default:
		r.Checks = append(r.Checks, natCheck("output.present", "fail", "no native output found"))
	}
	fidelityAndProvenance(out, &r, expectedFunctions(out))
	natInfoChecks(&r)
	natFinish(&r)
	return r, nil
}

// Collect implements Collector.
func (Delphi) Collect(_ context.Context, in Input) (runmodel.QualityReport, error) {
	r := natBase("delphi")
	out := in.Output
	ev, err := readEvidence(out)
	if err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}
	var dec, idr *StepEvidence
	if ev != nil {
		dec, idr = ev.Decompile, ev.IDR
	}
	idrBasis := "IDR output (idr/) is not parsed: Delphi unit/class/method inventory is not measured"
	r.Types = natCoverageAll(natUnknown(idrBasis))
	r.Checks = append(r.Checks, natCheck("types.coverage", "unknown", idrBasis))
	ghidraSection(out, dec, "", &r)

	idrFiles := natCountFiles(filepath.Join(out, "idr"))
	rel, size, _ := combinedC(out, filepath.Join(out, "src"))
	var present []string
	if size > 0 {
		present = append(present, "Ghidra pseudocode")
	}
	if idrFiles > 0 {
		present = append(present, fmt.Sprintf("IDR output (%d files)", idrFiles))
	}
	if idr != nil && idr.Status != StepSucceeded {
		r.Warnings = append(r.Warnings, fmt.Sprintf("IDR %s: %s", idr.Status, idr.Reason))
	} else if idr != nil && idrFiles == 0 {
		r.Warnings = append(r.Warnings, "IDR reported success but idr/ is empty")
	}
	switch {
	case size > 0 && idrFiles > 0:
		r.Checks = append(r.Checks, natCheck("output.present", "pass", strings.Join(present, " + "), natFile(rel, "combined decompiled .c")))
	case len(present) > 0:
		r.Checks = append(r.Checks, natCheck("output.present", "warn", "partial: only "+strings.Join(present, " + ")))
	default:
		r.Checks = append(r.Checks, natCheck("output.present", "fail", "neither IDR output nor Ghidra pseudocode produced"))
	}
	fidelityAndProvenance(out, &r, expectedFunctions(out),
		"IDR artifacts carry no per-function provenance (no frozen producer step ID for IDR)")
	natInfoChecks(&r)
	natFinish(&r)
	return r, nil
}

// Collect implements Collector.
func (UE5) Collect(_ context.Context, in Input) (runmodel.QualityReport, error) {
	r := natBase("ue5")
	out := in.Output
	ev, err := readEvidence(out)
	if err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}
	var dec *StepEvidence
	var ext *ExtractionEvidence
	var um *UsmapEvidence
	if ev != nil {
		dec, ext, um = ev.Decompile, ev.Extraction, ev.Usmap
	}

	// Types: reflected class layout comes only from a usable .usmap.
	switch {
	case um != nil && um.Parsed:
		found := natMeasured(int64(um.Classes), "classes in the .usmap mapping file", natFile("name_resolution.json", um.Path))
		emitted := natMeasured(int64(um.SDKClasses), "classes written to sdk/ from that usmap", natFile("sdk", "offline SDK dump"))
		r.Types = natCoverage(found, emitted, true)
		r.Checks = append(r.Checks, natCheck("types.coverage", "pass",
			fmt.Sprintf("%d/%d usmap classes dumped (properties only; methods/offsets are not in a usmap)", um.SDKClasses, um.Classes)))
	case um != nil && um.Found:
		b := "usmap present but unusable (" + um.Error + "): property layout unknown"
		r.Types = natCoverageAll(natUnknown(b))
		r.Checks = append(r.Checks, natCheck("types.coverage", "unknown", b))
	case um != nil:
		b := "no .usmap mapping file found: reflected class/property layout unknown"
		r.Types = natCoverageAll(natUnknown(b))
		r.Checks = append(r.Checks, natCheck("types.coverage", "unknown", b))
	default:
		b := "usmap step did not run (SDK dump disabled or older output): reflected class/property layout unknown"
		r.Types = natCoverageAll(natUnknown(b))
		r.Checks = append(r.Checks, natCheck("types.coverage", "unknown", b))
	}

	na := ""
	if dec != nil && dec.Status == StepSkipped && dec.Binary == "" {
		na = "assets-only target: no UE shipping executable was decompiled (" + dec.Reason + ")"
	}
	ghidraSection(out, dec, na, &r)

	// Assets: parsed totals are asset metadata, never executable-code coverage.
	var parts []string
	extFiles := natCountFiles(filepath.Join(out, "extracted"))
	if ext != nil {
		switch {
		case ext.Status == StepSucceeded:
			parts = append(parts, fmt.Sprintf("%d asset files extracted from %d dir(s)", ext.FilesProduced, ext.Dirs))
		case ext.FilesProduced > 0:
			parts = append(parts, fmt.Sprintf("partial extraction: %d files, %d of %d dir(s) failed", ext.FilesProduced, ext.FailedDirs, ext.Dirs))
			r.Warnings = append(r.Warnings, "asset extraction partial: "+ext.LastError)
		default:
			r.Warnings = append(r.Warnings, fmt.Sprintf("asset extraction %s: %s %s", ext.Status, ext.Reason, ext.LastError))
		}
		low := strings.ToLower(ext.LastError)
		if strings.Contains(low, "aes") || strings.Contains(low, "encrypt") || strings.Contains(low, "key") {
			r.Warnings = append(r.Warnings, "containers appear encrypted and no AES key was supplied: their assets are missing, not empty")
		}
	}
	if ai, ok := natAssetsIndex(out); ok {
		parts = append(parts, fmt.Sprintf("uasset index: %d parsed, %d failed", ai[0], ai[1]))
		if ai[1] > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%d extracted assets failed to parse (assets_index.json)", ai[1]))
		}
	}
	if extFiles > 0 {
		r.Warnings = append(r.Warnings, "extracted assets carry no provenance rows (no frozen producer step ID for retoc extraction)")
	}
	rel, size, _ := combinedC(out, filepath.Join(out, "src"))
	if size > 0 {
		parts = append(parts, "Ghidra pseudocode")
	}
	switch {
	case len(parts) > 0 && (extFiles > 0 || size > 0):
		ev := []runmodel.Evidence{}
		if rel != "" {
			ev = append(ev, natFile(rel, "combined decompiled .c"))
		}
		if extFiles > 0 {
			ev = append(ev, natFile("extracted", fmt.Sprintf("%d files", extFiles)))
		}
		st := "pass"
		if ext != nil && ext.Status != StepSucceeded && ext.Status != StepDisabled {
			st = "warn"
		}
		r.Checks = append(r.Checks, natCheck("output.present", st, strings.Join(parts, "; "), ev...))
	default:
		r.Checks = append(r.Checks, natCheck("output.present", "fail", "no extracted assets and no pseudocode"))
	}
	fidelityAndProvenance(out, &r, expectedFunctions(out))
	natInfoChecks(&r)
	natFinish(&r)
	return r, nil
}

func natExists(p string) bool { _, err := os.Stat(p); return err == nil }

func natCountFiles(root string) int64 {
	var n int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func natAssetsIndex(out string) ([2]int64, bool) {
	data, err := os.ReadFile(filepath.Join(out, "assets_index.json")) //nolint:gosec // G304: fixed file inside the target output
	if err != nil {
		return [2]int64{}, false
	}
	var ai struct {
		Parsed int64 `json:"assets_parsed"`
		Failed int64 `json:"assets_failed"`
	}
	if json.Unmarshal(data, &ai) != nil {
		return [2]int64{}, false
	}
	return [2]int64{ai.Parsed, ai.Failed}, true
}
