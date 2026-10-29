package recipe

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// ilspyCommandArgs keeps reference resolution identical in project, flat and
// per-type mode. Related assemblies include raw extracted siblings, whose
// directories remain relevant after a child is rewritten into intermediate/.
func ilspyCommandArgs(ctx *Context, target string, options []string, related ...string) []string {
	args := append([]string(nil), options...)
	if ctx.Config != nil && ctx.Config.CSharpLanguageVersion != "" && ctx.Config.CSharpLanguageVersion != "Auto" {
		args = append(args, "--languageversion", ctx.Config.CSharpLanguageVersion)
	}
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return
		}
		seen[key] = true
		args = append(args, "-r", abs)
	}
	for _, dir := range ctx.ReferencePaths {
		add(dir)
	}
	if ctx.Target != "" {
		add(filepath.Dir(ctx.Target))
	}
	add(filepath.Dir(target))
	for _, assembly := range related {
		if assembly != "" {
			add(filepath.Dir(assembly))
		}
	}
	return append(args, target)
}

// saveToolResult preserves raw diagnostics before any parser consumes them.
// Names are unique per invocation so project/flat/per-type attempts all survive.
func saveToolResult(ctx *Context, tool string, result *util.CmdResult, arguments ...[]string) {
	if result == nil {
		return
	}
	dir := filepath.Join(ctx.Output, "logs")
	err := os.MkdirAll(dir, 0755)
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(dir, tool+"-*.log")
		if err == nil {
			_, err = fmt.Fprintf(f, "%s\n%s", result.Stdout, result.Stderr)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				args := []string{}
				if len(arguments) > 0 {
					args = append(args, arguments[0]...)
				}
				data, _ := json.Marshal(struct {
					Version  int      `json:"schemaVersion"`
					Tool     string   `json:"tool"`
					ExitCode int      `json:"exitCode"`
					Args     []string `json:"args"`
				}{1, tool, result.ExitCode, args})
				err = os.WriteFile(f.Name()+".json", data, 0644)
			}
		}
	}
	if err != nil && ctx.Log != nil {
		ctx.Log <- "[" + tool + "] quality evidence write failed: " + err.Error()
	}
}

func runILSpy(ctx *Context, binary string, args []string) (*util.CmdResult, error) {
	r, err := util.RunCmd(ctx.Ctx, binary, args, "")
	saveToolResult(ctx, "ilspycmd", r, args)
	return r, err
}

func selectedProducer(ctx *Context, tool, step string, args []string) runmodel.ProvenanceProducer {
	p := runmodel.ProvenanceProducer{Tool: tool, Version: "unknown", StepID: step, Args: append([]string{}, args...)}
	root := ctx.SharedOut
	if root == "" {
		root = filepath.Dir(ctx.Output)
	}
	if b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(runmodel.RunToolsLockPath))); e == nil {
		var lock tools.RunToolsLock
		if json.Unmarshal(b, &lock) == nil {
			for _, id := range lock.Tools {
				if id.Name == tool {
					p.Version = id.Version
					break
				}
			}
		}
	}
	return p
}

// The engine-bound sink enriches identity too. Persisted metadata indexes need
// that identity before serialization, so read the already-discovered target.
func provenanceTargetID(ctx *Context) string {
	root := ctx.SharedOut
	if root == "" {
		root = filepath.Dir(ctx.Output)
	}
	b, err := os.ReadFile(filepath.Join(root, runmodel.ManifestPath))
	if err != nil {
		return ""
	}
	var workspace runmodel.Workspace
	if json.Unmarshal(b, &workspace) != nil {
		return ""
	}
	for _, target := range workspace.Targets {
		if strings.EqualFold(filepath.Clean(target.InputPath), filepath.Clean(ctx.Target)) {
			return target.ID
		}
	}
	return ""
}

// recordManagedProducts emits exact input identities as metadata, separately
// from decompiled files whose declarations have no stable ILSpy token mapping.
func recordManagedProducts(ctx *Context, module, src string, args []string, targetID string) error {
	if targetID == "" {
		targetID = provenanceTargetID(ctx)
	}
	hash, err := util.SHA256File(module)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}{module, hash})
	if err = os.WriteFile(filepath.Join(ctx.Output, "managed-module.json"), b, 0644); err != nil {
		return err
	}
	path := filepath.Join(ctx.Output, "managed-identities.ndjson")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	producer := selectedProducer(ctx, "ilspycmd", "managed.decompile", args)
	_, inventoryErr := quality.ManagedInventory(module, func(id quality.Identity) error {
		token, name := id.Token, id.Name
		r := runmodel.ProvenanceRecord{SchemaVersion: 1, ID: hash + ":" + token, TargetID: targetID, InputSHA256: hash, Module: filepath.Base(module), ArtifactPath: "managed-identities.ndjson", ArtifactKind: "metadata", Token: &token, OriginalName: &name, NameSource: "metadata", Confidence: "high", Evidence: []runmodel.Evidence{}, Producer: runmodel.ProvenanceProducer{Tool: "morgue", Version: "unknown", StepID: "managed.inventory", Args: []string{}}}
		if id.RVA != "" {
			rva := id.RVA
			r.RVA = &rva
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
		if ctx.Provenance != nil {
			return ctx.Provenance.Write(r)
		}
		return nil
	})
	closeErr := f.Close()
	if inventoryErr != nil {
		return inventoryErr
	}
	if closeErr != nil {
		return closeErr
	}
	if ctx.Artifacts != nil {
		if err := ctx.Artifacts.Record(path, "metadata", "managed.inventory", "morgue", false); err != nil {
			return err
		}
	}
	return recordSourceProducts(ctx, src, module, hash, targetID, producer, ".cs")
}

func recordSourceProducts(ctx *Context, src, module, hash, targetID string, producer runmodel.ProvenanceProducer, ext string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		rel, err := filepath.Rel(ctx.Output, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ctx.Artifacts != nil {
			if err := ctx.Artifacts.Record(path, "decompiled-source", producer.StepID, producer.Tool, false); err != nil {
				return err
			}
		}
		if ctx.Provenance == nil {
			return nil
		}
		detail := "Decompiler output; source declarations have no stable bytecode identity mapping"
		if len(producer.Args) == 0 {
			detail += "; mixed fallback parameters are retained in logs/*.log.json"
		}
		return ctx.Provenance.Write(runmodel.ProvenanceRecord{SchemaVersion: 1, ID: hash + ":" + rel, TargetID: targetID, InputSHA256: hash, Module: filepath.Base(module), ArtifactPath: rel, ArtifactKind: "decompiled-source", NameSource: "unknown", Confidence: "unknown", Evidence: []runmodel.Evidence{{Path: rel, Detail: detail}}, Producer: producer})
	})
}

func qualityWarning(ctx *Context, err error) {
	if err != nil && ctx.Log != nil {
		ctx.Log <- "[quality] " + err.Error()
	}
}

func countNonemptySource(dir, ext string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ext {
			if info, e := d.Info(); e == nil && info.Size() > 0 {
				count++
			}
		}
		return nil
	})
	return count
}
