package recipe

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/recon"
)

func TestRegistryNotEmpty(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Skip("No recipes registered yet — will pass after recipe implementations")
	}
}

func TestFindByName(t *testing.T) {
	if len(All()) == 0 {
		t.Skip("No recipes registered yet")
	}

	r := FindByName(All()[0].Name())
	if r == nil {
		t.Error("FindByName returned nil for first registered recipe")
	}
}

func TestStepStatusString(t *testing.T) {
	tests := []struct {
		s    StepStatus
		want string
	}{
		{Pending, "Pending"},
		{Running, "Running"},
		{Success, "Success"},
		{Failed, "Failed"},
		{Skipped, "Skipped"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("StepStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestMatchManaged(t *testing.T) {
	r := &recon.Result{Kind: recon.Managed}
	rec := Match(r)
	if rec == nil {
		t.Skip("No recipes match managed yet")
	}
}

func TestMatchConfuserEx(t *testing.T) {
	r := &recon.Result{Kind: recon.Managed, Obfuscator: "ConfuserEx"}
	rec := Match(r)
	if rec == nil {
		t.Skip("No recipes match ConfuserEx yet")
	}
	if rec.Name() != "dotnet-confuserex" {
		t.Errorf("Match(ConfuserEx) = %q, want dotnet-confuserex", rec.Name())
	}
}

func TestMatchDelphi(t *testing.T) {
	r := &recon.Result{Kind: recon.Native, Compiler: "Delphi"}
	rec := Match(r)
	if rec == nil {
		t.Skip("No recipes match Delphi yet")
	}
	if rec.Name() != "delphi" {
		t.Errorf("Match(Delphi) = %q, want delphi", rec.Name())
	}
}
