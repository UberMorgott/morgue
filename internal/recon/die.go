package recon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// dieDetect represents a single detection entry from DiE JSON output.
type dieDetect struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	String  string `json:"string"`
	Version string `json:"version"`
}

// dieOutput represents the top-level DiE JSON output.
type dieOutput struct {
	Detects []dieDetect `json:"detects"`
}

// RunDiE runs Detect It Easy on a target binary and enriches the Result (best-effort).
func RunDiE(r *Result, diePath, targetPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmdResult, err := util.RunCmd(ctx, diePath, []string{"-j", targetPath}, "")
	if err != nil || cmdResult.ExitCode != 0 {
		return
	}

	stdout := strings.TrimSpace(cmdResult.Stdout)
	if stdout == "" {
		return
	}

	var output dieOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		return
	}

	for _, d := range output.Detects {
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
