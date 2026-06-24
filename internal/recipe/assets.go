package recipe

import "embed"

// cfxExtractAssets holds the embedded source for the ConfuserEx embedded-assembly
// extractor (a small .NET tool built on-demand via the dotnet SDK).
//
//go:embed assets/cfxextract/extract.csproj assets/cfxextract/Program.cs
var cfxExtractAssets embed.FS

// cfxStringsAssets holds the embedded source for the ConfuserEx custom
// string-decryptor pass (a small AsmResolver-based .NET tool built on-demand).
// It statically rewrites resource-keyed custom string decryptors that de4dot's
// `-p crx` does not handle (see assets/cfxstrings/Program.cs).
//
//go:embed assets/cfxstrings/cfxstrings.csproj assets/cfxstrings/Program.cs
var cfxStringsAssets embed.FS

// cfxCflowAssets holds the embedded source for the ConfuserEx control-flow
// deobfuscation pass (a small AsmResolver-based .NET tool built on-demand). It
// statically un-flattens switch-dispatch control flow driven by stateless
// constant-provider methods that de4dot does not remove (see
// assets/cfxcflow/Program.cs). Static-only: it never executes target code.
//
//go:embed assets/cfxcflow/cfxcflow.csproj assets/cfxcflow/Program.cs
var cfxCflowAssets embed.FS
