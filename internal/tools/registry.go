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
		Name:        "nofuserex",
		Description: "ConfuserEx anti-tamper remover",
		Category:    CategoryDeobfuscator,
		Method:      MethodDirectURL,
		DownloadURLs: []string{
			"https://github.com/timnboys/NoFuserEx/releases/download/1.0.11/NoFuserEx.exe",
			"https://github.com/timnboys/NoFuserEx/releases/download/1.0.11/dnlib.dll",
		},
		Binary:      "NoFuserEx.exe",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "confuserex-killer",
		Description: "ConfuserEx unpacker & deobfuscator",
		Category:    CategoryUnpacker,
		Method:      MethodDirectURL,
		URL:         "https://github.com/wwh1004/ConfuserExTools/releases/download/v0.1.0.0-beta/ConfuserExTools.zip",
		Binary:      "ConfuserExKiller.dll",
		RuntimeDeps: []RuntimeKind{RuntimeDotnet},
	},
	{
		Name:        "proxycall-remover",
		Description: "ConfuserEx proxy call remover",
		Category:    CategoryDeobfuscator,
		Method:      MethodDirectURL,
		URL:         "https://github.com/wwh1004/ConfuserExTools/releases/download/v0.1.0.0-beta/ConfuserExTools.zip",
		Binary:      "ProxyKiller.dll",
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
