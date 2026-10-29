package quality

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// UnityDataEvidenceFile is the target-relative file Unity recipes write about
// their asset (data-layer) export. Asset counts are data, never code coverage.
const UnityDataEvidenceFile = "evidence/unitydata.json"

// Asset export states. "disabled" is a requested skip (code-only / config),
// distinct from an unavailable tool, a missing *_Data dir, or a failure.
const (
	UnityAssetsDisabled    = "disabled"
	UnityAssetsToolMissing = "tool-missing"
	UnityAssetsNoDataDir   = "no-data-dir"
	UnityAssetsFailed      = "failed"
	UnityAssetsExported    = "exported"
	UnityAssetsReused      = "reused"
)

// UnityDataEvidence describes one target's view of the per-game asset export.
type UnityDataEvidence struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recipe        string `json:"recipe"`
	State         string `json:"state"`
	Reason        string `json:"reason"`
	// GameDataDir is the consumed *_Data input directory (absolute input path).
	GameDataDir string `json:"gameDataDir"`
	// GlobalGameManagersSHA256 identifies the game build when readable.
	GlobalGameManagersSHA256 string `json:"globalGameManagersSha256"`
	Mode                     string `json:"mode"` // primary-content | unity-project
	// ExportDir is relative to the target output ("../_assets/..." for the shared
	// per-game export) or absolute when outside the workspace.
	ExportDir   string     `json:"exportDir"`
	Shared      bool       `json:"shared"`
	Tool        string     `json:"tool"`
	ToolVersion string     `json:"toolVersion"`
	Files       *int64     `json:"files"`
	AssetFiles  *int64     `json:"assetFiles"`
	Organize    *UnityStep `json:"organize,omitempty"`
	Decode      *UnityStep `json:"decode,omitempty"`
}

// UnityStep is an optional follow-up over the export (organize / Odin decode).
type UnityStep struct {
	State    string `json:"state"` // skipped | failed | done | reused
	Reason   string `json:"reason"`
	Output   string `json:"output"`
	External bool   `json:"external"`
	Count    *int64 `json:"count"`
	Failed   *int64 `json:"failed"`
	Unit     string `json:"unit"`
}

func init() { Register("unitydata", UnityData{}) }

// UnityData collects asset-export quality for unity-mono / unity-il2cpp. It is
// a data-layer report: function/type coverage is not applicable by design.
type UnityData struct{}

var unityDataMandatory = map[string]bool{"output.present": true, "artifact.fidelity": true, "provenance.complete": true}

func (UnityData) Collect(_ context.Context, in Input) (runmodel.QualityReport, error) {
	dir := unityTargetDir(in, UnityDataEvidenceFile)
	na := "asset export is game data, not executable code; parsed asset totals are not code coverage"
	r := runmodel.QualityReport{
		SchemaVersion:        runmodel.QualityVersion,
		Family:               "unitydata",
		Functions:            unityNACoverage(na),
		Types:                unityNACoverage(na),
		DecompilerErrors:     unityNA(na),
		UnknownResultTypes:   unityNA(na),
		UnresolvedReferences: unityNA(na),
		ResidualObfuscation:  unityNA(na),
		StringDecryption:     unityNACoverage(na),
		Warnings:             []string{},
		Checks:               []runmodel.QualityCheck{},
	}
	var ev UnityDataEvidence
	if err := unityReadJSON(filepath.Join(dir, filepath.FromSlash(UnityDataEvidenceFile)), &ev); err != nil {
		r.Status = "unknown"
		r.Warnings = append(r.Warnings, "asset export evidence unavailable: "+err.Error())
		r.Checks = append(r.Checks, unityCheck("output.present", "unknown", "asset export evidence unavailable"))
		return r, nil
	}
	exportEv := unityEv(ev.ExportDir, ev.Mode)
	switch ev.State {
	case UnityAssetsDisabled:
		r.Status = "not-applicable"
		r.Checks = append(r.Checks, unityCheck("output.present", "not-applicable", "asset extraction disabled: "+ev.Reason))
		return r, nil
	case UnityAssetsToolMissing, UnityAssetsNoDataDir:
		r.Checks = append(r.Checks, unityCheck("output.present", "warn", fmt.Sprintf("asset export not performed (%s): %s", ev.State, ev.Reason)))
		r.Warnings = append(r.Warnings, "game assets/config were NOT exported; data-layer conclusions cannot be drawn from this output")
	case UnityAssetsFailed:
		r.Checks = append(r.Checks, unityCheck("output.present", "fail", "asset export failed: "+ev.Reason, exportEv))
		r.Warnings = append(r.Warnings, "asset export failed; any partial files under the export dir are incomplete")
	case UnityAssetsExported, UnityAssetsReused:
		if ev.Files == nil || *ev.Files == 0 {
			r.Checks = append(r.Checks, unityCheck("output.present", "fail", "asset export reported success but produced no files", exportEv))
			break
		}
		msg := fmt.Sprintf("%d exported files (%s), %s", *ev.Files, ev.Mode, unityCount(ev.AssetFiles, ".asset files"))
		if ev.Shared {
			msg += "; per-game export shared by every target of this game"
		}
		if ev.State == UnityAssetsReused {
			msg += "; reused from an earlier target in this live run"
		}
		r.Checks = append(r.Checks, unityCheck("output.present", "pass", msg, exportEv))
		if ev.Mode == "primary-content" {
			r.Warnings = append(r.Warnings, "primary-content export: scripts/ScriptableObjects/TextAssets may be absent; not a full project export")
		}
	default:
		r.Checks = append(r.Checks, unityCheck("output.present", "unknown", "unrecognized asset export state "+ev.State))
	}

	fid := "pass"
	fmsg := "exported assets are AssetRipper reconstructions (kind asset), not original project files"
	for _, s := range []struct {
		name string
		step *UnityStep
	}{{"organize", ev.Organize}, {"odin decode", ev.Decode}} {
		if s.step == nil {
			continue
		}
		fmsg += fmt.Sprintf("; %s %s", s.name, s.step.State)
		if s.step.Count != nil {
			fmsg += fmt.Sprintf(" %d %s", *s.step.Count, s.step.Unit)
		}
		if s.step.Failed != nil && *s.step.Failed > 0 {
			fmsg += fmt.Sprintf(", %d failed", *s.step.Failed)
			fid = "warn"
		}
		if s.step.State == "failed" {
			fid = "warn"
			fmsg += ": " + s.step.Reason
		}
		if s.step.External {
			fmsg += " (external destination " + s.step.Output + ")"
		}
	}
	r.Checks = append(r.Checks, unityCheck("artifact.fidelity", fid, fmsg, exportEv))

	pstatus, pmsg := "pass", "tool "+ev.Tool+" "+ev.ToolVersion
	if ev.ToolVersion == "" {
		pstatus, pmsg = "warn", "asset tool version unknown"
	}
	if ev.GlobalGameManagersSHA256 != "" {
		pmsg += "; globalgamemanagers sha256 " + ev.GlobalGameManagersSHA256
	} else {
		pstatus = "warn"
		pmsg += "; game build identity (globalgamemanagers) not hashed"
	}
	pmsg += "; *_Data directory contents are not hashed file-by-file"
	r.Checks = append(r.Checks, unityCheck("provenance.complete", pstatus, pmsg, unityEv("", "input *_Data "+ev.GameDataDir)))
	r.Status = unityStatus(r.Checks, unityDataMandatory)
	return r, nil
}

func unityCount(n *int64, unit string) string {
	if n == nil {
		return "unknown " + unit
	}
	return fmt.Sprintf("%d %s", *n, unit)
}
