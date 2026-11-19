package recon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// dieDetect represents a single detection entry from DiE JSON output. DiE 3.x+
// nests the real detections under each file part's "values".
type dieDetect struct {
	Type    string      `json:"type"`
	Name    string      `json:"name"`
	String  string      `json:"string"`
	Version string      `json:"version"`
	Values  []dieDetect `json:"values"`
}

// dieOutput represents the top-level DiE JSON output.
type dieOutput struct {
	Detects []dieDetect `json:"detects"`
}

// RunDiE runs Detect It Easy on a target binary and enriches the Result (best-effort).
func RunDiE(parent context.Context, r *Result, diePath, targetPath string) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()

	cmdResult, err := util.RunCmd(ctx, diePath, []string{"-j", targetPath}, "")
	if err != nil || cmdResult.ExitCode != 0 {
		return
	}

	applyDiEJSON(r, cmdResult.Stdout)
}

// applyDiEJSON enriches r from `diec -j` output (best-effort).
func applyDiEJSON(r *Result, stdout string) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return
	}

	var output dieOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		return
	}

	var detects []dieDetect
	for _, d := range output.Detects {
		detects = append(append(detects, d), d.Values...)
	}
	for _, d := range detects {
		switch strings.ToLower(d.Type) {
		case "compiler":
			if r.Compiler == "" {
				r.Compiler = d.Name
				if d.Version != "" {
					r.Compiler += " " + d.Version
				}
			}
		case "packer":
			r.Packed = true
			if r.Obfuscator == "" {
				r.Obfuscator = d.Name
			}
		case "protector", "obfuscator":
			if r.Obfuscator == "" {
				r.Obfuscator = d.Name
			}
		}
	}
}
