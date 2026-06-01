package recipe

import "embed"

// cfxExtractAssets holds the embedded source for the ConfuserEx embedded-assembly
// extractor (a small .NET tool built on-demand via the dotnet SDK).
//
//go:embed assets/cfxextract/extract.csproj assets/cfxextract/Program.cs
var cfxExtractAssets embed.FS
