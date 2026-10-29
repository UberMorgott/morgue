package recipe

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/util"
)

// newUnityDataEvidence starts the asset-export evidence for one target. The
// state stays empty until the export step decides; an unset state at write time
// is recorded as failed so a crashed step can never read as success.
func newUnityDataEvidence(ctx *Context, recipeName, gameDataDir, exportDir string, shared bool) quality.UnityDataEvidence {
	ev := quality.UnityDataEvidence{
		SchemaVersion: 1,
		Recipe:        recipeName,
		GameDataDir:   gameDataDir,
		Mode:          "primary-content",
		ExportDir:     unityRelSlash(ctx.Output, exportDir),
		Shared:        shared,
		Tool:          "assetripper",
		ToolVersion:   unityToolVersion(ctx, "assetripper"),
	}
	if gameDataDir != "" {
		if h, err := util.SHA256File(filepath.Join(gameDataDir, "globalgamemanagers")); err == nil {
			ev.GlobalGameManagersSHA256 = h
		}
	}
	return ev
}

// countUnityExport measures what the export actually wrote (all files and the
// .asset subset). Counts are data-file totals, never code coverage.
func countUnityExport(ev *quality.UnityDataEvidence, dir string) {
	var files, assets int64
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable entries are simply not counted
		}
		files++
		if strings.EqualFold(filepath.Ext(path), ".asset") {
			assets++
		}
		return nil
	})
	ev.Files, ev.AssetFiles = &files, &assets
}

// writeUnityDataEvidence persists the evidence and an artifact-level
// provenance row for the export (unity.extract). Failures are logged: the
// collector then reports unknown rather than a reassuring pass.
func writeUnityDataEvidence(ctx *Context, ev *quality.UnityDataEvidence, logTool func(tool, msg string)) {
	if ev.State == "" {
		ev.State, ev.Reason = quality.UnityAssetsFailed, "export step did not complete"
	}
	if err := unityWriteEvidence(ctx.Output, quality.UnityDataEvidenceFile, ev); err != nil {
		logTool("assetripper", fmt.Sprintf("write asset evidence: %v", err))
	}
	if ev.State == quality.UnityAssetsExported || ev.State == quality.UnityAssetsReused {
		detail := fmt.Sprintf("AssetRipper %s export of %s", ev.Mode, filepath.Base(ev.GameDataDir))
		if ev.Shared {
			detail += " (shared per-game product)"
		}
		if err := unityArtifactProv(ctx, ev.GlobalGameManagersSHA256, ev.ExportDir, "asset", filepath.Base(ev.GameDataDir), "assetripper", unityStepExtract, []string{"--headless"}, detail); err != nil {
			logTool("assetripper", fmt.Sprintf("asset provenance: %v", err))
		}
	}
}
