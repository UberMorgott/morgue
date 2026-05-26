package recon

import (
	"context"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// RunDiE runs Detect It Easy on a target binary (best-effort).
// Returns the raw output string. Errors are silently ignored since DiE is optional.
func RunDiE(diePath, targetPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := util.RunCmd(ctx, diePath, []string{"-j", targetPath}, "")
	if err != nil || result.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}
