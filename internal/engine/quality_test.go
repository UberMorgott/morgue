package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

const provRecipeName = "__m4_provenance_fixture"

type provRecipe struct{ terminalRecipe }

func (provRecipe) Name() string { return provRecipeName }
func (provRecipe) Execute(ctx *recipe.Context) error {
	if ctx.Provenance == nil {
		return os.ErrInvalid
	}
	addr := "0x140001000"
	if err := ctx.Provenance.Write(runmodel.ProvenanceRecord{ID: "m:fn:" + addr, ArtifactPath: "src/a.c", ArtifactKind: runmodel.FidelityPseudocode,
		Module: "ok.dll", Address: &addr, NameSource: "unknown", Confidence: "unknown", Producer: runmodel.ProvenanceProducer{Tool: "fixture", Version: "1", StepID: "native.decompile"}}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ctx.Output, "fixture.txt"), []byte("completed"), 0644)
}

func init() { recipe.Register(provRecipe{}) }

func TestEveryRegisteredRecipeHasQualityFamily(t *testing.T) {
	for _, r := range recipe.All() {
		if strings.HasPrefix(r.Name(), "__") || r.Name() == "stub" || r.Name() == "reference-capture" {
			continue // engine test fixtures
		}
		if len(quality.FamiliesForRecipe(r.Name())) == 0 {
			t.Errorf("recipe %s has no quality collector family", r.Name())
		}
	}
}

// The engine binds provenance.ndjson per target (filling targetId and
// inputSha256), measures quality after execution, writes quality.json and
// projects both into morgue.json.
func TestRunBindsProvenanceAndPublishesQuality(t *testing.T) {
	input := terminalInput(t, "ok.dll")
	root := t.TempDir()
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: root, Recipe: provRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := r.Execute(context.Background(), nil, nil); err != nil || res.Outcome != OutcomeSuccess {
		t.Fatalf("run = %+v, %v", res, err)
	}
	b, err := os.ReadFile(filepath.Join(root, runmodel.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	var ws runmodel.Workspace
	if err := json.Unmarshal(b, &ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Targets) != 1 {
		t.Fatalf("targets = %d", len(ws.Targets))
	}
	tg := ws.Targets[0]
	if tg.Quality == nil || tg.Quality.Status != "unknown" || tg.Quality.Validate() != nil {
		t.Fatalf("fixture recipe without collector must publish an explicit unknown report: %+v", tg.Quality)
	}
	if tg.Provenance == nil || *tg.Provenance != tg.Output+"/"+quality.ProvenanceFile {
		t.Fatalf("provenance = %v (output %s)", tg.Provenance, tg.Output)
	}
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(*tg.Provenance)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	rows := 0
	for sc.Scan() {
		var rec runmodel.ProvenanceRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		rows++
		if rec.TargetID != tg.ID || tg.SHA256 == nil || rec.InputSHA256 != *tg.SHA256 || rec.SchemaVersion != 1 || rec.Producer.Args == nil {
			t.Fatalf("row identity not bound: %+v (target %s)", rec, tg.ID)
		}
	}
	if rows != 1 {
		t.Fatalf("provenance rows = %d", rows)
	}
	kinds := map[string]bool{}
	for _, a := range ws.Artifacts {
		kinds[filepath.Base(a.Path)+"|"+a.Kind] = true
	}
	if !kinds["quality.json|metadata"] || !kinds["provenance.ndjson|index"] {
		t.Fatalf("quality/provenance artifacts not recorded: %v", kinds)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(tg.Output), quality.QualityFile)); err != nil {
		t.Fatal(err)
	}
}

// A target that streamed no provenance keeps provenance null and leaves no
// empty stream behind.
func TestRunWithoutProvenanceRowsLeavesNoStream(t *testing.T) {
	input := terminalInput(t, "ok.dll")
	root := t.TempDir()
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: root, Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, runmodel.ManifestPath))
	var ws runmodel.Workspace
	if err := json.Unmarshal(b, &ws); err != nil {
		t.Fatal(err)
	}
	tg := ws.Targets[0]
	if tg.Provenance != nil {
		t.Fatalf("provenance = %v", *tg.Provenance)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(tg.Output), quality.ProvenanceFile)); !os.IsNotExist(err) {
		t.Fatalf("empty provenance stream left behind: %v", err)
	}
}
