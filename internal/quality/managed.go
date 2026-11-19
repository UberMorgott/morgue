package quality

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/util"
	pe "github.com/saferwall/pe"
)

// Inventory counts definitions with bodies. No-body declarations are excluded;
// compiler-generated bodies remain included and are not assumed to emit separately.
type Inventory struct{ Types, Methods, ExcludedMethods int64 }
type Identity struct{ Token, RVA, Name, Kind string }

func ManagedInventory(path string, visit func(Identity) error) (Inventory, error) {
	f, err := pe.New(util.LongPath(path), nil)
	if err != nil {
		return Inventory{}, err
	}
	defer func() { _ = f.Close() }()
	if err = f.Parse(); err != nil {
		return Inventory{}, err
	}
	return managedRows(f, visit)
}
func managedRows(f *pe.File, visit func(Identity) error) (Inventory, error) {
	var inv Inventory
	tt, mt := f.CLR.MetadataTables[pe.TypeDef], f.CLR.MetadataTables[pe.MethodDef]
	if tt == nil || mt == nil {
		return inv, fmt.Errorf("CLR TypeDef/MethodDef tables unavailable")
	}
	types, tok := tt.Content.([]pe.TypeDefTableRow)
	methods, mok := mt.Content.([]pe.MethodDefTableRow)
	if !tok || !mok {
		return inv, fmt.Errorf("unsupported CLR metadata table representation")
	}
	heap := f.CLR.MetadataStreams["#Strings"]
	str := func(off uint32) string {
		if uint64(off) >= uint64(len(heap)) {
			return ""
		}
		s := heap[off:]
		if i := strings.IndexByte(string(s), 0); i >= 0 {
			s = s[:i]
		}
		return string(s)
	}
	for i, t := range types {
		name := str(t.TypeName)
		if name == "<Module>" {
			continue
		}
		inv.Types++
		if ns := str(t.TypeNamespace); ns != "" {
			name = ns + "." + name
		}
		if visit != nil {
			if err := visit(Identity{Token: fmt.Sprintf("0x%08x", 0x02000001+i), Name: name, Kind: "type"}); err != nil {
				return inv, err
			}
		}
	}
	for i, m := range methods {
		if m.RVA == 0 || m.Flags&0x2400 != 0 || m.ImplFlags&0x1003 != 0 {
			inv.ExcludedMethods++
			continue
		}
		inv.Methods++
		if visit != nil {
			if err := visit(Identity{Token: fmt.Sprintf("0x%08x", 0x06000001+i), RVA: fmt.Sprintf("0x%x", m.RVA), Name: str(m.Name), Kind: "method"}); err != nil {
				return inv, err
			}
		}
	}
	return inv, nil
}

type Managed struct{}

func init() { Register("managed", Managed{}) }
func unknownMetric(reason string) runmodel.Metric {
	return runmodel.Metric{Status: "unknown", Basis: reason, Evidence: []runmodel.Evidence{}}
}
func countMetric(n int64, status, basis string, ev []runmodel.Evidence) runmodel.Metric {
	if ev == nil {
		ev = []runmodel.Evidence{}
	}
	return runmodel.Metric{Value: &n, Status: status, Basis: basis, Evidence: ev}
}
func unknownCoverage(reason string) runmodel.Coverage {
	return runmodel.Coverage{Found: unknownMetric(reason), Emitted: unknownMetric(reason)}
}
func emptyFamily(family string) runmodel.QualityReport {
	u := unknownMetric("No supported diagnostic evidence collected")
	return runmodel.QualityReport{SchemaVersion: 1, Family: family, Status: "unknown", Functions: unknownCoverage("No stable body mapping"), Types: unknownCoverage("No stable type mapping"), Names: runmodel.NamesNotApplicable("managed/JVM names come from bytecode metadata (original or obfuscator-renamed), not decompiler defaults; renamed identifiers are counted under residualObfuscation"), DecompilerErrors: u, UnknownResultTypes: u, UnresolvedReferences: u, ResidualObfuscation: u, StringDecryption: unknownCoverage("Candidate call-site inventory unavailable; readable text and absent PUA do not establish decryption"), Warnings: []string{}, Checks: []runmodel.QualityCheck{}}
}
func (Managed) Collect(ctx context.Context, in Input) (runmodel.QualityReport, error) {
	r := emptyFamily("managed")
	inv, err := ManagedInventory(in.Target.InputPath, nil)
	if err == nil {
		r.Functions.Found = countMetric(inv.Methods, "measured", fmt.Sprintf("CLR MethodDef with IL bodies; excluded %d abstract, P/Invoke, runtime/native or RVA=0 methods; compiler-generated bodies included", inv.ExcludedMethods), nil)
		r.Types.Found = countMetric(inv.Types, "measured", "CLR TypeDef excluding <Module>; compiler-generated types included", nil)
	} else {
		r.Warnings = append(r.Warnings, "Managed inventory unavailable: "+err.Error())
	}
	if err := collectSource(ctx, in, ".cs", &r); err != nil {
		return r, err
	}
	collectInvocationDiagnostics(in, "ilspycmd", &r)
	// Reclassify the final transformed module when recipes recorded it. Never
	// interpret lack of a detected heuristic as proof of deobfuscation.
	module := in.Target.InputPath
	if b, e := os.ReadFile(filepath.Join(in.Output, "managed-module.json")); e == nil {
		var m struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(b, &m) == nil && m.Path != "" {
			module = m.Path
		}
	}
	if rr, e := recon.Classify(ctx, module); e == nil && rr.Kind != recon.Unknown {
		n := residualIndicators(rr)
		r.ResidualObfuscation = countMetric(n, "estimated", "Recon heuristic indicators in final transformed module; zero is not proof of removal", nil)
	}
	logs := filepath.Join(in.Output, "logs")
	_ = filepath.WalkDir(logs, func(path string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.Contains(d.Name(), "cfxstrings") || filepath.Ext(path) != ".log" {
			return nil //nolint:nilerr // unreadable or unrelated log entries are skipped, not fatal
		}
		b, er := os.ReadFile(path)
		if er == nil {
			c := stringCoverage(string(b), relative(in.Output, path))
			if c.Found.Status == "measured" {
				r.StringDecryption = c
			}
		}
		return nil
	})
	finishFamily(&r)
	return r, nil
}

func residualIndicators(r recon.Result) int64 {
	var n int64
	// Feature strings such as "Resources" occur in clean framework assemblies.
	// Attribute feature hits only when recon actually classified an obfuscator.
	if r.Obfuscator != "" {
		n = 1 + int64(len(r.ObfuscatorFeatures))
	}
	if r.Packed {
		n++
	}
	return n
}

var csTypeDecl = regexp.MustCompile(`\b(class|struct|interface|enum|delegate|record)\s+[\pL_@]`)
var csMethodDecl = regexp.MustCompile(`(?m)^\s*(?:(?:public|private|protected|internal|static|virtual|override|sealed|async|unsafe|extern|new|partial)\s+)*[\w<>,.\[\]?]+\s+[\w@]+\s*\([^;]*\)\s*(?:\{|=>|$)`)
var javaTypeDecl = regexp.MustCompile(`\b(class|interface|enum|record)\s+[\pL_$]`)
var javaMethodDecl = regexp.MustCompile(`(?m)^\s*(?:(?:public|private|protected|static|final|synchronized|native|abstract|default)\s+)*[\w<>,.\[\]?]+\s+[\w$]+\s*\([^;]*\)\s*(?:\{|$)`)

func relative(root, path string) string { r, _ := filepath.Rel(root, path); return filepath.ToSlash(r) }
func collectSource(ctx context.Context, in Input, ext string, r *runmodel.QualityReport) error {
	var files, methods, types int64
	counts := map[string]int64{}
	evidence := map[string][]runmodel.Evidence{}
	err := filepath.WalkDir(filepath.Join(in.Output, "src"), func(path string, d fs.DirEntry, e error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			return e
		}
		if d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer func() { _ = f.Close() }()
		info, e := f.Stat()
		if e != nil {
			return e
		}
		if info.Size() == 0 {
			return nil
		}
		files++
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 65536), 16*1024*1024)
		var line int64
		for scan.Scan() {
			line++
			s := scan.Text()
			tr, mr := csTypeDecl, csMethodDecl
			if ext == ".java" {
				tr, mr = javaTypeDecl, javaMethodDecl
			}
			types += int64(len(tr.FindAllStringIndex(s, -1)))
			methods += int64(len(mr.FindAllStringIndex(s, -1)))
			lower := strings.ToLower(s)
			if !strings.HasPrefix(strings.TrimSpace(lower), "//") {
				continue
			}
			for id, match := range map[string]bool{"types.unknown-result": strings.Contains(lower, "unknown result type"), "references.unresolved": strings.Contains(lower, "could not resolve") || strings.Contains(lower, "failed to resolve assembly") || strings.Contains(lower, "assemblyresolutionexception"), "decompiler.errors": strings.Contains(lower, "error decompiling") || strings.Contains(lower, "couldn't be decompiled") || strings.Contains(lower, "method could not be decompiled")} {
				if match {
					counts[id]++
					if len(evidence[id]) < 64 {
						ln := line
						evidence[id] = append(evidence[id], runmodel.Evidence{Path: relative(in.Output, path), Line: &ln, Detail: strings.TrimSpace(s)})
					}
				}
			}
		}
		return scan.Err()
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	r.Functions.Emitted = countMetric(methods, "estimated", "Regex source declarations; excludes no known identity universe, not exact method bodies", nil)
	r.Types.Emitted = countMetric(types, "estimated", "Regex source declarations; nested/generated forms may differ from bytecode", nil)
	status := "pass"
	if files == 0 {
		status = "fail"
	}
	r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "output.present", Status: status, Message: fmt.Sprintf("%d nonempty %s files", files, ext), Evidence: []runmodel.Evidence{}})
	// Counts are exact occurrences of supported diagnostics, not a promise that
	// an unknown/new tool format emitted no other errors.
	for id, dest := range map[string]*runmodel.Metric{"types.unknown-result": &r.UnknownResultTypes, "references.unresolved": &r.UnresolvedReferences, "decompiler.errors": &r.DecompilerErrors} {
		if counts[id] > 0 {
			*dest = countMetric(counts[id], "measured", "Supported diagnostic occurrences in emitted source; repeated messages counted separately", evidence[id])
		}
	}
	return nil
}
func finishFamily(r *runmodel.QualityReport) {
	for id, m := range map[string]runmodel.Metric{"decompiler.errors": r.DecompilerErrors, "types.unknown-result": r.UnknownResultTypes, "references.unresolved": r.UnresolvedReferences, "obfuscation.residual": r.ResidualObfuscation} {
		status := "unknown"
		if m.Value != nil {
			status = "pass"
			if *m.Value > 0 {
				status = "warn"
			}
		}
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: id, Status: status, Message: m.Basis, Evidence: m.Evidence})
	}
	for id, c := range map[string]runmodel.Coverage{"functions.coverage": r.Functions, "types.coverage": r.Types, "strings.decryption": r.StringDecryption} {
		status := "unknown"
		if c.Ratio != nil {
			status = "pass"
			if *c.Ratio < 1 {
				status = "warn"
			}
		}
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: id, Status: status, Message: c.Emitted.Basis, Evidence: c.Emitted.Evidence})
	}
	r.Status = "unknown"
	for _, c := range r.Checks {
		if c.Status == "warn" || c.Status == "fail" {
			r.Status = "degraded"
			break
		}
	}
	sort.Slice(r.Checks, func(i, j int) bool { return r.Checks[i].ID < r.Checks[j].ID })
}

// Invocation counts are exact tool attempts, not failed-method counts. Source
// diagnostics and raw log diagnostics deliberately name their counting basis.
func collectInvocationDiagnostics(in Input, tool string, r *runmodel.QualityReport) {
	var attempts, failed int64
	counts := map[string]int64{}
	ev := map[string][]runmodel.Evidence{}
	_ = filepath.WalkDir(filepath.Join(in.Output, "logs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), tool+"-") || !strings.HasSuffix(path, ".log.json") {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		var meta struct {
			Version  int    `json:"schemaVersion"`
			Tool     string `json:"tool"`
			ExitCode *int   `json:"exitCode"`
		}
		if json.Unmarshal(b, &meta) != nil || meta.Version != 1 || meta.Tool != tool || meta.ExitCode == nil {
			return nil
		}
		attempts++
		if *meta.ExitCode != 0 {
			failed++
		}
		logPath := strings.TrimSuffix(path, ".json")
		f, e := os.Open(logPath)
		if e != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 65536), 16*1024*1024)
		var line int64
		for s.Scan() {
			line++
			lower := strings.ToLower(s.Text())
			for id, match := range map[string]bool{"types.unknown-result": strings.Contains(lower, "unknown result type"), "references.unresolved": strings.Contains(lower, "could not resolve") || strings.Contains(lower, "failed to resolve assembly") || strings.Contains(lower, "assemblyresolutionexception"), "decompiler.errors": strings.Contains(lower, "error decompiling") || strings.Contains(lower, "couldn't be decompiled") || strings.Contains(lower, "method could not be decompiled") || strings.Contains(lower, "exception while decompiling")} {
				if match {
					counts[id]++
					if len(ev[id]) < 64 {
						ln := line
						ev[id] = append(ev[id], runmodel.Evidence{Path: relative(in.Output, logPath), Line: &ln, Detail: strings.TrimSpace(s.Text())})
					}
				}
			}
		}
		return nil
	})
	if attempts > 0 {
		status := "pass"
		if failed > 0 {
			status = "warn"
		}
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "decompiler.invocations", Status: status, Message: fmt.Sprintf("%d completed tool invocations, %d failed; attempts include fallbacks and are not method coverage", attempts, failed), Evidence: []runmodel.Evidence{}})
	}
	for id, dest := range map[string]*runmodel.Metric{"types.unknown-result": &r.UnknownResultTypes, "references.unresolved": &r.UnresolvedReferences, "decompiler.errors": &r.DecompilerErrors} {
		if counts[id] > 0 {
			n := counts[id]
			all := ev[id]
			if dest.Value != nil {
				n += *dest.Value
				all = append(dest.Evidence, all...)
			}
			*dest = countMetric(n, "measured", "Supported diagnostic occurrences in source and raw logs; not distinct failing methods; missing/new formats remain unknown", all)
		}
	}
}

var sitePattern = regexp.MustCompile(`^SITE:(0x[0-9a-f]+):([0-9]+):(replaced|residual)$`)

func stringCoverage(log, path string) runmodel.Coverage {
	c := unknownCoverage("No complete supported call-site inventory; aggregate rewrites or PUA scans cannot prove coverage")
	ids := map[string]string{}
	var total *int64
	valid := true
	for line := range strings.SplitSeq(log, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SITES:") {
			var n int64
			if _, err := fmt.Sscanf(line, "SITES:%d", &n); err == nil && n >= 0 {
				total = &n
			} else {
				valid = false
			}
		}
		if strings.HasPrefix(line, "SITE:") {
			m := sitePattern.FindStringSubmatch(line)
			if len(m) != 4 {
				valid = false
				continue
			}
			id := m[1] + ":" + m[2]
			if _, ok := ids[id]; ok {
				valid = false
			}
			ids[id] = m[3]
		}
	}
	if !valid || total == nil || *total != int64(len(ids)) {
		return c
	}
	var emitted int64
	for _, status := range ids {
		if status == "replaced" {
			emitted++
		}
	}
	ev := []runmodel.Evidence{{Path: path, Detail: "Stable MethodDef token + original call index; limited to detected helper-supported decryptor sites"}}
	c.Found = countMetric(*total, "measured", "Detected candidate call sites for supported decryptor shape; not all encryption schemes", ev)
	c.Emitted = countMetric(emitted, "measured", "Successful IL replacements in the same detected call-site set; plaintext correctness remains heuristic", ev)
	c.Comparable = true
	if *total > 0 {
		ratio := float64(emitted) / float64(*total)
		c.Ratio = &ratio
	}
	return c
}
