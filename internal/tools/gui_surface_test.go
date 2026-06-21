package tools

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestCheckAllIncludesNewTools(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	statuses := m.CheckAll()
	seen := map[string]bool{}
	for _, s := range statuses {
		seen[s.Name] = true
	}
	for _, want := range []string{"il2cppinspector", "assetripper", "il2cppdumper"} {
		if !seen[want] {
			t.Fatalf("CheckAll (Tools page source) missing %q", want)
		}
	}
}

func TestCheckRuntimesIncludesAspNet(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	statuses := m.CheckRuntimes()
	seen := map[RuntimeKind]bool{}
	for _, s := range statuses {
		seen[s.Kind] = true
	}
	if !seen[RuntimeAspNet] {
		t.Fatalf("CheckRuntimes (Tools page runtimes panel) missing RuntimeAspNet")
	}
}
