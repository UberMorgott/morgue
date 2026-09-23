package tools

// Registry contains all known tool definitions.
var Registry = []ToolDef{
	{
		Name:        "diec",
		Description: "Detect It Easy (console)",
		Category:    CategoryDetector,
		Method:      MethodGitHubRelease,
		Repo:        "horsicq/DIE-engine",
		AssetGlob:   "die_win64_portable_*",
		Binary:      "diec.exe",
	},
	{
		Name:        "ilspycmd",
		Description: "ILSpy command-line decompiler",
		Category:    CategoryDecompiler,
		Method:      MethodNuGet,
		DotnetID:    "ilspycmd",
		Binary:      "ilspycmd.dll",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "strings",
		Description: "Sysinternals Strings utility",
		Category:    CategoryAnalyzer,
		Method:      MethodDirectURL,
		URL:         "https://download.sysinternals.com/files/Strings.zip",
		Binary:      "strings64.exe",
		Optional:    true,
	},
	{
		Name:        "de4dot-cex",
		Description: "de4dot fork for ConfuserEx",
		Category:    CategoryDeobfuscator,
		Method:      MethodGitHubRelease,
		Repo:        "ViRb3/de4dot-cex",
		AssetGlob:   "de4dot-cex*",
		Binary:      "de4dot.exe",
	},
	{
		Name:        "ghidra",
		Description: "NSA Ghidra reverse engineering framework",
		Category:    CategoryDecompiler,
		Method:      MethodGitHubRelease,
		Repo:        "NationalSecurityAgency/ghidra",
		AssetGlob:   "ghidra_*_PUBLIC_*.zip",
		Binary:      "ghidraRun.bat",
		Optional:    true,
		RuntimeDeps: []RuntimeKind{RuntimeJava},
	},
	{
		// Ghidra extension recovering .NET NativeAOT metadata (method tables,
		// vtables, frozen strings). Upstream ships builds only up to Ghidra
		// 12.0.1; Ghidra 12.1.x loads that build as-is (verified headless).
		Name:        "ghidra-nativeaot",
		Description: "Ghidra .NET NativeAOT metadata recovery extension",
		Category:    CategoryDecompiler,
		Method:      MethodGitHubRelease,
		Repo:        "washi1337/ghidra-nativeaot",
		AssetGlob:   "ghidra_12.0.1_PUBLIC_*_ghidra-nativeaot.zip",
		Version:     "v1.1.0",
		Binary:      "ghidra-nativeaot.jar",
		Optional:    true,
	},
	{
		Name:        "nofuserex",
		Description: "ConfuserEx anti-tamper remover",
		Category:    CategoryDeobfuscator,
		Method:      MethodDirectURL,
		DownloadURLs: []string{
			"https://github.com/timnboys/NoFuserEx/releases/download/1.0.11/NoFuserEx.exe",
			"https://github.com/timnboys/NoFuserEx/releases/download/1.0.11/dnlib.dll",
		},
		Binary:      "NoFuserEx.exe",
		Version:     "1.0.11",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "confuserex-killer",
		Description: "ConfuserEx unpacker & deobfuscator",
		Category:    CategoryUnpacker,
		Method:      MethodDirectURL,
		URL:         "https://github.com/wwh1004/ConfuserExTools/releases/download/v0.1.0.0-beta/ConfuserExTools.zip",
		Binary:      "ConfuserExKiller.dll",
		Version:     "v0.1.0.0-beta",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "proxycall-remover",
		Description: "ConfuserEx proxy call remover",
		Category:    CategoryDeobfuscator,
		Method:      MethodDirectURL,
		URL:         "https://github.com/wwh1004/ConfuserExTools/releases/download/v0.1.0.0-beta/ConfuserExTools.zip",
		Binary:      "ProxyKiller.dll",
		Version:     "v0.1.0.0-beta",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "idr",
		Description: "Interactive Delphi Reconstructor",
		Category:    CategoryDecompiler,
		Method:      MethodGitHubRelease,
		Repo:        "crypto2011/IDR",
		AssetGlob:   "Idr.exe",
		Binary:      "Idr.exe",
		Optional:    true,
	},
	{
		Name:        "goresym",
		Description: "Go symbol and type parser",
		Category:    CategoryAnalyzer,
		Method:      MethodGitHubRelease,
		Repo:        "mandiant/GoReSym",
		AssetGlob:   "GoReSym*Windows*",
		Binary:      "GoReSym.exe",
		Optional:    true,
	},
	{
		Name:        "retoc",
		Description: "Unreal Engine PAK/IoStore extractor",
		Category:    CategoryExtractor,
		Method:      MethodGitHubRelease,
		Repo:        "trumank/retoc",
		AssetGlob:   "*windows*",
		Binary:      "retoc.exe",
		Optional:    true,
	},
	{
		Name:        "il2cppdumper",
		Description: "Unity IL2CPP metadata extractor",
		Category:    CategoryExtractor,
		Method:      MethodGitHubRelease,
		Repo:        "Perfare/Il2CppDumper",
		AssetGlob:   "Il2CppDumper-win-*",
		Binary:      "Il2CppDumper.exe",
	},
	{
		Name:        "il2cppinspector",
		Description: "Il2CppInspectorRedux — modern IL2CPP dumper (metadata v16–v106)",
		Category:    CategoryExtractor,
		Method:      MethodGitHubRelease,
		Repo:        "LukeFZ/Il2CppInspectorRedux",
		AssetGlob:   "Il2CppInspectorRedux.CLI-win-x64*",
		Binary:      "Il2CppInspector.Redux.CLI.exe",
		Version:     "2026.2",
		RuntimeDeps: []RuntimeKind{RuntimeAspNet},
	},
	{
		Name:        "assetripper",
		Description: "AssetRipper Free — Unity asset/ScriptableObject extractor (headless)",
		Category:    CategoryExtractor,
		Method:      MethodGitHubRelease,
		Repo:        "AssetRipper/AssetRipper",
		AssetGlob:   "AssetRipper_win_x64*",
		Binary:      "AssetRipper.GUI.Free.exe",
		Version:     "2.0.0",
		Optional:    false,
	},
	{
		Name:        "assetstudiomod",
		Description: "AssetStudioMod CLI — Unity asset inventory/extractor",
		Category:    CategoryExtractor,
		Method:      MethodGitHubRelease,
		Repo:        "aelurum/AssetStudio",
		// Self-contained net8 build (no .NET runtime dep). The exe is nested one
		// level deep inside the zip (AssetStudioModCLI_net8_portable/), which
		// Resolve() finds via findBinaryRecursive on Binary by name.
		AssetGlob: "AssetStudioModCLI_net8_portable*",
		Binary:    "AssetStudioModCLI.exe",
		Version:   "v0.19.0",
		Optional:  true,
	},
	{
		Name:        "vineflower",
		Description: "Vineflower — Java bytecode decompiler (.jar/.class → .java)",
		Category:    CategoryDecompiler,
		Method:      MethodDirectURL,
		// DownloadURLs (not URL): a .jar is a zip, and the single-URL path would
		// try to extract it; the multi-URL path keeps non-.zip files as-is.
		DownloadURLs: []string{
			"https://github.com/Vineflower/vineflower/releases/download/1.12.0/vineflower-1.12.0.jar",
		},
		Binary:      "vineflower-1.12.0.jar",
		Version:     "1.12.0",
		RuntimeDeps: []RuntimeKind{RuntimeJava},
	},
}

// FindByName looks up a tool definition by name.
func FindByName(name string) (ToolDef, bool) {
	for _, t := range Registry {
		if t.Name == name {
			return t, true
		}
	}
	return ToolDef{}, false
}

// ByCategory returns all tools matching the given category.
func ByCategory(cat Category) []ToolDef {
	var result []ToolDef
	for _, t := range Registry {
		if t.Category == cat {
			result = append(result, t)
		}
	}
	return result
}
