package quality

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

func TestEveryRecipeFamilyHasCollector(t *testing.T) {
	for recipe, families := range recipeFamilies {
		for _, f := range families {
			if ForFamily(f) == nil {
				t.Errorf("recipe %s family %s has no collector", recipe, f)
			}
		}
	}
}

func TestCompanionMetadataIsNotApplicable(t *testing.T) {
	r := CollectTarget(context.Background(), "unity-il2cpp", Input{Target: runmodel.TargetRecord{InputPath: `C:\g\Game_Data\il2cpp_data\Metadata\global-metadata.dat`}, Output: t.TempDir()})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Status != "not-applicable" || r.Checks[0].Status != "not-applicable" {
		t.Fatalf("companion report = %s %+v", r.Status, r.Checks)
	}
}

func TestUnknownRecipeIsExplicitlyUnknown(t *testing.T) {
	r := CollectTarget(context.Background(), "__fixture", Input{Output: t.TempDir()})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Status != "unknown" || len(r.Warnings) == 0 {
		t.Fatalf("report = %+v", r)
	}
}

func TestUnityMonoMergesNamedDataSection(t *testing.T) {
	out := t.TempDir()
	r := CollectTarget(context.Background(), "unity-mono", Input{Target: runmodel.TargetRecord{InputPath: filepath.Join(out, "missing.dll")}, Output: out})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Family != "managed+unitydata" {
		t.Fatalf("family = %s", r.Family)
	}
	var section bool
	for _, c := range r.Checks {
		if strings.HasPrefix(c.ID, "unitydata/") {
			section = true
		}
	}
	if !section || r.Status == "pass" {
		t.Fatalf("data section missing or missing evidence passed: %s %+v", r.Status, r.Checks)
	}
}

func TestContainerReportsUnpackedPayload(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "recon.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := CollectTarget(context.Background(), "nsis", Input{Output: out})
	if r.Status != "degraded" {
		t.Fatalf("empty container status = %s", r.Status)
	}
	if err := os.MkdirAll(filepath.Join(out, "$INSTDIR"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "$INSTDIR", "game.exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = CollectTarget(context.Background(), "nsis", Input{Output: out})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Status != "not-applicable" || r.Functions.Found.Status != "not-applicable" {
		t.Fatalf("container report = %s %+v", r.Status, r.Functions)
	}
}

func TestWorstStatus(t *testing.T) {
	if worstStatus("pass", "not-applicable") != "pass" || worstStatus("pass", "unknown") != "unknown" || worstStatus("degraded", "unknown") != "degraded" {
		t.Fatal("status precedence wrong")
	}
}
