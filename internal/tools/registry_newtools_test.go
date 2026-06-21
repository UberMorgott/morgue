package tools

import "testing"

func TestInspectorAndRipperRegistered(t *testing.T) {
	insp, ok := FindByName("il2cppinspector")
	if !ok {
		t.Fatal("il2cppinspector not registered")
	}
	if insp.Binary != "Il2CppInspector.Redux.CLI.exe" {
		t.Fatalf("inspector binary = %q", insp.Binary)
	}
	if len(insp.RuntimeDeps) == 0 || insp.RuntimeDeps[0] != RuntimeAspNet {
		t.Fatalf("inspector must depend on RuntimeAspNet, got %v", insp.RuntimeDeps)
	}
	rip, ok := FindByName("assetripper")
	if !ok {
		t.Fatal("assetripper not registered")
	}
	if rip.Binary != "AssetRipper.GUI.Free.exe" {
		t.Fatalf("ripper binary = %q", rip.Binary)
	}
	if len(rip.RuntimeDeps) != 0 {
		t.Fatalf("assetripper is self-contained, must have no RuntimeDeps, got %v", rip.RuntimeDeps)
	}
}
