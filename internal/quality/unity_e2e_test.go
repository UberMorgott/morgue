package quality

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// TestUnityRealWorkspace evaluates the Unity collectors against a real local
// workspace (MORGUE_E2E_WORKSPACE=<output root>). Real-game evidence stays
// local; CI never sets the variable.
func TestUnityRealWorkspace(t *testing.T) {
	root := os.Getenv("MORGUE_E2E_WORKSPACE")
	if root == "" || testing.Short() {
		t.Skip("set MORGUE_E2E_WORKSPACE and run without -short")
	}
	var ws struct {
		Targets []runmodel.TargetRecord `json:"targets"`
	}
	b, err := os.ReadFile(filepath.Join(root, "morgue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &ws); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tr := range ws.Targets {
		dir := filepath.Join(root, filepath.FromSlash(tr.Output))
		for family, file := range map[string]string{"il2cpp": IL2CPPEvidenceFile, "unitydata": UnityDataEvidenceFile} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(file))); err != nil {
				continue
			}
			r, err := ForFamily(family).Collect(ctx, Input{Target: tr, Output: dir})
			if err != nil {
				t.Fatal(err)
			}
			il2cppValidate(t, r)
			out, _ := json.Marshal(r)
			t.Logf("%s %s %s", tr.RelativeInput, family, out)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(IL2CPPEvidenceFile))); err != nil {
			continue
		}
		// Provenance sample: every 997th method row must resolve to its exact
		// declaration line preceded by the same Token attribute.
		var rows, checked int64
		_, err := ScanIL2CPPStubs(ctx, filepath.Join(dir, "src"), dir, func(m StubMember) error {
			if m.Kind != "method" {
				return nil
			}
			if rows++; rows%997 != 0 {
				return nil
			}
			checked++
			lines := unityReadLines(t, filepath.Join(dir, filepath.FromSlash(m.Path)), m.Line)
			if strings.TrimSuffix(strings.TrimSpace(lines[m.Line-1]), "{") != m.Name {
				t.Errorf("%s:%d does not hold %q", m.Path, m.Line, m.Name)
			}
			tok := false
			for i := m.Line - 2; i >= 0 && i >= m.Line-6; i-- {
				if m2 := il2cppTokenAttr.FindStringSubmatch(strings.TrimSpace(lines[i])); len(m2) > 1 && il2cppHex(m2[1]) == m.Token {
					tok = true
				}
			}
			if !tok {
				t.Errorf("%s:%d token %s not found above declaration", m.Path, m.Line, m.Token)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("provenance sample: %d method rows, %d sampled links resolved", rows, checked)
	}
}

func unityReadLines(t *testing.T, path string, upto int64) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var out []string
	for sc.Scan() && int64(len(out)) < upto {
		out = append(out, sc.Text())
	}
	return out
}
