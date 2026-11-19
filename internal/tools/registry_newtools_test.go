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

// Asset names from the current upstream releases (DIE-engine 4.0.0,
// aelurum/AssetStudio v0.20.0): each glob must pick exactly the intended zip.
func TestRegistryGlobsMatchCurrentAssets(t *testing.T) {
	cases := []struct {
		tool, want string
		assets     []string
	}{
		{"diec", "die_win64_4.0.0.zip", []string{"die_win32_4.0.0.zip", "die_win64_4.0.0.zip", "die_win64_qt6_4.0.0.zip", "die_winarm64_qt6_4.0.0.zip"}},
		{"assetstudiomod", "AssetStudioModCLI_net10_win64.zip", []string{"AssetStudioModCLI_net10_portable.zip", "AssetStudioModCLI_net10_win64.zip", "AssetStudioModGUI_net10_win64.zip", "AssetStudioModCLI_net9_portable.zip"}},
	}
	for _, c := range cases {
		def, _ := FindByName(c.tool)
		var in []assetInfo
		for _, n := range c.assets {
			in = append(in, assetInfo{Name: n})
		}
		got := matchAssets(in, def.AssetGlob)
		if len(got) != 1 || got[0].Name != c.want {
			t.Errorf("%s glob %q matched %v, want only %s", c.tool, def.AssetGlob, got, c.want)
		}
	}
}
